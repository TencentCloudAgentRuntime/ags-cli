package complete

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential"
)

// Cloud API Action: CompleteOAuth2AccessTokenAuth
func Module() command.Module {
	spec := command.Spec{
		ID:    "credential.oauth2.complete",
		Path:  []string{"credential", "oauth2", "complete"},
		Use:   "complete",
		Short: "Complete an OAuth2 authorization session",
		Long: `Confirm completion of an OAuth2 authorization session.

Called after the user has authorized via the browser and the return URL callback
has been received. This finalizes the session so AccessToken can be obtained.`,
		Examples: []string{
			"agr credential oauth2 complete --session-uri 'urn:ietf:params:oauth:request_uri:xxx' --user-id user-1",
		},
		Flags: []command.FlagSpec{
			{Name: "session-uri", Usage: "OAuth2 session URI (required)", Type: command.FlagString, Required: true},
			{Name: "user-id", Usage: "Current logged-in user ID", Type: command.FlagString},
		},
		SupportsJSON: true,
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Groups: []command.GroupSpec{
				{Path: []string{"credential"}, Use: "credential", Short: "Manage credentials and providers"},
				{Path: []string{"credential", "oauth2"}, Use: "oauth2", Short: "OAuth2 authorization workflows"},
			},
			Source: "workflow",
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp := deps.ControlPlane.(credential.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				params := map[string]any{
					"SessionUri": req.Flags["session-uri"].String,
				}
				if uid := req.Flags["user-id"]; uid.Changed {
					params["UserId"] = uid.String
				}
				_, err := cp.Call(ctx, "CompleteOAuth2AccessTokenAuth", params)
				if err != nil {
					return nil, err
				}
				return &command.Result{
					Data: map[string]any{"SessionUri": params["SessionUri"], "Completed": true},
					Text: func(w io.Writer) {
						fmt.Fprintln(w, "OAuth2 session completed.")
						fmt.Fprintln(w, "You can now acquire the AccessToken with 'agr credential oauth2 acquire --session-uri ...'")
					},
				}, nil
			})}, nil
		},
	}
}
