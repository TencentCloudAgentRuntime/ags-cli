package get

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module returns the "credential provider get" command module.
// Cloud API Action: DescribeCredentialProviderList (with ProviderIds filter for single item)
// Note: There's no dedicated "Get" action; we use List with a single ID.
func Module() command.Module {
	spec := command.Spec{
		ID:           "credential.provider.get",
		Path:         []string{"credential", "provider", "get"},
		Use:          "get <provider-id>",
		Short:        "Get credential provider details",
		Examples:     []string{"agr credential provider get agc-xxxx", "agr credential provider get agc-xxxx -o json"},
		Args:         []command.ArgSpec{{Name: "provider-id", Description: "Provider ID", Required: true}},
		SupportsJSON: true,
		Output:       command.OutputSpec{DataType: "CredentialProvider"},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec:   spec,
			Groups: []command.GroupSpec{{Path: []string{"credential"}, Use: "credential", Short: "Manage credentials and providers"}, {Path: []string{"credential", "provider"}, Use: "provider", Short: "Manage credential providers"}},
			Source: "workflow",
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp := deps.ControlPlane.(credential.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				providerID := req.Args[0]
				// Use DescribeCredentialProviderList with single ID to get details.
				resp, err := cp.Call(ctx, "DescribeCredentialProviderList", map[string]any{
					"ProviderIds": []string{providerID},
					"Limit":       1,
				})
				if err != nil {
					return nil, err
				}
				data, _ := resp.(map[string]any)
				if data == nil {
					data = map[string]any{"raw": resp}
				}

				// Extract single provider from ProviderSet.
				var provider map[string]any
				if set, ok := data["ProviderSet"].([]any); ok && len(set) > 0 {
					provider, _ = set[0].(map[string]any)
				}
				if provider == nil {
					return nil, output.NewNotFoundError("PROVIDER_NOT_FOUND",
						fmt.Sprintf("provider %s not found", providerID),
						"Verify the provider ID with: agr credential provider list")
				}

				return &command.Result{
					Data: provider,
					Text: func(w io.Writer) {
						fmt.Fprintf(w, "Provider:    %s\n", sv(provider, "ProviderId"))
						fmt.Fprintf(w, "Name:        %s\n", sv(provider, "Name"))
						fmt.Fprintf(w, "Type:        %s\n", sv(provider, "Type"))
						fmt.Fprintf(w, "Status:      %s\n", sv(provider, "Status"))
						if desc := sv(provider, "Description"); desc != "" {
							fmt.Fprintf(w, "Description: %s\n", desc)
						}
						fmt.Fprintf(w, "Created:     %s\n", sv(provider, "CreateTime"))
						fmt.Fprintf(w, "Updated:     %s\n", sv(provider, "UpdateTime"))
					},
				}, nil
			})}, nil
		},
	}
}

func sv(m map[string]any, k string) string {
	if v, ok := m[k]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}
