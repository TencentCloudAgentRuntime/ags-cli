package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// TestNewProxy validates proxy creation with various option combinations.
func TestNewProxy(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid options",
			opts: Options{
				InstanceID: "sandbox-test",
				Domain:     "ap-guangzhou.tencentags.com",
				RemotePort: 3000,
				Token:      "token123",
			},
			wantErr: false,
		},
		{
			name: "valid with custom listen address",
			opts: Options{
				InstanceID:    "sandbox-test",
				Domain:        "ap-guangzhou.tencentags.com",
				RemotePort:    8080,
				Token:         "t",
				ListenAddress: "127.0.0.1:9999",
			},
			wantErr: false,
		},
		{
			name: "missing instanceID",
			opts: Options{
				Domain:     "ap-guangzhou.tencentags.com",
				RemotePort: 3000,
				Token:      "t",
			},
			wantErr: true,
			errMsg:  "instanceID",
		},
		{
			name: "missing domain",
			opts: Options{
				InstanceID: "sandbox-test",
				RemotePort: 3000,
				Token:      "t",
			},
			wantErr: true,
			errMsg:  "domain",
		},
		{
			name: "empty token",
			opts: Options{
				InstanceID: "sandbox-test",
				Domain:     "ap-guangzhou.tencentags.com",
				RemotePort: 3000,
			},
			wantErr: true,
			errMsg:  "token",
		},
		{
			name: "invalid remote port zero",
			opts: Options{
				InstanceID: "sandbox-test",
				Domain:     "ap-guangzhou.tencentags.com",
				RemotePort: 0,
				Token:      "t",
			},
			wantErr: true,
			errMsg:  "remotePort",
		},
		{
			name: "invalid remote port negative",
			opts: Options{
				InstanceID: "sandbox-test",
				Domain:     "ap-guangzhou.tencentags.com",
				RemotePort: -1,
				Token:      "t",
			},
			wantErr: true,
			errMsg:  "remotePort",
		},
		{
			name: "invalid remote port too large",
			opts: Options{
				InstanceID: "sandbox-test",
				Domain:     "ap-guangzhou.tencentags.com",
				RemotePort: 70000,
				Token:      "t",
			},
			wantErr: true,
			errMsg:  "remotePort",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p == nil {
				t.Fatal("proxy should not be nil")
			}
		})
	}
}

// TestTargetHost validates target host construction.
func TestTargetHost(t *testing.T) {
	tests := []struct {
		name       string
		opts       Options
		wantTarget string
	}{
		{
			name: "standard host",
			opts: Options{
				InstanceID: "sandbox-aaa",
				Domain:     "ap-guangzhou.tencentags.com",
				RemotePort: 3000,
				Token:      "t",
			},
			wantTarget: "3000-sandbox-aaa.ap-guangzhou.tencentags.com",
		},
		{
			name: "sdt prefix ID",
			opts: Options{
				InstanceID: "sdt-1gqmhtgz",
				Domain:     "ap-guangzhou.tencentags.com",
				RemotePort: 8080,
				Token:      "t",
			},
			wantTarget: "8080-sdt-1gqmhtgz.ap-guangzhou.tencentags.com",
		},
		{
			name: "internal domain",
			opts: Options{
				InstanceID: "sandbox-bbb",
				Domain:     "ap-guangzhou.internal.tencentags.com",
				RemotePort: 5173,
				Token:      "t",
			},
			wantTarget: "5173-sandbox-bbb.ap-guangzhou.internal.tencentags.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(tt.opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.targetHost != tt.wantTarget {
				t.Errorf("targetHost = %q, want %q", p.targetHost, tt.wantTarget)
			}
		})
	}
}

// TestProxyStartStop tests the start/stop lifecycle.
func TestProxyStartStop(t *testing.T) {
	p, err := New(Options{
		InstanceID:    "test",
		Domain:        "test.example.com",
		RemotePort:    3000,
		Token:         "t",
		ListenAddress: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	addr, err := p.Start()
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	if addr == "" {
		t.Error("addr should not be empty")
	}

	if p.LocalAddr() == "" {
		t.Error("LocalAddr should not be empty after Start")
	}

	// Verify we can connect to the listener
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("should be able to connect to proxy listener: %v", err)
	}
	_ = conn.Close()

	// Stop and verify listener is closed.
	// p.Stop() blocks until the server has fully shut down (up to 5 s),
	// so no sleep is needed before probing the port.
	p.Stop()

	_, err = net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err == nil {
		t.Error("should not be able to connect after Stop")
	}
}

// TestProxyLocalAddrBeforeStart tests LocalAddr returns empty before Start.
func TestProxyLocalAddrBeforeStart(t *testing.T) {
	p, err := New(Options{
		InstanceID: "test",
		Domain:     "test.example.com",
		RemotePort: 3000,
		Token:      "t",
	})
	if err != nil {
		t.Fatal(err)
	}

	if p.LocalAddr() != "" {
		t.Error("LocalAddr should be empty before Start")
	}
}

// TestProxyHTTPForwarding tests end-to-end HTTP request proxying with token injection.
func TestProxyHTTPForwarding(t *testing.T) {
	// Start a mock upstream HTTPS server
	var receivedToken string
	var receivedHost string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("X-Access-Token")
		receivedHost = r.Host
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "hello from upstream")
	}))
	defer upstream.Close()

	// Extract host from upstream URL
	upstreamHost := strings.TrimPrefix(upstream.URL, "https://")

	// Create proxy that points to the test server
	p, err := New(Options{
		InstanceID:    "test-sandbox",
		Domain:        "test.example.com",
		RemotePort:    3000,
		Token:         "my-secret-token",
		ListenAddress: "127.0.0.1:0",
		Insecure:      true,
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Override targetHost to point to our test server
	p.targetHost = upstreamHost

	addr, err := p.Start()
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	defer p.Stop()

	// Make a request through the proxy
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/test-path", addr))
	if err != nil {
		t.Fatalf("HTTP request through proxy failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	// Verify response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if string(body) != "hello from upstream" {
		t.Errorf("body = %q, want %q", string(body), "hello from upstream")
	}

	// Verify token was injected
	if receivedToken != "my-secret-token" {
		t.Errorf("X-Access-Token = %q, want %q", receivedToken, "my-secret-token")
	}

	// Verify Host header was set (changeOrigin)
	if receivedHost != upstreamHost {
		t.Errorf("Host = %q, want %q", receivedHost, upstreamHost)
	}
}

// TestProxyWebSocketForwarding tests end-to-end WebSocket proxying.
func TestProxyWebSocketForwarding(t *testing.T) {
	var receivedToken string

	// Start a mock upstream WebSocket server (TLS)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("X-Access-Token")

		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Echo back messages
		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(msgType, msg); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()

	upstreamHost := strings.TrimPrefix(upstream.URL, "https://")

	// Create and start proxy
	p, err := New(Options{
		InstanceID:    "test-sandbox",
		Domain:        "test.example.com",
		RemotePort:    3000,
		Token:         "ws-token-123",
		ListenAddress: "127.0.0.1:0",
		Insecure:      true,
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// Override targetHost to point to our test server
	p.targetHost = upstreamHost

	addr, err := p.Start()
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	defer p.Stop()

	// Connect WebSocket through the proxy
	dialer := &websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}
	wsURL := fmt.Sprintf("ws://%s/ws-test", addr)
	conn, _, err := dialer.DialContext(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial through proxy failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send message and verify echo
	testMsg := "hello websocket"
	if err := conn.WriteMessage(websocket.TextMessage, []byte(testMsg)); err != nil {
		t.Fatalf("failed to write WS message: %v", err)
	}

	msgType, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read WS message: %v", err)
	}

	if msgType != websocket.TextMessage {
		t.Errorf("message type = %d, want %d", msgType, websocket.TextMessage)
	}
	if string(msg) != testMsg {
		t.Errorf("echo = %q, want %q", string(msg), testMsg)
	}

	// Verify token was injected on the upstream side
	if receivedToken != "ws-token-123" {
		t.Errorf("X-Access-Token = %q, want %q", receivedToken, "ws-token-123")
	}
}

// TestProxyWebSocketBidirectional tests bidirectional WebSocket data transfer.
func TestProxyWebSocketBidirectional(t *testing.T) {
	// Start upstream that sends messages to the client
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Server sends a greeting first
		if err := conn.WriteMessage(websocket.TextMessage, []byte("server-hello")); err != nil {
			return
		}

		// Then echo client messages with prefix
		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			reply := fmt.Sprintf("echo:%s", msg)
			if err := conn.WriteMessage(msgType, []byte(reply)); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()

	upstreamHost := strings.TrimPrefix(upstream.URL, "https://")

	p, err := New(Options{
		InstanceID:    "test-sandbox",
		Domain:        "test.example.com",
		RemotePort:    3000,
		Token:         "token",
		ListenAddress: "127.0.0.1:0",
		Insecure:      true,
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}
	p.targetHost = upstreamHost

	addr, err := p.Start()
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	defer p.Stop()

	dialer := &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(context.Background(), fmt.Sprintf("ws://%s/ws", addr), nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Read server greeting
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read server greeting: %v", err)
	}
	if string(msg) != "server-hello" {
		t.Errorf("greeting = %q, want %q", string(msg), "server-hello")
	}

	// Send and receive echo
	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	_, msg, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read echo: %v", err)
	}
	if string(msg) != "echo:ping" {
		t.Errorf("echo = %q, want %q", string(msg), "echo:ping")
	}
}

// TestProxyMultipleConcurrentHTTPRequests tests handling multiple concurrent HTTP requests.
func TestProxyMultipleConcurrentHTTPRequests(t *testing.T) {
	var mu sync.Mutex
	requestCount := 0

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer upstream.Close()

	upstreamHost := strings.TrimPrefix(upstream.URL, "https://")

	p, err := New(Options{
		InstanceID:    "test-sandbox",
		Domain:        "test.example.com",
		RemotePort:    3000,
		Token:         "token",
		ListenAddress: "127.0.0.1:0",
		Insecure:      true,
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}
	p.targetHost = upstreamHost

	addr, err := p.Start()
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	defer p.Stop()

	// Fire 10 concurrent requests
	const numRequests = 10
	var wg sync.WaitGroup
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{
				Timeout: 5 * time.Second,
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
				},
			}
			resp, err := client.Get(fmt.Sprintf("http://%s/", addr))
			if err != nil {
				errors <- err
				return
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errors <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent request error: %v", err)
	}

	mu.Lock()
	if requestCount != numRequests {
		t.Errorf("requestCount = %d, want %d", requestCount, numRequests)
	}
	mu.Unlock()
}

// TestIsWebSocketRequest tests WebSocket request detection.
func TestIsWebSocketRequest(t *testing.T) {
	tests := []struct {
		name    string
		headers http.Header
		want    bool
	}{
		{
			name:    "standard websocket",
			headers: http.Header{"Connection": {"Upgrade"}, "Upgrade": {"websocket"}},
			want:    true,
		},
		{
			name:    "case insensitive",
			headers: http.Header{"Connection": {"upgrade"}, "Upgrade": {"WebSocket"}},
			want:    true,
		},
		{
			name:    "comma-separated connection header",
			headers: http.Header{"Connection": {"keep-alive, Upgrade"}, "Upgrade": {"websocket"}},
			want:    true,
		},
		{
			name:    "normal HTTP request",
			headers: http.Header{"Connection": {"keep-alive"}},
			want:    false,
		},
		{
			name:    "no connection header",
			headers: http.Header{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{Header: tt.headers}
			got := isWebSocketRequest(r)
			if got != tt.want {
				t.Errorf("isWebSocketRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestProxyStopWithActiveWebSocket tests that Stop() unblocks active WebSocket
// connections rather than hanging indefinitely.
func TestProxyStopWithActiveWebSocket(t *testing.T) {
	// upstream: accept a WebSocket and hold it open until the client disconnects.
	connectedCh := make(chan struct{})
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		close(connectedCh) // signal that the WS is established
		// Block until the proxy closes the upstream connection.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()

	upstreamHost := strings.TrimPrefix(upstream.URL, "https://")

	p, err := New(Options{
		InstanceID:    "test-sandbox",
		Domain:        "test.example.com",
		RemotePort:    3000,
		Token:         "token",
		ListenAddress: "127.0.0.1:0",
		Insecure:      true,
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}
	p.targetHost = upstreamHost

	addr, err := p.Start()
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}

	// Establish an active WebSocket connection through the proxy.
	dialer := &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	wsConn, _, err := dialer.DialContext(context.Background(), fmt.Sprintf("ws://%s/ws", addr), nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer func() { _ = wsConn.Close() }()

	// Wait for the upstream to confirm the connection is established.
	select {
	case <-connectedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream WebSocket connection not established in time")
	}

	// Stop the proxy with an active WebSocket connection; must return promptly.
	doneCh := make(chan struct{})
	go func() {
		p.Stop()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		// Stop returned — correct behaviour.
	case <-time.After(10 * time.Second):
		t.Fatal("p.Stop() blocked too long with an active WebSocket connection")
	}
}

// TestProxyVerboseLogging verifies that verbose mode logs HTTP request details.
func TestProxyVerboseLogging(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	upstreamHost := strings.TrimPrefix(upstream.URL, "https://")

	var logBuf strings.Builder
	logger := log.New(&logBuf, "", 0)

	p, err := New(Options{
		InstanceID:    "test-sandbox",
		Domain:        "test.example.com",
		RemotePort:    3000,
		Token:         "token",
		ListenAddress: "127.0.0.1:0",
		Insecure:      true,
		Verbose:       true,
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}
	p.targetHost = upstreamHost

	addr, err := p.Start()
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	defer p.Stop()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/health", addr))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	// Give the logger a moment to flush (it's synchronous, so this is instant).
	logged := logBuf.String()
	if !strings.Contains(logged, "GET") || !strings.Contains(logged, "/health") {
		t.Errorf("verbose log should contain method and path, got: %q", logged)
	}
}
