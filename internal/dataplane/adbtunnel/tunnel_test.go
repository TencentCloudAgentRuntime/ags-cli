package adbtunnel

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func requireLocalListen() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		Skip("local listener unavailable: " + err.Error())
	}
	_ = ln.Close()
}

var wsUpgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// mockWSServer starts a test WebSocket server with a custom per-connection handler.
func mockWSServer(handler func(conn *websocket.Conn)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handler(conn)
	}))
}

// newTestTunnel creates a Tunnel pointed at the given test server endpoint.
func newTestTunnel(endpoint string) (*Tunnel, error) {
	t, err := New(TunnelOptions{
		InstanceID:    "test-sandbox",
		Domain:        "test.local",
		TokenProvider: func() (string, error) { return "mock-token", nil },
		Endpoint:      endpoint,
	})
	if err != nil {
		return nil, err
	}
	// Switch to ws:// for httptest (no TLS).
	t.wsURL = strings.Replace(t.wsURL, "wss://", "ws://", 1)
	return t, nil
}

var _ = Describe("ADB tunnel", func() {
	It("validates tunnel options", func() {
		_, err := New(TunnelOptions{InstanceID: "sandbox-test", Domain: "ap-guangzhou.tencentags.com", TokenProvider: func() (string, error) { return "token", nil }})
		Expect(err).NotTo(HaveOccurred())
		_, err = New(TunnelOptions{Domain: "ap-guangzhou.tencentags.com", TokenProvider: func() (string, error) { return "token", nil }})
		Expect(err).To(MatchError(ContainSubstring("instanceID")))
		_, err = New(TunnelOptions{InstanceID: "sandbox-test", TokenProvider: func() (string, error) { return "token", nil }})
		Expect(err).To(MatchError(ContainSubstring("domain")))
		_, err = New(TunnelOptions{InstanceID: "sandbox-test", Domain: "ap-guangzhou.tencentags.com"})
		Expect(err).To(MatchError(ContainSubstring("tokenProvider")))
	})

	It("constructs websocket URL and host", func() {
		tunnel, err := New(TunnelOptions{InstanceID: "sandbox-aaa", Domain: "ap-guangzhou.tencentags.com", TokenProvider: func() (string, error) { return "token", nil }})
		Expect(err).NotTo(HaveOccurred())
		Expect(tunnel.wsURL).To(Equal("wss://5556-sandbox-aaa.ap-guangzhou.tencentags.com/adb/ws"))
		Expect(tunnel.e2bHost).To(Equal("5556-sandbox-aaa.ap-guangzhou.tencentags.com"))
	})

	It("can reserve a local listener", func() { requireLocalListen() })

	It("reconnects after receiving legacy 4001 close code", func() {
		// Architecture note: in ags-cli's tunnel design, one local TCP connection
		// maps to one WS session. When the WS closes (for any reason, including 4001),
		// the local TCP is closed so ADB retries via a fresh `adb connect`.
		// The test verifies that:
		//   (a) the tunnel does NOT hang or panic on 4001, and
		//   (b) a second ADB connect after the first session ends creates a new WS.
		var connCount atomic.Int32
		srv := mockWSServer(func(conn *websocket.Conn) {
			n := connCount.Add(1)
			if n == 1 {
				// First connection: send legacy 4001 close.
				_ = conn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(closeCodePreempted, "legacy preemption"),
					time.Now().Add(time.Second),
				)
				return
			}
			// Subsequent connections: keep alive briefly.
			time.Sleep(500 * time.Millisecond)
		})
		defer srv.Close()

		endpoint := strings.TrimPrefix(srv.URL, "http://")
		tunnel, err := newTestTunnel(endpoint)
		Expect(err).NotTo(HaveOccurred())

		localAddr, err := tunnel.Start()
		Expect(err).NotTo(HaveOccurred())
		defer tunnel.Stop()

		// First ADB connect — triggers WS #1 which receives 4001 and closes.
		adbConn1, err := net.Dial("tcp", localAddr)
		Expect(err).NotTo(HaveOccurred())

		// Wait for WS #1 to close (4001 round-trip + local TCP close).
		time.Sleep(500 * time.Millisecond)
		adbConn1.Close()

		// Second ADB connect — simulates `adb connect` after device went offline.
		// This should produce WS #2.
		adbConn2, err := net.Dial("tcp", localAddr)
		Expect(err).NotTo(HaveOccurred())
		defer adbConn2.Close()

		// Give tunnel time to establish WS #2.
		time.Sleep(500 * time.Millisecond)

		Expect(connCount.Load()).To(BeNumerically(">=", 2),
			"expected a second WS connection after the ADB client reconnected")
	})

	It("does not reconnect when local ADB client disconnects", func() {
		var connCount atomic.Int32
		srv := mockWSServer(func(conn *websocket.Conn) {
			connCount.Add(1)
			for {
				mt, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				_ = conn.WriteMessage(mt, msg)
			}
		})
		defer srv.Close()

		endpoint := strings.TrimPrefix(srv.URL, "http://")
		tunnel, err := newTestTunnel(endpoint)
		Expect(err).NotTo(HaveOccurred())

		localAddr, err := tunnel.Start()
		Expect(err).NotTo(HaveOccurred())
		defer tunnel.Stop()

		adbConn, err := net.Dial("tcp", localAddr)
		Expect(err).NotTo(HaveOccurred())
		_, _ = adbConn.Write([]byte("hello"))
		time.Sleep(100 * time.Millisecond)
		adbConn.Close() // local disconnect

		// Wait long enough that a reconnect would have occurred if buggy.
		time.Sleep(2 * time.Second)

		Expect(connCount.Load()).To(Equal(int32(1)),
			"expected exactly 1 WS connection (no reconnect on local close)")
	})
})
