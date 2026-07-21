package create

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	toolget "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/tool/get"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/progress"
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
			deps = deps.WithDefaults()
			builder := apicli.NewRequestBuilder(api)
			executor := apicli.NewExecutor(api, deps.ControlPlane)
			return command.Runtime{
				Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
					apiReq, err := builder.Build(req)
					if err != nil {
						return nil, err
					}
					if !requestFlag(req) {
						if err := validateConvenienceRequest(apiReq); err != nil {
							return nil, err
						}
					}

					// Show spinner for interactive text mode only.
					sp := progress.NewForCLI(deps.IO.ErrOut, cli.IsJSONOutput(), cli.NonInteractive(), deps.IO.IsStderrTTY(), cli.NoColor())
					sp.Start("Creating tool...")

					result, err := executor.Execute(ctx, apiReq)
					if err != nil {
						sp.Stop("✗", "Failed to create tool")
						return nil, err
					}

					if applyCreateResultText(result, req) {
						sp.Stop("✓", "Tool created")
					} else {
						// Response type mismatch or missing ToolId — cannot confirm success.
						sp.Cleanup()
					}
					if resourcewait.Requested(req) {
						response, ok := result.Data.(*ags.CreateSandboxToolResponseParams)
						if !ok || derefString(response.ToolId) == "" {
							return nil, missingToolIDError()
						}
						getter, ok := deps.ControlPlane.(resourcewait.ToolGetter)
						if !ok {
							return nil, fmt.Errorf("tool.create --wait requires GetTool support")
						}
						tool, err := resourcewait.WaitForTool(ctx, derefString(response.ToolId), getter.GetTool, resourcewait.OptionsFromDeps(deps))
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

func missingToolIDError() error {
	return output.NewCLIError(&output.Failure{
		Code:    "INTERNAL_ERROR",
		Kind:    output.KindGenericError,
		Message: "cannot wait because the create response did not include a tool id",
		Hint:    "Rerun with --debug. If the issue persists, inspect the control-plane response.",
	})
}

// applyCreateResultText enriches the command result with text rendering and
// effects when the response is a valid CreateSandboxToolResponseParams with a
// non-empty ToolId. Returns true if the response was confirmed valid.
func applyCreateResultText(result *command.Result, req command.Request) bool {
	if result == nil {
		return false
	}
	response, ok := result.Data.(*ags.CreateSandboxToolResponseParams)
	if !ok {
		return false
	}
	toolID := derefString(response.ToolId)
	if toolID == "" {
		return false
	}
	result.Effects = append(result.Effects, output.Effect{Kind: "create", Resource: "tool", Id: toolID})
	result.Text = func(w io.Writer) {
		fmt.Fprintf(w, "Tool created: %s\n", toolID)
		if requestFlag(req) {
			return
		}
		printKV(w, []kv{
			{key: "ID", value: toolID},
			{key: "Name", value: stringFlag(req, "tool-name")},
			{key: "Type", value: stringFlag(req, "tool-type")},
			{key: "Description", value: stringFlag(req, "description")},
		})
	}
	return true
}

type kv struct {
	key   string
	value string
}

func printKV(w io.Writer, pairs []kv) {
	for _, pair := range pairs {
		if pair.value == "" {
			continue
		}
		fmt.Fprintf(w, "%-14s %s\n", pair.key+":", pair.value)
	}
}

func stringFlag(req command.Request, name string) string {
	flag, ok := req.Flags[name]
	if !ok {
		return ""
	}
	return flag.String
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func validateConvenienceRequest(req map[string]any) error {
	if strings.TrimSpace(stringValue(req["ToolName"])) == "" {
		return output.NewUsageError("MISSING_REQUIRED_FLAG", "tool name (-n/--tool-name) is required", "Provide a non-empty value for --tool-name.")
	}
	if strings.TrimSpace(stringValue(req["ToolType"])) == "" {
		return output.NewUsageError("MISSING_REQUIRED_FLAG", "tool type (-t/--tool-type) is required", "Provide a non-empty value for --tool-type.")
	}
	if mounts, ok := req["StorageMounts"]; ok && collectionLen(mounts) > 0 && strings.TrimSpace(stringValue(req["RoleArn"])) == "" {
		return output.NewUsageError("MISSING_REQUIRED_FLAG", "--role-arn is required when --storage-mounts is specified", "Provide --role-arn when using --storage-mounts.")
	}
	return nil
}

func collectionLen(value any) int {
	switch v := value.(type) {
	case []any:
		return len(v)
	case []map[string]any:
		return len(v)
	default:
		rv := fmt.Sprintf("%v", value)
		if rv == "[]" || rv == "<nil>" {
			return 0
		}
		return 1
	}
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func requestFlag(req command.Request) bool {
	flag, ok := req.Flags["request"]
	return ok && flag.Changed && strings.TrimSpace(flag.String) != ""
}
