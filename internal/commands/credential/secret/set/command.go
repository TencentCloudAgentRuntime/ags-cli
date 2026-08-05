package set

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli/request"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Cloud API Action: SetManagedSecret
func Module() command.Module {
	spec := command.Spec{
		ID:    "credential.secret.set",
		Path:  []string{"credential", "secret", "set"},
		Use:   "set",
		Short: "Store a managed secret",
		Long: `Store or update a per-user managed secret under a SecretMultiUser credential provider.

The secret value is accepted via --secret or --from-stdin (piped input).`,
		Examples: []string{
			"agr credential secret set --credential-provider-id agc-xxx --user-id user-1 --secret 'mysecret'",
			"agr credential secret set --credential-provider-id agc-xxx --user-id user-1 --secret 'new' --scope read:user --overwrite-allowed",
			"echo 'secret' | agr credential secret set --credential-provider-id agc-xxx --user-id user-1 --from-stdin",
		},
		Flags: []command.FlagSpec{
			{Name: "credential-provider-id", Usage: "SecretMultiUser provider ID (required)", Type: command.FlagString, Required: true},
			{Name: "user-id", Usage: "User ID (required, max 128 bytes)", Type: command.FlagString, Required: true},
			{Name: "secret", Usage: "Secret value (max 65536 bytes, mutually exclusive with --from-stdin)", Type: command.FlagString},
			{Name: "from-stdin", Usage: "Read secret from stdin", Type: command.FlagBool},
			{Name: "scope", Usage: "Secret scope (max 128 bytes)", Type: command.FlagString},
			{Name: "overwrite-allowed", Usage: "Allow overwriting existing secret with same scope", Type: command.FlagBool},
			{Name: "metadata", Usage: "Metadata as JSON array of {Name,Value} or @file (max 20 items)", Type: command.FlagString, Format: "json"},
		},
		SupportsJSON: true,
		Output:       command.OutputSpec{Effects: []string{"create:secret"}},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Groups: []command.GroupSpec{
				{Path: []string{"credential"}, Use: "credential", Short: "Manage credentials and providers"},
				{Path: []string{"credential", "secret"}, Use: "secret", Short: "Manage per-user managed secrets"},
			},
			Source: "workflow",
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp := deps.ControlPlane.(credential.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				params := map[string]any{
					"CredentialProviderId": req.Flags["credential-provider-id"].String,
					"UserId":               req.Flags["user-id"].String,
				}
				if s := req.Flags["secret"]; s.Changed {
					if req.Flags["from-stdin"].Bool {
						return nil, output.NewUsageError("CONFLICTING_INPUTS", "--secret and --from-stdin are mutually exclusive", "Provide the secret via --secret OR --from-stdin, not both.")
					}
					params["Secret"] = s.String
				} else if req.Flags["from-stdin"].Bool {
					// LimitReader+ReadAll: reads all of stdin, detects overflow beyond
					// the 65536-byte cap, and surfaces read errors instead of silently
					// truncating. A single Read() on a pipe can return a partial buffer,
					// so ReadAll is required for completeness.
					const maxSecretBytes = 65536
					buf, err := io.ReadAll(io.LimitReader(req.Stdin, maxSecretBytes+1))
					if err != nil {
						return nil, output.NewUsageError("INVALID_SECRET", fmt.Sprintf("failed to read secret from stdin: %v", err), "Check the piped input and retry.")
					}
					if len(buf) > maxSecretBytes {
						return nil, output.NewUsageError("INVALID_SECRET", fmt.Sprintf("secret exceeds the %d-byte limit", maxSecretBytes), "Provide a smaller secret value.")
					}
					params["Secret"] = string(buf)
				} else {
					return nil, output.NewUsageError("MISSING_SECRET", "must provide --secret or --from-stdin", "Provide the secret via --secret or pipe it with --from-stdin.")
				}
				if scope := req.Flags["scope"]; scope.Changed {
					params["Scope"] = scope.String
				}
				if req.Flags["overwrite-allowed"].Bool {
					params["OverwriteAllowed"] = true
				}
				if meta := req.Flags["metadata"]; meta.Changed {
					var metadata []map[string]string
					if err := request.ParseJSONFlagValue("metadata", meta.String, &metadata); err != nil {
						return nil, err
					}
					params["Metadata"] = metadata
				}

				_, err := cp.Call(ctx, "SetManagedSecret", params)
				if err != nil {
					return nil, err
				}
				return &command.Result{
					Data:    map[string]any{"CredentialProviderId": params["CredentialProviderId"], "UserId": params["UserId"], "Set": true},
					Effects: []output.Effect{{Kind: "create", Resource: "secret"}},
					Text: func(w io.Writer) {
						fmt.Fprintf(w, "Secret stored for provider %s, user %s\n", params["CredentialProviderId"], params["UserId"])
					},
				}, nil
			})}, nil
		},
	}
}
