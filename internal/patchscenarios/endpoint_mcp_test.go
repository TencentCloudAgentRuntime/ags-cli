package patchscenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestEndpointMCPCleanupSurvivesInitializationFailure(t *testing.T) {
	for _, tc := range []struct{ name, response, version string }{
		{"malformed response", "invalid JSON", ""},
		{"wrong version", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"unexpected"}}`, ""},
		{"notification rejected", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25"}}`, endpointMCPVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var deleted atomic.Bool
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Error("expected the original bearer")
				}
				if r.Method == http.MethodDelete {
					if r.Header.Get("Mcp-Session-Id") != "fixture-session" ||
						r.Header.Get("MCP-Protocol-Version") != tc.version {
						t.Error("delete did not identify the initialized session")
					}
					deleted.Store(true)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				var request struct{ Method string }
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				switch request.Method {
				case "initialize":
					w.Header().Set("Mcp-Session-Id", "fixture-session")
					_, _ = io.WriteString(w, tc.response)
				case "notifications/initialized":
					w.WriteHeader(http.StatusBadRequest)
				case "ping":
					if !deleted.Load() || r.Header.Get("Mcp-Session-Id") != "fixture-session" {
						t.Error("expected a probe for the deleted session")
					}
					w.WriteHeader(http.StatusNotFound)
				default:
					t.Errorf("unexpected MCP method %s", request.Method)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()

			client := newEndpointMCPSession(server.URL, "fixture-token")
			client.client = server.Client()
			var cleanup func(context.Context) error
			err := client.open(t.Context(), func(closeSession func(context.Context) error) {
				if cleanup != nil {
					t.Fatal("session cleanup was registered twice")
				}
				cleanup = closeSession
			})
			if err == nil || cleanup == nil {
				t.Fatalf("failed initialization must still register session cleanup: err=%v", err)
			}
			if err := cleanup(t.Context()); err != nil || !deleted.Load() || !client.closed {
				t.Fatalf("session cleanup failed: %v", err)
			}
		})
	}
}

func TestEndpointMCPCleanupRequiresConfirmation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sequence := requests.Add(1)
		if r.Header.Get("Mcp-Session-Id") != "fixture-session" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("missing session or bearer")
		}
		switch sequence {
		case 1:
			w.WriteHeader(http.StatusUnauthorized)
		case 2:
			if r.Method != http.MethodDelete {
				t.Error("expected DELETE")
			}
			w.WriteHeader(http.StatusNoContent)
		case 3:
			// A successful DELETE must not mask a session that still works.
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
		case 4, 5:
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Error("confirmed cleanup should not repeat HTTP requests")
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := newEndpointMCPSession(server.URL, "fixture-token")
	client.client, client.session = server.Client(), "fixture-session"
	if err := client.close(t.Context()); err == nil || client.closed {
		t.Fatal("accepted an unauthorized deletion")
	}
	if err := client.close(t.Context()); err == nil || client.closed {
		t.Fatal("accepted an unconfirmed deletion")
	}
	if err := client.close(t.Context()); err != nil || !client.closed {
		t.Fatalf("failed to confirm session absent on retry: %v", err)
	}
	if err := client.close(t.Context()); err != nil || requests.Load() != 5 {
		t.Fatalf("confirmed cleanup was not idempotent: requests=%d err=%v", requests.Load(), err)
	}
}

func TestEndpointMetricFixturePreservesJSONNumbersOnWire(t *testing.T) {
	const arguments = `{"id":9007199254740993,"values":[12345678901234567890.123]}`
	t.Setenv("AGR_ENDPOINT_FIXTURE_TOOL", "fixture")
	t.Setenv("AGR_ENDPOINT_FIXTURE_ARGUMENTS", arguments)
	fixture, err := loadEndpointMetricFixture()
	if err != nil {
		t.Fatal(err)
	}
	var called atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     int
			Method string
			Params struct {
				Name      string
				Arguments json.RawMessage
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		switch request.Method {
		case "tools/list":
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"mapped-fixture","_meta":{"com.tencentags/endpoint":{"upstreamToolName":"fixture"}}}]}}`, request.ID)
		case "tools/call":
			if request.Params.Name != "mapped-fixture" || string(request.Params.Arguments) != arguments {
				t.Errorf("unexpected tool request: %s %s", request.Params.Name, request.Params.Arguments)
			}
			called.Store(true)
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"done"}]}}`, request.ID)
		default:
			t.Errorf("unexpected MCP method %s", request.Method)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := newEndpointMCPSession(server.URL, "fixture-token")
	client.client = server.Client()
	if err := client.invokeFixture(t.Context(), fixture, 1); err != nil || !called.Load() {
		t.Fatalf("fixture tool did not run: %v", err)
	}
}
