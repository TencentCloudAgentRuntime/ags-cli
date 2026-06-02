package list

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/tunnelstore"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
)

func TestModuleListsConnections(t *testing.T) {
	created := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	ios, _, stdout, _ := iostreams.Test()
	runtime, err := Module().Build(command.Deps{
		IO: ios,
		DataPlane: RuntimeDeps{NewStore: func() (Store, error) {
			return fakeStore{entries: map[string]tunnelstore.TunnelEntry{"ins-1": {PID: 123, Port: 5555, CreatedAt: created}}}, nil
		}},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	data := result.Data.(map[string]any)
	if data["Total"] != 1 {
		t.Fatalf("data=%#v", data)
	}
	result.Text(stdout)
	if !strings.Contains(stdout.String(), "ins-1") || !strings.Contains(stdout.String(), "127.0.0.1:5555") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestModuleRendersEmptyConnections(t *testing.T) {
	ios, _, _, stderr := iostreams.Test()
	runtime, err := Module().Build(command.Deps{
		IO: ios,
		DataPlane: RuntimeDeps{NewStore: func() (Store, error) {
			return fakeStore{entries: map[string]tunnelstore.TunnelEntry{}}, nil
		}},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	result.Text(ioDiscard{})
	if !strings.Contains(stderr.String(), "No active connections") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestModuleHandlesCorruptStoreRecovery(t *testing.T) {
	ios, _, _, stderr := iostreams.Test()
	recovered := &tunnelstore.CorruptStoreRecoveredError{Path: "bad", BackupPath: "backup"}
	runtime, err := Module().Build(command.Deps{
		IO: ios,
		DataPlane: RuntimeDeps{NewStore: func() (Store, error) {
			return fakeStore{err: recovered}, nil
		}},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings=%#v", result.Warnings)
	}
	result.Text(ioDiscard{})
	if !strings.Contains(stderr.String(), "Warning:") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestModuleReturnsListError(t *testing.T) {
	runtime, err := Module().Build(command.Deps{
		IO: testIO(),
		DataPlane: RuntimeDeps{NewStore: func() (Store, error) {
			return fakeStore{err: errors.New("boom")}, nil
		}},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{})
	if err == nil || !strings.Contains(err.Error(), "failed to list tunnels") {
		t.Fatalf("error=%v, want list error", err)
	}
}

type fakeStore struct {
	entries map[string]tunnelstore.TunnelEntry
	err     error
}

func (f fakeStore) List() (map[string]tunnelstore.TunnelEntry, error) {
	return f.entries, f.err
}

func (f fakeStore) Cleanup(sandboxID string) error {
	return nil
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func testIO() *iostreams.IOStreams {
	return &iostreams.IOStreams{In: &bytes.Buffer{}, Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}
}
