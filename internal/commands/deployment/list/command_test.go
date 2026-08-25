package list

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func TestModuleRendersFrozenDeploymentTableAndPreservesResponse(t *testing.T) {
	id, name, status, created := "dpl-list", "workspace-service", "ACTIVE", "2026-08-22T08:00:00Z"
	total := int64(1)
	response := &ags.DescribeDeploymentListResponseParams{TotalCount: &total, DeploymentSet: []*ags.Deployment{{DeploymentId: &id, DeploymentName: &name, Status: &status, CreatedTime: &created}}}
	cp := &fakeControlPlane{response: response}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if cp.action != "DescribeDeploymentList" || result.Data != response {
		t.Fatalf("action=%q data=%T", cp.action, result.Data)
	}
	var text bytes.Buffer
	result.Text(&text)
	if !strings.Contains(text.String(), "ID") || !strings.Contains(text.String(), "LIFECYCLE") || !strings.Contains(text.String(), "AFFINITY") || !strings.Contains(text.String(), "AGE") {
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
