package adbtunnel

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// TestNewTunnel validates tunnel creation with various option combinations.
func TestNewTunnel(t *testing.T) {
	tests := []struct {
		name    string
		opts    TunnelOptions
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid options",
			opts: TunnelOptions{
				InstanceID:    "sandbox-test",
				Domain:        "ap-guangzhou.tencentags.com",
				TokenProvider: func() (string, error) { return "token123", nil },
			},
			wantErr: false,
		},
		{
			name: "valid with endpoint override",
			opts: TunnelOptions{
				InstanceID:    "sdt-abc",
				Domain:        "ap-guangzhou.tencentags.com",
				TokenProvider: func() (string, error) { return "t", nil },
				Endpoint:      "10.0.0.1:443",
			},
			wantErr: false,
		},
		{
			name: "missing instanceID",
			opts: TunnelOptions{
				Domain:        "ap-guangzhou.tencentags.com",
				TokenProvider: func() (string, error) { return "t", nil },
			},
			wantErr: true,
			errMsg:  "instanceID",
		},
		{
			name: "missing domain",
			opts: TunnelOptions{
				InstanceID:    "sandbox-test",
				TokenProvider: func() (string, error) { return "t", nil },
			},
			wantErr: true,
			errMsg:  "domain",
		},
		{
			name: "nil token provider",
			opts: TunnelOptions{
				InstanceID: "sandbox-test",
				Domain:     "ap-guangzhou.tencentags.com",
			},
			wantErr: true,
			errMsg:  "tokenProvider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tunnel, err := New(tt.opts)
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
			if tunnel == nil {
				t.Fatal("tunnel should not be nil")
			}
		})
	}
}

// TestTunnelWSURL validates WebSocket URL construction.
func TestTunnelWSURL(t *testing.T) {
	tokenFn := func() (string, error) { return "t", nil }

	tests := []struct {
		name     string
		opts     TunnelOptions
		wantURL  string
		wantHost string
	}{
		{
			name: "standard URL",
			opts: TunnelOptions{
				InstanceID:    "sandbox-aaa",
				Domain:        "ap-guangzhou.tencentags.com",
				TokenProvider: tokenFn,
			},
			wantURL:  "wss://5556-sandbox-aaa.ap-guangzhou.tencentags.com/adb/ws",
			wantHost: "5556-sandbox-aaa.ap-guangzhou.tencentags.com",
		},
		{
			name: "sdt prefix ID",
			opts: TunnelOptions{
				InstanceID:    "sdt-1gqmhtgz",
				Domain:        "ap-guangzhou.tencentags.com",
				TokenProvider: tokenFn,
			},
			wantURL:  "wss://5556-sdt-1gqmhtgz.ap-guangzhou.tencentags.com/adb/ws",
			wantHost: "5556-sdt-1gqmhtgz.ap-guangzhou.tencentags.com",
		},
		{
			name: "endpoint override",
			opts: TunnelOptions{
				InstanceID:    "sandbox-aaa",
				Domain:        "ap-guangzhou.tencentags.com",
				TokenProvider: tokenFn,
				Endpoint:      "10.0.0.1:443",
			},
			wantURL:  "wss://10.0.0.1:443/adb/ws",
			wantHost: "5556-sandbox-aaa.ap-guangzhou.tencentags.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tunnel, err := New(tt.opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tunnel.wsURL != tt.wantURL {
				t.Errorf("wsURL = %q, want %q", tunnel.wsURL, tt.wantURL)
			}
			if tunnel.e2bHost != tt.wantHost {
				t.Errorf("e2bHost = %q, want %q", tunnel.e2bHost, tt.wantHost)
			}
		})
	}
}

// TestTunnelBridging tests end-to-end TCP↔WS↔TCP data transfer.
func TestTunnelBridging(t *testing.T) {
	// Start a mock TCP "adb" server
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start TCP listener: %v", err)
	}
	defer func() { _ = tcpListener.Close() }()

	// Mock adb server echoes data back
	go func() {
		for {
			conn, err := tcpListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(c, c) // echo
			}(conn)
		}
	}()

	// Start a mock TLS WebSocket adb-websockify server
	wsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate auth header
		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Bridge to mock TCP adb server
		tcpConn, err := net.Dial("tcp", tcpListener.Addr().String())
		if err != nil {
			return
		}
		defer func() { _ = tcpConn.Close() }()

		var wg sync.WaitGroup
		wg.Add(2)

		// WS → TCP
		go func() {
			defer wg.Done()
			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if _, err := tcpConn.Write(msg); err != nil {
					return
				}
			}
		}()

		// TCP → WS
		go func() {
			defer wg.Done()
			buf := make([]byte, 32*1024)
			for {
				n, err := tcpConn.Read(buf)
				if err != nil {
					return
				}
				if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					return
				}
			}
		}()

		wg.Wait()
	}))
	defer wsServer.Close()

	// Extract wss URL from https URL
	wsURL := strings.Replace(wsServer.URL, "https://", "", 1)

	tunnel, err := New(TunnelOptions{
		InstanceID:    "test-sandbox",
		Domain:        "test.example.com",
		TokenProvider: func() (string, error) { return "test-token", nil },
		Endpoint:      wsURL,
		Insecure:      true,
		ListenAddress: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("failed to create tunnel: %v", err)
	}

	addr, err := tunnel.Start()
	if err != nil {
		t.Fatalf("failed to start tunnel: %v", err)
	}
	defer tunnel.Stop()

	// Connect as an adb client
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("failed to connect to tunnel: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send data and verify echo
	testData := "CNXN\x00\x00\x00\x01"
	if _, err := conn.Write([]byte(testData)); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	buf := make([]byte, len(testData))
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("failed to read echo: %v", err)
	}

	if string(buf[:n]) != testData {
		t.Errorf("echo mismatch: got %q, want %q", string(buf[:n]), testData)
	}
}

// TestProbeSuccess tests the Probe method with a reachable server.
func TestProbeSuccess(t *testing.T) {
	wsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Read until close
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer wsServer.Close()

	wsURL := strings.Replace(wsServer.URL, "https://", "", 1)

	tunnel, err := New(TunnelOptions{
		InstanceID:    "test-sandbox",
		Domain:        "test.example.com",
		TokenProvider: func() (string, error) { return "token", nil },
		Endpoint:      wsURL,
		Insecure:      true,
	})
	if err != nil {
		t.Fatalf("failed to create tunnel: %v", err)
	}

	if err := tunnel.Probe(); err != nil {
		t.Errorf("Probe() failed unexpectedly: %v", err)
	}
}

// TestProbeFailure tests the Probe method with an unreachable server.
func TestProbeFailure(t *testing.T) {
	tunnel, err := New(TunnelOptions{
		InstanceID:    "test-sandbox",
		Domain:        "test.example.com",
		TokenProvider: func() (string, error) { return "token", nil },
		Endpoint:      "127.0.0.1:1", // unreachable port
		Insecure:      true,
	})
	if err != nil {
		t.Fatalf("failed to create tunnel: %v", err)
	}

	if err := tunnel.Probe(); err == nil {
		t.Error("Probe() should have failed for unreachable server")
	}
}

// TestProbeTokenFailure tests Probe when token provider returns an error.
func TestProbeTokenFailure(t *testing.T) {
	tunnel, err := New(TunnelOptions{
		InstanceID:    "test-sandbox",
		Domain:        "test.example.com",
		TokenProvider: func() (string, error) { return "", context.DeadlineExceeded },
		Endpoint:      "127.0.0.1:1",
		Insecure:      true,
	})
	if err != nil {
		t.Fatalf("failed to create tunnel: %v", err)
	}

	err = tunnel.Probe()
	if err == nil {
		t.Error("Probe() should have failed with token error")
	}
	if !strings.Contains(err.Error(), "token provider failed") {
		t.Errorf("error %q should mention token provider", err.Error())
	}
}

// TestIsPreemptionError tests the preemption detection logic.
func TestIsPreemptionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "preemption close error",
			err:  &websocket.CloseError{Code: closeCodePreempted, Text: "Preempted"},
			want: true,
		},
		{
			name: "normal close error",
			err:  &websocket.CloseError{Code: websocket.CloseNormalClosure, Text: "bye"},
			want: false,
		},
		{
			name: "going away close error",
			err:  &websocket.CloseError{Code: websocket.CloseGoingAway, Text: ""},
			want: false,
		},
		{
			name: "generic error",
			err:  io.EOF,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPreemptionError(tt.err)
			if got != tt.want {
				t.Errorf("isPreemptionError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestTunnelStartStop tests the start/stop lifecycle.
func TestTunnelStartStop(t *testing.T) {
	tunnel, err := New(TunnelOptions{
		InstanceID:    "test",
		Domain:        "test.example.com",
		TokenProvider: func() (string, error) { return "t", nil },
		ListenAddress: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("failed to create tunnel: %v", err)
	}

	addr, err := tunnel.Start()
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	if addr == "" {
		t.Error("addr should not be empty")
	}

	if tunnel.LocalAddr() == "" {
		t.Error("LocalAddr should not be empty after Start")
	}

	// Verify we can connect to the listener
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("should be able to connect to tunnel listener: %v", err)
	}
	_ = conn.Close()

	// Stop and verify listener is closed
	tunnel.Stop()

	_, err = net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err == nil {
		t.Error("should not be able to connect after Stop")
	}
}

// TestTunnelLocalAddrBeforeStart tests LocalAddr returns empty before Start.
func TestTunnelLocalAddrBeforeStart(t *testing.T) {
	tunnel, err := New(TunnelOptions{
		InstanceID:    "test",
		Domain:        "test.example.com",
		TokenProvider: func() (string, error) { return "t", nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	if tunnel.LocalAddr() != "" {
		t.Error("LocalAddr should be empty before Start")
	}
}
