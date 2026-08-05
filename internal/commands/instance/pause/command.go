package pause

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

// Module returns this package's command module.
func Module() command.Module {
	api := APIDescriptor()
	spec := api.CommandSpec()
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Generated: &command.Descriptor{
				Spec:   spec,
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
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				apiReq, err := builder.Build(req)
				if err != nil {
					return nil, err
				}
				result, err := executor.Execute(ctx, apiReq)
				if err != nil {
					return nil, err
				}
				instanceID := instanceID(req, apiReq)
				result.Text = func(w io.Writer) {
					fmt.Fprintln(w, pauseResultText(result.Data, instanceID))
				}
				return result, nil
			})}, nil
		},
	}
}

func instanceID(req command.Request, apiReq map[string]any) string {
	if id, _ := apiReq["InstanceId"].(string); id != "" {
		return id
	}
	if id := req.ArgValues["instance-id"]; id != "" {
		return id
	}
	if len(req.Args) > 0 {
		return req.Args[0]
	}
	return ""
}
