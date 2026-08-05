package acquire

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential"
)

// Cloud API Action: AcquireOAuth2AccessToken
func Module() command.Module {
	spec := command.Spec{
		ID:    "credential.oauth2.acquire",
		Path:  []string{"credential", "oauth2", "acquire"},
		Use:   "acquire",
		Short: "Acquire OAuth2 access token or initiate authorization",
		Long: `Acquire an OAuth2 AccessToken for a third-party service.

If the user already has an active authorization, the token is returned directly.
Otherwise an AuthorizationUrl and SessionUri are returned to start the 3LO flow.
Use "agr credential oauth2 acquire --session-uri <uri>" to poll the session.`,
		Examples: []string{
			"agr credential oauth2 acquire --token eyJ... --credential-provider-id agc-xxx --flow AUTHORIZATION_CODE --scopes read:user --return-url https://example.com/cb",
			"agr credential oauth2 acquire --token eyJ... --credential-provider-id agc-xxx --flow AUTHORIZATION_CODE --session-uri urn:ietf:... -o json",
		},
		Flags: []command.FlagSpec{
			{Name: "token", Usage: "WorkloadIdentityToken (required)", Type: command.FlagString, Required: true},
			{Name: "credential-provider-id", Usage: "OAuth2 CredentialProvider ID (required)", Type: command.FlagString, Required: true},
			{Name: "flow", Usage: "OAuth2 flow: AUTHORIZATION_CODE (required)", Type: command.FlagString, Required: true, Values: []string{"AUTHORIZATION_CODE"}},
			{Name: "scopes", Usage: "OAuth2 scopes (repeatable)", Type: command.FlagStringArray},
			{Name: "return-url", Usage: "OAuth2 return URL (must be in AllowedOAuth2ReturnUrls)", Type: command.FlagString},
			{Name: "custom-state", Usage: "Custom state for return URL callback", Type: command.FlagString},
			{Name: "force-authentication", Usage: "Force re-authentication", Type: command.FlagBool},
			{Name: "session-uri", Usage: "Existing session URI to poll status", Type: command.FlagString},
		},
		SupportsJSON: true,
		Output:       command.OutputSpec{DataType: "AcquireOAuth2Result"},
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
					"WorkloadIdentityToken": req.Flags["token"].String,
					"CredentialProviderId":  req.Flags["credential-provider-id"].String,
					"OAuth2Flow":            req.Flags["flow"].String,
				}
				if scopes := req.Flags["scopes"]; scopes.Changed {
					params["Scopes"] = scopes.Strings
				}
				if url := req.Flags["return-url"]; url.Changed {
					params["OAuth2ReturnUrl"] = url.String
				}
				if state := req.Flags["custom-state"]; state.Changed {
					params["CustomState"] = state.String
				}
				if req.Flags["force-authentication"].Bool {
					params["ForceAuthentication"] = true
				}
				if uri := req.Flags["session-uri"]; uri.Changed {
					params["SessionUri"] = uri.String
				}

				resp, err := cp.Call(ctx, "AcquireOAuth2AccessToken", params)
				if err != nil {
					return nil, err
				}
				data, _ := resp.(map[string]any)
				if data == nil {
					data = map[string]any{"raw": resp}
				}
				return &command.Result{Data: data, Text: func(w io.Writer) {
					status, _ := data["SessionStatus"].(string)
					if status != "" {
						fmt.Fprintf(w, "Session Status: %s\n", status)
					}
					if authURL, _ := data["AuthorizationUrl"].(string); authURL != "" {
						fmt.Fprintf(w, "Authorization URL:\n  %s\n", authURL)
					}
					if sessionURI, _ := data["SessionUri"].(string); sessionURI != "" {
						fmt.Fprintf(w, "Session URI: %s\n", sessionURI)
						fmt.Fprintln(w, "Poll with: agr credential oauth2 acquire --session-uri <uri> ...")
					}
					if token, _ := data["AccessToken"].(string); token != "" {
						if len(token) > 12 {
							fmt.Fprintf(w, "AccessToken: %s...%s\n", token[:6], token[len(token)-4:])
						} else {
							fmt.Fprintf(w, "AccessToken: ****\n")
						}
						fmt.Fprintln(w, "Use -o json for full token.")
					}
					if exp, _ := data["ExpiresAt"].(string); exp != "" {
						fmt.Fprintf(w, "Expires: %s\n", exp)
					}
				}}, nil
			})}, nil
		},
	}
}
