package list

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential"
)

// Cloud API Action: DescribeManagedSecretList
func Module() command.Module {
	spec := command.Spec{
		ID:    "credential.secret.list",
		Path:  []string{"credential", "secret", "list"},
		Use:   "list",
		Short: "List managed secrets for a provider",
		Examples: []string{
			"agr credential secret list --credential-provider-id agc-xxx",
			"agr credential secret list --credential-provider-id agc-xxx --user-ids user-1,user-2",
			"agr credential secret list --credential-provider-id agc-xxx --filter scope=read:user",
		},
		Flags: []command.FlagSpec{
			{Name: "credential-provider-id", Usage: "SecretMultiUser provider ID (required)", Type: command.FlagString, Required: true},
			{Name: "user-ids", Usage: "Filter by user IDs (comma-separated)", Type: command.FlagString},
			{Name: "filter", Usage: "Filter (repeatable). Supported: scope", Type: command.FlagStringArray},
			{Name: "limit", Usage: "Max results [0-100] (default 20)", Type: command.FlagInt, Default: 20},
			{Name: "offset", Usage: "Pagination offset", Type: command.FlagInt, Default: 0},
		},
		SupportsJSON: true,
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
					"Limit":                req.Flags["limit"].Int,
					"Offset":               req.Flags["offset"].Int,
				}
				if ids := req.Flags["user-ids"]; ids.Changed {
					params["UserIds"] = splitComma(ids.String)
				}
				if filters := req.Flags["filter"]; filters.Changed {
					params["Filters"] = parseFilters(filters.Strings)
				}
				resp, err := cp.Call(ctx, "DescribeManagedSecretList", params)
				if err != nil {
					return nil, err
				}
				data, _ := resp.(map[string]any)
				if data == nil {
					data = map[string]any{"raw": resp}
				}
				return &command.Result{Data: data, Text: func(w io.Writer) {
					items, _ := data["ManagedSecretSet"].([]any)
					if len(items) == 0 {
						fmt.Fprintln(w, "No secrets found.")
						return
					}
					fmt.Fprintf(w, "%-20s  %-14s  %-12s  %s\n", "USER_ID", "MASKED_SECRET", "SCOPE", "CREATED")
					for _, item := range items {
						m, _ := item.(map[string]any)
						fmt.Fprintf(w, "%-20v  %-14v  %-12v  %v\n", m["UserId"], m["MaskedSecret"], m["Scope"], m["CreatedAt"])
					}
					if total := data["TotalCount"]; total != nil {
						fmt.Fprintf(w, "\nTotal: %v\n", total)
					}
				}}, nil
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
