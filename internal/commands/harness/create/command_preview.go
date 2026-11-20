//go:build preview

package create

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
)

// Module wraps CreateHarness with model, registry and role shortcuts and an
// optional wait for revision 1 to publish.
func Module() command.Module {
	return harnesscli.Mixed(APIDescriptor(), func(spec *command.Spec) {
		for i := range spec.Flags {
			if spec.Flags[i].Name == "initialize-configuration" {
				// --role-arn, --subnet-id and --security-group-id fill this object.
				spec.Flags[i].Usage = strings.TrimSuffix(spec.Flags[i].Usage, " (required)")
				spec.Flags[i].Required = false
			}
		}
		spec.Flags = append(spec.Flags, harnesscli.SpecFlags(false)...)
		spec.Flags = append(spec.Flags, harnesscli.InitializationFlags()...)
		spec.Flags = append(spec.Flags, harnesscli.DryRunFlag(), resourcewait.Flag())
		spec.Output.DataType = "HarnessCreateData"
		spec.Output.Description = "Created Harness; with --wait also the final state of revision 1."
	}, run)
}

func run(ctx context.Context, req command.Request, env harnesscli.Env) (*command.Result, error) {
	request, err := harnesscli.BuildCreateRequest(ctx, req, env)
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
	if harnessID == "" {
		return nil, fmt.Errorf("CreateHarness response is missing HarnessId")
	}
	data := map[string]any{"Harness": harness, "RequestId": response.String("RequestId")}
	result.Data = data
	if !resourcewait.Requested(req) {
		result.Text = func(w io.Writer) {
			harnesscli.RenderHarness(w, harness)
			env.Next("agr harness status %s   # revision 1 is publishing", harnessID)
		}
		return result, nil
	}
	options := resourcewait.OptionsFromDeps(env.Deps)
	revision, err := resourcewait.WaitForHarnessRevisionPublish(ctx, harnessID, 1, env.Client.RevisionGetter(harnessID, 1), options)
	if err != nil {
		return nil, harnesscli.WithCreated(err, map[string]any{"HarnessId": harnessID, "Revision": 1},
			fmt.Sprintf("harness %s was created but revision 1 did not publish", harnessID),
			fmt.Sprintf("agr harness status %s", harnessID))
	}
	data["Revision"] = revision
	result.Text = func(w io.Writer) {
		harnesscli.RenderHarness(w, harness)
		fmt.Fprintf(w, "\nRevision 1 is %s.\n", revision.String("Status"))
		env.Next("agr harness endpoint %s --check", harnessID)
	}
	return result, nil
}
