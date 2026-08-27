package get

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func TestModuleRendersDeploymentDetailsAndMapsPositionalID(t *testing.T) {
	id, name := "dpl-get", "workspace-service"
	response := &ags.DescribeDeploymentResponseParams{Deployment: &ags.Deployment{DeploymentId: &id, DeploymentName: &name}}
	cp := &fakeControlPlane{response: response}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{ArgValues: map[string]string{"deployment-id": id}})
	if err != nil {
		t.Fatal(err)
	}
	if cp.action != "DescribeDeployment" || cp.request["DeploymentId"] != id {
		t.Fatalf("action=%q request=%#v", cp.action, cp.request)
	}
	if result.Data != response {
		t.Fatal("JSON data was normalized instead of preserved")
	}
	var text bytes.Buffer
	result.Text(&text)
	if !strings.Contains(text.String(), "ID:            dpl-get") {
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
