package delete

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module returns this package's command module.
func Module() command.Module {
	api := APIDescriptor()
	spec := api.CommandSpec()
	spec.Output = command.OutputSpec{
		DataType:    "APIKeyDeleteData",
		Description: "API key delete result.",
		Effects:     []string{"delete:apikey"},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Generated: &command.Descriptor{
				Spec:   api.CommandSpec(),
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
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				apiReq, err := builder.Build(req)
				if err != nil {
					return nil, err
				}
				result, err := executor.Execute(ctx, apiReq)
				if err != nil {
					return nil, err
				}
				keyID := keyID(req, apiReq)
				result.Data = map[string]any{"KeyId": keyID}
				result.Effects = append(result.Effects, output.Effect{Kind: "delete", Resource: "apikey", Id: keyID})
				result.Text = func(w io.Writer) {
					fmt.Fprintf(w, "API key deleted: %s\n", keyID)
				}
				return result, nil
			})}, nil
		},
	}
}

func keyID(req command.Request, apiReq map[string]any) string {
	if id, _ := apiReq["KeyId"].(string); id != "" {
		return id
	}
	if id := req.ArgValues["key-id"]; id != "" {
		return id
	}
	if len(req.Args) > 0 {
		return req.Args[0]
	}
	return ""
}
