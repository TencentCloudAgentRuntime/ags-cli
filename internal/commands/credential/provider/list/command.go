package list

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential"
)

// Module returns the "credential provider list" command module.
// Cloud API Action: DescribeCredentialProviderList
// Response: { ProviderSet: [...], TotalCount: N }
func Module() command.Module {
	spec := command.Spec{
		ID:    "credential.provider.list",
		Path:  []string{"credential", "provider", "list"},
		Use:   "list",
		Short: "List credential providers",
		Long: `List credential providers with optional filtering.

Supports filtering by provider IDs, type (via --filter type=AKSK), and tags.`,
		Examples: []string{
			"agr credential provider list",
			"agr credential provider list --limit 10 --provider-ids agc-xxx,agc-yyy",
			"agr credential provider list --filter type=OAuth2 -o json",
		},
		Flags: []command.FlagSpec{
			{Name: "provider-ids", Usage: "Filter by provider IDs (comma-separated)", Type: command.FlagString},
			{Name: "filter", Usage: "Filter in name=value format (repeatable). Supported: type, tag-key, tag-value, tag:<key>", Type: command.FlagStringArray},
			{Name: "limit", Usage: "Maximum results [0-100] (default 20)", Type: command.FlagInt, Default: 20},
			{Name: "offset", Usage: "Pagination offset (default 0)", Type: command.FlagInt, Default: 0},
		},
		SupportsJSON: true,
		Output:       command.OutputSpec{DataType: "CredentialProviderList"},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Groups: []command.GroupSpec{
				{Path: []string{"credential"}, Use: "credential", Short: "Manage credentials and providers"},
				{Path: []string{"credential", "provider"}, Use: "provider", Short: "Manage credential providers"},
			},
			Source: command.SourceWorkflow,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp := deps.ControlPlane.(credential.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				params := map[string]any{
					"Limit":  req.Flags["limit"].Int,
					"Offset": req.Flags["offset"].Int,
				}
				if ids := req.Flags["provider-ids"]; ids.Changed {
					params["ProviderIds"] = splitComma(ids.String)
				}
				if filters := req.Flags["filter"]; filters.Changed {
					params["Filters"] = parseFilters(filters.Strings)
				}

				resp, err := cp.Call(ctx, "DescribeCredentialProviderList", params)
				if err != nil {
					return nil, err
				}
				data, _ := resp.(map[string]any)
				if data == nil {
					data = map[string]any{"raw": resp}
				}
				return &command.Result{
					Data: data,
					Text: func(w io.Writer) {
						items, _ := data["ProviderSet"].([]any)
						if len(items) == 0 {
							fmt.Fprintln(w, "No providers found.")
							return
						}
						fmt.Fprintf(w, "%-14s  %-20s  %-16s  %-8s  %s\n", "ID", "NAME", "TYPE", "STATUS", "CREATED")
						for _, item := range items {
							m, _ := item.(map[string]any)
							fmt.Fprintf(w, "%-14s  %-20s  %-16s  %-8s  %s\n",
								sv(m, "ProviderId"), sv(m, "Name"), sv(m, "Type"), sv(m, "Status"), sv(m, "CreateTime"))
						}
						if total := data["TotalCount"]; total != nil {
							fmt.Fprintf(w, "\nTotal: %v\n", total)
						}
					},
				}, nil
			})}, nil
		},
	}
}

func splitComma(s string) []string {
	var result []string
	start := 0
	for i, c := range s {
		if c == ',' {
			if i > start {
				result = append(result, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

func parseFilters(filters []string) []map[string]any {
	var result []map[string]any
	for _, f := range filters {
		for i, c := range f {
			if c == '=' {
				result = append(result, map[string]any{
					"Name":   f[:i],
					"Values": []string{f[i+1:]},
				})
				break
			}
		}
	}
	return result
}

func sv(m map[string]any, k string) string {
	if v, ok := m[k]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}
