package delete

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// ControlPlane is the minimal tool deletion dependency required by the
// workflow. It keeps multi-delete behavior testable without a full SDK client.
type ControlPlane interface {
	DeleteTool(ctx context.Context, toolID string) error
}

// NotFoundClassifier lets the delete waiter distinguish deletion completion
// from other GetTool failures.
type NotFoundClassifier interface {
	IsNotFound(error) bool
}

// Summary aggregates per-tool delete outcomes for text and JSON rendering.
type Summary struct {
	Deleted    int
	Failed     int
	DeletedIDs []string
	FailedIDs  []string
}

// Data converts the summary into the command's canonical JSON shape.
func (s Summary) Data() map[string]any {
	data := map[string]any{"Deleted": s.Deleted, "Failed": s.Failed}
	if len(s.FailedIDs) > 0 {
		data["FailedIds"] = append([]string(nil), s.FailedIDs...)
	}
	return data
}

// Module returns this package's command module.
func Module() command.Module {
	api := APIDescriptor()
	generatedSpec := api.CommandSpec()
	spec := generatedSpec
	spec.Use = "delete <tool-id> [tool-id...]"
	spec.Args = []command.ArgSpec{
		{Name: "tool-id", Required: true, Repeatable: true, Description: "Sandbox tool ID."},
	}
	spec.Flags = append(spec.Flags,
		resourcewait.Flag(),
		command.FlagSpec{
			Name:     "dry-run",
			Usage:    "List resources that would be deleted without actually executing the deletion",
			Type:     command.FlagBool,
			Workflow: true,
		},
		command.FlagSpec{
			Name:      "yes",
			Shorthand: "y",
			Usage:     "Skip confirmation prompt",
			Type:      command.FlagBool,
			Workflow:  true,
		},
	)
	spec.Output = command.OutputSpec{
		DataType:    "DeleteData",
		Description: "Delete result with multi-tool handling.",
	}

	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Generated: &command.Descriptor{
				Spec:   generatedSpec,
				Groups: api.Groups,
				API:    api,
				Source: command.SourceAPICli,
			},
			Groups: api.Groups,
			API:    api,
			Source: command.SourceMixedAPI,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp, ok := deps.ControlPlane.(ControlPlane)
			if !ok {
				return command.Runtime{}, fmt.Errorf("tool.delete requires command.Deps.ControlPlane implementing tool/delete.ControlPlane")
			}
			builder := apicli.NewRequestBuilder(api)
			return command.Runtime{
				Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
					dryRun := isDryRun(req)
					skipConfirm := isYes(req)

					requestMode := requestFlag(req)
					var ids []string
					if requestFlag(req) {
						if len(req.Args) > 1 {
							return nil, output.NewUsageError("REQUEST_FLAG_CONFLICT", "--request only supports a single tool id", "Use --request for one ToolId at a time, or pass multiple positional arguments without --request.")
						}
						apiReq, err := builder.Build(req)
						if err != nil {
							return nil, err
						}
						toolID, _ := apiReq["ToolId"].(string)
						if strings.TrimSpace(toolID) == "" {
							return nil, output.NewUsageError("MISSING_REQUIRED_ARG", "missing tool id", "Provide <tool-id>.")
						}
						ids = []string{toolID}
					} else {
						ids = req.Args
					}

					// --dry-run: preview only, no actual deletion.
					if dryRun {
						return dryRunResult(ids, deps.IO.ErrOut), nil
					}

					// Confirmation prompt (unless --yes, --non-interactive, or non-TTY stdin).
					if !skipConfirm && !cli.NonInteractive() && deps.IO != nil && deps.IO.IsStdinTTY() {
						fmt.Fprintf(deps.IO.ErrOut, "The following tools will be deleted:\n")
						for _, id := range ids {
							fmt.Fprintf(deps.IO.ErrOut, "  - %s\n", id)
						}
						fmt.Fprintf(deps.IO.ErrOut, "\nProceed? [y/N] ")
						var answer string
						if _, err := fmt.Fscanln(deps.IO.In, &answer); err != nil || (answer != "y" && answer != "Y") {
							return &command.Result{
								Data: map[string]any{"Cancelled": true},
								Text: func(w io.Writer) { fmt.Fprintln(w, "Cancelled.") },
							}, nil
						}
					}

					// Execute deletion.
					summary := Summary{}
					var warnings []string
					waitRequested := resourcewait.Requested(req)
					var getter resourcewait.ToolGetter
					var classifier NotFoundClassifier
					if waitRequested {
						var ok bool
						getter, ok = deps.ControlPlane.(resourcewait.ToolGetter)
						if !ok {
							return nil, fmt.Errorf("tool.delete --wait requires GetTool support")
						}
						classifier, ok = deps.ControlPlane.(NotFoundClassifier)
						if !ok {
							return nil, fmt.Errorf("tool.delete --wait requires not-found classification support")
						}
					}

					acceptedIDs := make([]string, 0, len(ids))
					for _, toolID := range ids {
						if err := cp.DeleteTool(ctx, toolID); err != nil {
							if requestMode {
								return nil, err
							}
							summary.Failed++
							summary.FailedIDs = append(summary.FailedIDs, toolID)
							warnings = append(warnings, fmt.Sprintf("failed to delete %s: %v", toolID, err))
							continue
						}
						if waitRequested {
							acceptedIDs = append(acceptedIDs, toolID)
							continue
						}
						summary.Deleted++
						summary.DeletedIDs = append(summary.DeletedIDs, toolID)
					}

					if waitRequested {
						options := resourcewait.OptionsFromDeps(deps)
						options.IsNotFound = classifier.IsNotFound
						for _, toolID := range acceptedIDs {
							_, err := resourcewait.WaitForToolWithPolicy(
								ctx,
								toolID,
								getter.GetTool,
								resourcewait.ToolPolicy(resourcewait.OperationDelete),
								options,
							)
							if err != nil {
								if requestMode {
									return nil, err
								}
								summary.Failed++
								summary.FailedIDs = append(summary.FailedIDs, toolID)
								warnings = append(warnings, fmt.Sprintf("failed waiting for %s to be deleted: %v", toolID, err))
								continue
							}
							summary.Deleted++
							summary.DeletedIDs = append(summary.DeletedIDs, toolID)
						}
					}
					return resultFromSummary(summary, warnings, deps.IO.ErrOut), nil
				}),
			}, nil
		},
	}
}

func resultFromSummary(summary Summary, warnings []string, errOut io.Writer) *command.Result {
	result := &command.Result{
		Data:     summary.Data(),
		Warnings: warnings,
		Text: func(w io.Writer) {
			for _, toolID := range summary.DeletedIDs {
				fmt.Fprintf(w, "Tool deleted: %s\n", toolID)
			}
			for _, warning := range warnings {
				fmt.Fprintf(errOut, "Warning: %s\n", warning)
			}
		},
	}
	if summary.Failed > 0 {
		result.Failure = &output.Failure{
			Code:    "PARTIAL_DELETE_FAILED",
			Kind:    output.KindPartialSuccess,
			Message: "failed to delete one or more tools",
			Hint:    "Inspect Data.FailedIds and retry failed tool IDs.",
		}
		result.ExitCode = output.ExitPartialSuccess
	}
	return result
}

func requestFlag(req command.Request) bool {
	flag, ok := req.Flags["request"]
	return ok && flag.Changed && strings.TrimSpace(flag.String) != ""
}

func isDryRun(req command.Request) bool {
	flag, ok := req.Flags["dry-run"]
	return ok && flag.Changed && flag.Bool
}

func isYes(req command.Request) bool {
	flag, ok := req.Flags["yes"]
	return ok && flag.Changed && flag.Bool
}

func dryRunResult(ids []string, errOut io.Writer) *command.Result {
	return &command.Result{
		Data: map[string]any{
			"DryRun":      true,
			"WouldDelete": ids,
		},
		Text: func(w io.Writer) {
			fmt.Fprintln(w, "Dry run — the following tools would be deleted:")
			for _, id := range ids {
				fmt.Fprintf(w, "  - %s\n", id)
			}
			fmt.Fprintln(w, "\nNo changes were made.")
		},
	}
}
