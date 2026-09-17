package commands

import (
	"context"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	apikeylist "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/apikey/list"
	deploymentlist "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/deployment/list"
	instancelist "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/instance/list"
	toollist "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/tool/list"
	"testing"
)

type listResponseControlPlane struct{ response map[string]any }

func (f listResponseControlPlane) Call(context.Context, string, map[string]any) (any, error) {
	return f.response, nil
}

func TestListResponseValidation(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		module      command.Module
		all         bool
	}{
		{"instance", "InstanceSet", instancelist.Module(), false},
		{"instance-all", "InstanceSet", instancelist.Module(), true},
		{"tool", "SandboxToolSet", toollist.Module(), false},
		{"apikey", "APIKeySet", apikeylist.Module(), false},
		{"deployment", "DeploymentSet", deploymentlist.Module(), false},
	} {
		for _, value := range []struct {
			name  string
			data  any
			valid bool
		}{
			{"empty", []any{}, true}, {"resource", []any{map[string]any{"InstanceId": "ssi-test", "ToolId": "sdt-test", "KeyId": "key-test", "DeploymentId": "dpl-test"}}, true}, {"null", nil, true},
			{"object", map[string]any{"Id": "resource"}, false},
			{"scalar", "invalid", false}, {"bad-member", []any{"invalid"}, false},
			{"null-member", []any{nil}, false},
		} {
			t.Run(tc.name+"/"+value.name, func(t *testing.T) {
				runtime, err := tc.module.Build(command.Deps{ControlPlane: listResponseControlPlane{map[string]any{tc.field: value.data, "TotalCount": 0}}})
				if err != nil {
					t.Fatal(err)
				}
				flags := map[string]command.FlagValue{
					"limit":  {Name: "limit", Type: command.FlagInt, Int: 20},
					"offset": {Name: "offset", Type: command.FlagInt, Int: 0},
					"all":    {Name: "all", Type: command.FlagBool, Bool: tc.all, Changed: tc.all},
				}
				result, err := runtime.Handler.Run(t.Context(), command.Request{Flags: flags})
				if value.valid {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil || result != nil {
					t.Fatalf("malformed response succeeded: result=%#v err=%v", result, err)
				}
			})
		}
	}
}
