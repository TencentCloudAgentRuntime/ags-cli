package pause

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

func TestGeneratedModuleBuildsPauseInstanceRequest(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := GeneratedModule().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		ArgValues: map[string]string{"instance-id": "ins-unit"},
		Flags: map[string]command.FlagValue{
			"memory": {Name: "memory", Type: command.FlagBool, Bool: true, Changed: true},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.action != "PauseSandboxInstance" || cp.request["InstanceId"] != "ins-unit" {
		t.Fatalf("action=%q request=%#v", cp.action, cp.request)
	}
	if cp.request["Memory"] != true {
		t.Fatalf("request Memory = %#v", cp.request["Memory"])
	}
}
