package create

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli/request"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module returns the "credential provider create" command module.
// Cloud API Action: CreateCredentialProvider
// Fields aligned with api schema: Name, Type, Config, Description, Tags.
func Module() command.Module {
	spec := command.Spec{
		ID:    "credential.provider.create",
		Path:  []string{"credential", "provider", "create"},
		Use:   "create",
		Short: "Create a credential provider",
		Long: `Create a CredentialProvider configuration for managing third-party credentials.

Supported types: AKSK, SecretMultiUser, OAuth2.

Config is a JSON array of {Key, Value} pairs. Required config keys depend on type:
  - AKSK: secret_id, secret_key
  - OAuth2: client_id, client_secret, token_endpoint, authorization_endpoint
  - SecretMultiUser: (none required)`,
		Examples: []string{
			"agr credential provider create --name my-oauth --type OAuth2 --provider-config '[{\"Key\":\"client_id\",\"Value\":\"xxx\"}]'",
			"agr credential provider create --name static-cred --type SecretMultiUser",
			"agr credential provider create --name my-aksk --type AKSK --provider-config @config.json -o json",
		},
		Flags: []command.FlagSpec{
			{Name: "name", Shorthand: "n", Usage: "Provider name (required, max 128 chars)", Type: command.FlagString, Required: true},
			{Name: "type", Shorthand: "t", Usage: "Provider type (required): AKSK, SecretMultiUser, OAuth2", Type: command.FlagString, Required: true, Values: []string{"AKSK", "SecretMultiUser", "OAuth2"}},
			{Name: "description", Usage: "Description (max 256 chars)", Type: command.FlagString},
			{Name: "provider-config", Usage: "Provider config as JSON array of {Key,Value} or @file", Type: command.FlagString, Format: "json", Examples: []string{`[{"Key":"client_id","Value":"xxx"},{"Key":"client_secret","Value":"yyy"}]`}},
			{Name: "tags", Usage: "Tags in key=value format (repeatable)", Type: command.FlagStringArray},
		},
		SupportsJSON: true,
		Output: command.OutputSpec{
			DataType: "CreateCredentialProviderResult",
			Effects:  []string{"create:credential-provider"},
		},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec:   spec,
			Groups: credentialProviderGroups(),
			Source: "workflow",
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp := deps.ControlPlane.(credential.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				params := map[string]any{
					"Name": req.Flags["name"].String,
					"Type": req.Flags["type"].String,
				}
				if desc := req.Flags["description"]; desc.Changed {
					params["Description"] = desc.String
				}
				if cfg := req.Flags["provider-config"]; cfg.Changed {
					var config []map[string]string
					if err := request.ParseJSONFlagValue("provider-config", cfg.String, &config); err != nil {
						return nil, err
					}
					params["Config"] = config
				}
				if tags := req.Flags["tags"]; tags.Changed {
					params["Tags"] = parseTags(tags.Strings)
				}

				resp, err := cp.Call(ctx, "CreateCredentialProvider", params)
				if err != nil {
					return nil, err
				}

				data := normalizeResponse(resp)
				providerID := strVal(data, "ProviderId")

				return &command.Result{
					Data:    data,
					Effects: []output.Effect{{Kind: "create", Resource: "credential-provider", Id: providerID}},
					Text: func(w io.Writer) {
						fmt.Fprintf(w, "Provider created: %s\n", providerID)
						fmt.Fprintf(w, "Name: %s\n", req.Flags["name"].String)
						fmt.Fprintf(w, "Type: %s\n", req.Flags["type"].String)
					},
				}, nil
			})}, nil
		},
	}
}

func credentialProviderGroups() []command.GroupSpec {
	return []command.GroupSpec{
		{Path: []string{"credential"}, Use: "credential", Short: "Manage credentials and providers"},
		{Path: []string{"credential", "provider"}, Use: "provider", Short: "Manage credential providers"},
	}
}

func parseTags(tags []string) []map[string]string {
	var result []map[string]string
	for _, t := range tags {
		for i, c := range t {
			if c == '=' {
				result = append(result, map[string]string{"Key": t[:i], "Value": t[i+1:]})
				break
			}
		}
	}
	return result
}

func normalizeResponse(resp any) map[string]any {
	if m, ok := resp.(map[string]any); ok {
		return m
	}
	return map[string]any{"raw": resp}
}

func strVal(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}
