package create

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/identity"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module returns the "identity create" command module.
// Cloud API Action: CreateWorkloadIdentity
func Module() command.Module {
	spec := command.Spec{
		ID:    "identity.create",
		Path:  []string{"identity", "create"},
		Use:   "create",
		Short: "Create a workload identity",
		Long: `Create a new Workload Identity for an Agent or workload.

A Workload Identity represents the identity of an Agent product and is used
to obtain WorkloadAccessTokens and associate with CredentialProviders.`,
		Examples: []string{
			"agr identity create --name my-agent",
			"agr identity create --name my-agent --allowed-oauth2-return-urls https://example.com/callback",
			"agr identity create --name my-agent --tags env=prod -o json",
		},
		Flags: []command.FlagSpec{
			{Name: "name", Shorthand: "n", Usage: "Identity name (max 128 chars)", Type: command.FlagString},
			{Name: "allowed-oauth2-return-urls", Usage: "Allowed OAuth2 return URLs (repeatable)", Type: command.FlagStringArray},
			{Name: "tags", Usage: "Tags in key=value format (repeatable)", Type: command.FlagStringArray},
		},
		SupportsJSON: true,
		Output: command.OutputSpec{
			DataType: "CreateWorkloadIdentityResult",
			Effects:  []string{"create:identity"},
		},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec:   spec,
			Groups: identityGroups(),
			Source: command.SourceWorkflow,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp := deps.ControlPlane.(identity.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				params := map[string]any{}
				if name := req.Flags["name"]; name.Changed {
					params["Name"] = name.String
				}
				if urls := req.Flags["allowed-oauth2-return-urls"]; urls.Changed {
					params["AllowedOAuth2ReturnUrls"] = urls.Strings
				}
				if tags := req.Flags["tags"]; tags.Changed {
					params["Tags"] = parseTags(tags.Strings)
				}

				resp, err := cp.Call(ctx, "CreateWorkloadIdentity", params)
				if err != nil {
					return nil, err
				}

				data := normalizeResponse(resp)
				wid := strVal(data, "WorkloadIdentityId")

				return &command.Result{
					Data:    data,
					Effects: []output.Effect{{Kind: "create", Resource: "identity", Id: wid}},
					Text: func(w io.Writer) {
						fmt.Fprintf(w, "Identity created: %s\n", wid)
					},
				}, nil
			})}, nil
		},
	}
}

func identityGroups() []command.GroupSpec {
	return []command.GroupSpec{
		{Path: []string{"identity"}, Use: "identity", Short: "Manage workload identities"},
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
