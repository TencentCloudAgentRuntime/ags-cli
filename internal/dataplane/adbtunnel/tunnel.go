// Package adbtunnel provides a TCP-to-WebSocket tunnel for ADB connections.
//
// It bridges local TCP connections (from adb clients) to a remote adb-websockify
// server via WebSocket, enabling secure ADB access through SandPortal's TLS-encrypted
// gateway. The tunnel supports automatic reconnection with exponential backoff,
// token refresh on reconnect, graceful handling of server-side preemption, and
// degraded mode with automatic recovery probing.
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
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// closeCodePreempted is the custom WebSocket close code (4001) sent by
	// adb-websockify when a new connection preempts the current one.
	closeCodePreempted = 4001

	// maxBackoff is the upper bound for reconnection delay.
	maxBackoff = 30 * time.Second

	// maxDialFailures is the maximum number of consecutive WebSocket dial failures
	// (e.g., bad handshake due to deleted sandbox or invalid token) before entering
	// degraded mode. Transient network errors that occur after a successful connection
	// do not count.
	maxDialFailures = 5

	// probeTimeout is the maximum time allowed for a Probe() handshake.
	probeTimeout = 10 * time.Second

	// probeBaseDelay is the initial delay between recovery probes in degraded mode.
	probeBaseDelay = 5 * time.Second

	// probeMaxDelay is the maximum delay between recovery probes in degraded mode.
	probeMaxDelay = 30 * time.Second
)

// TunnelState represents the health state of the tunnel.
type TunnelState int32

const (
	// StateHealthy means the tunnel is operational and bridging data.
	StateHealthy TunnelState = iota
	// StateDegraded means the WebSocket upstream is unreachable. The TCP listener
	// remains active but incoming connections are immediately closed (causing ADB
	// to report the device as "offline"). Background probing attempts recovery.
	StateDegraded
)

// String returns a human-readable label for the tunnel state.
func (s TunnelState) String() string {
	switch s {
	case StateHealthy:
		return "connected"
	case StateDegraded:
		return "unreachable"
	default:
		return "unknown"
	}
}

// TunnelOptions defines configuration for the ADB WebSocket tunnel.
type TunnelOptions struct {
	InstanceID    string                 // e.g. "sandbox-xxx"
	Domain        string                 // e.g. "ap-guangzhou.tencentags.com"
	TokenProvider func() (string, error) // Dynamic token provider; called on each (re)connect
	Endpoint      string                 // Optional, overrides WebSocket destination (e.g. gateway IP)
	Insecure      bool                   // Skip TLS verification
	ListenAddress string                 // e.g. "127.0.0.1:0" for random port
	Logger        *log.Logger            // Optional logger; defaults to log.Default()

	// OnStateChange is called when the tunnel transitions between states.
	// It is called from a background goroutine; the callback must be safe for
	// concurrent use. If nil, state changes are only logged.
	OnStateChange func(state TunnelState)
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

	// state tracks the current health state (StateHealthy or StateDegraded).
	state      atomic.Int32
	degradedMu sync.Mutex
	degradedAt time.Time

	// probing guards against launching multiple recovery probes concurrently.
	probing atomic.Bool
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

// State returns the current tunnel state.
func (t *Tunnel) State() TunnelState {
	return TunnelState(t.state.Load())
}

// DegradedAt returns the time when the tunnel entered degraded mode.
// Returns zero time if the tunnel is healthy.
func (t *Tunnel) DegradedAt() time.Time {
	t.degradedMu.Lock()
	defer t.degradedMu.Unlock()
	return t.degradedAt
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

// setState transitions the tunnel to the given state and fires the callback.
// The degradedMu lock serialises transitions so concurrent callers cannot
// interleave state-swap and callback invocations.
func (t *Tunnel) setState(newState TunnelState) {
	t.degradedMu.Lock()
	defer t.degradedMu.Unlock()

	old := TunnelState(t.state.Swap(int32(newState)))
	if old == newState {
		return // no-op
	}
	if newState == StateDegraded {
		t.degradedAt = time.Now()
		t.logger.Printf("[WARN] Tunnel entering degraded mode (upstream unreachable)")
	} else {
		t.degradedAt = time.Time{}
		t.logger.Printf("[INFO] Tunnel recovered from degraded mode")
	}
	if t.options.OnStateChange != nil {
		t.options.OnStateChange(newState)
	}
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

		// In degraded mode, accept TCP connections but close immediately.
		// This causes ADB to mark the device as "offline" rather than "device",
		// giving accurate status visibility via `adb devices`.
		if t.State() == StateDegraded {
			_ = conn.Close()
			continue
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
// After maxDialFailures consecutive dial failures, it enters degraded mode and closes
// the local connection (new connections will be rejected by acceptLoop).
func (t *Tunnel) handleConnectionWithReconnect(localConn net.Conn) {
	defer func() { _ = localConn.Close() }()

	attempt := 0
	consecutiveDialFailures := 0
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

		// Track consecutive dial failures (connection never established).
		// A dial failure means the error occurred instantly (< 1s), indicating
		// the server rejected us (bad handshake, sandbox deleted, token invalid).
		if time.Since(connStart) < time.Second {
			consecutiveDialFailures++
		} else {
			consecutiveDialFailures = 0
		}

		if consecutiveDialFailures >= maxDialFailures {
			t.logger.Printf("[ERROR] %d consecutive connection failures. Entering degraded mode.", consecutiveDialFailures)
			t.setState(StateDegraded)
			t.startRecoveryProbe()
			return // Close this local connection; acceptLoop will reject new ones
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

		t.logger.Printf("[WARN] WebSocket connection lost: %v. Reconnecting in %v... (attempt %d)", err, delay, attempt)

		select {
		case <-t.ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// startRecoveryProbe launches a background goroutine that periodically probes
// the upstream WebSocket endpoint. When a probe succeeds, the tunnel transitions
// back to StateHealthy and subsequent ADB connections will be bridged normally.
// Only one probe goroutine can be active at a time (guarded by t.probing).
func (t *Tunnel) startRecoveryProbe() {
	// Ensure only one recovery probe runs at a time. If another goroutine
	// already started one, this is a no-op.
	if !t.probing.CompareAndSwap(false, true) {
		return
	}
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		defer t.probing.Store(false)

		attempt := 0
		for {
			select {
			case <-t.ctx.Done():
				return
			default:
			}

			// Exponential backoff for probe: 5s, 10s, 20s, 30s cap
			attempt++
			delay := time.Duration(math.Min(
				float64(probeBaseDelay)*math.Pow(2, float64(attempt-1)),
				float64(probeMaxDelay),
			))

			t.logger.Printf("[INFO] Recovery probe scheduled in %v (attempt %d)", delay, attempt)

			select {
			case <-t.ctx.Done():
				return
			case <-time.After(delay):
			}

			// Already recovered (another goroutine may have triggered recovery)
			if t.State() != StateDegraded {
				return
			}

			if err := t.Probe(); err != nil {
				t.logger.Printf("[WARN] Recovery probe failed: %v", err)
				continue
			}

			// Probe succeeded — recover
			t.setState(StateHealthy)
			t.logger.Printf("[INFO] Recovery probe succeeded. Tunnel is healthy again.")
			return
		}
	}()
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

	// If we were in degraded mode and got here (shouldn't normally happen, but
	// guard against races), transition back to healthy.
	if t.State() == StateDegraded {
		t.setState(StateHealthy)
	}

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

			// ReadDeadline renewal is handled exclusively by PongHandler.
			// Do NOT renew on data frames — this ensures we detect Pong loss
			// even when the server keeps sending data (prevents "half-dead"
			// connections from staying alive indefinitely).

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
