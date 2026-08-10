package delete

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/identity"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Cloud API Action: DeleteWorkloadIdentity
func Module() command.Module {
	spec := command.Spec{
		ID:           "identity.delete",
		Path:         []string{"identity", "delete"},
		Use:          "delete <workload-identity-id>",
		Short:        "Delete a workload identity",
		Examples:     []string{"agr identity delete wi-xxxx"},
		Args:         []command.ArgSpec{{Name: "workload-identity-id", Description: "Workload Identity ID", Required: true}},
		SupportsJSON: true,
		Output:       command.OutputSpec{Effects: []string{"delete:identity"}},
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
				_, err := cp.Call(ctx, "DeleteWorkloadIdentity", map[string]any{"WorkloadIdentityId": wid})
				if err != nil {
					return nil, err
				}
				return &command.Result{
					Data:    map[string]any{"WorkloadIdentityId": wid, "Deleted": true},
					Effects: []output.Effect{{Kind: "delete", Resource: "identity", Id: wid}},
					Text:    func(w io.Writer) { fmt.Fprintf(w, "Identity deleted: %s\n", wid) },
				}, nil
			})}, nil
		},
	}
}
