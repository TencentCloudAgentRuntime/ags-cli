//go:build preview

package delete

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

const keeps = "Kept: the Sandbox Tool, model providers, session spaces, Registry assets and credentials."

// Module wraps DeleteHarness with confirmation, preview and deletion wait.
func Module() command.Module {
	return harnesscli.Mixed(APIDescriptor(), func(spec *command.Spec) {
		spec.Flags = append(spec.Flags, harnesscli.YesFlag(), harnesscli.DryRunFlag(), resourcewait.Flag())
	}, run)
}

func run(ctx context.Context, req command.Request, env harnesscli.Env) (*command.Result, error) {
	request, err := env.Builder.Build(harnesscli.WithoutFlags(req, "yes", "dry-run", "wait"))
	if err != nil {
		return nil, err
	}
	harnessID, _ := request["HarnessId"].(string)
	needsPrompt := !harnesscli.Bool(req, "yes") && !harnesscli.Bool(req, "dry-run")
	if needsPrompt && (cli.NonInteractive() || !env.Deps.IO.IsStdinTTY()) {
		return nil, output.NewUsageError("CONFIRMATION_REQUIRED",
			fmt.Sprintf("deleting harness %s requires confirmation", harnessID),
			fmt.Sprintf("Re-run with --yes: agr harness delete %s --yes", harnessID))
	}
	harness, err := env.Client.Harness(ctx, harnessID)
	if err != nil {
		return nil, err
	}
	deploymentID := harness.Object("ResourceInfo").String("DeploymentId")
	toolID := harness.Object("ResourceInfo").String("ToolId")
	plan := map[string]any{"HarnessId": harnessID, "HarnessName": harness.String("HarnessName"), "DeploymentId": deploymentID, "KeptToolId": toolID}
	describe := func(w io.Writer) {
		fmt.Fprintf(w, "Harness %s (%s) and its revisions will be deleted.\n", harnessID, harness.String("HarnessName"))
		if deploymentID != "" {
			fmt.Fprintf(w, "Deployment %s will be deleted.\n", deploymentID)
		}
		fmt.Fprintln(w, keeps)
	}
	if harnesscli.Bool(req, "dry-run") {
		plan["DryRun"] = true
		return &command.Result{Data: plan, Text: func(w io.Writer) {
			fmt.Fprintln(w, "Dry run:")
			describe(w)
			fmt.Fprintln(w, "\nNo changes were made.")
		}}, nil
	}
	if needsPrompt {
		describe(env.Deps.IO.ErrOut)
		fmt.Fprint(env.Deps.IO.ErrOut, "\nProceed? [y/N] ")
		var answer string
		if _, err := fmt.Fscanln(env.Deps.IO.In, &answer); err != nil || (answer != "y" && answer != "Y") {
			return &command.Result{Data: map[string]any{"Cancelled": true}, Text: func(w io.Writer) { fmt.Fprintln(w, "Cancelled.") }}, nil
		}
	}
	result, response, err := env.Execute(ctx, request)
	if err != nil {
		return nil, err
	}
	plan["RequestId"] = response.String("RequestId")
	result.Data = plan
	if !resourcewait.Requested(req) {
		plan["Status"] = "DELETING"
		result.Text = func(w io.Writer) {
			fmt.Fprintf(w, "Deletion of harness %s accepted.\n%s\n", harnessID, keeps)
			env.Next("agr harness get %s   # DELETING until the Deployment is removed", harnessID)
		}
		return result, nil
	}
	options := resourcewait.OptionsFromDeps(env.Deps)
	options.IsNotFound = harnesscli.IsNotFound
	if _, err := resourcewait.WaitForHarnessDeletion(ctx, harnessID, env.Client.HarnessGetter(), options); err != nil {
		return nil, err
	}
	plan["Status"] = "DELETED"
	result.Text = func(w io.Writer) {
		fmt.Fprintf(w, "Harness %s deleted.\n%s\n", harnessID, keeps)
		if toolID != "" {
			env.Next("agr tool delete %s   # only if the Sandbox Tool is no longer needed", toolID)
		}
	}
	return result, nil
}
