package update

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

type fakeControlPlane struct {
	action         string
	request        map[string]any
	err            error
	calls          int
	getCalls       int
	updateReturned time.Time
	firstGet       time.Time
}

func (f *fakeControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	f.updateReturned = time.Now()
	return map[string]any{"ok": true}, nil
}

func (f *fakeControlPlane) GetTool(_ context.Context, toolID string) (*ags.SandboxTool, error) {
	f.getCalls++
	if f.firstGet.IsZero() {
		f.firstGet = time.Now()
	}
	status := "ACTIVE"
	return &ags.SandboxTool{ToolId: &toolID, Status: &status}, nil
}

func TestModuleWaitsAfterUpdatingExactlyOnce(t *testing.T) {
	const interval = 20 * time.Millisecond
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, Values: map[string]any{
		resourcewait.OptionsKey: resourcewait.Options{Interval: interval, Timeout: 100 * time.Millisecond},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"sdt-unit"},
		ArgValues: map[string]string{"tool-id": "sdt-unit"},
		Flags: map[string]command.FlagValue{
			"description": {Name: "description", Type: command.FlagString, String: "updated", Changed: true},
			"request":     {Name: "request", Type: command.FlagString},
			"wait":        {Name: "wait", Type: command.FlagBool, Bool: true},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cp.calls != 1 || cp.getCalls != 1 {
		t.Fatalf("Call = %d, GetTool = %d", cp.calls, cp.getCalls)
	}
	if elapsed := cp.firstGet.Sub(cp.updateReturned); elapsed < interval {
		t.Fatalf("first GetTool started %s after update returned, want at least %s", elapsed, interval)
	}
	if result.Data.(map[string]any)["Status"] != "ACTIVE" {
		t.Fatalf("result = %#v", result.Data)
	}
}

func TestModuleBuildsUpdateRequest(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"sdt-unit"},
		ArgValues: map[string]string{"tool-id": "sdt-unit"},
		Flags: map[string]command.FlagValue{
			"description":           {Name: "description", Type: command.FlagString, String: "updated", Changed: true},
			"network-configuration": {Name: "network-configuration", Type: command.FlagString, String: `{"NetworkMode":"SANDBOX"}`, Changed: true},
			"tags":                  {Name: "tags", Type: command.FlagString, String: `[{"Key":"env","Value":"unit"}]`, Changed: true},
			"request":               {Name: "request", Type: command.FlagString},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.action != "UpdateSandboxTool" {
		t.Fatalf("action = %q", cp.action)
	}
	if cp.request["ToolId"] != "sdt-unit" || cp.request["Description"] != "updated" {
		t.Fatalf("request = %#v", cp.request)
	}
	network, ok := cp.request["NetworkConfiguration"].(map[string]any)
	if !ok || network["NetworkMode"] != "SANDBOX" {
		t.Fatalf("NetworkConfiguration = %#v", cp.request["NetworkConfiguration"])
	}
	tags, ok := cp.request["Tags"].([]any)
	if !ok || len(tags) != 1 {
		t.Fatalf("Tags = %#v", cp.request["Tags"])
	}
	tag, ok := tags[0].(map[string]any)
	if !ok || tag["Key"] != "env" || tag["Value"] != "unit" {
		t.Fatalf("Tags = %#v", cp.request["Tags"])
	}
}

func TestModuleRejectsMissingUpdateField(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"sdt-unit"},
		ArgValues: map[string]string{"tool-id": "sdt-unit"},
		Flags: map[string]command.FlagValue{
			"request": {Name: "request", Type: command.FlagString},
		},
	})
	if err == nil {
		t.Fatalf("expected missing update field error")
	}
}

func TestModuleMergesPositionalIntoRequest(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"sdt-unit"},
		ArgValues: map[string]string{"tool-id": "sdt-unit"},
		Flags: map[string]command.FlagValue{
			"request": {Name: "request", Type: command.FlagString, String: `{"Description":"from-json"}`, Changed: true},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.request["ToolId"] != "sdt-unit" || cp.request["Description"] != "from-json" {
		t.Fatalf("request = %#v", cp.request)
	}
}

func TestModuleReturnsExecutorError(t *testing.T) {
	cp := &fakeControlPlane{err: errors.New("remote boom")}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"sdt-unit"},
		ArgValues: map[string]string{"tool-id": "sdt-unit"},
		Flags: map[string]command.FlagValue{
			"description": {Name: "description", Type: command.FlagString, String: "updated", Changed: true},
			"request":     {Name: "request", Type: command.FlagString},
		},
	})
	if err == nil || err.Error() != "remote boom" {
		t.Fatalf("error = %v, want remote boom", err)
	}
}

func TestToolID(t *testing.T) {
	id, err := ToolID(command.Request{Args: []string{"sdt-arg"}})
	if err != nil || id != "sdt-arg" {
		t.Fatalf("ToolID from Args = %q, %v", id, err)
	}
	id, err = ToolID(command.Request{ArgValues: map[string]string{"tool-id": "sdt-value"}})
	if err != nil || id != "sdt-value" {
		t.Fatalf("ToolID from ArgValues = %q, %v", id, err)
	}
	if _, err := ToolID(command.Request{}); err == nil {
		t.Fatalf("expected missing tool-id error")
	} else {
		cliErr := output.ClassifyError(err)
		if cliErr == nil || cliErr.Failure == nil || cliErr.Failure.Code != "MISSING_REQUIRED_ARG" {
			t.Fatalf("error = %#v, want MISSING_REQUIRED_ARG", err)
		}
	}
}

var _ apicli.ControlPlane = (*fakeControlPlane)(nil)
