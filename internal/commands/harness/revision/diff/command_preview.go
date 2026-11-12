//go:build preview

// Package diff compares harness revisions and the draft.
package diff

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/harness/revision/get"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module returns the read-only diff workflow.
func Module() command.Module {
	spec := command.Spec{
		ID:    "harness.revision.diff",
		Path:  []string{"harness", "revision", "diff"},
		Use:   "diff <harness-id> <from> <to>",
		Short: "Compare two revisions, or a revision and the draft",
		Long: "Show field-level differences between two harness configurations. Each side is a revision number, 'live' (the PUBLISHED revision), 'latest' (the newest revision) or 'draft' (the editable configuration). Comparing with 'draft' shows what the next 'revision create' would freeze.\n\n" +
			"Between two revisions, a changed model provider pin (provider@revision) is reported because it changes the runtime configuration. The draft has no pins, so they are ignored when one side is 'draft'. Differences do not change the exit code; read Data.Changed. Available in preview builds.",
		Examples: []string{
			"Example - What would the next publish change:\n  agr harness revision diff hns-a1b2c3d4 live draft -o text",
			"Example - Compare two revisions:\n  agr harness revision diff hns-a1b2c3d4 1 2 -o text",
			"Example - Fail a script when the draft is unpublished:\n  agr harness revision diff hns-a1b2c3d4 live draft -o json --jq '.Data.Changed'",
		},
		SupportsJSON: true,
		Args: []command.ArgSpec{
			{Name: "harness-id", Required: true, Description: "Harness ID, such as hns-a1b2c3d4."},
			{Name: "from", Required: true, Description: "Revision number, live, latest or draft."},
			{Name: "to", Required: true, Description: "Revision number, live, latest or draft."},
		},
		Output: command.OutputSpec{DataType: "HarnessRevisionDiffData", Description: "Resolved sides, Changed flag and field-level changes.", Effects: []string{"read:harness"}},
	}
	return command.Module{
		Descriptor: command.Descriptor{Spec: spec, Groups: harnesscli.GroupsOf(get.APIDescriptor(), 2), Source: command.SourceWorkflow},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			client, err := harnesscli.NewClient(deps, spec.ID)
			if err != nil {
				return command.Runtime{}, err
			}
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				return run(ctx, client, req.ArgValues["harness-id"], req.ArgValues["from"], req.ArgValues["to"])
			})}, nil
		},
	}
}

type side struct {
	Label    string
	Revision int64
	Spec     apivalue.Object
	Draft    bool
}

func run(ctx context.Context, client harnesscli.Client, harnessID, fromRef, toRef string) (*command.Result, error) {
	resolver := &resolver{ctx: ctx, client: client, harnessID: harnessID}
	from, err := resolver.resolve(fromRef)
	if err != nil {
		return nil, err
	}
	to, err := resolver.resolve(toRef)
	if err != nil {
		return nil, err
	}
	changes := harnesscli.DiffSpecs(from.Spec, to.Spec, from.Draft || to.Draft)
	data := map[string]any{
		"HarnessId": harnessID,
		"From":      sideData(from),
		"To":        sideData(to),
		"Changed":   len(changes) > 0,
		"Changes":   changes,
	}
	return &command.Result{Data: data, Text: func(w io.Writer) {
		if len(changes) == 0 {
			fmt.Fprintf(w, "No differences between %s and %s.\n", from.Label, to.Label)
			return
		}
		fmt.Fprintf(w, "%s -> %s: %d change(s)\n", from.Label, to.Label, len(changes))
		for _, change := range changes {
			fmt.Fprintln(w, harnesscli.FormatChange(change))
		}
	}}, nil
}

func sideData(s side) map[string]any {
	data := map[string]any{"Label": s.Label, "Draft": s.Draft}
	if !s.Draft {
		data["Revision"] = s.Revision
	}
	return data
}

type resolver struct {
	ctx       context.Context
	client    harnesscli.Client
	harnessID string
	revisions []apivalue.Object
}

func (r *resolver) list() ([]apivalue.Object, error) {
	if r.revisions == nil {
		revisions, err := r.client.Revisions(r.ctx, r.harnessID)
		if err != nil {
			return nil, err
		}
		r.revisions = revisions
	}
	return r.revisions, nil
}

func (r *resolver) resolve(ref string) (side, error) {
	switch strings.ToLower(strings.TrimSpace(ref)) {
	case "draft":
		harness, err := r.client.Harness(r.ctx, r.harnessID)
		if err != nil {
			return side{}, err
		}
		return side{Label: "draft", Spec: harness.Object("Spec"), Draft: true}, nil
	case "live", "latest":
		revisions, err := r.list()
		if err != nil {
			return side{}, err
		}
		live, latest := harnesscli.LiveRevision(revisions)
		pick := latest
		if strings.EqualFold(ref, "live") {
			pick = live
		}
		if pick == nil {
			return side{}, output.NewNotFoundError("REVISION_NOT_FOUND", fmt.Sprintf("harness %s has no %s revision", r.harnessID, strings.ToLower(ref)),
				fmt.Sprintf("List revisions with 'agr harness revision list %s'.", r.harnessID))
		}
		return fromRevision(strings.ToLower(ref), pick), nil
	}
	number, err := strconv.ParseInt(ref, 10, 64)
	if err != nil || number < 1 {
		return side{}, output.NewUsageError("INVALID_REVISION_REF", fmt.Sprintf("%q is not a revision number, live, latest or draft", ref),
			"Example: agr harness revision diff <harness-id> live draft")
	}
	revision, err := r.client.Revision(r.ctx, r.harnessID, number)
	if err != nil {
		return side{}, err
	}
	return fromRevision("", revision), nil
}

func fromRevision(alias string, revision apivalue.Object) side {
	number := revision.Int64("Revision")
	label := fmt.Sprintf("revision %d", number)
	if alias != "" {
		label = fmt.Sprintf("%s (revision %d)", alias, number)
	}
	return side{Label: label, Revision: number, Spec: revision.Object("Spec")}
}
