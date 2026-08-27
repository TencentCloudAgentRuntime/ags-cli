package update

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func TestModuleRendersModifiedDeploymentAndPreservesResponse(t *testing.T) {
	id, name := "dpl-update", "workspace-service"
	response := &ags.ModifyDeploymentResponseParams{Deployment: &ags.Deployment{DeploymentId: &id, DeploymentName: &name}}
	cp := &fakeControlPlane{response: response}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{ArgValues: map[string]string{"deployment-id": id}})
	if err != nil {
		t.Fatal(err)
	}
	if cp.action != "ModifyDeployment" || cp.request["DeploymentId"] != id || result.Data != response {
		t.Fatalf("action=%q request=%#v data=%T", cp.action, cp.request, result.Data)
	}
	if len(result.Effects) != 1 || result.Effects[0].Kind != "update" || result.Effects[0].Resource != "deployment" || result.Effects[0].Id != id {
		t.Fatalf("Effects = %#v", result.Effects)
	}
	var text bytes.Buffer
	result.Text(&text)
	if !strings.Contains(text.String(), "Name:          workspace-service") {
		t.Fatalf("text = %q", text.String())
	}
}

func TestModulePreservesExplicitEmptyTags(t *testing.T) {
	id := "dpl-update"
	response := &ags.ModifyDeploymentResponseParams{Deployment: &ags.Deployment{DeploymentId: &id}}
	cp := &fakeControlPlane{response: response}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		ArgValues: map[string]string{"deployment-id": id},
		Flags: map[string]command.FlagValue{
			"tags": {Name: "tags", Type: command.FlagString, String: "[]", Changed: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	tags, ok := cp.request["Tags"].([]any)
	if !ok || tags == nil || len(tags) != 0 {
		t.Fatalf("Tags = %#v, want explicit empty array", cp.request["Tags"])
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
