package list

import (
	"bytes"
	"context"
	"fmt"
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

func TestModuleForwardsEverySupportedDeploymentFilter(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"deployment-id", "dpl-a1b2c3d4"},
		{"deployment-name", "workspace-service"},
		{"deployment-name-like", "workspace"},
		{"tool-id", "sdt-a1b2c3d4"},
		{"status", "ACTIVE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp := &fakeControlPlane{response: &ags.DescribeDeploymentListResponseParams{}}
			runtime, err := Module().Build(command.Deps{ControlPlane: cp})
			if err != nil {
				t.Fatal(err)
			}
			_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: map[string]command.FlagValue{
				"filters": {
					Name:    "filters",
					Type:    command.FlagString,
					String:  fmt.Sprintf(`[{"Name":%q,"Values":[%q]}]`, tc.name, tc.value),
					Changed: true,
				},
			}})
			if err != nil {
				t.Fatal(err)
			}
			filters, ok := cp.request["Filters"].([]any)
			if !ok || len(filters) != 1 {
				t.Fatalf("Filters = %#v", cp.request["Filters"])
			}
			filter, ok := filters[0].(map[string]any)
			if !ok || filter["Name"] != tc.name {
				t.Fatalf("Filters = %#v, want Name %q", cp.request["Filters"], tc.name)
			}
			values, ok := filter["Values"].([]any)
			if !ok || len(values) != 1 || values[0] != tc.value {
				t.Fatalf("Filters = %#v, want Values [%q]", cp.request["Filters"], tc.value)
			}
		})
	}
}

func TestFilterHelpDocumentsSupportedSemantics(t *testing.T) {
	var filterInputFound bool
	for _, field := range APIDescriptor().Fields {
		if field.Name != "Filters" || len(field.Inputs) == 0 {
			continue
		}
		filterInputFound = true
		input := field.Inputs[0]
		if input.Format != `[{"Name":"<filter>","Values":["<value>"]}]` {
			t.Errorf("Format = %q", input.Format)
		}
		help := input.Usage + "\n" + strings.Join(input.Values, "\n")
		for _, want := range []string{
			"case-sensitive",
			"AND",
			"OR",
			"deployment-id: exact Deployment ID",
			"deployment-name: exact Deployment name",
			"deployment-name-like: literal substring",
			"tool-id: exact Sandbox Tool ID",
			"status: exact status; ACTIVE, DELETING, or DELETE_FAILED",
		} {
			if !strings.Contains(help, want) {
				t.Errorf("filter help missing %q:\n%s", want, help)
			}
		}
	}
	if !filterInputFound {
		t.Fatal("Filters input not found")
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
