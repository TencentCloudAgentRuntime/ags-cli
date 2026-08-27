package create

import (
	"context"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	deploymentview "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/deployment/internal/deploymentview"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// Module returns the generated API command with Deployment text rendering.
func Module() command.Module {
	api := APIDescriptor()
	return command.Module{
		Descriptor: mixedDescriptor(api),
		Build: func(deps command.Deps) (command.Runtime, error) {
			builder := apicli.NewRequestBuilder(api)
			executor := apicli.NewExecutor(api, deps.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				apiRequest, err := builder.Build(req)
				if err != nil {
					return nil, err
				}
				result, err := executor.Execute(ctx, apiRequest)
				if err != nil {
					return nil, err
				}
				if response, ok := result.Data.(*ags.CreateDeploymentResponseParams); ok && response.Deployment != nil {
					result.Effects = append(result.Effects, output.Effect{Kind: "create", Resource: "deployment", Id: stringValue(response.Deployment.DeploymentId)})
					result.Text = func(w io.Writer) { deploymentview.RenderDetails(w, response.Deployment) }
				}
				return result, nil
			})}, nil
		},
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func mixedDescriptor(api apicli.APIDescriptor) command.Descriptor {
	return command.Descriptor{
		Spec: api.CommandSpec(),
		Generated: &command.Descriptor{
			Spec: api.CommandSpec(), Groups: api.Groups, API: api, Source: command.SourceAPICli,
		},
		Groups: api.Groups, API: api, Source: command.SourceMixedAPI,
	}
}
