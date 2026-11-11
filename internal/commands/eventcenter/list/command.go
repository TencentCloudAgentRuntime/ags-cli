// Package list queries account- and region-scoped operational events.
package list

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module retains generated request metadata and lossless execution, adding
// query validation and a mode-aware text presentation of the same wire data.
func Module() command.Module {
	api := APIDescriptor()
	spec := api.CommandSpec()
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec, Groups: api.Groups, API: api, Source: command.SourceMixedAPI,
			Generated: &command.Descriptor{Spec: spec, Groups: api.Groups, API: api, Source: command.SourceAPICli},
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			builder := apicli.NewRequestBuilder(api)
			executor := apicli.NewExecutor(api, deps.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				request, err := builder.Build(req)
				if err != nil {
					return nil, err
				}
				if err := validateQuery(request); err != nil {
					return nil, err
				}
				result, err := executor.Execute(ctx, request)
				if err != nil {
					return nil, err
				}
				response, err := apivalue.Decode(result.Data)
				if err != nil {
					return nil, err
				}
				items, err := validateResponse(response)
				if err != nil {
					return nil, err
				}
				mode := response.String("Mode")
				if mode == "" {
					mode, _ = request["Mode"].(string)
				}
				result.Text = func(w io.Writer) { renderList(w, response, items, mode) }
				return result, nil
			})}, nil
		},
	}
}

func invalidQuery(field, problem string) error {
	return output.NewUsageError("INVALID_REQUEST_JSON", field+": "+problem,
		"Run 'agr event-center list --help' for query constraints and examples, or 'agr schema event-center.list -o json' for request fields.")
}

// Validate the assembled payload, so flags and every --request transport have
// identical semantics. Only the service clock decides the rolling retention
// boundary and defaults; omitted members stay omitted on the wire.
func validateQuery(request map[string]any) error {
	catalog, err := apimeta.Get()
	if err != nil {
		return err
	}
	if err := catalog.Spec.ValidateRequest("DescribeEventCenterEvents", request); err != nil {
		return invalidQuery("request", err.Error())
	}
	query, err := apivalue.Decode(request)
	if err != nil {
		return invalidQuery("request", err.Error())
	}
	for _, field := range []struct {
		name     string
		min, max int64
	}{{"Offset", 0, 10000}, {"Limit", 1, 100}} {
		if value, present := query[field.name]; present && !integerInRange(value, field.min, field.max) {
			return invalidQuery(field.name, fmt.Sprintf("must be an integer from %d to %d", field.min, field.max))
		}
	}
	for _, field := range []struct {
		name   string
		values []string
	}{
		{"Mode", []string{"Raw", "Aggregate"}}, {"Order", []string{"Asc", "Desc"}},
	} {
		if value, present := query[field.name]; present {
			text, ok := value.(string)
			if !ok || !slices.Contains(field.values, text) {
				return invalidQuery(field.name, "must be "+strings.Join(field.values, " or ")+" (case-sensitive)")
			}
		}
	}
	times := map[string]time.Time{}
	for _, field := range []string{"StartTime", "EndTime"} {
		if value, present := query[field]; present {
			stamp, ok := value.(string)
			parsed, err := time.Parse(time.RFC3339Nano, stamp)
			if !ok || err != nil {
				return invalidQuery(field, "must be an RFC3339 timestamp with a timezone")
			}
			times[field] = parsed
		}
	}
	start, hasStart := times["StartTime"]
	end, hasEnd := times["EndTime"]
	if hasStart && hasEnd && !start.Before(end) {
		return invalidQuery("StartTime", "must be earlier than EndTime; the window is [StartTime, EndTime)")
	}
	if filters, present := query["Filters"]; present {
		return validateFilters(filters)
	}
	return nil
}

func validateFilters(value any) error {
	filters, ok := value.([]any)
	if !ok || len(filters) > 12 {
		return invalidQuery("Filters", "must be an array of at most 12 conditions")
	}
	seen := map[string]bool{}
	for i, value := range filters {
		field := fmt.Sprintf("Filters[%d]", i)
		filter, ok := value.(map[string]any)
		if !ok {
			return invalidQuery(field, "must contain Name and Values")
		}
		name, _ := filter["Name"].(string)
		switch name {
		case "Source", "ObjectType", "ObjectId", "EventType", "EventLevel", "Reason", "EventId", "ToolId", "ToolType", "Message":
		default:
			return invalidQuery(field+".Name", "use a supported, case-sensitive filter name from 'agr event-center list --help'")
		}
		if seen[name] {
			return invalidQuery(field+".Name", "filter names must not repeat; put values in one condition")
		}
		seen[name] = true
		values, ok := filter["Values"].([]any)
		if !ok || len(values) < 1 || len(values) > 20 {
			return invalidQuery(field+".Values", "must contain 1 to 20 nonempty strings")
		}
		for _, value := range values {
			text, ok := value.(string)
			if !ok || len(text) == 0 || len(text) > 256 {
				return invalidQuery(field+".Values", "each value must be a nonempty string of at most 256 bytes")
			}
		}
	}
	return nil
}

func integerInRange(value any, min, max int64) bool {
	number, ok := value.(json.Number)
	if !ok {
		return false
	}
	rational, ok := new(big.Rat).SetString(string(number))
	return ok && rational.IsInt() && rational.Num().IsInt64() && rational.Num().Int64() >= min && rational.Num().Int64() <= max
}

func validateResponse(response apivalue.Object) ([]apivalue.Object, error) {
	invalid := func(field string) error {
		return fmt.Errorf("DescribeEventCenterEvents: invalid or missing %s in API response", field)
	}
	if response["Events"] == nil || !integerInRange(response["TotalCount"], 0, math.MaxInt64) || response.String("RequestId") == "" {
		return nil, invalid("Events, TotalCount or RequestId")
	}
	items, err := response.ReadObjects("Events")
	if err != nil {
		return nil, err
	}
	if mode, present := response["Mode"]; present && mode != "Raw" && mode != "Aggregate" {
		return nil, invalid("Mode")
	}
	for _, field := range []string{"StartTime", "EndTime"} {
		if value, present := response[field]; present && !validTimestamp(value) {
			return nil, invalid(field)
		}
	}
	for i, item := range items {
		for _, field := range []string{"EventId", "Source", "ObjectType", "ObjectId", "EventLevel", "EventType", "Message"} {
			if _, ok := item[field].(string); !ok || (field == "EventId" && item.String(field) == "") {
				return nil, invalid(fmt.Sprintf("Events[%d].%s", i, field))
			}
		}
		for _, field := range []string{"Reason", "Extensions"} {
			if value, present := item[field]; present {
				if _, ok := value.(string); !ok {
					return nil, invalid(fmt.Sprintf("Events[%d].%s", i, field))
				}
			}
		}
		for _, field := range []string{"OccurredAt", "FirstOccurredAt", "LastOccurredAt"} {
			if value, present := item[field]; present && !validTimestamp(value) {
				return nil, invalid(fmt.Sprintf("Events[%d].%s", i, field))
			}
		}
		if value, present := item["Count"]; present && !integerInRange(value, 1, math.MaxInt64) {
			return nil, invalid(fmt.Sprintf("Events[%d].Count", i))
		}
	}
	return items, nil
}

func validTimestamp(value any) bool {
	stamp, ok := value.(string)
	_, err := time.Parse(time.RFC3339Nano, stamp)
	return ok && err == nil
}

func renderList(w io.Writer, response apivalue.Object, items []apivalue.Object, mode string) {
	if len(items) == 0 {
		fmt.Fprintln(w, "No operational events found")
	} else {
		table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fields := []string{"EventId", "ObjectType", "ObjectId", "EventType", "EventLevel", "Reason"}
		headers := "EVENT ID\tOBJECT TYPE\tOBJECT ID\tEVENT TYPE\tLEVEL\tREASON"
		if mode == "Aggregate" {
			fields = append(fields, "Count", "FirstOccurredAt", "LastOccurredAt")
			headers += "\tCOUNT\tFIRST OCCURRED AT\tLAST OCCURRED AT"
		} else {
			fields = append(fields, "OccurredAt")
			headers += "\tOCCURRED AT"
		}
		fmt.Fprintln(table, headers)
		for _, item := range items {
			cells := make([]string, 0, len(fields))
			for _, field := range fields {
				cell := item.Display(field)
				if cell == "" {
					cell = "-"
				}
				cells = append(cells, strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(cell))
			}
			fmt.Fprintln(table, strings.Join(cells, "\t"))
		}
		_ = table.Flush()
	}
	fmt.Fprintf(w, "Total: %s\n", response.Display("TotalCount"))
	for _, field := range []string{"StartTime", "EndTime", "Mode"} {
		if value := response.String(field); value != "" {
			fmt.Fprintf(w, "%s: %s\n", field, value)
		}
	}
}
