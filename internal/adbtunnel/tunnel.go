// Package adbtunnel provides a TCP-to-WebSocket tunnel for ADB connections.
//
// It bridges local TCP connections (from adb clients) to a remote adb-websockify
// server via WebSocket, enabling secure ADB access through SandPortal's TLS-encrypted
// gateway. The tunnel supports automatic reconnection with exponential backoff,
// token refresh on reconnect, and graceful handling of server-side preemption.
package adbtunnel

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// closeCodePreempted is the custom WebSocket close code (4001) sent by
	// adb-websockify when a new connection preempts the current one.
	closeCodePreempted = 4001

	// maxBackoff is the upper bound for reconnection delay.
	maxBackoff = 30 * time.Second

	// probeTimeout is the maximum time allowed for a Probe() handshake.
	probeTimeout = 10 * time.Second
)

// TunnelOptions defines configuration for the ADB WebSocket tunnel.
type TunnelOptions struct {
	InstanceID    string                 // e.g. "sandbox-xxx"
	Domain        string                 // e.g. "ap-guangzhou.tencentags.com"
	TokenProvider func() (string, error) // Dynamic token provider; called on each (re)connect
	Endpoint      string                 // Optional, overrides WebSocket destination (e.g. gateway IP)
	Insecure      bool                   // Skip TLS verification
	ListenAddress string                 // e.g. "127.0.0.1:0" for random port
	Logger        *log.Logger            // Optional logger; defaults to log.Default()
}

// Tunnel manages an active bridging service between local ADB clients and
// a cloud sandbox via SandPortal WebSocket proxy.
type Tunnel struct {
	options  TunnelOptions
	listener net.Listener
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
	wsURL    string
	e2bHost  string
	logger   *log.Logger
}

// New creates and initializes a new ADB tunnel but does not start accepting connections.
func New(opts TunnelOptions) (*Tunnel, error) {
	if opts.InstanceID == "" || opts.TokenProvider == nil || opts.Domain == "" {
		return nil, fmt.Errorf("instanceID, tokenProvider, and domain are required")
	}

	if opts.ListenAddress == "" {
		opts.ListenAddress = "127.0.0.1:0" // Ephemeral port
	}

	e2bHost := fmt.Sprintf("5556-%s.%s", opts.InstanceID, opts.Domain)
	var wsURL string
	if opts.Endpoint != "" {
		wsURL = fmt.Sprintf("wss://%s/adb/ws", opts.Endpoint)
	} else {
		wsURL = fmt.Sprintf("wss://%s/adb/ws", e2bHost)
	}

	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Tunnel{
		options: opts,
		ctx:     ctx,
		cancel:  cancel,
		wsURL:   wsURL,
		e2bHost: e2bHost,
		logger:  logger,
	}, nil
}

// Start binds to the local address and begins accepting TCP connections in the background.
// It returns the actual listen address (useful when port 0 is specified).
func (t *Tunnel) Start() (string, error) {
	listener, err := net.Listen("tcp", t.options.ListenAddress)
	if err != nil {
		return "", fmt.Errorf("failed to bind local address: %w", err)
	}
	t.listener = listener

	t.logger.Printf("ADB Tunnel listening on %s (bridging to %s)", listener.Addr().String(), t.wsURL)

	t.wg.Add(1)
	go t.acceptLoop()

	return listener.Addr().String(), nil
}

// LocalAddr returns the listener's local address, or empty string if not started.
func (t *Tunnel) LocalAddr() string {
	if t.listener == nil {
		return ""
	}
	return t.listener.Addr().String()
}

// Stop closes the listener and forces graceful shutdown of all active bridge connections.
func (t *Tunnel) Stop() {
	t.cancel()
	if t.listener != nil {
		_ = t.listener.Close()
	}
	t.wg.Wait()
	t.logger.Println("ADB Tunnel stopped.")
}

// Probe performs a lightweight WebSocket handshake to verify the upstream tunnel
// endpoint is reachable and the token is valid. It connects, then immediately
// sends a Close frame and disconnects. Returns nil if the probe succeeds.
func (t *Tunnel) Probe() error {
	dialer := t.newDialer()

	headers := http.Header{}
	token, err := t.options.TokenProvider()
	if err != nil {
		return fmt.Errorf("token provider failed: %w", err)
	}
	headers.Add("Authorization", "Bearer "+token)
	if t.options.Endpoint != "" {
		headers.Set("Host", t.e2bHost)
	}

	probeCtx, probeCancel := context.WithTimeout(t.ctx, probeTimeout)
	defer probeCancel()

	wsConn, _, err := dialer.DialContext(probeCtx, t.wsURL, headers)
	if err != nil {
		return fmt.Errorf("upstream WS handshake failed: %w", err)
	}

	// Send a clean close and disconnect immediately
	_ = wsConn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "probe"),
		time.Now().Add(3*time.Second),
	)
	_ = wsConn.Close()

	return nil
}

func (t *Tunnel) newDialer() *websocket.Dialer {
	dialer := &websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}
	if t.options.Insecure {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return dialer
}

func (t *Tunnel) acceptLoop() {
	defer t.wg.Done()

	for {
		conn, err := t.listener.Accept()
		if err != nil {
			select {
			case <-t.ctx.Done():
				return // Shutdown requested
			default:
				t.logger.Printf("Tunnel accept failed: %v", err)
				continue
			}
		}

		t.wg.Add(1)
		go func(c net.Conn) {
			defer t.wg.Done()
			t.handleConnectionWithReconnect(c)
		}(conn)
	}
}

// handleConnectionWithReconnect wraps handleConnection with automatic reconnection.
// On WebSocket disconnection (except preemption), it re-establishes the WS connection
// while keeping the local TCP connection alive, so the adb client doesn't need to reconnect.
func (t *Tunnel) handleConnectionWithReconnect(localConn net.Conn) {
	defer func() { _ = localConn.Close() }()

	attempt := 0
	for {
		connStart := time.Now()
		preempted, err := t.handleConnection(localConn)
		if err == nil {
			// Normal close (context cancelled or clean shutdown)
			return
		}

		// If preempted by server (close code 4001), do NOT reconnect
		if preempted {
			t.logger.Printf("[WARN] Connection preempted by new client. Not reconnecting.")
			return
		}

		// Check if context is cancelled (shutdown)
		select {
		case <-t.ctx.Done():
			return
		default:
		}

		// Reset backoff if the connection was stable (lasted > 30s),
		// so transient blips after a long session start fresh at 1s.
		if time.Since(connStart) > 30*time.Second {
			attempt = 0
		}

		// Exponential backoff: 1s, 2s, 4s, 8s, 16s, 30s cap
		attempt++
		delay := time.Duration(math.Min(
			float64(time.Second)*math.Pow(2, float64(attempt-1)),
			float64(maxBackoff),
		))

		t.logger.Printf("[WARN] WebSocket connection lost: %v. Reconnecting in %v... (attempt %d/∞)", err, delay, attempt)

		select {
		case <-t.ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// handleConnection bridges a single local TCP connection to a WebSocket upstream.
// Returns (preempted, error) where preempted=true means server sent close code 4001.
func (t *Tunnel) handleConnection(localConn net.Conn) (preempted bool, err error) {
	dialer := t.newDialer()

	headers := http.Header{}
	token, tokenErr := t.options.TokenProvider()
	if tokenErr != nil {
		return false, fmt.Errorf("token provider failed: %w", tokenErr)
	}
	headers.Add("Authorization", "Bearer "+token)
	if t.options.Endpoint != "" {
		headers.Set("Host", t.e2bHost)
	}

	wsConn, _, dialErr := dialer.DialContext(t.ctx, t.wsURL, headers)
	if dialErr != nil {
		return false, fmt.Errorf("WebSocket dial failed: %w", dialErr)
	}

	t.logger.Printf("[INFO] WebSocket connected to %s", t.wsURL)

	var wsMu sync.Mutex
	pingInterval := 30 * time.Second
	readTimeout := pingInterval * 5 // Allow up to 4 missed pings before timeout
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	wsConn.SetPongHandler(func(appData string) error {
		_ = wsConn.SetReadDeadline(time.Now().Add(readTimeout))
		return nil
	})

	doneRead := make(chan struct{})
	doneWrite := make(chan error, 1) // buffered to capture close error

	var transferWg sync.WaitGroup
	transferWg.Add(2)

	// Local TCP -> WS Write
	go func() {
		defer transferWg.Done()
		defer close(doneRead)
		buf := make([]byte, 32*1024)
		for {
			n, readErr := localConn.Read(buf)
			if readErr != nil {
				if readErr != io.EOF && !strings.Contains(readErr.Error(), "use of closed network connection") {
					t.logger.Printf("Local read error: %v", readErr)
				}
				return
			}
			wsMu.Lock()
			writeErr := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n])
			wsMu.Unlock()
			if writeErr != nil {
				t.logger.Printf("WebSocket write error: %v", writeErr)
				return
			}
		}
	}()

	// WS Read -> Local TCP
	go func() {
		defer transferWg.Done()
		defer close(doneWrite)
		_ = wsConn.SetReadDeadline(time.Now().Add(readTimeout))
		var lastErr error
		for {
			msgType, reader, readErr := wsConn.NextReader()
			if readErr != nil {
				if !websocket.IsUnexpectedCloseError(readErr, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					lastErr = nil // expected close
				} else {
					lastErr = readErr
				}
				doneWrite <- lastErr
				return
			}

			// Reset deadline on valid read (in addition to PongHandler)
			_ = wsConn.SetReadDeadline(time.Now().Add(readTimeout))

			if msgType == websocket.BinaryMessage || msgType == websocket.TextMessage {
				if _, copyErr := io.Copy(localConn, reader); copyErr != nil {
					t.logger.Printf("Local write error: %v", copyErr)
					doneWrite <- copyErr
					return
				}
			}
		}
	}()

	// Orchestrator: Watch for completion or context cancellation.
	// Close connections first to unblock goroutines, then wait for them to finish.
	var wsCloseErr error
	defer func() {
		_ = wsConn.Close()
		// Do NOT close localConn here — it's managed by handleConnectionWithReconnect
		transferWg.Wait()
	}()

	for {
		select {
		case <-t.ctx.Done():
			// Send clean close frame before exit
			wsMu.Lock()
			_ = wsConn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"),
				time.Now().Add(3*time.Second),
			)
			wsMu.Unlock()
			return false, nil
		case <-doneRead:
			// Local TCP closed (adb client disconnected) — normal, no reconnect
			return false, nil
		case wsCloseErr = <-doneWrite:
			// WebSocket read goroutine exited — check if preempted
			if isPreemptionError(wsCloseErr) {
				return true, wsCloseErr
			}
			if wsCloseErr != nil {
				return false, wsCloseErr
			}
			return false, nil
		case <-pingTicker.C:
			wsMu.Lock()
			_ = wsConn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second))
			wsMu.Unlock()
		}
	}
}

// isPreemptionError checks if a WebSocket error indicates server-side preemption (close code 4001).
func isPreemptionError(err error) bool {
	if err == nil {
		return false
	}
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		return closeErr.Code == closeCodePreempted
	}
	return false
}
