package delete

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module returns the "credential provider delete" command module.
// Cloud API Action: DeleteCredentialProvider
// Note: SecretMultiUser/OAuth2 types must be DISABLED before deletion.
func Module() command.Module {
	spec := command.Spec{
		ID:    "credential.provider.delete",
		Path:  []string{"credential", "provider", "delete"},
		Use:   "delete <provider-id>",
		Short: "Delete a credential provider",
		Long: `Delete a credential provider.

For SecretMultiUser and OAuth2 types, the provider must be in DISABLED status
before deletion. Use "agr credential provider update --status DISABLED" first.
SecretMultiUser providers also require all secrets to be removed first.`,
		Examples:     []string{"agr credential provider delete agc-xxxx"},
		Args:         []command.ArgSpec{{Name: "provider-id", Description: "Provider ID to delete", Required: true}},
		SupportsJSON: true,
		Output:       command.OutputSpec{Effects: []string{"delete:credential-provider"}},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec:   spec,
			Groups: []command.GroupSpec{{Path: []string{"credential"}, Use: "credential", Short: "Manage credentials and providers"}, {Path: []string{"credential", "provider"}, Use: "provider", Short: "Manage credential providers"}},
			Source: command.SourceWorkflow,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp := deps.ControlPlane.(credential.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				pid := req.Args[0]
				_, err := cp.Call(ctx, "DeleteCredentialProvider", map[string]any{"ProviderId": pid})
				if err != nil {
					return nil, err
				}
				return &command.Result{
					Data:    map[string]any{"ProviderId": pid, "Deleted": true},
					Effects: []output.Effect{{Kind: "delete", Resource: "credential-provider", Id: pid}},
					Text:    func(w io.Writer) { fmt.Fprintf(w, "Provider deleted: %s\n", pid) },
				}, nil
			})}, nil
		},
	}
}
