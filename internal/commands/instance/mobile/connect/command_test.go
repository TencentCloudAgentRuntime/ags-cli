package connect

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/tunnelstore"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
)

func TestModuleDescriptor(t *testing.T) {
	module := Module()
	spec := module.Descriptor.Spec
	if spec.ID != "instance.mobile.connect" || len(module.Descriptor.Groups) != 2 || !spec.SupportsJSON {
		t.Fatalf("descriptor = %#v", module.Descriptor)
	}
}

func TestRunConnectStartsTunnelAndSavesMapping(t *testing.T) {
	store := &fakeStore{}
	stderr := &bytes.Buffer{}
	result, err := buildRuntime(t, store, stderr, nil).Handler.Run(context.Background(), command.Request{
		Args:      []string{"ins-1"},
		ArgValues: map[string]string{"instance-id": "ins-1"},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	data := result.Data.(map[string]any)
	if data["AdbAddress"] != "127.0.0.1:5555" || store.savedID != "ins-1" || store.saved.Port != 5555 {
		t.Fatalf("result=%#v store=%#v", result, store)
	}
	result.Text(io.Discard)
	if !strings.Contains(stderr.String(), "connected to ins-1") || !strings.Contains(stderr.String(), "/tmp/tunnel.log") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestRunConnectDisconnectsStaleTunnel(t *testing.T) {
	store := &fakeStore{entries: map[string]tunnelstore.TunnelEntry{"ins-1": {Port: 4444}}}
	_, err := buildRuntime(t, store, &bytes.Buffer{}, nil).Handler.Run(context.Background(), command.Request{Args: []string{"ins-1"}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if store.cleaned != "ins-1" || store.disconnected != "127.0.0.1:4444" {
		t.Fatalf("store=%#v", store)
	}
}

func TestRunConnectTreatsADBConnectFailureAsWarning(t *testing.T) {
	store := &fakeStore{}
	stderr := &bytes.Buffer{}
	result, err := buildRuntime(t, store, stderr, errors.New("adb refused")).Handler.Run(context.Background(), command.Request{Args: []string{"ins-1"}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	result.Text(io.Discard)
	if !strings.Contains(stderr.String(), "adb connect failed") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestRunConnectRequiresADB(t *testing.T) {
	runtime, err := Module().Build(command.Deps{
		IO: testIO(&bytes.Buffer{}),
		DataPlane: RuntimeDeps{
			RequireADB: func() (string, error) { return "", errors.New("missing adb") },
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{Args: []string{"ins-1"}})
	if err == nil || !strings.Contains(err.Error(), "missing adb") {
		t.Fatalf("error=%v, want adb error", err)
	}
}

func buildRuntime(t *testing.T, store *fakeStore, stderr *bytes.Buffer, adbConnectErr error) command.Runtime {
	t.Helper()
	runtime, err := Module().Build(command.Deps{
		IO:  testIO(stderr),
		Now: func() time.Time { return time.Unix(100, 0) },
		DataPlane: RuntimeDeps{
			RequireADB:     func() (string, error) { return "/adb", nil },
			ValidateConfig: func() error { return nil },
			NewStore:       func() (Store, error) { return store, nil },
			DisconnectADB: func(_ string, addr string) error {
				store.disconnected = addr
				return nil
			},
			StartTunnel: func(_ context.Context, _ string, _ int) (TunnelReady, error) {
				return TunnelReady{Port: 5555, PID: 123, ExePath: "/agr", LogPath: "/tmp/tunnel.log"}, nil
			},
			ConnectADB: func(_ string, _ string, _ int, _ io.Writer) error {
				return adbConnectErr
			},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	return runtime
}

func testIO(stderr *bytes.Buffer) *iostreams.IOStreams {
	return &iostreams.IOStreams{In: &bytes.Buffer{}, Out: &bytes.Buffer{}, ErrOut: stderr}
}

type fakeStore struct {
	entries      map[string]tunnelstore.TunnelEntry
	cleaned      string
	savedID      string
	saved        tunnelstore.TunnelEntry
	disconnected string
}

func (f *fakeStore) Get(id string) (tunnelstore.TunnelEntry, bool, error) {
	entry, ok := f.entries[id]
	return entry, ok, nil
}

func (f *fakeStore) Cleanup(id string) error {
	f.cleaned = id
	return nil
}

func (f *fakeStore) Save(id string, entry tunnelstore.TunnelEntry) error {
	f.savedID = id
	f.saved = entry
	return nil
}
