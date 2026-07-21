package resume

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	instanceget "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/instance/get"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
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
					fmt.Fprintf(w, "Instance resumed: %s\n", instanceID)
				}
				if resourcewait.Requested(req) {
					getter, ok := deps.ControlPlane.(resourcewait.InstanceGetter)
					if !ok {
						return nil, fmt.Errorf("instance.resume --wait requires GetInstance support")
					}
					instance, err := resourcewait.WaitForInstance(ctx, instanceID, getter.GetInstance, resourcewait.OptionsFromDeps(deps))
					if err != nil {
						return nil, err
					}
					return resourcewait.PreserveMutationMetadata(instanceget.Result(instance), result), nil
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
