package create

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	instanceview "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/instance/internal/instanceview"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/progress"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// Module returns this package's command module.
func Module() command.Module {
	api := APIDescriptor()
	spec := api.CommandSpec()
	spec.Output = command.OutputSpec{
		DataType:    "InstanceCreateData",
		Description: "Instance create result with normalized instance data.",
		Effects:     []string{"create:instance"},
	}
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
			deps = deps.WithDefaults()
			builder := apicli.NewRequestBuilder(api)
			executor := apicli.NewExecutor(api, deps.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				if !requestFlag(req) {
					if err := validateToolSelection(req); err != nil {
						return nil, err
					}
				}
				apiReq, err := builder.Build(req)
				if err != nil {
					return nil, err
				}

				// Show spinner for interactive text mode only.
				sp := progress.NewForCLI(deps.IO.ErrOut, cli.IsJSONOutput(), cli.NonInteractive(), deps.IO.IsStderrTTY(), cli.NoColor())
				sp.Start("Creating instance...")

				result, err := executor.Execute(ctx, apiReq)
				if err != nil {
					sp.Stop("✗", "Failed to create instance")
					return nil, err
				}
				response, ok := result.Data.(*ags.StartSandboxInstanceResponseParams)
				if !ok {
					// Response type mismatch — we cannot confirm creation succeeded.
					sp.Cleanup()
					return result, nil
				}
				if response.Instance == nil {
					sp.Stop("✗", "Failed to create instance")
					return nil, output.NewCLIError(&output.Failure{
						Code:    "INTERNAL_ERROR",
						Kind:    output.KindGenericError,
						Message: "no instance returned from API",
						Hint:    "Rerun with --debug. If the issue persists, inspect the control-plane response.",
					})
				}
				sp.Stop("✓", "Instance created")
				return instanceCreateResult(response, result), nil
			})}, nil
		},
	}
}

func validateToolSelection(req command.Request) error {
	toolName := stringFlag(req, "tool-name")
	toolID := stringFlag(req, "tool-id")
	if toolName != "" && toolID != "" {
		return output.NewUsageError("CONFLICTING_FLAGS", "cannot specify both --tool-name and --tool-id", "Provide either --tool-name or --tool-id.")
	}
	if toolName == "" && toolID == "" {
		return output.NewUsageError("MISSING_REQUIRED_FLAG", "must specify either --tool-name/-t or --tool-id", "Provide --tool-name for a named tool or --tool-id for a cloud tool ID.")
	}
	return nil
}

func instanceCreateResult(response *ags.StartSandboxInstanceResponseParams, base *command.Result) *command.Result {
	instance := response.Instance
	data := instanceview.CanonicalData(instance)
	data["Instance"] = instance
	data["RequestId"] = instanceview.DerefString(response.RequestId)
	instanceID := instanceview.DerefString(instance.InstanceId)
	return &command.Result{
		Data:      data,
		Warnings:  base.Warnings,
		Effects:   append(base.Effects, output.Effect{Kind: "create", Resource: "instance", Id: instanceID}),
		ExitCode:  base.ExitCode,
		Failure:   base.Failure,
		MetaExtra: base.MetaExtra,
		Text: func(w io.Writer) {
			renderCreatedInstance(w, instance)
		},
	}
}

func renderCreatedInstance(w io.Writer, instance *ags.SandboxInstance) {
	instanceID := instanceview.DerefString(instance.InstanceId)
	fmt.Fprintf(w, "Instance created: %s\n", instanceID)
	kvs := []instanceview.KeyValue{
		{Key: "ID", Value: instanceID},
		{Key: "Tool", Value: instanceview.DerefString(instance.ToolName)},
		{Key: "Status", Value: instanceview.DerefString(instance.Status)},
		{Key: "Created", Value: instanceview.DerefString(instance.CreateTime)},
	}
	if len(instance.MountOptions) > 0 {
		kvs = append(kvs, instanceview.KeyValue{Key: "MountOptions", Value: instanceview.MountOptionsSummary(instance.MountOptions)})
	}
	instanceview.PrintKV(w, kvs)
}

func requestFlag(req command.Request) bool {
	flag, ok := req.Flags["request"]
	return ok && flag.Changed && strings.TrimSpace(flag.String) != ""
}

func stringFlag(req command.Request, name string) string {
	flag, ok := req.Flags[name]
	if !ok {
		return ""
	}
	return flag.String
}
