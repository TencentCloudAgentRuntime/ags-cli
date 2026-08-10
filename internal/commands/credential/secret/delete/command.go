package delete

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Cloud API Action: DeleteManagedSecret
func Module() command.Module {
	spec := command.Spec{
		ID:    "credential.secret.delete",
		Path:  []string{"credential", "secret", "delete"},
		Use:   "delete",
		Short: "Delete a managed secret",
		Long: `Delete managed secrets from a SecretMultiUser provider.

If --scope is provided, only the secret with that scope is deleted.
If omitted, all secrets for the user are deleted.`,
		Examples: []string{
			"agr credential secret delete --credential-provider-id agc-xxx --user-id user-1",
			"agr credential secret delete --credential-provider-id agc-xxx --user-id user-1 --scope read:user",
		},
		Flags: []command.FlagSpec{
			{Name: "credential-provider-id", Usage: "SecretMultiUser provider ID (required)", Type: command.FlagString, Required: true},
			{Name: "user-id", Usage: "User ID (required)", Type: command.FlagString, Required: true},
			{Name: "scope", Usage: "Scope to delete (omit to delete all scopes)", Type: command.FlagString},
		},
		SupportsJSON: true,
		Output:       command.OutputSpec{Effects: []string{"delete:secret"}},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Groups: []command.GroupSpec{
				{Path: []string{"credential"}, Use: "credential", Short: "Manage credentials and providers"},
				{Path: []string{"credential", "secret"}, Use: "secret", Short: "Manage per-user managed secrets"},
			},
			Source: command.SourceWorkflow,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp := deps.ControlPlane.(credential.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				params := map[string]any{
					"CredentialProviderId": req.Flags["credential-provider-id"].String,
					"UserId":               req.Flags["user-id"].String,
				}
				if scope := req.Flags["scope"]; scope.Changed {
					params["Scope"] = scope.String
				}
				_, err := cp.Call(ctx, "DeleteManagedSecret", params)
				if err != nil {
					return nil, err
				}
				return &command.Result{
					Data:    map[string]any{"CredentialProviderId": params["CredentialProviderId"], "UserId": params["UserId"], "Deleted": true},
					Effects: []output.Effect{{Kind: "delete", Resource: "secret"}},
					Text:    func(w io.Writer) { fmt.Fprintf(w, "Secret deleted for user %s\n", params["UserId"]) },
				}, nil
			})}, nil
		},
	}
}
