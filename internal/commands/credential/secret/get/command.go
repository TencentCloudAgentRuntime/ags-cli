package get

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential"
)

// Cloud API Action: GetManagedSecret
// Note: Requires WorkloadIdentityToken (not direct user credentials).
func Module() command.Module {
	spec := command.Spec{
		ID:    "credential.secret.get",
		Path:  []string{"credential", "secret", "get"},
		Use:   "get",
		Short: "Get a managed secret (requires WorkloadIdentityToken)",
		Long: `Retrieve a managed secret from a SecretMultiUser provider.

This action requires a WorkloadIdentityToken which encodes the workload identity
and user context. The token determines which user's secret is returned.`,
		Examples: []string{
			"agr credential secret get --credential-provider-id agc-xxx --token eyJ...",
			"agr credential secret get --credential-provider-id agc-xxx --token eyJ... --scope read:user -o json",
		},
		Flags: []command.FlagSpec{
			{Name: "credential-provider-id", Usage: "SecretMultiUser provider ID (required)", Type: command.FlagString, Required: true},
			{Name: "token", Usage: "WorkloadIdentityToken (required)", Type: command.FlagString, Required: true},
			{Name: "scope", Usage: "Secret scope filter", Type: command.FlagString},
		},
		SupportsJSON: true,
		Output:       command.OutputSpec{DataType: "ManagedSecret"},
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
					"CredentialProviderId":  req.Flags["credential-provider-id"].String,
					"WorkloadIdentityToken": req.Flags["token"].String,
				}
				if scope := req.Flags["scope"]; scope.Changed {
					params["Scope"] = scope.String
				}
				resp, err := cp.Call(ctx, "GetManagedSecret", params)
				if err != nil {
					return nil, err
				}
				data, _ := resp.(map[string]any)
				if data == nil {
					data = map[string]any{"raw": resp}
				}
				return &command.Result{Data: data, Text: func(w io.Writer) {
					// Mask secret in text mode for security.
					if secret, ok := data["Secret"].(string); ok && secret != "" {
						if len(secret) > 8 {
							fmt.Fprintf(w, "Secret: %s****%s\n", secret[:2], secret[len(secret)-2:])
						} else {
							fmt.Fprintf(w, "Secret: ****\n")
						}
						fmt.Fprintln(w, "Use -o json to retrieve the full secret value.")
					} else {
						// Distinguish a known-empty secret from a silent failure so the
						// user never sees a blank result for a get command.
						fmt.Fprintln(w, "Secret: (empty or not found)")
						fmt.Fprintln(w, "Use -o json to inspect the full response.")
					}
					if meta, ok := data["Metadata"].([]any); ok && len(meta) > 0 {
						fmt.Fprintf(w, "Metadata: %v\n", meta)
					}
				}}, nil
			})}, nil
		},
	}
}
