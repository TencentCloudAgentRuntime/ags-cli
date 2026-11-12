//go:build preview

package acpclient

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

func endpoint(server *httptest.Server) Endpoint {
	return Endpoint{
		URL:            server.URL + "/acp",
		Token:          func(context.Context) (string, error) { return "tok-1", nil },
		AffinityHeader: affinityHeader,
		HTTP:           server.Client(),
	}
}

type recorder struct {
	mu      sync.Mutex
	text    strings.Builder
	tools   int
	denials int
}

func (r *recorder) SessionUpdate(_ context.Context, n acp.SessionNotification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n.Update.AgentMessageChunk != nil {
		r.text.WriteString(TextOf(n.Update.AgentMessageChunk.Content))
	}
	if n.Update.ToolCall != nil {
		r.tools++
	}
	return nil
}

func (r *recorder) RequestPermission(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	r.mu.Lock()
	r.denials++
	r.mu.Unlock()
	return Cancelled(), nil
}

func TestSessionPromptOverStreamableHTTP(t *testing.T) {
	agent := newFakeAgent(t)
	agent.permission = true
	server := httptest.NewServer(agent)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rec := &recorder{}
	session, err := Connect(ctx, endpoint(server), rec)
	if err != nil {
		t.Fatal(err)
	}
	if session.Info.Name != "fake-pi" || !session.Info.LoadSession {
		t.Fatalf("agent info = %+v", session.Info)
	}
	created, err := session.Conn.NewSession(ctx, acp.NewSessionRequest{Cwd: "/workspace", McpServers: []acp.McpServer{}})
	if err != nil || created.SessionId != "sess-1" {
		t.Fatalf("new session: %v %v", created, err)
	}
	result, err := session.Conn.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("hi")}})
	if err != nil || result.StopReason != acp.StopReasonEndTurn {
		t.Fatalf("prompt: %v %v", result, err)
	}
	if session.Bridge.Affinity() != "aff-1" {
		t.Fatalf("affinity not learned: %q", session.Bridge.Affinity())
	}
	session.Close()

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.text.String() != "hello world" || rec.tools != 1 || rec.denials != 1 {
		t.Fatalf("text=%q tools=%d denials=%d", rec.text.String(), rec.tools, rec.denials)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if !agent.deleted {
		t.Fatal("close did not DELETE the ACP connection")
	}
	for i, token := range agent.tokens {
		if token != "tok-1" {
			t.Fatalf("request %d sent token %q", i, token)
		}
	}
	// The first request cannot carry affinity; every later one must echo it.
	for i, affinity := range agent.affinities[1:] {
		if affinity != "aff-1" {
			t.Fatalf("request %d affinity = %q", i+1, affinity)
		}
	}
}

func TestLoadSessionReceivesReplayOnSessionStream(t *testing.T) {
	agent := newFakeAgent(t)
	server := httptest.NewServer(agent)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := Connect(ctx, endpoint(server), &recorder{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Conn.LoadSession(ctx, acp.LoadSessionRequest{SessionId: "sess-9", Cwd: "/workspace", McpServers: []acp.McpServer{}}); err != nil {
		t.Fatalf("load: %v", err)
	}
}

func TestCancelStopsTurn(t *testing.T) {
	agent := newFakeAgent(t)
	agent.block = make(chan struct{})
	server := httptest.NewServer(agent)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := Connect(ctx, endpoint(server), &recorder{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	created, err := session.Conn.NewSession(ctx, acp.NewSessionRequest{Cwd: "/workspace", McpServers: []acp.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan acp.PromptResponse, 1)
	go func() {
		result, _ := session.Conn.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("long")}})
		done <- result
	}()
	time.Sleep(100 * time.Millisecond)
	if err := session.Conn.Cancel(ctx, acp.CancelNotification{SessionId: created.SessionId}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.StopReason != acp.StopReasonCancelled {
			t.Fatalf("stop reason = %s", result.StopReason)
		}
	case <-ctx.Done():
		t.Fatal("prompt did not finish after cancel")
	}
}

func TestPlatformErrorIsClassified(t *testing.T) {
	agent := newFakeAgent(t)
	agent.status = http.StatusServiceUnavailable
	server := httptest.NewServer(agent)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := Probe(ctx, endpoint(server))
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != 503 || httpErr.PlatformCode != "sandbox_tool_unavailable" || httpErr.RequestID != "REQ1" {
		t.Fatalf("err = %#v", err)
	}
	var cliErr *output.CLIError
	if !errors.As(Classify(err, "hint"), &cliErr) || cliErr.Failure.Code != "ACP_UNAVAILABLE" || cliErr.Failure.Details["PlatformRequestId"] != "REQ1" {
		t.Fatalf("classified = %#v", cliErr)
	}
	agent.status = http.StatusUnauthorized
	_, err = Probe(ctx, endpoint(server))
	if !errors.As(Classify(err, "hint"), &cliErr) || cliErr.Failure.Kind != output.KindAuthOrPermission {
		t.Fatalf("401 classified = %#v", cliErr)
	}
}

// A redirect would make Go forward X-Access-Token to the redirect target.
func TestRedirectDoesNotLeakToken(t *testing.T) {
	var leaked sync.Map
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := r.Header.Get("X-Access-Token"); token != "" {
			leaked.Store("token", token)
		}
	}))
	defer other.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Probe(ctx, endpoint(redirect)); err == nil {
		t.Fatal("redirected initialize succeeded")
	}
	if _, ok := leaked.Load("token"); ok {
		t.Fatal("access token followed the redirect")
	}
}

// The stdio bridge must keep stdout to ACP messages only.
func TestServeStdioWritesOnlyJSONRPC(t *testing.T) {
	agent := newFakeAgent(t)
	server := httptest.NewServer(agent)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		_, err := ServeStdio(ctx, endpoint(server), inR, outW)
		_ = outW.CloseWithError(errOrEOF(err))
	}()
	lines := bufio.NewScanner(outR)
	send := func(line string) { _, _ = io.WriteString(inW, line+"\n") }
	read := func() string {
		if !lines.Scan() {
			t.Fatalf("bridge closed: %v", lines.Err())
		}
		return lines.Text()
	}
	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}`)
	if got := read(); !strings.Contains(got, `"fake-pi"`) {
		t.Fatalf("initialize reply = %s", got)
	}
	send(`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/workspace","mcpServers":[]}}`)
	if got := read(); !strings.Contains(got, `"sess-1"`) {
		t.Fatalf("session/new reply = %s", got)
	}
	send(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"sess-1","prompt":[{"type":"text","text":"hi"}]}}`)
	for {
		got := read()
		if !strings.HasPrefix(got, `{"jsonrpc":"2.0"`) && !strings.HasPrefix(got, `{"id"`) && !strings.HasPrefix(got, `{"method"`) {
			if !strings.HasPrefix(got, "{") {
				t.Fatalf("non-JSON line on stdout: %q", got)
			}
		}
		if strings.Contains(got, `"end_turn"`) {
			break
		}
	}
	_ = inW.Close()
}
