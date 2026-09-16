package cloudapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestJSONTransportPreservesFieldsAndTemporaryCredentials(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-TC-Token") != "session-token" {
			t.Error("temporary credential omitted")
		}
		if r.Header.Get("X-TC-Action") != "StartSandboxInstance" || r.Header.Get("Authorization") == "" {
			t.Error("request was not signed for the action")
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Error(err)
		}
		if got["ClientToken"] != "once" || got["Preview"] == nil {
			t.Errorf("request fields lost: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"Response":{"Preview":{"Value":"kept"},"RequestId":"test"}}`)
	}))
	defer server.Close()
	caller, err := NewWithToken("id", "key", "session-token", "region", strings.TrimPrefix(server.URL, "https://"))
	if err != nil {
		t.Fatal(err)
	}
	caller.client.WithHttpTransport(server.Client().Transport)
	got, err := caller.Call(t.Context(), "StartSandboxInstance", []byte(`{"ClientToken":"once","Preview":{"Nested":[1,"two"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"Value":"kept"`) || calls.Load() != 1 {
		t.Fatalf("response=%s calls=%d", got, calls.Load())
	}
}

func TestJSONTransportNeverResendsBusinessFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"Response":{"Error":{"Code":"InternalError","Message":"failed"},"RequestId":"test"}}`)
	}))
	defer server.Close()
	caller, err := New("id", "key", "region", strings.TrimPrefix(server.URL, "https://"))
	if err != nil {
		t.Fatal(err)
	}
	caller.client.WithHttpTransport(server.Client().Transport)
	if _, err := caller.Call(t.Context(), "CreateSandboxTool", []byte(`{}`)); err == nil {
		t.Fatal("business error hidden")
	}
	if calls.Load() != 1 {
		t.Fatalf("write sent %d times", calls.Load())
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := caller.Call(ctx, "CreateSandboxTool", []byte(`{}`)); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("cancelled request reached server")
	}
}
