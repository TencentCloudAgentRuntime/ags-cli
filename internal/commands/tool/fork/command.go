package fork

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/toolcopy"
	toolcreate "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/tool/create"
	toolget "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/tool/get"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// ControlPlane supplies the source lookup and create call used by tool fork.
type ControlPlane interface {
	GetTool(ctx context.Context, toolID string) (apivalue.Object, error)
	Call(ctx context.Context, action string, request map[string]any) (any, error)
}

// Module returns this package's command module.
func Module() command.Module {
	api := forkAPIDescriptor()
	spec := api.CommandSpec()
	spec.Flags = append(spec.Flags, resourcewait.Flag())
	for i := range spec.Flags {
		if spec.Flags[i].Name == "tool-name" {
			spec.Flags[i].Required = true
		}
	}
	groups := []command.GroupSpec{
		{Path: []string{"tool"}, Use: "tool", Short: "Manage sandbox tools", Long: "Manage sandbox tools (templates). Tools define the type and capabilities of sandbox instances.", Aliases: []string{"t"}},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec:   spec,
			Groups: groups,
			API:    api,
			Source: command.SourceWorkflow,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			cp, ok := deps.ControlPlane.(ControlPlane)
			if !ok {
				return command.Runtime{}, fmt.Errorf("tool.fork requires command.Deps.ControlPlane implementing tool/fork.ControlPlane")
			}
			builder := apicli.NewRequestBuilder(api)
			return command.Runtime{
				Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
					sourceID := sourceToolID(req)
					if strings.TrimSpace(sourceID) == "" {
						return nil, output.NewUsageError("MISSING_REQUIRED_ARG", "missing source tool id", "Provide <source-tool-id>.")
					}
					overrides, err := builder.Build(req)
					if err != nil {
						return nil, err
					}
					applyExplicitEmptyStringOverrides(overrides, req, api.Fields)
					if strings.TrimSpace(stringValue(overrides["ToolName"])) == "" {
						return nil, output.NewUsageError("MISSING_REQUIRED_FLAG", "tool name (-n/--tool-name) is required", "Provide a non-empty value for --tool-name.")
					}
					source, err := cp.GetTool(ctx, sourceID)
					if err != nil {
						return nil, err
					}
					createReq, err := toolcopy.Request(source)
					if err != nil {
						return nil, err
					}
					for key, value := range overrides {
						createReq[key] = value
					}
					result, err := cp.Call(ctx, "CreateSandboxTool", createReq)
					if err != nil {
						return nil, err
					}
					if createdToolID(result) == "" {
						return nil, fmt.Errorf("create response is missing ToolId")
					}
					mutationResult := createResult(result, req)
					if !resourcewait.Requested(req) {
						return mutationResult, nil
					}
					toolID := createdToolID(result)
					if toolID == "" {
						return nil, output.NewCLIError(&output.Failure{
							Code:    "INTERNAL_ERROR",
							Kind:    output.KindGenericError,
							Message: "cannot wait because the create response did not include a tool id",
							Hint:    "Rerun with --debug. If the issue persists, inspect the control-plane response.",
						})
					}
					tool, err := resourcewait.WaitForToolWithPolicy(
						ctx,
						toolID,
						cp.GetTool,
						resourcewait.ToolPolicy(resourcewait.OperationFork),
						resourcewait.OptionsFromDeps(deps),
					)
					if err != nil {
						return nil, err
					}
					return resourcewait.PreserveMutationMetadata(toolget.Result(tool), mutationResult), nil
				}),
			}, nil
		},
	}
}

func forkAPIDescriptor() apicli.APIDescriptor {
	return forkDescriptor(toolcreate.APIDescriptor())
}

func forkDescriptor(createAPI apicli.APIDescriptor) apicli.APIDescriptor {
	// The channel descriptor owns field presence, parsing and flag names. Fork
	// changes only required/default behavior and compatible editorial help.
	helpFields := forkHelpFields()
	createAPI.Fields = slices.Clone(createAPI.Fields)
	for i := range createAPI.Fields {
		field := &createAPI.Fields[i]
		field.Required = field.Name == "ToolName"
		field.Inputs = slices.Clone(field.Inputs)
		for j := range field.Inputs {
			input := &field.Inputs[j]
			input.Default = nil
			input.SendDefault = false
			for _, help := range helpFields {
				if help.Name != field.Name || help.Parser != field.Parser {
					continue
				}
				for _, editorial := range help.Inputs {
					if editorial.Flag == input.Flag && editorial.Type == input.Type {
						input.Usage = editorial.Usage
						if editorial.Format != "" {
							input.Format = editorial.Format
						}
						if len(editorial.Examples) > 0 {
							input.Examples = slices.Clone(editorial.Examples)
						}
					}
				}
			}
		}
	}
	createAPI.Spec = command.Spec{
		ID:           "tool.fork",
		Path:         []string{"tool", "fork"},
		Use:          "fork <source-tool-id>",
		Short:        "Fork a sandbox tool",
		Long:         "Create a new sandbox tool by copying create-capable settings from an existing tool. Override copied settings with create-like flags.",
		Examples:     []string{`agr tool fork sdt-xxxx --tool-name my-copy`, `agr tool fork sdt-xxxx -n my-copy --description "copied for testing" --persistent=false`},
		SupportsJSON: true,
		Args: []command.ArgSpec{
			{Name: "source-tool-id", Required: true, Description: "Source sandbox tool ID."},
		},
		Output: command.OutputSpec{
			DataType:    "CreateSandboxToolResponse",
			Description: "Sandbox tool create response.",
			Effects:     []string{"create:tool"},
		},
	}
	createAPI.DisableRequestFlag = true
	return createAPI
}

// forkHelpFields supplies editorial overrides only; it never adds fields or inputs.
func forkHelpFields() []apicli.FieldSpec {
	return []apicli.FieldSpec{
		{Name: "ToolName", Parser: "common.default_string", Required: true, Inputs: []apicli.InputSpec{{Name: "tool-name", Flag: "tool-name", Shorthand: "n", Usage: "New tool name (required)", Type: command.FlagString}}},
		{Name: "ToolType", Parser: "common.default_string", Inputs: []apicli.InputSpec{{Name: "tool-type", Flag: "tool-type", Shorthand: "t", Usage: "Override tool type", Type: command.FlagString}}},
		{Name: "NetworkConfiguration", Parser: "common.default_json", Inputs: []apicli.InputSpec{{Name: "network-configuration", Flag: "network-configuration", Usage: "Override NetworkConfiguration as JSON object, @file, or - for stdin", Type: command.FlagString}}},
		{Name: "Description", Parser: "common.default_string", Inputs: []apicli.InputSpec{{Name: "description", Flag: "description", Shorthand: "d", Usage: "Override tool description", Type: command.FlagString}}},
		{Name: "DefaultTimeout", Parser: "common.default_string", Inputs: []apicli.InputSpec{{Name: "default-timeout", Flag: "default-timeout", Usage: "Override default timeout, for example 5m, 300s, or 1h", Type: command.FlagString}}},
		{Name: "Tags", Parser: "common.default_json", Inputs: []apicli.InputSpec{{Name: "tags", Flag: "tags", Usage: "Override tags as JSON array, @file, or - for stdin", Type: command.FlagString}}},
		{Name: "ClientToken", Parser: "common.default_string", Inputs: []apicli.InputSpec{{Name: "client-token", Flag: "client-token", Usage: "Client token for duplicate creation protection", Type: command.FlagString}}},
		{Name: "RoleArn", Parser: "common.default_string", Inputs: []apicli.InputSpec{{Name: "role-arn", Flag: "role-arn", Usage: "Override role ARN for COS access", Type: command.FlagString}}},
		{Name: "StorageMounts", Parser: "common.default_json", Inputs: []apicli.InputSpec{{Name: "storage-mounts", Flag: "storage-mounts", Usage: "Override StorageMounts as JSON array, @file, or - for stdin", Type: command.FlagString}}},
		{Name: "CustomConfiguration", Parser: "common.default_json", Inputs: []apicli.InputSpec{{Name: "custom-configuration", Flag: "custom-configuration", Usage: "Override CustomConfiguration JSON object, @file, or - for stdin", Type: command.FlagString}}},
		{Name: "ComputerConfiguration", Parser: "common.default_json", Inputs: []apicli.InputSpec{{Name: "computer-configuration", Flag: "computer-configuration", Usage: "Override ComputerConfiguration JSON object, @file, or - for stdin", Format: "{\"OSWorldConfiguration\":{\"Version\":\"osworld2\"}}", Examples: []string{"agr tool fork sdt-xxxx -n my-copy --region ap-guangzhou --custom-configuration '{\"Resources\":{\"CPU\":\"4\",\"Memory\":\"8Gi\",\"Storage\":\"16Gi\"}}' --computer-configuration '{\"OSWorldConfiguration\":{\"Version\":\"osworld2\"}}'"}, Type: command.FlagString}}},
		{Name: "LogConfiguration", Parser: "common.default_json", Inputs: []apicli.InputSpec{{Name: "log-configuration", Flag: "log-configuration", Usage: "Override LogConfiguration JSON object, @file, or - for stdin", Type: command.FlagString}}},
		{Name: "Persistent", Parser: "common.default_bool", Inputs: []apicli.InputSpec{{Name: "persistent", Flag: "persistent", Usage: "Override whether the sandbox tool creates persistent sandboxes", Type: command.FlagBool}}},
	}
}

func sourceToolID(req command.Request) string {
	sourceID := req.ArgValues["source-tool-id"]
	if sourceID == "" && len(req.Args) > 0 {
		sourceID = req.Args[0]
	}
	return sourceID
}

func applyExplicitEmptyStringOverrides(overrides map[string]any, req command.Request, fields []apicli.FieldSpec) {
	for _, field := range fields {
		if field.Parser != "common.default_string" {
			continue
		}
		for _, input := range field.Inputs {
			if input.Positional {
				continue
			}
			value, ok := req.Flags[input.Flag]
			if ok && value.Changed && value.String == "" {
				overrides[field.Name] = ""
			}
		}
	}
}

func baseCreateRequestFromTool(value any) map[string]any {
	req, _ := toolcopy.Request(value)
	return req
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func createResult(data any, req command.Request) *command.Result {
	toolID := createdToolID(data)
	result := &command.Result{
		Data:    data,
		Effects: []output.Effect{{Kind: "create", Resource: "tool", Id: toolID}},
	}
	result.Text = func(w io.Writer) {
		fmt.Fprintf(w, "Tool created: %s\n", toolID)
		printKV(w, []kv{
			{key: "ID", value: toolID},
			{key: "Name", value: stringValueFromFlag(req, "tool-name")},
			{key: "Type", value: stringValueFromFlag(req, "tool-type")},
			{key: "Description", value: stringValueFromFlag(req, "description")},
		})
	}
	return result
}

func createdToolID(data any) string {
	switch v := data.(type) {
	case *ags.CreateSandboxToolResponseParams:
		if v.ToolId != nil {
			return *v.ToolId
		}
	case map[string]any:
		if s, ok := v["ToolId"].(string); ok {
			return s
		}
	}
	return ""
}

type kv struct {
	key   string
	value string
}

func printKV(w io.Writer, pairs []kv) {
	for _, pair := range pairs {
		if pair.value == "" {
			continue
		}
		fmt.Fprintf(w, "%-14s %s\n", pair.key+":", pair.value)
	}
}

func stringValueFromFlag(req command.Request, name string) string {
	flag, ok := req.Flags[name]
	if !ok {
		return ""
	}
	return flag.String
}
