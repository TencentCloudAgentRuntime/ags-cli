package tunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/client"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/adbtunnel"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

func TestProbeFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		kind  string
		code  string
		exit  int
	}{
		{"auth", sdkerrors.NewTencentCloudSDKError("AuthFailure.Test", "denied", "req-test"), output.KindAuthOrPermission, "AuthFailure.Test", 4},
		{"timeout", &adbtunnel.HandshakeError{Cause: context.DeadlineExceeded}, output.KindTimeout, "TIMEOUT", 1},
		{"canceled", &adbtunnel.HandshakeError{Cause: context.Canceled}, output.KindGenericError, "CANCELED", 1},
		{"handshake", &adbtunnel.HandshakeError{Cause: errors.New("bad handshake"), HTTPStatus: 502, Endpoint: "wss://endpoint.invalid/adb/ws", TimeoutMs: 10000, RequestID: "ws-req"}, output.KindNetwork, "NETWORK_ERROR", 1},
		{"dns", &adbtunnel.HandshakeError{Cause: &net.DNSError{Err: "no such host", Name: "endpoint.invalid", IsNotFound: true}, Endpoint: "wss://endpoint.invalid/adb/ws", TimeoutMs: 10000}, output.KindNetwork, "DNS_ERROR", 1},
		{"unauthorized", &adbtunnel.HandshakeError{Cause: errors.New("bad handshake"), HTTPStatus: 401}, output.KindAuthOrPermission, "TUNNEL_AUTH_FAILED", 4},
		{"forbidden", &adbtunnel.HandshakeError{Cause: errors.New("bad handshake"), HTTPStatus: 403}, output.KindAuthOrPermission, "TUNNEL_AUTH_FAILED", 4},
		{"token-client", errors.New("failed to create API client"), output.KindGenericError, "TUNNEL_ERROR", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeTunnel{probeErr: tc.cause}
			_, err := buildRuntime(t, &bytes.Buffer{}, fake, nil).Handler.Run(t.Context(), request(false, 0))
			got := client.ClassifyError(err)
			if got.Failure.Kind != tc.kind || got.Failure.Code != tc.code || got.ExitCode != tc.exit || !errors.Is(got, tc.cause) {
				t.Fatalf("classification=%#v failure=%#v", got, got.Failure)
			}
			if tc.name == "handshake" && (got.Failure.Details["HTTPStatus"] != 502 || got.Failure.Details["Stage"] != "websocket_handshake") {
				t.Fatalf("details=%#v", got.Failure.Details)
			}
			if tc.name == "handshake" && (got.Failure.Details["Endpoint"] != "wss://endpoint.invalid/adb/ws" || got.Failure.Details["TimeoutMs"] != int64(10000) || got.Failure.Details["RequestId"] != "ws-req") {
				t.Fatalf("lost handshake context: %#v", got.Failure.Details)
			}
			if tc.name == "dns" && (got.Failure.Details["Stage"] != "websocket_handshake" || got.Failure.Details["HTTPStatus"] != nil || got.Failure.Details["RequestId"] != nil) {
				t.Fatalf("invented DNS observations: %#v", got.Failure.Details)
			}
			if tc.name == "auth" && got.Failure.Details["RequestId"] != "req-test" {
				t.Fatal("lost cloud RequestId")
			}
			if !fake.stopped {
				t.Fatal("failed probe did not stop tunnel")
			}
		})
	}
}

func TestModuleDescriptor(t *testing.T) {
	module := Module()
	spec := module.Descriptor.Spec
	if spec.ID != "instance.mobile.tunnel" || spec.Output.DataType != "MobileTunnel" {
		t.Fatalf("spec = %#v", spec)
	}
}

func TestRunTunnelForegroundLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name        string
		delayedStop bool
	}{
		{"immediate_stop", false},
		{"stop_after_timeout", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			stopRelease := make(chan struct{})
			releaseStop := sync.OnceFunc(func() { close(stopRelease) })
			t.Cleanup(releaseStop)
			fake := &fakeTunnel{addr: "127.0.0.1:5555", stopWait: stopRelease, stopDone: make(chan struct{})}
			if !tc.delayedStop {
				releaseStop()
			}
			result, err := buildRuntime(t, out, fake, nil).Handler.Run(t.Context(), request(false, 0))
			if err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if tc.delayedStop && !strings.Contains(out.String(), "Graceful shutdown timed out") {
				t.Fatalf("expected bounded shutdown to time out, out=%q", out.String())
			}
			releaseStop()
			// Run may return at its shutdown deadline before Stop finishes. Wait
			// for the actual worker before reading the state it writes.
			select {
			case <-fake.stopDone:
			case <-time.After(5 * time.Second):
				t.Fatal("tunnel did not stop")
			}
			if !result.StreamDone || !fake.stopped || !strings.Contains(out.String(), "ADB Tunnel established") {
				t.Fatalf("result=%#v stopped=%t out=%q", result, fake.stopped, out.String())
			}
		})
	}
}

func TestRunTunnelDaemonWritesReadyMessage(t *testing.T) {
	out := &bytes.Buffer{}
	fake := &fakeTunnel{addr: "127.0.0.1:5555"}
	_, err := buildRuntime(t, out, fake, nil).Handler.Run(context.Background(), request(true, 0))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	var msg adbtunnel.ReadyMessage
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &msg); err != nil {
		t.Fatalf("ready json=%q err=%v", out.String(), err)
	}
	if msg.Status != "ready" || msg.Port != 5555 {
		t.Fatalf("message=%#v", msg)
	}
}

func TestRunTunnelRejectsInvalidPort(t *testing.T) {
	_, err := buildRuntime(t, &bytes.Buffer{}, &fakeTunnel{}, nil).Handler.Run(context.Background(), request(false, 70000))
	if err == nil || !strings.Contains(err.Error(), "--port must be between") {
		t.Fatalf("error=%v, want invalid port", err)
	}
}

func TestRunTunnelDaemonWritesProbeError(t *testing.T) {
	out := &bytes.Buffer{}
	fake := &fakeTunnel{addr: "127.0.0.1:5555", probeErr: errors.New("upstream unavailable")}
	_, err := buildRuntime(t, out, fake, nil).Handler.Run(context.Background(), request(true, 0))
	if err == nil || !strings.Contains(err.Error(), "upstream probe failed") {
		t.Fatalf("error=%v, want probe error", err)
	}
	if !fake.stopped || !strings.Contains(out.String(), `"status":"error"`) {
		t.Fatalf("stopped=%t out=%q", fake.stopped, out.String())
	}
}

func buildRuntime(t *testing.T, out *bytes.Buffer, tunnel *fakeTunnel, newErr error) command.Runtime {
	t.Helper()
	runtime, err := Module().Build(command.Deps{
		IO: &iostreams.IOStreams{In: &bytes.Buffer{}, Out: out, ErrOut: &bytes.Buffer{}},
		DataPlane: RuntimeDeps{
			AcquireToken:   func(context.Context, string) (string, error) { return "token", nil },
			ValidateConfig: func() error { return nil },
			NewTunnel: func(opts adbtunnel.TunnelOptions) (Tunnel, error) {
				tunnel.opts = opts
				return tunnel, newErr
			},
			Wait:        func(context.Context) {},
			StopTimeout: time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	return runtime
}

func request(daemon bool, port int) command.Request {
	return command.Request{
		Args:      []string{"ins-1"},
		ArgValues: map[string]string{"instance-id": "ins-1"},
		Flags: map[string]command.FlagValue{
			"daemon": {Name: "daemon", Type: command.FlagBool, Bool: daemon, Changed: daemon},
			"port":   {Name: "port", Type: command.FlagInt, Int: port, Changed: port != 0},
		},
	}
}

type fakeTunnel struct {
	addr     string
	startErr error
	probeErr error
	stopped  bool
	stopWait <-chan struct{}
	stopDone chan struct{}
	opts     adbtunnel.TunnelOptions
}

func (f *fakeTunnel) Start() (string, error) {
	if f.startErr != nil {
		return "", f.startErr
	}
	if f.addr == "" {
		return "127.0.0.1:5555", nil
	}
	return f.addr, nil
}

func (f *fakeTunnel) Probe() error { return f.probeErr }

func (f *fakeTunnel) Stop() {
	if f.stopWait != nil {
		<-f.stopWait
	}
	f.stopped = true
	if f.stopDone != nil {
		close(f.stopDone)
	}
}

func TestTunnelStartupFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fake   *fakeTunnel
		newErr error
		code   string
		exit   int
	}{
		{"creation", &fakeTunnel{}, errors.New("missing tunnel configuration"), "TUNNEL_ERROR", 1},
		{"port", &fakeTunnel{startErr: syscall.EADDRINUSE}, nil, "PORT_IN_USE", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildRuntime(t, &bytes.Buffer{}, tc.fake, tc.newErr).Handler.Run(t.Context(), request(false, 5555))
			got := client.ClassifyError(err)
			if got.Failure.Code != tc.code || got.ExitCode != tc.exit || got.Failure.Message == "internal error" {
				t.Fatalf("failure=%#v", got)
			}
			cause := tc.newErr
			if cause == nil {
				cause = tc.fake.startErr
			}
			if !errors.Is(err, cause) {
				t.Fatal("lost cause")
			}
		})
	}
}

func TestTunnelOccupiedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close listener: %v", err)
		}
	})
	port := listener.Addr().(*net.TCPAddr).Port
	rt := runtimeDeps(RuntimeDeps{ValidateConfig: func() error { return nil }})
	_, err = runTunnel(t.Context(), request(false, port), command.Deps{IO: &iostreams.IOStreams{Out: &bytes.Buffer{}}}, rt)
	got := client.ClassifyError(err)
	if got.Failure.Code != "PORT_IN_USE" || got.ExitCode != 2 {
		t.Fatalf("failure=%#v", got)
	}
}

type failingReadyWriter struct{ err error }

func (w failingReadyWriter) Write([]byte) (int, error) { return 0, w.err }

func TestTunnelReadyWriteFailure(t *testing.T) {
	cause := errors.New("output closed")
	fake := &fakeTunnel{}
	rt := runtimeDeps(RuntimeDeps{ValidateConfig: func() error { return nil }, NewTunnel: func(adbtunnel.TunnelOptions) (Tunnel, error) { return fake, nil }})
	_, err := runTunnel(t.Context(), request(true, 0), command.Deps{IO: &iostreams.IOStreams{Out: failingReadyWriter{cause}}}, rt)
	got := client.ClassifyError(err)
	if got.Failure.Code != "TUNNEL_ERROR" || !strings.Contains(got.Failure.Message, "failed to write ready message: output closed") || !errors.Is(err, cause) || !fake.stopped {
		t.Fatalf("error=%v stopped=%t", err, fake.stopped)
	}
}
