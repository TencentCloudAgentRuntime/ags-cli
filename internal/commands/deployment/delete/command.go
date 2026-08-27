package delete

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// ControlPlane supplies the generated delete call and status polling seam.
type ControlPlane interface {
	apicli.ControlPlane
	GetDeployment(context.Context, string) (*ags.Deployment, error)
	IsDeploymentNotFound(error) bool
}

// Module returns the generated delete operation with its asynchronous wait workflow.
func Module() command.Module {
	api := APIDescriptor()
	generatedSpec := api.CommandSpec()
	spec := generatedSpec
	spec.Flags = append(spec.Flags, resourcewait.Flag())
	spec.Output = command.OutputSpec{
		DataType:    "DeleteDeploymentResponse",
		Description: "Deployment delete response.",
		Effects:     []string{"delete:deployment"},
	}

	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Generated: &command.Descriptor{
				Spec: generatedSpec, Groups: api.Groups, API: api, Source: command.SourceAPICli,
			},
			Groups: api.Groups, API: api, Source: command.SourceMixedAPI,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp, ok := deps.ControlPlane.(ControlPlane)
			if !ok {
				return command.Runtime{}, fmt.Errorf("deployment.delete requires Deployment control-plane support")
			}
			builder := apicli.NewRequestBuilder(api)
			executor := apicli.NewExecutor(api, cp)
			waitOptions := resourcewait.OptionsFromDeps(deps)
			waitOptions.IsNotFound = cp.IsDeploymentNotFound
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				apiRequest, err := builder.Build(req)
				if err != nil {
					return nil, err
				}
				deploymentID, _ := apiRequest["DeploymentId"].(string)
				if strings.TrimSpace(deploymentID) == "" {
					return nil, output.NewUsageError("MISSING_REQUIRED_ARG", "missing deployment id", "Provide <deployment-id>.")
				}
				result, err := executor.Execute(ctx, apiRequest)
				if err != nil {
					return nil, err
				}
				result.Effects = append(result.Effects, output.Effect{Kind: "delete", Resource: "deployment", Id: deploymentID})
				if !resourcewait.Requested(req) {
					result.Text = func(w io.Writer) { fmt.Fprintf(w, "Deployment deletion requested: %s\n", deploymentID) }
					return result, nil
				}
				if _, err := resourcewait.WaitForDeploymentDeletion(
					ctx,
					deploymentID,
					cp.GetDeployment,
					waitOptions,
				); err != nil {
					return nil, err
				}
				result.Text = func(w io.Writer) { fmt.Fprintf(w, "Deployment deleted: %s\n", deploymentID) }
				return result, nil
			})}, nil
		},
	}
}
