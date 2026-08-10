package create

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// Module returns this package's command module.
func Module() command.Module {
	api := APIDescriptor()
	spec := api.CommandSpec()
	spec.Output = command.OutputSpec{
		DataType:    "APIKeyCreateData",
		Description: "API key create result.",
		Effects:     []string{"create:apikey"},
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
			deps = deps.WithDefaults()
			builder := apicli.NewRequestBuilder(api)
			executor := apicli.NewExecutor(api, deps.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				if !requestFlag(req) {
					name := stringFlag(req, "name")
					if strings.TrimSpace(name) == "" {
						return nil, output.NewUsageError("MISSING_REQUIRED_FLAG", "API key name (-n/--name) is required", "Provide a non-empty value for --name.")
					}
				}
				apiReq, err := builder.Build(req)
				if err != nil {
					return nil, err
				}
				result, err := executor.Execute(ctx, apiReq)
				if err != nil {
					return nil, err
				}
				response, ok := result.Data.(*ags.CreateAPIKeyResponseParams)
				if !ok {
					return result, nil
				}
				keyID := derefString(response.KeyId)
				result.Data = map[string]any{
					"KeyId":  keyID,
					"Name":   derefString(response.Name),
					"ApiKey": derefString(response.APIKey),
				}
				result.Effects = append(result.Effects, output.Effect{Kind: "create", Resource: "apikey", Id: keyID})
				result.Text = func(w io.Writer) {
					fmt.Fprintf(w, "API key created: %s\n", keyID)
					fmt.Fprintf(deps.IO.ErrOut, "Warning: Save this API key securely - it will not be shown again!\n")
					printKV(w, []keyValue{
						{key: "KeyID", value: keyID},
						{key: "Name", value: derefString(response.Name)},
						{key: "APIKey", value: derefString(response.APIKey)},
					})
				}
				return result, nil
			})}, nil
		},
	}
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

type keyValue struct {
	key   string
	value string
}

func printKV(w io.Writer, pairs []keyValue) {
	maxLen := 0
	for _, kv := range pairs {
		if len(kv.key) > maxLen {
			maxLen = len(kv.key)
		}
	}
	for _, kv := range pairs {
		fmt.Fprintf(w, "%-*s  %s\n", maxLen, kv.key+":", kv.value)
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
