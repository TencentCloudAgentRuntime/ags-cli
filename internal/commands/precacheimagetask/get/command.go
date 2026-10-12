package get

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
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
				if err := validateSelector(apiReq); err != nil {
					return nil, err
				}
				result, err := executor.Execute(ctx, apiReq)
				if err != nil {
					return nil, err
				}
				result.Text = func(w io.Writer) { fmt.Fprintln(w, "OK") }
				return result, nil
			})}, nil
		},
	}
}

// Validate the assembled request so flags and every JSON transport share the
// same selector contract before any control-plane call.
func validateSelector(req map[string]any) error {
	id, hasID := req["PreCacheImageId"]
	tripleFields := []string{"Image", "ImageDigest", "ImageRegistryType"}
	hasTriple := false
	completeTriple := true
	for _, field := range tripleFields {
		value, present := req[field]
		hasTriple = hasTriple || present
		text, ok := value.(string)
		completeTriple = completeTriple && ok && strings.TrimSpace(text) != ""
	}
	hint := "Provide --pre-cache-image-id alone, or <image-digest> with --image and --image-registry-type."
	if hasID && hasTriple {
		return output.NewUsageError("CONFLICTING_INPUTS", "task ID and image triple are mutually exclusive", hint)
	}
	if hasID {
		text, ok := id.(string)
		if ok && strings.TrimSpace(text) != "" {
			return nil
		}
	} else if completeTriple {
		return nil
	}
	return output.NewUsageError("MISSING_REQUIRED_INPUT", "provide a non-empty task ID or a complete image triple", hint)
}
