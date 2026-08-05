package list

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/identity"
)

// Module returns the "identity list" command module.
// Cloud API Action: DescribeWorkloadIdentityList
func Module() command.Module {
	spec := command.Spec{
		ID:    "identity.list",
		Path:  []string{"identity", "list"},
		Use:   "list",
		Short: "List workload identities",
		Examples: []string{
			"agr identity list",
			"agr identity list --identity-ids wi-xxx,wi-yyy --limit 10",
			"agr identity list --filter tag-key=env -o json",
		},
		Flags: []command.FlagSpec{
			{Name: "identity-ids", Usage: "Filter by identity IDs (comma-separated)", Type: command.FlagString},
			{Name: "filter", Usage: "Filter in name=value format (repeatable). Supported: tag-key, tag-value, tag:<key>", Type: command.FlagStringArray},
			{Name: "limit", Usage: "Maximum results [0-100] (default 20)", Type: command.FlagInt, Default: 20},
			{Name: "offset", Usage: "Pagination offset (default 0)", Type: command.FlagInt, Default: 0},
		},
		SupportsJSON: true,
		Output:       command.OutputSpec{DataType: "WorkloadIdentityList"},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec:   spec,
			Groups: []command.GroupSpec{{Path: []string{"identity"}, Use: "identity", Short: "Manage workload identities"}},
			Source: "workflow",
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp := deps.ControlPlane.(identity.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				params := map[string]any{
					"Limit":  req.Flags["limit"].Int,
					"Offset": req.Flags["offset"].Int,
				}
				if ids := req.Flags["identity-ids"]; ids.Changed {
					params["WorkloadIdentityIds"] = splitComma(ids.String)
				}
				if filters := req.Flags["filter"]; filters.Changed {
					params["Filters"] = parseFilters(filters.Strings)
				}

				resp, err := cp.Call(ctx, "DescribeWorkloadIdentityList", params)
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
						items, _ := data["WorkloadIdentitySet"].([]any)
						if len(items) == 0 {
							fmt.Fprintln(w, "No identities found.")
							return
						}
						fmt.Fprintf(w, "%-14s  %s\n", "ID", "NAME")
						for _, item := range items {
							m, _ := item.(map[string]any)
							fmt.Fprintf(w, "%-14s  %s\n", sv(m, "WorkloadIdentityId"), sv(m, "Name"))
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
				result = append(result, map[string]any{"Name": f[:i], "Values": []string{f[i+1:]}})
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
