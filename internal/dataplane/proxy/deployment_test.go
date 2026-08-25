package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTokenProviderAllowsLazyDeploymentCredentials(t *testing.T) {
	calls := 0
	p, err := New(Options{
		InstanceID: "dpl-a1b2c3d4",
		Domain:     "ap-guangzhou.tencentags.com",
		RemotePort: 8080,
		TokenProvider: func(context.Context) (string, error) {
			calls++
			return "dpt_secret", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("token provider called eagerly %d times", calls)
	}
	token, err := p.tokenForRequest(context.Background())
	if err != nil || token != "dpt_secret" || calls != 1 {
		t.Fatalf("tokenForRequest = (%q, %v), calls=%d", token, err, calls)
	}
}

func TestProxyForwardsHTTPSSEAndWebSocketWithDynamicToken(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Access-Token") != "dpt_dynamic" || r.Header.Get("Authorization") != "Bearer business" || r.Header.Get("X-Business-Header") != "keep" {
			http.Error(w, "headers missing", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Origin") != "https://8080-dpl-a1b2c3d4.ap-guangzhou.agents.tencentags.com" {
			http.Error(w, "origin not normalized", http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/http":
			fmt.Fprint(w, "http-ok")
		case "/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: ready\n\n")
			w.(http.Flusher).Flush()
		case "/ws":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = conn.Close() }()
			messageType, payload, err := conn.ReadMessage()
			if err == nil {
				_ = conn.WriteMessage(messageType, payload)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	var tokenCalls atomic.Int32
	p, err := New(Options{
		InstanceID:      "dpl-a1b2c3d4",
		Domain:          "ap-guangzhou.agents.tencentags.com",
		RemotePort:      8080,
		RewriteOrigin:   true,
		PreserveHeaders: true,
		ListenAddress:   "127.0.0.1:0",
		Insecure:        true,
		Logger:          log.New(io.Discard, "", 0),
		TokenProvider: func(context.Context) (string, error) {
			tokenCalls.Add(1)
			return "dpt_dynamic", nil
		},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, upstream.Listener.Addr().String())
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	address, err := p.Start()
	if err != nil {
		t.Fatal(err)
	}

	headers := http.Header{
		"Authorization":     []string{"Bearer business"},
		"X-Business-Header": []string{"keep"},
		"Origin":            []string{"https://local.example"},
	}
	for _, path := range []string{"/http", "/sse"} {
		request, _ := http.NewRequest(http.MethodGet, "http://"+address+path, nil)
		request.Header = headers.Clone()
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d body=%q", path, response.StatusCode, body)
		}
		if path == "/sse" && (response.Header.Get("Content-Type") != "text/event-stream" || string(body) != "data: ready\n\n") {
			t.Fatalf("SSE response headers=%v body=%q", response.Header, body)
		}
	}

	websocketURL := "ws://" + address + "/ws"
	connection, response, err := websocket.DefaultDialer.Dial(websocketURL, headers)
	if err != nil {
		if response != nil {
			defer func() { _ = response.Body.Close() }()
		}
		t.Fatalf("websocket dial: %v", err)
	}
	defer func() { _ = connection.Close() }()
	if err := connection.WriteMessage(websocket.TextMessage, []byte("echo")); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := connection.ReadMessage()
	if err != nil || string(payload) != "echo" {
		t.Fatalf("websocket echo = %q, %v", payload, err)
	}
	if tokenCalls.Load() != 3 {
		t.Fatalf("token provider calls = %d, want one per incoming request/connection", tokenCalls.Load())
	}
	stopped := make(chan struct{})
	go func() {
		p.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not close an active WebSocket promptly")
	}
	if _, _, err := connection.ReadMessage(); err == nil {
		t.Fatal("active WebSocket remained open after Stop")
	}
}

func TestDeploymentHeadersPreserveBusinessHeadersAndNormalizeOrigin(t *testing.T) {
	p, err := New(Options{
		InstanceID:      "dpl-a1b2c3d4",
		Domain:          "ap-guangzhou.agents.tencentags.com",
		RemotePort:      8080,
		Token:           "static",
		RewriteOrigin:   true,
		PreserveHeaders: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/socket", nil)
	request.Header.Set("Authorization", "Bearer business-token")
	request.Header.Set("X-Business-Header", "keep-me")
	request.Header.Set("X-Access-Token", "client-supplied")
	request.Header.Set("Origin", "https://any-origin.example")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Key", "key")

	headers := p.webSocketUpstreamHeaders(request, "dpt_secret")
	if headers.Get("Authorization") != "Bearer business-token" || headers.Get("X-Business-Header") != "keep-me" {
		t.Fatalf("business headers not preserved: %#v", headers)
	}
	if headers.Get("X-Access-Token") != "dpt_secret" {
		t.Fatalf("X-Access-Token = %q", headers.Get("X-Access-Token"))
	}
	if headers.Get("Origin") != "https://8080-dpl-a1b2c3d4.ap-guangzhou.agents.tencentags.com" {
		t.Fatalf("Origin = %q", headers.Get("Origin"))
	}
	for _, removed := range []string{"Connection", "Upgrade", "Sec-WebSocket-Key"} {
		if headers.Get(removed) != "" {
			t.Fatalf("%s leaked upstream: %#v", removed, headers)
		}
	}
}

func TestDeploymentAffinityCapturesAndReusesResponseID(t *testing.T) {
	const affinityHeader = "X-Workspace-Session"
	var requests atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			if got := r.Header.Get(affinityHeader); got != "" {
				http.Error(w, "unexpected initial affinity id: "+got, http.StatusBadRequest)
				return
			}
			w.Header().Set(affinityHeader, "session-from-upstream")
		case 2:
			if got := r.Header.Get(affinityHeader); got != "session-from-upstream" {
				http.Error(w, "affinity id not reused: "+got, http.StatusBadRequest)
				return
			}
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer upstream.Close()

	changed := make(chan string, 1)
	p := newAffinityTestProxy(t, upstream, AffinityOptions{
		HeaderName: affinityHeader,
		OnIDChange: func(id string) { changed <- id },
	})
	address, err := p.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	for i := 0; i < 2; i++ {
		request, _ := http.NewRequest(http.MethodGet, "http://"+address+"/", nil)
		request.Header.Set(affinityHeader, "client-must-not-control-session")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("request %d status=%d body=%q", i+1, response.StatusCode, body)
		}
	}
	select {
	case id := <-changed:
		if id != "session-from-upstream" {
			t.Fatalf("affinity change = %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("affinity ID change was not reported")
	}
}

func TestDeploymentAffinityInitialIDAndServerRotation(t *testing.T) {
	const affinityHeader = "X-Workspace-Session"
	var requests atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "session-from-flag"
		if requests.Add(1) == 2 {
			want = "session-rotated"
		}
		if got := r.Header.Get(affinityHeader); got != want {
			http.Error(w, fmt.Sprintf("affinity id = %q, want %q", got, want), http.StatusBadRequest)
			return
		}
		if want == "session-from-flag" {
			w.Header().Set(affinityHeader, "session-rotated")
		}
		fmt.Fprint(w, "ok")
	}))
	defer upstream.Close()

	changed := make(chan string, 1)
	p := newAffinityTestProxy(t, upstream, AffinityOptions{
		HeaderName: affinityHeader,
		InitialID:  "session-from-flag",
		OnIDChange: func(id string) { changed <- id },
	})
	address, err := p.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	for i := 0; i < 2; i++ {
		response, err := http.Get("http://" + address + "/")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("request %d status=%d body=%q", i+1, response.StatusCode, body)
		}
	}
	select {
	case id := <-changed:
		if id != "session-rotated" {
			t.Fatalf("affinity change = %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("rotated affinity ID was not reported")
	}
}

func TestDeploymentWebSocketAffinityCapturesAndReusesResponseID(t *testing.T) {
	const affinityHeader = "X-Workspace-Session"
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var requests atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber := requests.Add(1)
		if requestNumber == 1 {
			if got := r.Header.Get(affinityHeader); got != "" {
				http.Error(w, "unexpected initial affinity id: "+got, http.StatusBadRequest)
				return
			}
			w.Header().Set(affinityHeader, "websocket-session")
		} else if got := r.Header.Get(affinityHeader); got != "websocket-session" {
			http.Error(w, "affinity id not reused: "+got, http.StatusBadRequest)
			return
		}
		conn, err := upgrader.Upgrade(w, r, w.Header())
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer upstream.Close()

	changed := make(chan string, 1)
	p := newAffinityTestProxy(t, upstream, AffinityOptions{
		HeaderName: affinityHeader,
		OnIDChange: func(id string) { changed <- id },
	})
	address, err := p.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	for i := 0; i < 2; i++ {
		conn, response, err := websocket.DefaultDialer.Dial("ws://"+address+"/socket", nil)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if err != nil {
			t.Fatalf("websocket request %d: %v", i+1, err)
		}
		_ = conn.Close()
	}
	select {
	case id := <-changed:
		if id != "websocket-session" {
			t.Fatalf("affinity change = %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("WebSocket affinity ID change was not reported")
	}
}

func TestAffinityStateIgnoresStaleConcurrentResponses(t *testing.T) {
	var changes []string
	state := &affinityState{onChange: func(id string) { changes = append(changes, id) }}

	state.observe("", "first-session")
	state.observe("", "late-unbound-session")
	state.observe("first-session", "rotated-session")
	state.observe("first-session", "late-old-session")

	if got := state.currentID(); got != "rotated-session" {
		t.Fatalf("current affinity ID = %q, want rotated-session", got)
	}
	if len(changes) != 2 || changes[0] != "first-session" || changes[1] != "rotated-session" {
		t.Fatalf("affinity changes = %#v", changes)
	}
}

func newAffinityTestProxy(t *testing.T, upstream *httptest.Server, affinity AffinityOptions) *Proxy {
	t.Helper()
	p, err := New(Options{
		InstanceID:      "dpl-a1b2c3d4",
		Domain:          "ap-guangzhou.agents.tencentags.com",
		RemotePort:      8080,
		Token:           "dpt_dynamic",
		PreserveHeaders: true,
		ListenAddress:   "127.0.0.1:0",
		Insecure:        true,
		Logger:          log.New(io.Discard, "", 0),
		Affinity:        &affinity,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, upstream.Listener.Addr().String())
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInstanceWebSocketHeadersRemainLegacyCompatible(t *testing.T) {
	p, err := New(Options{InstanceID: "ins-a1b2c3d4", Domain: "ap-guangzhou.tencentags.com", RemotePort: 8080, Token: "instance-token"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/socket", nil)
	request.Header.Set("Authorization", "Bearer business-token")
	request.Header.Set("Cookie", "session=secret")
	request.Header.Set("X-Business-Header", "new-behavior")
	request.Header.Set("Origin", "https://legacy-origin.example")
	request.Header.Set("Sec-WebSocket-Protocol", "legacy-protocol")

	headers := p.webSocketUpstreamHeaders(request, "instance-token")
	for _, absent := range []string{"Authorization", "Cookie", "X-Business-Header"} {
		if headers.Get(absent) != "" {
			t.Fatalf("instance proxy unexpectedly forwards %s: %#v", absent, headers)
		}
	}
	if headers.Get("Origin") != "https://legacy-origin.example" || headers.Get("Sec-WebSocket-Protocol") != "legacy-protocol" || headers.Get("X-Access-Token") != "instance-token" {
		t.Fatalf("legacy WebSocket headers changed: %#v", headers)
	}
}

func TestTokenFailureReturnsSecretSafeBadGateway(t *testing.T) {
	var logs strings.Builder
	p, err := New(Options{
		InstanceID: "dpl-a1b2c3d4",
		Domain:     "ap-guangzhou.tencentags.com",
		RemotePort: 8080,
		Logger:     log.New(&logs, "", 0),
		Verbose:    true,
		TokenProvider: func(context.Context) (string, error) {
			return "", errors.New("token endpoint failed with dpt_do-not-leak")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if _, ok := p.acquireRequestToken(recorder, httptest.NewRequest(http.MethodGet, "/", nil)); ok {
		t.Fatal("token acquisition unexpectedly succeeded")
	}
	if recorder.Code != http.StatusBadGateway || strings.Contains(recorder.Body.String(), "dpt_do-not-leak") {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(logs.String(), "dpt_do-not-leak") {
		t.Fatalf("logs leaked token-shaped secret: %q", logs.String())
	}
}
