package list

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	instanceview "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/instance/internal/instanceview"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

const allPageLimit = 100

// Module returns this package's command module.
func Module() command.Module {
	api := APIDescriptor()
	spec := api.CommandSpec()
	spec.Output = command.OutputSpec{
		DataType:    "InstanceListData",
		Description: "Instance list with normalized items and pagination metadata.",
	}
	spec.Long = "List sandbox instances with optional filters.\n\nUse --all to fetch every page in the current configured region instead of a single paginated response."
	spec.Examples = append(spec.Examples, "agr instance list --all")
	spec.Flags = append(spec.Flags, command.FlagSpec{
		Name:     "all",
		Usage:    "Fetch all pages of instances in the current configured region",
		Type:     command.FlagBool,
		Workflow: true,
	})
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Generated: &command.Descriptor{
				Spec:   api.CommandSpec(),
				Groups: api.Groups,
				API:    api,
				Source: "apicli",
			},
			Groups: api.Groups,
			API:    api,
			Source: "mixed-api",
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
					if boolFlag(req, "all") {
						return listAllInstances(ctx, executor, apiReq)
					}
					result, err := executor.Execute(ctx, apiReq)
					if err != nil {
						return nil, err
					}
					response, ok := result.Data.(*ags.DescribeSandboxInstanceListResponseParams)
					if !ok {
						return result, nil
					}
					offset := intFlag(req, "offset")
					limit := effectiveLimit(req)
					return instanceListResult(response, offset, limit, result), nil
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
	if boolFlag(req, "all") && (flagChanged(req, "offset") || flagChanged(req, "limit")) {
		return output.NewUsageError("INVALID_PAGINATION", "--all cannot be combined with --offset or --limit", "Use either --all for the complete list, or --offset/--limit for one page.")
	}
	return nil
}

func listAllInstances(ctx context.Context, executor *apicli.Executor, baseRequest map[string]any) (*command.Result, error) {
	request := cloneRequest(baseRequest)
	request["Limit"] = allPageLimit
	request["Offset"] = 0

	var all []*ags.SandboxInstance
	var total int
	var base *command.Result
	for {
		result, err := executor.Execute(ctx, request)
		if err != nil {
			return nil, err
		}
		if base == nil {
			base = result
		}
		response, ok := result.Data.(*ags.DescribeSandboxInstanceListResponseParams)
		if !ok {
			return result, nil
		}
		all = append(all, response.InstanceSet...)
		total = instanceview.DerefInt64(response.TotalCount)
		if len(response.InstanceSet) == 0 || (total > 0 && len(all) >= total) || len(response.InstanceSet) < allPageLimit {
			response.InstanceSet = all
			response.TotalCount = int64Ptr(total)
			return instanceListResult(response, 0, intPtr(allPageLimit), base, listRenderOptions{
				All:    true,
				Region: config.GetRegion(),
			}), nil
		}
		request["Offset"] = len(all)
	}
}

func cloneRequest(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+2)
	for key, value := range in {
		out[key] = value
	}
	return out
}

func intPtr(value int) *int {
	return &value
}

func int64Ptr(value int) *int64 {
	out := int64(value)
	return &out
}

type listRenderOptions struct {
	All    bool
	Region string
}

func instanceListResult(response *ags.DescribeSandboxInstanceListResponseParams, offset int, limit *int, base *command.Result, opts ...listRenderOptions) *command.Result {
	var renderOpts listRenderOptions
	if len(opts) > 0 {
		renderOpts = opts[0]
	}
	items := make([]map[string]any, len(response.InstanceSet))
	for i, instance := range response.InstanceSet {
		items[i] = instanceview.CanonicalData(instance)
		if renderOpts.Region != "" {
			items[i]["Region"] = renderOpts.Region
		}
	}
	pagination := map[string]any{
		"Offset":     offset,
		"Total":      instanceview.DerefInt64(response.TotalCount),
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
			renderInstanceList(w, response, renderOpts)
		},
	}
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

func intFlag(req command.Request, name string) int {
	flag, ok := req.Flags[name]
	if !ok {
		return 0
	}
	return flag.Int
}

func boolFlag(req command.Request, name string) bool {
	flag, ok := req.Flags[name]
	return ok && flag.Bool
}

func flagChanged(req command.Request, name string) bool {
	flag, ok := req.Flags[name]
	return ok && flag.Changed
}

func renderInstanceList(w io.Writer, response *ags.DescribeSandboxInstanceListResponseParams, opts ...listRenderOptions) {
	if len(response.InstanceSet) == 0 {
		fmt.Fprintln(w, "No instances found")
		return
	}

	headers := []string{"ID", "TOOL", "STATUS", "TIMEOUT", "EXPIRES", "MOUNTS", "CREATED"}
	var renderOpts listRenderOptions
	if len(opts) > 0 {
		renderOpts = opts[0]
	}
	if renderOpts.Region != "" {
		headers = append([]string{"REGION"}, headers...)
	}
	rows := make([][]string, len(response.InstanceSet))
	for i, inst := range response.InstanceSet {
		timeout := "-"
		if inst.TimeoutSeconds != nil {
			timeout = instanceview.Timeout(*inst.TimeoutSeconds)
		}
		expires := "-"
		if inst.ExpiresAt != nil && *inst.ExpiresAt != "" {
			expires = instanceview.TimeShort(*inst.ExpiresAt)
		}
		rows[i] = []string{
			instanceview.DerefString(inst.InstanceId),
			instanceview.DerefString(inst.ToolName),
			instanceview.DerefString(inst.Status),
			timeout,
			expires,
			instanceview.MountOptionsSummary(inst.MountOptions),
			instanceview.TimeShort(instanceview.DerefString(inst.CreateTime)),
		}
		if renderOpts.Region != "" {
			rows[i] = append([]string{renderOpts.Region}, rows[i]...)
		}
	}
	instanceview.PrintTableWithPagination(w, headers, rows, len(response.InstanceSet), instanceview.DerefInt64(response.TotalCount))
}
