package pause

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

type fakeMixedControlPlane struct {
	action  string
	request map[string]any
}

func (f *fakeMixedControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	return &ags.PauseSandboxInstanceResponseParams{}, nil
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
