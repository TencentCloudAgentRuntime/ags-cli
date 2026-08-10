package get

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/identity"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Cloud API: DescribeWorkloadIdentityList with single ID
func Module() command.Module {
	spec := command.Spec{
		ID:           "identity.get",
		Path:         []string{"identity", "get"},
		Use:          "get <workload-identity-id>",
		Short:        "Get workload identity details",
		Examples:     []string{"agr identity get wi-xxxx", "agr identity get wi-xxxx -o json"},
		Args:         []command.ArgSpec{{Name: "workload-identity-id", Description: "Workload Identity ID", Required: true}},
		SupportsJSON: true,
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec:   spec,
			Groups: []command.GroupSpec{{Path: []string{"identity"}, Use: "identity", Short: "Manage workload identities"}},
			Source: command.SourceWorkflow,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp := deps.ControlPlane.(identity.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				wid := req.Args[0]
				resp, err := cp.Call(ctx, "DescribeWorkloadIdentityList", map[string]any{
					"WorkloadIdentityIds": []string{wid}, "Limit": 1,
				})
				if err != nil {
					return nil, err
				}
				data, _ := resp.(map[string]any)
				var item map[string]any
				if set, ok := data["WorkloadIdentitySet"].([]any); ok && len(set) > 0 {
					item, _ = set[0].(map[string]any)
				}
				if item == nil {
					return nil, output.NewNotFoundError("IDENTITY_NOT_FOUND",
						fmt.Sprintf("identity %s not found", wid),
						"Verify the identity ID with: agr identity list")
				}
				return &command.Result{Data: item, Text: func(w io.Writer) {
					fmt.Fprintf(w, "ID:   %v\n", item["WorkloadIdentityId"])
					fmt.Fprintf(w, "Name: %v\n", item["Name"])
					if urls, ok := item["AllowedOAuth2ReturnUrls"].([]any); ok && len(urls) > 0 {
						fmt.Fprintf(w, "OAuth2 Return URLs: %v\n", urls)
					}
				}}, nil
			})}, nil
		},
	}
}
