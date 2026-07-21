package update

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

type fakeMixedControlPlane struct {
	action         string
	request        map[string]any
	calls          int
	getCalls       int
	status         string
	updateReturned time.Time
	firstGet       time.Time
}

func (f *fakeMixedControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	f.calls++
	f.updateReturned = time.Now()
	return &ags.UpdateSandboxInstanceResponseParams{}, nil
}

func (f *fakeMixedControlPlane) GetInstance(_ context.Context, instanceID string) (*ags.SandboxInstance, error) {
	f.getCalls++
	if f.firstGet.IsZero() {
		f.firstGet = time.Now()
	}
	return &ags.SandboxInstance{InstanceId: &instanceID, Status: &f.status}, nil
}

func TestModuleUpdatesInstanceAndRendersText(t *testing.T) {
	cp := &fakeMixedControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"ins-unit"},
		ArgValues: map[string]string{"instance-id": "ins-unit"},
		Flags: map[string]command.FlagValue{
			"timeout": {Name: "timeout", Type: command.FlagString, String: "10m", Changed: true},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.action != "UpdateSandboxInstance" || cp.request["InstanceId"] != "ins-unit" || cp.request["Timeout"] != "10m" {
		t.Fatalf("action=%q request=%#v", cp.action, cp.request)
	}
	var text bytes.Buffer
	result.Text(&text)
	if !strings.Contains(text.String(), "Instance updated: ins-unit") {
		t.Fatalf("text = %q", text.String())
	}
}

func TestModuleWaitsAfterUpdatingExactlyOnce(t *testing.T) {
	const interval = 20 * time.Millisecond
	cp := &fakeMixedControlPlane{status: "RUNNING"}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, Values: map[string]any{
		resourcewait.OptionsKey: resourcewait.Options{Interval: interval, Timeout: 100 * time.Millisecond},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"ins-unit"},
		ArgValues: map[string]string{"instance-id": "ins-unit"},
		Flags: map[string]command.FlagValue{
			"timeout": {Name: "timeout", Type: command.FlagString, String: "10m", Changed: true},
			"wait":    {Name: "wait", Type: command.FlagBool, Bool: true},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cp.calls != 1 || cp.getCalls != 1 {
		t.Fatalf("Call = %d, GetInstance = %d", cp.calls, cp.getCalls)
	}
	if elapsed := cp.firstGet.Sub(cp.updateReturned); elapsed < interval {
		t.Fatalf("first GetInstance started %s after update returned, want at least %s", elapsed, interval)
	}
	if result.Data.(map[string]any)["Status"] != "RUNNING" {
		t.Fatalf("result = %#v", result.Data)
	}
}
