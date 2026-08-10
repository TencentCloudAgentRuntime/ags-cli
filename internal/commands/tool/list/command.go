package list

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// Module returns this package's command module.
func Module() command.Module {
	api := APIDescriptor()
	spec := api.CommandSpec()
	spec.Output = command.OutputSpec{
		DataType:    "ToolListData",
		Description: "Tool list with normalized items and pagination metadata.",
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Generated: &command.Descriptor{
				Spec:   api.CommandSpec(),
				Groups: api.Groups,
				API:    api,
				Source: command.SourceAPICli,
			},
			Groups: api.Groups,
			API:    api,
			Source: command.SourceMixedAPI,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			builder := apicli.NewRequestBuilder(api)
			executor := apicli.NewExecutor(api, deps.ControlPlane)
			return command.Runtime{
				Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
					if !requestFlag(req) {
						if err := validateRequest(req); err != nil {
							return nil, err
						}
					}
					apiReq, err := builder.Build(req)
					if err != nil {
						return nil, err
					}
					result, err := executor.Execute(ctx, apiReq)
					if err != nil {
						return nil, err
					}
					response, ok := result.Data.(*ags.DescribeSandboxToolListResponseParams)
					if !ok {
						return result, nil
					}
					offset := intFlag(req, "offset")
					limit := effectiveLimit(req)
					return toolListResult(response, offset, limit, result), nil
				}),
			}, nil
		},
	}
}

func validateRequest(req command.Request) error {
	offset := intFlag(req, "offset")
	if offset < 0 {
		return output.NewUsageError("INVALID_PAGINATION", fmt.Sprintf("--offset must be >= 0 (got %d)", offset), "Use a non-negative pagination offset.")
	}
	if limitChanged(req) {
		limit := intFlag(req, "limit")
		if limit < 0 {
			return output.NewUsageError("INVALID_PAGINATION", fmt.Sprintf("--limit must be >= 0 (got %d)", limit), "Use a non-negative pagination limit.")
		}
	}
	return nil
}

func toolListResult(response *ags.DescribeSandboxToolListResponseParams, offset int, limit *int, base *command.Result) *command.Result {
	items := make([]map[string]any, len(response.SandboxToolSet))
	for i, tool := range response.SandboxToolSet {
		items[i] = canonicalToolData(tool)
	}
	pagination := map[string]any{
		"Offset":     offset,
		"Total":      derefInt64(response.TotalCount),
		"NextCursor": nil,
	}
	if limit != nil {
		pagination["Limit"] = *limit
	}
	data := map[string]any{
		"Items":      items,
		"Pagination": pagination,
	}
	return &command.Result{
		Data:      data,
		Warnings:  base.Warnings,
		Effects:   base.Effects,
		ExitCode:  base.ExitCode,
		Failure:   base.Failure,
		MetaExtra: base.MetaExtra,
		Text: func(w io.Writer) {
			renderToolList(w, response)
		},
	}
}

func renderToolList(w io.Writer, response *ags.DescribeSandboxToolListResponseParams) {
	if len(response.SandboxToolSet) == 0 {
		fmt.Fprintln(w, "No tools found")
		return
	}
	headers := []string{"ID", "NAME", "TYPE", "STATUS", "NETWORK", "DESCRIPTION", "TAGS", "CREATED"}
	rows := make([][]string, len(response.SandboxToolSet))
	for i, tool := range response.SandboxToolSet {
		networkMode := "-"
		if tool.NetworkConfiguration != nil && tool.NetworkConfiguration.NetworkMode != nil && *tool.NetworkConfiguration.NetworkMode != "" {
			networkMode = *tool.NetworkConfiguration.NetworkMode
		}
		status := derefString(tool.Status)
		if status == "" {
			status = "-"
		}
		rows[i] = []string{
			derefString(tool.ToolId),
			derefString(tool.ToolName),
			derefString(tool.ToolType),
			status,
			networkMode,
			output.TruncateString(derefString(tool.Description), 40),
			strings.Join(sortedTagStrings(tool.Tags), ","),
			formatShortTime(derefString(tool.CreateTime)),
		}
	}
	printTableWithPagination(w, headers, rows, len(rows), derefInt64(response.TotalCount))
}

func canonicalToolData(t *ags.SandboxTool) map[string]any {
	return map[string]any{
		"ToolId":                derefString(t.ToolId),
		"ToolName":              derefString(t.ToolName),
		"ToolType":              derefString(t.ToolType),
		"Status":                derefString(t.Status),
		"StatusReason":          derefString(t.StatusReason),
		"Persistent":            t.Persistent,
		"DefaultTimeoutSeconds": t.DefaultTimeoutSeconds,
		"NetworkConfiguration":  t.NetworkConfiguration,
		"Description":           derefString(t.Description),
		"Tags":                  sdkTagsToMap(t.Tags),
		"CreateTime":            derefString(t.CreateTime),
		"UpdateTime":            derefString(t.UpdateTime),
		"RoleArn":               derefString(t.RoleArn),
		"StorageMounts":         t.StorageMounts,
		"CustomConfiguration":   t.CustomConfiguration,
		"LogConfiguration":      t.LogConfiguration,
	}
}

func printTableWithPagination(w io.Writer, headers []string, rows [][]string, shown, total int) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
	if total > shown {
		fmt.Fprintf(w, "\nShowing %d of %d items (use --offset and --limit for pagination)\n", shown, total)
	}
}

func sdkTagsToMap(tags []*ags.Tag) map[string]string {
	result := make(map[string]string, len(tags))
	for _, tag := range tags {
		if tag != nil && tag.Key != nil && tag.Value != nil {
			result[*tag.Key] = *tag.Value
		}
	}
	return result
}

func sortedTagStrings(tags []*ags.Tag) []string {
	tagMap := sdkTagsToMap(tags)
	keys := make([]string, 0, len(tagMap))
	for key := range tagMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, fmt.Sprintf("%s=%s", key, tagMap[key]))
	}
	return out
}

func intFlag(req command.Request, name string) int {
	flag, ok := req.Flags[name]
	if !ok {
		return 0
	}
	return flag.Int
}

func effectiveLimit(req command.Request) *int {
	if !limitChanged(req) {
		return nil
	}
	limit := intFlag(req, "limit")
	return &limit
}

func limitChanged(req command.Request) bool {
	flag, ok := req.Flags["limit"]
	return ok && flag.Changed
}

func requestFlag(req command.Request) bool {
	flag, ok := req.Flags["request"]
	return ok && flag.Changed && strings.TrimSpace(flag.String) != ""
}

func formatShortTime(isoTime string) string {
	if isoTime == "" {
		return "-"
	}
	if parsed, err := time.Parse(time.RFC3339, isoTime); err == nil {
		return parsed.Format("01-02 15:04")
	}
	if len(isoTime) >= 16 {
		return isoTime[5:16]
	}
	return isoTime
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt64(i *int64) int {
	if i == nil {
		return 0
	}
	return int(*i)
}
