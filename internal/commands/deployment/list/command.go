package list

import (
	"context"
	"io"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	deploymentview "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/deployment/internal/deploymentview"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
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
				if response, ok := result.Data.(*ags.DescribeDeploymentListResponseParams); ok {
					total := len(response.DeploymentSet)
					if response.TotalCount != nil {
						total = int(*response.TotalCount)
					}
					result.Text = func(w io.Writer) { deploymentview.RenderList(w, response.DeploymentSet, total, time.Now()) }
				}
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
