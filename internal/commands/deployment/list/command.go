package list

import (
	"context"
	"io"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	deploymentview "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/deployment/internal/deploymentview"
)

// Module returns the generated API command with Deployment table rendering.
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
				deployments, err := response.ReadObjects("DeploymentSet")
				if err != nil {
					return nil, err
				}
				total := len(deployments)
				if response["TotalCount"] != nil {
					total = int(response.Int64("TotalCount"))
				}
				result.Text = func(w io.Writer) { deploymentview.RenderList(w, deployments, total, time.Now()) }
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
