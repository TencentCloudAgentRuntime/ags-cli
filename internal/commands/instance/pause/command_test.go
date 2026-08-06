package pause

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

type fakeMixedControlPlane struct {
	action   string
	request  map[string]any
	calls    int
	getCalls int
	status   string
	statuses []string
}

func (f *fakeMixedControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	f.calls++
	return &ags.PauseSandboxInstanceResponseParams{}, nil
}

func (f *fakeMixedControlPlane) GetInstance(_ context.Context, instanceID string) (*ags.SandboxInstance, error) {
	status := f.status
	if len(f.statuses) > 0 {
		index := f.getCalls
		if index >= len(f.statuses) {
			index = len(f.statuses) - 1
		}
		status = f.statuses[index]
	}
	f.getCalls++
	return &ags.SandboxInstance{InstanceId: &instanceID, Status: &status}, nil
}

func TestModulePausesInstanceAndRendersText(t *testing.T) {
	cp := &fakeMixedControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"ins-unit"},
		ArgValues: map[string]string{"instance-id": "ins-unit"},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.action != "PauseSandboxInstance" || cp.request["InstanceId"] != "ins-unit" {
		t.Fatalf("action=%q request=%#v", cp.action, cp.request)
	}
	var text bytes.Buffer
	result.Text(&text)
	if !strings.Contains(text.String(), "Pause requested: ins-unit") {
		t.Fatalf("text = %q", text.String())
	}
}

func TestModuleWaitsThroughStaleRunningAfterPausingExactlyOnce(t *testing.T) {
	cp := &fakeMixedControlPlane{statuses: []string{"RUNNING", "PAUSING", "PAUSED"}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, Values: map[string]any{
		resourcewait.OptionsKey: resourcewait.Options{Interval: time.Millisecond, Timeout: 50 * time.Millisecond},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"ins-unit"},
		ArgValues: map[string]string{"instance-id": "ins-unit"},
		Flags:     map[string]command.FlagValue{"wait": {Name: "wait", Type: command.FlagBool, Bool: true}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cp.calls != 1 || cp.getCalls != 3 {
		t.Fatalf("Call = %d, GetInstance = %d", cp.calls, cp.getCalls)
	}
	if result.Data.(map[string]any)["Status"] != "PAUSED" {
		t.Fatalf("result = %#v", result.Data)
	}
}

func TestModuleWaitReportsBackendRollbackToRunning(t *testing.T) {
	cp := &fakeMixedControlPlane{statuses: []string{"PAUSING", "RUNNING"}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, Values: map[string]any{
		resourcewait.OptionsKey: resourcewait.Options{Interval: time.Millisecond, Timeout: 50 * time.Millisecond},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"ins-unit"},
		ArgValues: map[string]string{"instance-id": "ins-unit"},
		Flags:     map[string]command.FlagValue{"wait": {Name: "wait", Type: command.FlagBool, Bool: true}},
	})
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) || cliErr.Failure.Code != "WAIT_FAILED" {
		t.Fatalf("error = %#v, want WAIT_FAILED", err)
	}
	if cp.calls != 1 || cp.getCalls != 2 {
		t.Fatalf("Call = %d, GetInstance = %d", cp.calls, cp.getCalls)
	}
}
