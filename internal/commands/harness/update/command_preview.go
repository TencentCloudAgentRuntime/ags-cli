//go:build preview

package update

import (
	"context"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
)

// Module wraps ModifyHarness with the shared spec shortcuts. It edits only the
// draft and points to the revision step that makes the change live.
func Module() command.Module {
	return harnesscli.Mixed(APIDescriptor(), func(spec *command.Spec) {
		spec.Flags = append(spec.Flags, harnesscli.SpecFlags(true)...)
		spec.Flags = append(spec.Flags, harnesscli.DryRunFlag())
	}, run)
}

func run(ctx context.Context, req command.Request, env harnesscli.Env) (*command.Result, error) {
	request, err := harnesscli.BuildUpdateRequest(req, env)
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
	harness := response.Object("Harness")
	harnessID := harness.String("HarnessId")
	onlyTags := len(request) == 2 && request["Tags"] != nil
	result.Text = func(w io.Writer) {
		harnesscli.RenderHarness(w, harness)
		if !onlyTags {
			env.Next("draft updated but not live; run 'agr harness revision create %s --publish --wait'", harnessID)
		}
	}
	return result, nil
}
