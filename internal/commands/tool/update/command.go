package update

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	toolget "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/tool/get"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module returns this package's command module.
func Module() command.Module {
	api := APIDescriptor()
	generatedSpec := api.CommandSpec()
	spec := generatedSpec
	waitFlag := resourcewait.Flag()
	waitFlag.Usage = "Wait for tool status ACTIVE or ISOLATED. Read back updated fields to verify the update."
	spec.Flags = append(spec.Flags, waitFlag)
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Generated: &command.Descriptor{
				Spec:   generatedSpec,
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
					apiReq, err := builder.Build(req)
					if err != nil {
						return nil, err
					}
					rawRequest := requestFlag(req)
					if err := validateStorageMounts(apiReq, rawRequest); err != nil {
						return nil, err
					}
					if !rawRequest && len(apiReq) <= 1 {
						return nil, output.NewUsageError("MISSING_REQUIRED_FLAG", "at least one of --description, --network-configuration, --storage-mounts, --tags, --custom-configuration, --computer-configuration, or --request must be specified", "Provide at least one field to update.")
					}
					result, err := executor.Execute(ctx, apiReq)
					if err != nil {
						return nil, err
					}
					toolID, _ := apiReq["ToolId"].(string)
					applyUpdateResultText(result, toolID)
					if resourcewait.Requested(req) {
						getter, ok := deps.ControlPlane.(resourcewait.ToolGetter)
						if !ok {
							return nil, fmt.Errorf("tool.update --wait requires GetTool support")
						}
						tool, err := resourcewait.WaitForToolWithPolicy(
							ctx,
							toolID,
							getter.GetTool,
							resourcewait.ToolPolicy(resourcewait.OperationUpdate),
							resourcewait.OptionsFromDeps(deps),
						)
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

func applyUpdateResultText(result *command.Result, toolID string) {
	if result == nil {
		return
	}
	if _, err := apivalue.Decode(result.Data); err != nil {
		return
	}
	result.Text = func(w io.Writer) {
		fmt.Fprintf(w, "Tool updated: %s\n", toolID)
	}
}

func requestFlag(req command.Request) bool {
	flag, ok := req.Flags["request"]
	return ok && flag.Changed && strings.TrimSpace(flag.String) != ""
}

// validateStorageMounts preserves the tool's mount settings when users update
// an image, including when they copy an image mount from tool get into a raw request.
func validateStorageMounts(request map[string]any, rawRequest bool) error {
	value, ok := request["StorageMounts"]
	if !ok || value == nil {
		return nil
	}
	mounts, ok := value.([]any)
	if !ok {
		// Shape validation against the API contract reports non-array values.
		return nil
	}
	mountInput := "each --storage-mounts entry"
	if rawRequest {
		mountInput = "the non-Image StorageMounts entry in --request"
	}
	for index, item := range mounts {
		mount, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if rawRequest {
			source, _ := mount["StorageSource"].(map[string]any)
			if image, _ := source["Image"].(map[string]any); image != nil {
				if _, ok := mount["MountPath"].(string); ok {
					delete(mount, "MountPath")
				}
				if _, ok := mount["ReadOnly"].(bool); ok {
					delete(mount, "ReadOnly")
				}
				continue
			}
		}
		for _, field := range []string{"MountPath", "ReadOnly"} {
			if fieldValue, present := mount[field]; present && fieldValue != nil {
				return output.NewUsageError(
					"INVALID_REQUEST_JSON",
					fmt.Sprintf("StorageMounts[%d].%s cannot be changed by tool update", index, field),
					fmt.Sprintf("Omit %s from %s to keep the tool's mount configuration.", field, mountInput),
				)
			}
		}
	}
	return nil
}
