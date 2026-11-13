//go:build preview

package delete

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module wraps DeleteModelProvider with confirmation and a reference hint.
func Module() command.Module {
	return harnesscli.Mixed(APIDescriptor(), func(spec *command.Spec) {
		spec.Flags = append(spec.Flags, harnesscli.YesFlag())
	}, func(ctx context.Context, req command.Request, env harnesscli.Env) (*command.Result, error) {
		request, err := env.Builder.Build(harnesscli.WithoutFlags(req, "yes"))
		if err != nil {
			return nil, err
		}
		providerID, _ := request["ModelProviderId"].(string)
		if !harnesscli.Bool(req, "yes") {
			if cli.NonInteractive() || !env.Deps.IO.IsStdinTTY() {
				return nil, output.NewUsageError("CONFIRMATION_REQUIRED",
					fmt.Sprintf("deleting model provider %s requires confirmation", providerID),
					fmt.Sprintf("Re-run with --yes: agr model-provider delete %s --yes", providerID))
			}
			fmt.Fprintf(env.Deps.IO.ErrOut, "Model provider %s will be deleted.\n\nProceed? [y/N] ", providerID)
			var answer string
			if _, err := fmt.Fscanln(env.Deps.IO.In, &answer); err != nil || (answer != "y" && answer != "Y") {
				return &command.Result{Data: map[string]any{"Cancelled": true}, Text: func(w io.Writer) { fmt.Fprintln(w, "Cancelled.") }}, nil
			}
		}
		result, _, err := env.Execute(ctx, request)
		if err != nil {
			if classified := output.ClassifyError(err); classified.Failure != nil && classified.Failure.Code == "ResourceInUse" {
				failure := *classified.Failure
				failure.Hint = "A harness draft or retained harness revision still references this provider. Switch the harness models with 'agr harness update', or delete the harness first."
				return nil, (&output.CLIError{Failure: &failure, ExitCode: classified.ExitCode}).WithCause(err)
			}
			return nil, err
		}
		result.Text = func(w io.Writer) { fmt.Fprintf(w, "Model provider %s deleted.\n", providerID) }
		return result, nil
	})
}
