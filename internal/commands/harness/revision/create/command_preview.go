//go:build preview

package create

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/harness/revision/publish"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module wraps CreateHarnessRevision with an optional publish step.
func Module() command.Module {
	return harnesscli.Mixed(APIDescriptor(), func(spec *command.Spec) {
		spec.Flags = append(spec.Flags,
			harnesscli.BoolFlag("publish", "Publish the new revision after creating it"),
			resourcewait.Flag(),
		)
		spec.Output.Effects = append(spec.Output.Effects, "update:harness-revision")
		spec.Output.DataType = "HarnessRevisionCreateData"
		spec.Output.Description = "Created revision; with --publish its publish state."
	}, run)
}

func run(ctx context.Context, req command.Request, env harnesscli.Env) (*command.Result, error) {
	if resourcewait.Requested(req) && !harnesscli.Bool(req, "publish") {
		return nil, output.NewUsageError("WAIT_REQUIRES_PUBLISH", "--wait requires --publish",
			"A new revision is UNPUBLISHED immediately; add --publish to publish and wait.")
	}
	request, err := env.Builder.Build(harnesscli.WithoutFlags(req, "publish", "wait"))
	if err != nil {
		return nil, err
	}
	result, response, err := env.Execute(ctx, request)
	if err != nil {
		return nil, err
	}
	revision := response.Object("HarnessRevision")
	harnessID, number := revision.String("HarnessId"), revision.Int64("Revision")
	if number < 1 {
		return nil, fmt.Errorf("CreateHarnessRevision response is missing Revision")
	}
	if !harnesscli.Bool(req, "publish") {
		result.Text = func(w io.Writer) {
			harnesscli.RenderRevision(w, revision)
			env.Next("agr harness revision publish %s %d --wait", harnessID, number)
		}
		return result, nil
	}
	published, err := publish.Publish(ctx, req, env.WithAPI(publish.APIDescriptor()), harnessID, number)
	if err != nil {
		// The create step is not rolled back; keep the new number visible.
		return nil, harnesscli.WithCreated(err, map[string]any{"HarnessId": harnessID, "Revision": number},
			fmt.Sprintf("revision %d was created but publishing failed", number),
			fmt.Sprintf("agr harness revision publish %s %d --wait", harnessID, number))
	}
	if data, ok := published.Data.(map[string]any); ok {
		data["Created"] = true
	}
	return published, nil
}
