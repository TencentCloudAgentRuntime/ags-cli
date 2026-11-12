//go:build preview

package publish

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
)

// Module wraps PublishHarnessRevision. An already published revision is a
// no-op success, so retries never hit the backend's InvalidParameter.
func Module() command.Module {
	return harnesscli.Mixed(APIDescriptor(), func(spec *command.Spec) {
		spec.Flags = append(spec.Flags, resourcewait.Flag())
		spec.Output.DataType = "HarnessRevisionPublishData"
		spec.Output.Description = "Target revision after publishing was accepted, or its final state with --wait."
	}, run)
}

func run(ctx context.Context, req command.Request, env harnesscli.Env) (*command.Result, error) {
	request, err := env.Builder.Build(harnesscli.WithoutFlags(req, "wait"))
	if err != nil {
		return nil, err
	}
	harnessID, _ := request["HarnessId"].(string)
	revisionNumber, err := harnesscli.RevisionNumber(request["Revision"])
	if err != nil {
		return nil, err
	}
	return Publish(ctx, req, env, harnessID, revisionNumber)
}

// Publish publishes one revision with the shared already-published and wait
// semantics; revision create --publish reuses it.
func Publish(ctx context.Context, req command.Request, env harnesscli.Env, harnessID string, revisionNumber int64) (*command.Result, error) {
	current, err := env.Client.Revision(ctx, harnessID, revisionNumber)
	if err != nil {
		return nil, err
	}
	if current.String("Status") == "PUBLISHED" {
		return &command.Result{
			Data: map[string]any{"HarnessRevision": current, "AlreadyPublished": true},
			Text: func(w io.Writer) {
				fmt.Fprintf(w, "Revision %d of %s is already PUBLISHED; nothing to do.\n", revisionNumber, harnessID)
			},
			MetaExtra: map[string]any{"Action": "PublishHarnessRevision", "Skipped": "AlreadyPublished"},
		}, nil
	}
	result, response, err := env.Execute(ctx, map[string]any{"HarnessId": harnessID, "Revision": revisionNumber})
	if err != nil {
		return nil, err
	}
	revision := response.Object("HarnessRevision")
	data := map[string]any{"HarnessRevision": revision, "AlreadyPublished": false, "RequestId": response.String("RequestId")}
	result.Data = data
	if !resourcewait.Requested(req) {
		result.Text = func(w io.Writer) {
			harnesscli.RenderRevision(w, revision)
			env.Next("agr harness revision get %s %d   # until PUBLISHED or PUBLISH_FAILED", harnessID, revisionNumber)
		}
		return result, nil
	}
	final, err := resourcewait.WaitForHarnessRevisionPublish(ctx, harnessID, revisionNumber, env.Client.RevisionGetter(harnessID, revisionNumber), resourcewait.OptionsFromDeps(env.Deps))
	if err != nil {
		return nil, err
	}
	data["HarnessRevision"] = final
	result.Text = func(w io.Writer) {
		harnesscli.RenderRevision(w, apivalue.Object(final))
		env.Next("agr harness endpoint %s --check   # new instances use this revision", harnessID)
	}
	return result, nil
}
