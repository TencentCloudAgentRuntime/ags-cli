// Package lookup provides precise instance lookups without live health polling.
package lookup

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/textview"
)

// Module returns the generated lookup contract with variadic IDs and text output.
func Module() command.Module {
	api := APIDescriptor()
	module := apicli.NewModule(api)
	generated := module.Descriptor
	module.Descriptor.Generated = &generated
	module.Descriptor.Source = command.SourceMixedAPI
	module.Descriptor.Spec.Use = "lookup <instance-id> [instance-id...]"
	module.Descriptor.Spec.Args = []command.ArgSpec{{
		Name: "instance-ids", Required: true, Repeatable: true,
		Description: "Sandbox instance IDs to look up (1 to 100).",
	}}
	module.Build = func(deps command.Deps) (command.Runtime, error) {
		builder := apicli.NewRequestBuilder(api)
		executor := apicli.NewExecutor(api, deps.ControlPlane)
		return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
			if req.Flags["request"].Changed && len(req.Args) > 0 {
				return nil, output.NewUsageError("REQUEST_ARG_CONFLICT", "--request cannot be combined with positional instance IDs", "Pass instance IDs as positional arguments, or include InstanceIds in --request.")
			}
			request, err := builder.Build(req)
			if err != nil {
				return nil, err
			}
			failureCode := "INVALID_REQUEST_JSON"
			if len(req.Args) > 0 {
				request["InstanceIds"] = slices.Clone(req.Args)
				failureCode = "INVALID_USAGE"
			}
			if err := validateIDs(request["InstanceIds"], failureCode); err != nil {
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
			if response["InstanceSet"] == nil {
				return nil, fmt.Errorf("lookup response is missing InstanceSet")
			}
			instances, err := response.ReadObjects("InstanceSet")
			if err != nil {
				return nil, err
			}
			for index, instance := range instances {
				if instance.String("InstanceId") == "" {
					return nil, fmt.Errorf("lookup response InstanceSet[%d] is missing a valid InstanceId", index)
				}
			}
			result.Text = func(w io.Writer) { renderInstances(w, instances) }
			return result, nil
		})}, nil
	}
	return module
}

func validateIDs(value any, failureCode string) error {
	invalid := func(message string) error {
		return output.NewUsageError(failureCode, message, "Provide 1 to 100 instance IDs, each non-empty, at most 64 bytes, and without surrounding whitespace.")
	}
	var ids []string
	if err := apivalue.Project(value, &ids); err != nil {
		return invalid("InstanceIds must be an array of strings")
	}
	if len(ids) < 1 || len(ids) > 100 {
		return invalid(fmt.Sprintf("InstanceIds must contain 1 to 100 elements, got %d", len(ids)))
	}
	for index, id := range ids {
		if strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) || len(id) > 64 {
			return invalid(fmt.Sprintf("InstanceIds[%d] must be non-empty, at most 64 bytes, and without surrounding whitespace", index))
		}
	}
	return nil
}

func renderInstances(w io.Writer, instances []apivalue.Object) {
	if len(instances) == 0 {
		fmt.Fprintln(w, "No instances found")
		return
	}
	rows := make([][]string, 0, len(instances))
	for _, instance := range instances {
		rows = append(rows, []string{instance.String("InstanceId"), instance.String("ToolId"), instance.String("Status")})
	}
	textview.PrintTable(w, []string{"ID", "TOOL", "STATUS"}, rows)
}
