package create

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	deploymentview "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/deployment/internal/deploymentview"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
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
				response, decodeErr := apivalue.Decode(result.Data)
				if decodeErr != nil {
					return nil, decodeErr
				}
				deployment := response.Object("Deployment")
				if deployment == nil || deployment.String("DeploymentId") == "" {
					return nil, fmt.Errorf("deployment response is missing DeploymentId")
				}
				result.Effects = append(result.Effects, output.Effect{Kind: "create", Resource: "deployment", Id: deployment.String("DeploymentId")})
				result.Text = func(w io.Writer) { deploymentview.RenderDetails(w, deployment) }
				return result, nil
			})}, nil
		},
	}
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
