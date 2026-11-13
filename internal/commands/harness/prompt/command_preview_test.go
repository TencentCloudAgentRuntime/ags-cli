//go:build preview

package prompt

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/acpclient"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

func TestPromptNDJSONIgnoresUpdatesAfterCompletion(t *testing.T) {
	var stdout bytes.Buffer
	sink := &updates{mode: renderNDJSON, events: output.NewNDJSONWriter(&stdout, "harness.prompt")}
	sink.reset()
	finish(renderNDJSON, sink, map[string]any{"SessionId": "sess-test"})
	if err := sink.SessionUpdate(t.Context(), acp.SessionNotification{Update: acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Content: acp.TextBlock("late reply")},
	}}); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&stdout)
	var event output.NDJSONEvent
	if err := decoder.Decode(&event); err != nil || event.Type != "completed" {
		t.Fatalf("terminal event = %+v, error = %v", event, err)
	}
	if err := decoder.Decode(&event); !errors.Is(err, io.EOF) {
		t.Fatalf("event after completion: %+v, error = %v", event, err)
	}
}

func TestPromptNDJSONTerminalEvent(t *testing.T) {
	flags := cli.RootCmd().PersistentFlags()
	flag := flags.Lookup("output")
	previous, changed := flag.Value.String(), flag.Changed
	if err := flags.Set("output", "ndjson"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flags.Set("output", previous); flag.Changed = changed })
	for _, tc := range []struct {
		name, code, kind string
	}{
		{name: "success"},
		{name: "timeout", code: "PROMPT_TIMEOUT", kind: output.KindTimeout},
		{name: "cancel", code: "PROMPT_FAILED", kind: output.KindGenericError},
		{name: "rpc-error", code: "PROMPT_FAILED", kind: output.KindGenericError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			updates := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Tencent-Agr-Affinity-Id", "aff-test")
				switch r.Method {
				case http.MethodGet:
					w.Header().Set("Content-Type", "text/event-stream")
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
					var events <-chan string
					if r.Header.Get("Acp-Session-Id") != "" {
						events = updates
					}
					for {
						select {
						case <-r.Context().Done():
							return
						case event := <-events:
							fmt.Fprintf(w, "data: %s\n\n", event)
							w.(http.Flusher).Flush()
						}
					}
				case http.MethodDelete:
					w.WriteHeader(http.StatusNoContent)
					return
				}
				var message struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
				}
				if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
					t.Error(err)
					return
				}
				switch message.Method {
				case "initialize":
					w.Header().Set("Acp-Connection-Id", "conn-test")
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}, "authMethods": []any{}}})
				case "session/new":
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": map[string]any{"sessionId": "sess-test"}})
				case "session/prompt":
					w.WriteHeader(http.StatusAccepted)
					switch tc.name {
					case "success", "rpc-error":
						updates <- `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"sess-test","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"partial reply"}}}}`
						outcome := `"result":{"stopReason":"end_turn"}`
						if tc.name == "rpc-error" {
							outcome = `"error":{"code":-32603,"message":"remote prompt failed"}`
						}
						updates <- fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,%s}`, message.ID, outcome)
					case "cancel":
						cancel()
					}
				case "session/cancel", "$/cancel_request":
					w.WriteHeader(http.StatusAccepted)
				default:
					t.Errorf("unexpected method %s", message.Method)
				}
			}))
			defer server.Close()
			ios, _, stdout, stderr := iostreams.Test()
			deps := command.Deps{IO: ios, Values: map[string]any{
				harnesscli.DataPlaneKey: func(string) (acpclient.Endpoint, harnesscli.Target) {
					return acpclient.Endpoint{URL: server.URL, HTTP: server.Client(), AffinityHeader: "X-Tencent-Agr-Affinity-Id", Token: func(context.Context) (string, error) { return "test-token", nil }}, harnesscli.Target{}
				},
			}}
			req := command.Request{ArgValues: map[string]string{"harness-id": "hns-test", "text": "hello"}}
			if tc.name == "timeout" {
				req.Flags = map[string]command.FlagValue{"timeout": {Changed: true, String: "300ms"}}
			}
			result, err := run(ctx, req, deps, harnesscli.Client{})
			var events []output.NDJSONEvent
			lines := bufio.NewScanner(stdout)
			for lines.Scan() {
				var event output.NDJSONEvent
				if err := json.Unmarshal(lines.Bytes(), &event); err != nil {
					t.Fatal(err)
				}
				if event.SchemaVersion != "agr.events.v1" || event.Command != "harness.prompt" {
					t.Fatalf("unexpected event envelope: %+v", event)
				}
				events = append(events, event)
			}
			if err := lines.Err(); err != nil {
				t.Fatal(err)
			}
			wantTypes := []string{"started", "failed"}
			switch tc.name {
			case "success":
				wantTypes = []string{"started", "update", "completed"}
			case "rpc-error":
				wantTypes = []string{"started", "update", "failed"}
			}
			var types []string
			for _, event := range events {
				types = append(types, event.Type)
			}
			if !reflect.DeepEqual(types, wantTypes) {
				t.Fatalf("event types = %v, want %v (returned error: %v)", types, wantTypes, err)
			}
			if err != nil || result == nil || !result.StreamDone {
				t.Fatalf("result = %+v, error = %v", result, err)
			}
			last := events[len(events)-1]
			data := last.Data.(map[string]any)
			if data["SessionId"] != "sess-test" || data["AffinityId"] != "aff-test" {
				t.Fatalf("missing recovery identifiers: %v", data)
			}
			if tc.code != "" {
				if result.ExitCode != output.ExitGenericError || result.Cause == nil || last.Failure == nil || last.Failure.Code != tc.code || last.Failure.Kind != tc.kind {
					t.Fatalf("failed result = %+v, event = %+v", result, last)
				}
				if !reflect.DeepEqual(last.Failure.Details, data) {
					t.Fatalf("failure details lost: %v, want %v", last.Failure.Details, data)
				}
			} else if result.ExitCode != 0 || last.Failure != nil {
				t.Fatalf("success result = %+v, event = %+v", result, last)
			}
			if tc.name == "success" || tc.name == "rpc-error" {
				if data["Reply"] != "partial reply" {
					t.Fatalf("reply lost: %v", data)
				}
			}
			if stderr.Len() != 0 {
				t.Fatalf("NDJSON wrote diagnostics to stderr: %s", stderr)
			}
		})
	}
}
