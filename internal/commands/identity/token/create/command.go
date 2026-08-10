package create

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/identity"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Cloud API Action: CreateWorkloadAccessTokenForUserId
func Module() command.Module {
	spec := command.Spec{
		ID:    "identity.token.create",
		Path:  []string{"identity", "token", "create"},
		Use:   "create",
		Short: "Create a workload access token for a user",
		Long: `Issue a WorkloadAccessToken binding an Agent workload identity with a user ID.

The token is displayed once and cannot be retrieved again. Use -o json to
get the full token value.`,
		Examples: []string{
			"agr identity token create --identity-id wi-xxxx --user-id user-123",
			"agr identity token create --identity-id wi-xxxx --user-id user-123 -o json",
		},
		Flags: []command.FlagSpec{
			{Name: "identity-id", Usage: "Workload Identity ID (required)", Type: command.FlagString, Required: true},
			{Name: "user-id", Usage: "User ID to bind (required)", Type: command.FlagString, Required: true},
		},
		SupportsJSON: true,
		Output: command.OutputSpec{
			DataType: "WorkloadAccessToken",
			Effects:  []string{"create:token"},
		},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Groups: []command.GroupSpec{
				{Path: []string{"identity"}, Use: "identity", Short: "Manage workload identities"},
				{Path: []string{"identity", "token"}, Use: "token", Short: "Manage workload access tokens"},
			},
			Source: command.SourceWorkflow,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp := deps.ControlPlane.(identity.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				params := map[string]any{
					"WorkloadIdentityId": req.Flags["identity-id"].String,
					"UserId":             req.Flags["user-id"].String,
				}

				resp, err := cp.Call(ctx, "CreateWorkloadAccessTokenForUserId", params)
				if err != nil {
					return nil, err
				}

				data, _ := resp.(map[string]any)
				if data == nil {
					data = map[string]any{"raw": resp}
				}

				return &command.Result{
					Data:    data,
					Effects: []output.Effect{{Kind: "create", Resource: "token"}},
					Text: func(w io.Writer) {
						fmt.Fprintln(w, "WorkloadAccessToken created.")
						fmt.Fprintln(w, "⚠️  The token is shown only once. Store it securely.")
						// Mask token in text mode for security.
						if token, ok := data["WorkloadAccessToken"].(string); ok && token != "" {
							if len(token) > 12 {
								fmt.Fprintf(w, "Token: %s...%s\n", token[:6], token[len(token)-6:])
							} else {
								fmt.Fprintf(w, "Token: ****\n")
							}
						}
						fmt.Fprintln(w, "Use -o json to retrieve the full token value.")
					},
				}, nil
			})}, nil
		},
	}
}
