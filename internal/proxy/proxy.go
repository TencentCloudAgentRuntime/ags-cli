// Package proxy provides an HTTP/WebSocket reverse proxy for sandbox port forwarding.
//
// It bridges local HTTP/WebSocket connections to a remote sandbox service through
// the AGS TLS-encrypted gateway. The proxy automatically injects access tokens
// into all requests, supporting both HTTP and WebSocket protocols seamlessly.
package proxy

import (
	"context"
	"crypto/tls"
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
	InstanceID string // e.g. "sandbox-xxx"
	Domain     string // e.g. "ap-guangzhou.tencentags.com" (region-qualified)
	RemotePort int    // Port on the remote sandbox to proxy to
	// Token is the access token for the sandbox. Its lifetime is bound to the
	// sandbox instance lifecycle — it stays valid as long as the instance is
	// running, so no refresh or 401-retry logic is required.
	Token         string
	ListenAddress string      // e.g. "127.0.0.1:3000"
	Logger        *log.Logger // Optional logger; defaults to log.Default()
	Insecure      bool        // Skip TLS verification
	Verbose       bool        // Enable verbose request logging
}

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
	if opts.InstanceID == "" || opts.Token == "" || opts.Domain == "" {
		return nil, fmt.Errorf("instanceID, token, and domain are required")
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

	// Customize the transport for TLS
	reverseProxy.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: p.options.Insecure, //nolint:gosec
		},
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	// Customize the Director to inject token and fix Host header
	originalDirector := reverseProxy.Director
	reverseProxy.Director = func(req *http.Request) {
		originalDirector(req)
		// Set the correct Host header (changeOrigin equivalent)
		req.Host = p.targetHost
		// Inject access token. The sandbox gateway authenticates via X-Access-Token only.
		req.Header.Set("X-Access-Token", p.options.Token)
		if p.options.Verbose {
			p.logger.Printf("[HTTP] %s %s", req.Method, req.URL.Path)
		}
	}

	// Error handler: log the full error internally, but only expose details
	// to the client when verbose mode is enabled to avoid leaking internal
	// host names or network topology to network-accessible clients.
	reverseProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		p.logger.Printf("[ERROR] Proxy error: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		if p.options.Verbose {
			fmt.Fprintf(w, "Bad Gateway: %v", err)
		} else {
			fmt.Fprint(w, "Bad Gateway")
		}
	}

	// Create a WebSocket upgrader (we use gorilla/websocket to handle WS proxying).
	// Use explicit buffer sizes so large WebSocket frames (e.g. binary payloads)
	// are handled efficiently without fragmentation.
	wsUpgrader := &websocket.Upgrader{
		ReadBufferSize:  65536,
		WriteBufferSize: 65536,
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
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		// ReadTimeout is intentionally omitted for the HTTP server: setting it
		// would also cap WebSocket connections (which are long-lived upgrades).
		// ReadHeaderTimeout alone is sufficient to mitigate Slowloris on the
		// handshake phase. For the HTTP-only path, the upstream
		// ResponseHeaderTimeout on the transport provides an additional bound.
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
		p.logger.Println("Proxy stopped.")
	}
}

// handleWebSocket bridges a WebSocket connection from the local client to the remote sandbox.
func (p *Proxy) handleWebSocket(w http.ResponseWriter, r *http.Request, upgrader *websocket.Upgrader) {
	// Build upstream WebSocket URL
	upstreamURL := fmt.Sprintf("wss://%s%s", p.targetHost, r.URL.RequestURI())

	// Prepare upstream headers with the pre-acquired token.
	// The sandbox gateway authenticates via X-Access-Token only.
	upstreamHeaders := http.Header{}
	upstreamHeaders.Set("X-Access-Token", p.options.Token)
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
		ReadBufferSize:   65536,
		WriteBufferSize:  65536,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: p.options.Insecure, //nolint:gosec
		},
	}

	upstreamConn, upstreamResp, err := dialer.DialContext(p.ctx, upstreamURL, upstreamHeaders)
	// Close the HTTP response body if present (dial failure with a non-101 HTTP response).
	if upstreamResp != nil && upstreamResp.Body != nil {
		defer func() { _ = upstreamResp.Body.Close() }()
	}
	if err != nil {
		p.logger.Printf("[ERROR] WebSocket upstream dial failed: %v", err)
		// Only expose error details in verbose mode to avoid leaking internal
		// host names or network topology to network-accessible clients.
		errMsg := "Bad Gateway"
		if p.options.Verbose {
			errMsg = fmt.Sprintf("WebSocket upstream connection failed: %v", err)
		}
		http.Error(w, errMsg, http.StatusBadGateway)
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

	if p.options.Verbose {
		p.logger.Printf("[WS] WebSocket connection established: %s", r.URL.Path)
	}

	// Bridge the two WebSocket connections bidirectionally.
	// When the proxy is stopped (p.ctx cancelled), force-unblock any pending
	// ReadMessage calls by setting an immediate read deadline on both sides.
	var wg sync.WaitGroup
	wg.Add(2)

	stopCh := make(chan struct{})
	go func() {
		select {
		case <-p.ctx.Done():
			// Proxy is shutting down: unblock bridgeWebSocket goroutines immediately.
			_ = clientConn.SetReadDeadline(time.Now())
			_ = upstreamConn.SetReadDeadline(time.Now())
		case <-stopCh:
		}
	}()

	// Client -> Upstream
	go func() {
		defer wg.Done()
		p.bridgeWebSocket(clientConn, upstreamConn, "client->upstream")
		_ = upstreamConn.Close() // signal the peer goroutine to exit
	}()

	// Upstream -> Client
	go func() {
		defer wg.Done()
		p.bridgeWebSocket(upstreamConn, clientConn, "upstream->client")
		_ = clientConn.Close() // signal the peer goroutine to exit
	}()

	wg.Wait()
	close(stopCh) // both bridges finished; stop the deadline-setter goroutine
	if p.options.Verbose {
		p.logger.Printf("[WS] WebSocket connection closed: %s", r.URL.Path)
	}
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
