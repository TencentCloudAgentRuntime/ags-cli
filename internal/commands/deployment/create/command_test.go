package create

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func TestModuleRendersCreatedDeploymentWithoutChangingJSONData(t *testing.T) {
	response := &ags.CreateDeploymentResponseParams{Deployment: testDeployment("dpl-create")}
	cp := &fakeControlPlane{response: response}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{
		"deployment-name": {Name: "deployment-name", Type: command.FlagString, String: "workspace-service", Changed: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if cp.action != "CreateDeployment" || cp.request["DeploymentName"] != "workspace-service" {
		t.Fatalf("action=%q request=%#v", cp.action, cp.request)
	}
	if result.Data != response {
		t.Fatalf("Data = %T, want original response pointer", result.Data)
	}
	if len(result.Effects) != 1 || result.Effects[0].Kind != "create" || result.Effects[0].Resource != "deployment" || result.Effects[0].Id != "dpl-create" {
		t.Fatalf("Effects = %#v", result.Effects)
	}
	var text bytes.Buffer
	result.Text(&text)
	if !strings.Contains(text.String(), "Name:          workspace-service") {
		t.Fatalf("text = %q", text.String())
	}
}

type fakeControlPlane struct {
	action   string
	request  map[string]any
	response any
}

func (f *fakeControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action, f.request = action, request
	return f.response, nil
}

func testDeployment(id string) *ags.Deployment {
	name, status := "workspace-service", "ACTIVE"
	return &ags.Deployment{DeploymentId: &id, DeploymentName: &name, Status: &status}
}
