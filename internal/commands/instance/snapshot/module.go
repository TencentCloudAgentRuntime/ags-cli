// Package snapshot owns sandbox snapshot validation and presentation.
package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/textview"
)

// NewModule adds snapshot validation and text output to a generated API module.
func NewModule(api apicli.APIDescriptor) command.Module {
	module := apicli.NewModule(api)
	generated := module.Descriptor
	module.Descriptor.Generated = &generated
	module.Descriptor.Source = command.SourceMixedAPI
	module.Descriptor.Spec.RequiresAuth = true
	module.Build = func(deps command.Deps) (command.Runtime, error) {
		builder := apicli.NewRequestBuilder(api)
		executor := apicli.NewExecutor(api, deps.ControlPlane)
		return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
			request, err := builder.Build(req)
			if err != nil {
				return nil, err
			}
			if api.Spec.ID == "instance.snapshot.list" {
				if err := validatePagination(request); err != nil {
					return nil, err
				}
			}
			result, err := executor.Execute(ctx, request)
			if err != nil {
				return nil, err
			}
			response, err := apivalue.Decode(result.Data)
			if err != nil {
				return nil, err
			}
			switch api.Spec.ID {
			case "instance.snapshot.create":
				snapshot := response.Object("Snapshot")
				if snapshot == nil || snapshot.String("SnapshotId") == "" {
					return nil, output.NewCLIError(&output.Failure{
						Code:    "INTERNAL_ERROR",
						Kind:    output.KindGenericError,
						Message: "snapshot response is missing SnapshotId",
						Hint:    "Rerun with --debug. If the issue persists, inspect the control-plane response.",
					})
				}
				result.Effects = append(result.Effects, output.Effect{Kind: "create", Resource: "snapshot", Id: snapshot.String("SnapshotId")})
				result.Text = func(w io.Writer) { renderSnapshot(w, snapshot) }
			case "instance.snapshot.list":
				snapshots, err := response.ReadObjects("SnapshotSet")
				if err != nil {
					return nil, err
				}
				result.Text = func(w io.Writer) { renderSnapshotList(w, snapshots, response) }
			case "instance.snapshot.delete":
				id, _ := request["SnapshotId"].(string)
				result.Effects = append(result.Effects, output.Effect{Kind: "delete", Resource: "snapshot", Id: id})
				result.Text = func(w io.Writer) { fmt.Fprintf(w, "Snapshot deletion requested: %s\n", id) }
			}
			return result, nil
		})}, nil
	}
	return module
}

func validatePagination(request map[string]any) error {
	next, _ := request["NextToken"].(string)
	offset := positiveInteger(request["Offset"])
	if next != "" && offset {
		return output.NewUsageError("INVALID_PAGINATION", "NextToken cannot be combined with a positive Offset", "Use --next-token without a positive --offset.")
	}
	if positiveInteger(request["MaxResults"]) && (offset || positiveInteger(request["Limit"])) {
		return output.NewUsageError("INVALID_PAGINATION", "positive MaxResults cannot be combined with positive Offset or Limit", "Use --max-results without positive --offset or --limit values.")
	}
	return nil
}

func positiveInteger(value any) bool {
	switch value := value.(type) {
	case int:
		return value > 0
	case int64:
		return value > 0
	case json.Number:
		n, err := value.Int64()
		return err == nil && n > 0
	default:
		return false
	}
}

func renderSnapshot(w io.Writer, snapshot apivalue.Object) {
	fields := []string{"SnapshotId", "SnapshotName", "SourceInstanceId", "MemoryIncluded", "Status", "StatusReason", "Tags", "CreateTime", "UpdateTime"}
	pairs := make([]textview.KeyValue, 0, len(fields))
	for _, field := range fields {
		if _, present := snapshot[field]; present {
			pairs = append(pairs, textview.KeyValue{Key: field, Value: snapshot.Display(field)})
		}
	}
	textview.PrintKV(w, pairs)
}

func renderSnapshotList(w io.Writer, snapshots []apivalue.Object, response apivalue.Object) {
	fields := []string{"SnapshotId", "SnapshotName", "SourceInstanceId", "MemoryIncluded", "Status", "CreateTime"}
	rows := make([][]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		row := make([]string, 0, len(fields))
		for _, field := range fields {
			row = append(row, snapshot.Display(field))
		}
		rows = append(rows, row)
	}
	textview.PrintTable(w, fields, rows)
	if _, present := response["TotalCount"]; present {
		fmt.Fprintf(w, "\nShowing %d of %s snapshots\n", len(snapshots), response.Display("TotalCount"))
	}
	if next := response.String("NextToken"); next != "" {
		fmt.Fprintf(w, "NextToken: %s\n", next)
	}
}
