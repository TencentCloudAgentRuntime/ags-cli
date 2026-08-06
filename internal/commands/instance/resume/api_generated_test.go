package resume

import (
	"context"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

type fakeControlPlane struct {
	action  string
	request map[string]any
}

func (f *fakeControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	return map[string]any{"ok": true}, nil
}

func TestGeneratedModuleBuildsResumeInstanceRequest(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := GeneratedModule().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		ArgValues: map[string]string{"instance-id": "ins-unit"},
		Flags: map[string]command.FlagValue{
			"timeout": {Name: "timeout", Type: command.FlagString, String: "10m", Changed: true},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.action != "ResumeSandboxInstance" || cp.request["InstanceId"] != "ins-unit" {
		t.Fatalf("action=%q request=%#v", cp.action, cp.request)
	}
	if cp.request["Timeout"] != "10m" {
		t.Fatalf("request Timeout = %#v", cp.request["Timeout"])
	}
}
