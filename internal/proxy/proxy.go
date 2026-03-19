// Package proxy provides an HTTP/WebSocket reverse proxy for sandbox port forwarding.
//
// It bridges local HTTP/WebSocket connections to a remote sandbox service through
// SandPortal's TLS-encrypted gateway. The proxy automatically injects access tokens
// into all requests, supporting both HTTP and WebSocket protocols seamlessly.
//
// This is the Go equivalent of the Node.js http-proxy based approach used in
// sandbox-vite-project, adapted for the AGS CLI architecture.
package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Options defines configuration for the port-forward proxy.
type Options struct {
	InstanceID    string                 // e.g. "sandbox-xxx"
	Domain        string                 // e.g. "ap-guangzhou.tencentags.com" (region-qualified)
	RemotePort    int                    // Port on the remote sandbox to proxy to
	TokenProvider func() (string, error) // Dynamic token provider; called for each request
	ListenAddress string                 // e.g. "127.0.0.1:3000"
	Logger        *log.Logger            // Optional logger; defaults to log.Default()
	Insecure      bool                   // Skip TLS verification
	Verbose       bool                   // Enable verbose request logging
}

// errTokenUnavailable is a sentinel error used to signal that the token could not be obtained.
var errTokenUnavailable = fmt.Errorf("access token unavailable")

// tokenAwareTransport wraps an http.RoundTripper and checks for a context-embedded
// token error before sending the request, returning a clear error to the client.
type tokenAwareTransport struct {
	base http.RoundTripper
}

func (t *tokenAwareTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err, ok := req.Context().Value(tokenErrorKey{}).(error); ok && err != nil {
		return nil, errTokenUnavailable
	}
	return t.base.RoundTrip(req)
}

// tokenErrorKey is the context key for passing token errors from Director to RoundTripper.
type tokenErrorKey struct{}

// Proxy manages an active HTTP/WebSocket reverse proxy that forwards local
// requests to a remote sandbox service through SandPortal.
type Proxy struct {
	options    Options
	listener   net.Listener
	server     *http.Server
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *log.Logger
	targetHost string // e.g. "3000-sandbox-xxx.ap-guangzhou.tencentags.com"
}

// New creates and initializes a new port-forward proxy but does not start it.
func New(opts Options) (*Proxy, error) {
	if opts.InstanceID == "" || opts.TokenProvider == nil || opts.Domain == "" {
		return nil, fmt.Errorf("instanceID, tokenProvider, and domain are required")
	}
	if opts.RemotePort <= 0 || opts.RemotePort > 65535 {
		return nil, fmt.Errorf("remotePort must be between 1 and 65535")
	}
	if opts.ListenAddress == "" {
		opts.ListenAddress = "127.0.0.1:0"
	}

	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}

	targetHost := fmt.Sprintf("%d-%s.%s", opts.RemotePort, opts.InstanceID, opts.Domain)

	ctx, cancel := context.WithCancel(context.Background())

	return &Proxy{
		options:    opts,
		ctx:        ctx,
		cancel:     cancel,
		logger:     logger,
		targetHost: targetHost,
	}, nil
}

// Start binds to the local address and begins serving proxy requests.
// It returns the actual listen address (useful when port 0 is specified).
func (p *Proxy) Start() (string, error) {
	listener, err := net.Listen("tcp", p.options.ListenAddress)
	if err != nil {
		return "", fmt.Errorf("failed to bind local address: %w", err)
	}
	p.listener = listener

	// Build the HTTP reverse proxy
	targetURL := &url.URL{
		Scheme: "https",
		Host:   p.targetHost,
	}

	reverseProxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Customize the transport for TLS and token-aware request interception
	baseTransport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: p.options.Insecure,
		},
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	reverseProxy.Transport = &tokenAwareTransport{base: baseTransport}

	// Customize the Director to inject token and fix Host header
	originalDirector := reverseProxy.Director
	reverseProxy.Director = func(req *http.Request) {
		originalDirector(req)
		// Set the correct Host header (changeOrigin equivalent)
		req.Host = p.targetHost
		// Inject access token
		token, err := p.options.TokenProvider()
		if err != nil {
			p.logger.Printf("[WARN] Failed to get token: %v", err)
			// Embed the error into the request context so the transport can reject it
			*req = *req.WithContext(context.WithValue(req.Context(), tokenErrorKey{}, err))
			return
		}
		req.Header.Set("X-Access-Token", token)
		req.Header.Set("Authorization", "Bearer "+token)
		if p.options.Verbose {
			p.logger.Printf("[HTTP] %s %s", req.Method, req.URL.Path)
		}
	}

	// Error handler
	reverseProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(err, errTokenUnavailable) {
			p.logger.Printf("[ERROR] Token unavailable, rejecting request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "Failed to obtain access token. Please check your credentials.", http.StatusServiceUnavailable)
			return
		}
		p.logger.Printf("[ERROR] Proxy error: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, "Bad Gateway: %v", err)
	}

	// Create a WebSocket upgrader (we use gorilla/websocket to handle WS proxying)
	wsUpgrader := &websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for local proxy
		},
	}

	// Build the HTTP handler that routes between HTTP proxy and WebSocket proxy
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketRequest(r) {
			p.handleWebSocket(w, r, wsUpgrader)
			return
		}
		reverseProxy.ServeHTTP(w, r)
	})

	p.server = &http.Server{
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return p.ctx
		},
	}

	p.logger.Printf("Proxy listening on %s (forwarding to https://%s)", listener.Addr().String(), p.targetHost)

	go func() {
		if err := p.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			p.logger.Printf("[ERROR] Server error: %v", err)
		}
	}()

	return listener.Addr().String(), nil
}

// LocalAddr returns the listener's local address, or empty string if not started.
func (p *Proxy) LocalAddr() string {
	if p.listener == nil {
		return ""
	}
	return p.listener.Addr().String()
}

// Stop gracefully shuts down the proxy server.
func (p *Proxy) Stop() {
	p.cancel()
	if p.server != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = p.server.Shutdown(shutdownCtx)
	}
	p.logger.Println("Proxy stopped.")
}

// handleWebSocket bridges a WebSocket connection from the local client to the remote sandbox.
func (p *Proxy) handleWebSocket(w http.ResponseWriter, r *http.Request, upgrader *websocket.Upgrader) {
	// Get token for upstream connection
	token, err := p.options.TokenProvider()
	if err != nil {
		p.logger.Printf("[ERROR] Failed to get token for WebSocket: %v", err)
		http.Error(w, "Failed to obtain access token. Please check your credentials.", http.StatusServiceUnavailable)
		return
	}

	// Build upstream WebSocket URL
	wsScheme := "wss"
	upstreamURL := fmt.Sprintf("%s://%s%s", wsScheme, p.targetHost, r.URL.RequestURI())

	// Prepare upstream headers
	upstreamHeaders := http.Header{}
	upstreamHeaders.Set("X-Access-Token", token)
	upstreamHeaders.Set("Authorization", "Bearer "+token)
	upstreamHeaders.Set("Host", p.targetHost)

	// Copy relevant headers from the original request
	if origin := r.Header.Get("Origin"); origin != "" {
		upstreamHeaders.Set("Origin", origin)
	}
	for _, proto := range r.Header[http.CanonicalHeaderKey("Sec-WebSocket-Protocol")] {
		upstreamHeaders.Add("Sec-WebSocket-Protocol", proto)
	}

	// Connect to upstream WebSocket
	dialer := &websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: p.options.Insecure,
		},
	}

	upstreamConn, upstreamResp, err := dialer.DialContext(p.ctx, upstreamURL, upstreamHeaders)
	if err != nil {
		p.logger.Printf("[ERROR] WebSocket upstream dial failed: %v", err)
		http.Error(w, fmt.Sprintf("WebSocket upstream connection failed: %v", err), http.StatusBadGateway)
		return
	}
	defer func() { _ = upstreamConn.Close() }()

	// Pass negotiated subprotocol back to client
	responseHeader := http.Header{}
	if upstreamResp != nil {
		if proto := upstreamResp.Header.Get("Sec-WebSocket-Protocol"); proto != "" {
			responseHeader.Set("Sec-WebSocket-Protocol", proto)
		}
	}

	// Upgrade client connection to WebSocket
	clientConn, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		p.logger.Printf("[ERROR] WebSocket client upgrade failed: %v", err)
		return
	}
	defer func() { _ = clientConn.Close() }()

	p.logger.Printf("[WS] WebSocket connection established: %s", r.URL.Path)

	// Bridge the two WebSocket connections bidirectionally
	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Upstream
	go func() {
		defer wg.Done()
		p.bridgeWebSocket(clientConn, upstreamConn, "client->upstream")
		_ = upstreamConn.Close() // 通知对端 goroutine 退出
	}()

	// Upstream -> Client
	go func() {
		defer wg.Done()
		p.bridgeWebSocket(upstreamConn, clientConn, "upstream->client")
		_ = clientConn.Close() // 通知对端 goroutine 退出
	}()

	wg.Wait()
	p.logger.Printf("[WS] WebSocket connection closed: %s", r.URL.Path)
}

// bridgeWebSocket copies messages from src to dst WebSocket connection.
func (p *Proxy) bridgeWebSocket(src, dst *websocket.Conn, direction string) {
	for {
		msgType, msg, err := src.ReadMessage()
		if err != nil {
			// Normal close or error
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				p.logger.Printf("[WS] %s read error: %v", direction, err)
			}
			// Forward the original close code if available, otherwise use NormalClosure
			closeCode := websocket.CloseNormalClosure
			closeText := ""
			if closeErr, ok := err.(*websocket.CloseError); ok {
				closeCode = closeErr.Code
				closeText = closeErr.Text
			}
			closeMsg := websocket.FormatCloseMessage(closeCode, closeText)
			_ = dst.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(3*time.Second))
			return
		}

		if err := dst.WriteMessage(msgType, msg); err != nil {
			p.logger.Printf("[WS] %s write error: %v", direction, err)
			return
		}
	}
}

// isWebSocketRequest checks if the HTTP request is a WebSocket upgrade request.
// Per RFC 6455, a valid WebSocket handshake must have both:
//   - Upgrade: websocket
//   - Connection: Upgrade
func isWebSocketRequest(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, v := range r.Header["Connection"] {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "upgrade") {
				return true
			}
		}
	}
	return false
}
