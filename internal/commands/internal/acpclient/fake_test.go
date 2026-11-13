//go:build preview

package acpclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
)

// fakeAgent implements the harness ACP Streamable HTTP transport on loopback:
// initialize answers inline, other requests get 202 and their responses and
// updates arrive on the connection or session SSE stream.
type fakeAgent struct {
	t          *testing.T
	mu         sync.Mutex
	streams    map[string]chan []byte
	deleted    bool
	tokens     []string
	affinities []string
	cancelled  bool
	reply      string
	block      chan struct{}
	permission bool
	status     int
}

const affinityHeader = "X-Tencent-Agr-Affinity-Id"

func newFakeAgent(t *testing.T) *fakeAgent {
	return &fakeAgent{t: t, streams: map[string]chan []byte{}, reply: "hello world"}
}

func (f *fakeAgent) stream(key string) chan []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.streams[key] == nil {
		f.streams[key] = make(chan []byte, 64)
	}
	return f.streams[key]
}

func (f *fakeAgent) push(key string, message any) {
	data, _ := json.Marshal(message)
	f.stream(key) <- data
}

func (f *fakeAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.tokens = append(f.tokens, r.Header.Get("X-Access-Token"))
	f.affinities = append(f.affinities, r.Header.Get(affinityHeader))
	status := f.status
	f.mu.Unlock()
	if status != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"code":"sandbox_tool_unavailable","message":"sandbox tool is unavailable","request_id":"REQ1"}`)
		return
	}
	w.Header().Set(affinityHeader, "aff-1")
	switch r.Method {
	case http.MethodDelete:
		f.mu.Lock()
		f.deleted = true
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	case http.MethodGet:
		if r.Header.Get("Acp-Connection-Id") != "conn-1" {
			http.Error(w, "unknown connection", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		ch := f.stream(r.Header.Get("Acp-Session-Id"))
		for {
			select {
			case <-r.Context().Done():
				return
			case data := <-ch:
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
				w.(http.Flusher).Flush()
			}
		}
	}
	var message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params map[string]any  `json:"params"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if message.Method == "initialize" {
		w.Header().Set("Acp-Connection-Id", "conn-1")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": map[string]any{
			"protocolVersion": 1, "agentCapabilities": map[string]any{"loadSession": true}, "agentInfo": map[string]any{"name": "fake-pi", "version": "1.3.0"}, "authMethods": []any{},
		}})
		return
	}
	if r.Header.Get("Acp-Connection-Id") != "conn-1" {
		http.Error(w, "missing connection", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	go f.handle(message.ID, message.Method, message.Params, message.Result)
}

func (f *fakeAgent) handle(id json.RawMessage, method string, params map[string]any, result json.RawMessage) {
	session, _ := params["sessionId"].(string)
	switch method {
	case "session/new":
		f.push("", map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"sessionId": "sess-1"}})
	case "session/load":
		f.push(session, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": session, "update": map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]any{"type": "text", "text": "replayed"}}}})
		f.push("", map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
	case "session/prompt":
		if f.permission {
			f.push(session, map[string]any{"jsonrpc": "2.0", "id": 900, "method": "session/request_permission", "params": map[string]any{
				"sessionId": session, "toolCall": map[string]any{"toolCallId": "t1"}, "options": []any{map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"}},
			}})
		}
		f.push(session, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": session, "update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "ls", "kind": "execute", "status": "in_progress"}}})
		f.push(session, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": session, "update": map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed"}}})
		for _, chunk := range []string{f.reply[:5], f.reply[5:]} {
			f.push(session, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": session, "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": chunk}}}})
		}
		if f.block != nil {
			<-f.block
			f.push(session, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"stopReason": "cancelled"}})
			return
		}
		f.push(session, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"stopReason": "end_turn"}})
	case "session/cancel":
		f.mu.Lock()
		f.cancelled = true
		block := f.block
		f.mu.Unlock()
		if block != nil {
			close(block)
		}
	case "":
		// A client response to the permission request.
		var outcome struct {
			Outcome struct {
				Outcome string `json:"outcome"`
			} `json:"outcome"`
		}
		_ = json.Unmarshal(result, &outcome)
		if outcome.Outcome.Outcome != "cancelled" {
			f.t.Errorf("permission outcome = %q, want cancelled", outcome.Outcome.Outcome)
		}
	}
}
