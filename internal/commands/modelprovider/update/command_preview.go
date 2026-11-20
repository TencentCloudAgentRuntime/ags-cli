//go:build preview

package update

import (
	"context"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
)

// Module wraps ModifyModelProvider with source and model shortcuts.
func Module() command.Module {
	return harnesscli.Mixed(APIDescriptor(), func(spec *command.Spec) {
		spec.Flags = append(spec.Flags, harnesscli.ProviderFlags(true)...)
		spec.Flags = append(spec.Flags, harnesscli.DryRunFlag())
	}, func(ctx context.Context, req command.Request, env harnesscli.Env) (*command.Result, error) {
		request, err := harnesscli.BuildProviderRequest(req, env, true)
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
		onlyTags := len(request) == 2 && request["Tags"] != nil
		result.Text = func(w io.Writer) {
			harnesscli.RenderModelProvider(w, provider)
			if !onlyTags {
				env.Next("harnesses use this change only after 'agr harness revision create <harness-id> --publish'")
			}
		}
		return result, nil
	})
}
