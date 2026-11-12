//go:build preview

// Package status summarizes what a harness is serving and what to do next.
package status

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/harness/get"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/textview"
)

// Module returns the read-only status workflow.
func Module() command.Module {
	spec := command.Spec{
		ID:    "harness.status",
		Path:  []string{"harness", "status"},
		Use:   "status <harness-id>",
		Short: "Show the live revision, pending publish and next step of a harness",
		Long: "Summarize a harness in one read-only call: the PUBLISHED revision new instances use, the newest revision and its publish status, whether the draft differs from the live revision, and the suggested next command.\n\n" +
			"Data.Next is a command string for agents; it is empty when nothing needs attention. Available in preview builds.",
		Examples: []string{
			"Example - Check a harness:\n  agr harness status hns-a1b2c3d4 -o text",
			"Example - Read the suggested next command:\n  agr harness status hns-a1b2c3d4 -o json --jq '.Data.Next'",
		},
		SupportsJSON: true,
		Args:         []command.ArgSpec{{Name: "harness-id", Required: true, Description: "Harness ID, such as hns-a1b2c3d4."}},
		Output:       command.OutputSpec{DataType: "HarnessStatusData", Description: "Harness, live and latest revisions, draft drift and next command.", Effects: []string{"read:harness"}},
	}
	return command.Module{
		Descriptor: command.Descriptor{Spec: spec, Groups: harnesscli.GroupsOf(get.APIDescriptor(), 1), Source: command.SourceWorkflow},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			client, err := harnesscli.NewClient(deps, spec.ID)
			if err != nil {
				return command.Runtime{}, err
			}
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				return run(ctx, client, req.ArgValues["harness-id"])
			})}, nil
		},
	}
}

func run(ctx context.Context, client harnesscli.Client, harnessID string) (*command.Result, error) {
	harness, err := client.Harness(ctx, harnessID)
	if err != nil {
		return nil, err
	}
	revisions, err := client.Revisions(ctx, harnessID)
	if err != nil {
		return nil, err
	}
	live, latest := harnesscli.LiveRevision(revisions)
	var draftChanges []harnesscli.Change
	if live != nil {
		draftChanges = harnesscli.DiffSpecs(live.Object("Spec"), harness.Object("Spec"), true)
	}
	next := nextStep(harness, live, latest, draftChanges)
	data := map[string]any{
		"Harness":        harness,
		"LiveRevision":   nullable(live),
		"LatestRevision": nullable(latest),
		"DraftChanged":   len(draftChanges) > 0,
		"DraftChanges":   draftChanges,
		"Next":           next,
	}
	return &command.Result{Data: data, Text: func(w io.Writer) { render(w, harness, live, latest, draftChanges, next) }}, nil
}

func nullable(value apivalue.Object) any {
	if value == nil {
		return nil
	}
	return value
}

func nextStep(harness, live, latest apivalue.Object, draftChanges []harnesscli.Change) string {
	id := harness.String("HarnessId")
	switch harness.String("Status") {
	case "DELETING", "DELETE_FAILED":
		return fmt.Sprintf("agr harness delete %s --yes --wait", id)
	}
	if latest != nil {
		switch latest.String("Status") {
		case "PUBLISHING", "PUBLISH_FAILED", "UNPUBLISHED":
			return fmt.Sprintf("agr harness revision publish %s %d --wait", id, latest.Int64("Revision"))
		}
	}
	if live == nil {
		return fmt.Sprintf("agr harness revision create %s --publish --wait", id)
	}
	if len(draftChanges) > 0 {
		return fmt.Sprintf("agr harness revision create %s --publish --wait", id)
	}
	return ""
}

func revisionLabel(revision apivalue.Object) string {
	if revision == nil {
		return "-"
	}
	label := fmt.Sprintf("revision %d (%s)  %s  %s", revision.Int64("Revision"), revision.String("HarnessRevisionName"),
		revision.String("Status"), harnesscli.EngineLabel(revision.Object("Spec")))
	if reason := revision.String("StatusReason"); reason != "" {
		label += ": " + reason
	}
	return label
}

func render(w io.Writer, harness, live, latest apivalue.Object, draftChanges []harnesscli.Change, next string) {
	resources := harness.Object("ResourceInfo")
	draft := "same as live revision"
	switch {
	case live == nil:
		draft = "no live revision to compare"
	case len(draftChanges) > 0:
		draft = fmt.Sprintf("%d change(s) not yet published; see 'agr harness revision diff %s live draft'", len(draftChanges), harness.String("HarnessId"))
	}
	rows := []textview.KeyValue{
		{Key: "Harness", Value: fmt.Sprintf("%s (%s)  %s", harness.String("HarnessId"), harness.String("HarnessName"), harness.String("Status"))},
		{Key: "Live", Value: revisionLabel(live)},
	}
	if latest != nil && (live == nil || latest.Int64("Revision") != live.Int64("Revision")) {
		rows = append(rows, textview.KeyValue{Key: "Latest", Value: revisionLabel(latest)})
	}
	rows = append(rows,
		textview.KeyValue{Key: "Draft", Value: draft},
		textview.KeyValue{Key: "Resources", Value: fmt.Sprintf("tool=%s deployment=%s", dash(resources.String("ToolId")), dash(resources.String("DeploymentId")))},
	)
	if reason := harness.String("StatusReason"); reason != "" {
		rows = append(rows, textview.KeyValue{Key: "Reason", Value: reason})
	}
	if next != "" {
		rows = append(rows, textview.KeyValue{Key: "Next", Value: next})
	}
	textview.PrintKV(w, rows)
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
