//go:build preview

package create

import (
	"context"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
)

// Module wraps CreateModelProvider with source and model shortcuts.
func Module() command.Module {
	return harnesscli.Mixed(APIDescriptor(), func(spec *command.Spec) {
		spec.Flags = append(spec.Flags, harnesscli.ProviderFlags(false)...)
		spec.Flags = append(spec.Flags, harnesscli.DryRunFlag())
	}, func(ctx context.Context, req command.Request, env harnesscli.Env) (*command.Result, error) {
		request, err := harnesscli.BuildProviderRequest(req, env, false)
		if err != nil {
			return nil, err
		}
		if harnesscli.Bool(req, "dry-run") {
			return env.DryRun(request), nil
		}
		result, response, err := env.Execute(ctx, request)
		if err != nil {
			return nil, err
		}
		provider := response.Object("ModelProvider")
		result.Text = func(w io.Writer) {
			harnesscli.RenderModelProvider(w, provider)
			env.Next("agr harness create --model %s:<model-id> ...", provider.String("ModelProviderId"))
		}
		return result, nil
	})
}
