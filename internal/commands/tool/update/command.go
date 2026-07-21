package update

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	toolget "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/tool/get"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// Module returns this package's command module.
func Module() command.Module {
	api := APIDescriptor()
	generatedSpec := api.CommandSpec()
	spec := generatedSpec
	spec.Flags = append(spec.Flags, resourcewait.Flag())
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Generated: &command.Descriptor{
				Spec:   generatedSpec,
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
					apiReq, err := builder.Build(req)
					if err != nil {
						return nil, err
					}
					if !requestFlag(req) && len(apiReq) <= 1 {
						return nil, output.NewUsageError("MISSING_REQUIRED_FLAG", "at least one of --description, --network-configuration, --tags, --custom-configuration, or --request must be specified", "Provide at least one field to update.")
					}
					result, err := executor.Execute(ctx, apiReq)
					if err != nil {
						return nil, err
					}
					applyUpdateResultText(result, req)
					if resourcewait.Requested(req) {
						getter, ok := deps.ControlPlane.(resourcewait.ToolGetter)
						if !ok {
							return nil, fmt.Errorf("tool.update --wait requires GetTool support")
						}
						toolID, err := ToolID(req)
						if err != nil {
							return nil, err
						}
						options := resourcewait.OptionsFromDeps(deps)
						options.DelayBeforeFirstPoll = true
						tool, err := resourcewait.WaitForTool(ctx, toolID, getter.GetTool, options)
						if err != nil {
							return nil, err
						}
						return resourcewait.PreserveMutationMetadata(toolget.Result(tool), result), nil
					}
					return result, nil
				}),
			}, nil
		},
	}
}

func applyUpdateResultText(result *command.Result, req command.Request) {
	if result == nil {
		return
	}
	if _, ok := result.Data.(*ags.UpdateSandboxToolResponseParams); !ok {
		return
	}
	toolID, _ := ToolID(req)
	result.Text = func(w io.Writer) {
		fmt.Fprintf(w, "Tool updated: %s\n", toolID)
	}
}

func requestFlag(req command.Request) bool {
	flag, ok := req.Flags["request"]
	return ok && flag.Changed && flag.String != ""
}

// ToolID resolves the target tool id from normalized positional values and
// returns the command's required-argument usage error when absent.
func ToolID(req command.Request) (string, error) {
	toolID := req.ArgValues["tool-id"]
	if toolID == "" && len(req.Args) > 0 {
		toolID = req.Args[0]
	}
	if toolID == "" {
		return "", output.NewUsageError("MISSING_REQUIRED_ARG", "missing tool id", "Provide <tool-id>.")
	}
	return toolID, nil
}
