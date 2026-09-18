package debug

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	requestio "github.com/TencentCloudAgentRuntime/ags-cli/internal/cli/request"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	instanceview "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/instance/internal/instanceview"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/toolcopy"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

const (
	envdMountName     = "envd"
	envdMountPath     = "/envd"
	envdImageRef      = "ccr.ccs.tencentyun.com/ags-image/envd:v0.5.14"
	envdImageSubPath  = "/usr/bin/envd"
	envdRegistryType  = "personal"
	debugPortName     = "envd"
	debugPort         = 49983
	debugPortProtocol = "TCP"
	debugProbeScheme  = "HTTP"
	debugHealthPath   = "/health"
	defaultNameMaxLen = 50
	defaultTimeout    = "1h"
	debugReadyTimeout = 10 * time.Minute
	debugPollInterval = 5 * time.Second
	debugCleanupWait  = 30 * time.Second
)

// ControlPlane supplies the resource operations used by the debug workflow.
type ControlPlane interface {
	GetTool(ctx context.Context, toolID string) (apivalue.Object, error)
	GetInstance(ctx context.Context, instanceID string) (apivalue.Object, error)
	DeleteTool(ctx context.Context, toolID string) error
	DeleteInstance(ctx context.Context, instanceID string) error
	Call(ctx context.Context, action string, request map[string]any) (any, error)
}

// Module returns this package's command module.
func Module() command.Module {
	spec := command.Spec{
		ID:           "instance.debug",
		Path:         []string{"instance", "debug"},
		Use:          "debug",
		Short:        "Create a debug instance from an existing tool",
		Long:         "Create a temporary debug tool from an existing tool, wait for it to be ready, then start a debug instance.",
		SupportsJSON: true,
		Flags: []command.FlagSpec{
			{Name: "tool-id", Usage: "Source sandbox tool ID", Type: command.FlagString, Workflow: true},
			{Name: "tool-name", Shorthand: "t", Usage: "Source sandbox tool name", Type: command.FlagString, Workflow: true},
			{Name: "timeout", Usage: "Instance lifetime timeout for the created debug instance", Type: command.FlagString, Default: defaultTimeout, Workflow: true},
			{Name: "auth-mode", Usage: "Auth mode for the debug instance: DEFAULT, TOKEN, NONE, PUBLIC", Values: []string{"DEFAULT", "TOKEN", "NONE", "PUBLIC"}, Type: command.FlagString, Workflow: true},
			{
				Name:     "mount-options",
				Usage:    "MountOptions as JSON array, @file, or - for stdin",
				Format:   `[{"Name":"<name>","MountPath":"<path>"}]`,
				Examples: []string{`agr instance debug --tool-id sdt-xxxx --mount-options '[{"Name":"data","MountPath":"/workspace"}]'`},
				Type:     command.FlagString, Workflow: true,
			},
			{
				Name:     "custom-configuration",
				Usage:    "CustomConfiguration JSON object for the debug instance, @file, or - for stdin",
				Format:   `{"Env":[{"Name":"KEY","Value":"VAL"}]}`,
				Examples: []string{`agr instance debug --tool-id sdt-xxxx --custom-configuration '{"Env":[{"Name":"MY_VAR","Value":"hello"}]}'`},
				Type:     command.FlagString, Workflow: true,
			},
			{
				Name:     "metadata",
				Usage:    "Metadata as JSON array, @file, or - for stdin",
				Format:   `[{"Name":"<name>","Value":"<value>"}]`,
				Examples: []string{`agr instance debug --tool-id sdt-xxxx --metadata '[{"Name":"env","Value":"debug"}]'`},
				Type:     command.FlagString, Workflow: true,
			},
			{Name: "client-token", Usage: "Client token for duplicate creation protection", Type: command.FlagString, Workflow: true},
		},
		Output: command.OutputSpec{
			DataType:    "DebugInstanceResult",
			Description: "Created debug tool and ready instance details.",
			Effects:     []string{"create:tool", "create:instance"},
		},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Groups: []command.GroupSpec{
				{
					Path:    []string{"instance"},
					Use:     "instance",
					Short:   "Manage sandbox instances",
					Long:    "Manage sandbox instances and related data-plane workflows.",
					Aliases: []string{"i"},
				},
			},
			Source: command.SourceWorkflow,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			cp, ok := deps.ControlPlane.(ControlPlane)
			if !ok {
				return command.Runtime{}, fmt.Errorf("instance.debug requires command.Deps.ControlPlane implementing instance/debug.ControlPlane")
			}
			deps = deps.WithDefaults()
			return command.Runtime{
				Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
					return runDebug(ctx, req, deps, cp)
				}),
			}, nil
		},
	}
}

func runDebug(ctx context.Context, req command.Request, deps command.Deps, cp ControlPlane) (*command.Result, error) {
	toolID := stringFlag(req, "tool-id")
	toolName := stringFlag(req, "tool-name")

	if toolID != "" && toolName != "" {
		return nil, output.NewUsageError("CONFLICTING_FLAGS", "cannot specify both --tool-id and --tool-name", "Provide either --tool-id or --tool-name.")
	}
	if toolID == "" && toolName == "" {
		return nil, output.NewUsageError("MISSING_REQUIRED_FLAG", "must specify either --tool-id or --tool-name/-t", "Provide --tool-id for a tool ID or --tool-name for a tool name.")
	}

	// Resolve tool-name to tool-id via DescribeSandboxToolList.
	sourceToolID := toolID
	if toolName != "" {
		resp, err := cp.Call(ctx, "DescribeSandboxToolList", map[string]any{
			"Filters": []map[string]any{
				{"Name": "ToolName", "Values": []string{toolName}},
			},
		})
		if err != nil {
			return nil, err
		}
		id := resolveToolIDFromList(resp, toolName)
		if id == "" {
			return nil, output.NewNotFoundError("TOOL_NOT_FOUND", fmt.Sprintf("tool not found: %s", toolName), "Run 'agr tool list' to find available tools.")
		}
		sourceToolID = id
	}

	sourceTool, err := cp.GetTool(ctx, sourceToolID)
	if err != nil {
		return nil, err
	}
	if err := validateDebugMountAvailable(sourceTool["StorageMounts"]); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sourceTool.String("RoleArn")) == "" {
		return nil, output.NewUsageError(
			"DEBUG_ROLE_ARN_REQUIRED",
			"source tool must have RoleArn to add the envd image mount",
			"Use a source tool with RoleArn configured because image storage mounts require it.",
		)
	}

	instanceTimeout := stringFlag(req, "timeout")
	if strings.TrimSpace(instanceTimeout) == "" {
		instanceTimeout = defaultTimeout
	}

	// Pre-parse instance flags early to fail fast on invalid JSON before creating resources.
	extraInstanceParams := map[string]any{}
	if v := stringFlag(req, "auth-mode"); v != "" {
		extraInstanceParams["AuthMode"] = v
	}
	for _, flagDef := range []struct {
		flag string
		key  string
	}{
		{"mount-options", "MountOptions"},
		{"custom-configuration", "CustomConfiguration"},
		{"metadata", "Metadata"},
	} {
		if v := stringFlag(req, flagDef.flag); v != "" {
			data, err := requestio.ReadFlagFrom(v, deps.IO.In)
			if err != nil {
				return nil, err
			}
			var parsed any
			if err := json.Unmarshal(data, &parsed); err != nil {
				return nil, output.NewUsageError(
					"INVALID_JSON_FLAG",
					fmt.Sprintf("invalid JSON for --%s: %v", flagDef.flag, err),
					fmt.Sprintf("Provide a valid JSON value for --%s, @file, or - for stdin.", flagDef.flag),
				)
			}
			extraInstanceParams[flagDef.key] = parsed
		}
	}

	debugToolName := defaultDebugToolName(sourceTool.String("ToolName"), sourceToolID, deps.Now())
	description := fmt.Sprintf("Debug tool for %s (%s)", displayToolName(sourceTool, sourceToolID), sourceToolID)

	addedMount := envdMount()
	createReq, err := buildCreateRequest(sourceTool, debugToolName, description, stringFlag(req, "client-token"), addedMount)
	if err != nil {
		return nil, err
	}
	resp, err := cp.Call(ctx, "CreateSandboxTool", createReq)
	if err != nil {
		return nil, err
	}

	debugToolID := responseToolID(resp)
	if strings.TrimSpace(debugToolID) == "" {
		return nil, fmt.Errorf("no tool id returned from CreateSandboxTool")
	}
	if _, err := waitForToolReady(ctx, cp, debugToolID); err != nil {
		cleanupDebugResources(deps, cp, "", debugToolID)
		return nil, err
	}

	startResp, err := cp.Call(ctx, "StartSandboxInstance", func() map[string]any {
		r := map[string]any{
			"ToolId":  debugToolID,
			"Timeout": instanceTimeout,
		}
		for k, v := range extraInstanceParams {
			r[k] = v
		}
		return r
	}())
	if err != nil {
		cleanupDebugResources(deps, cp, "", debugToolID)
		return nil, err
	}
	instanceID, _ := responseInstance(startResp)
	if strings.TrimSpace(instanceID) == "" {
		cleanupDebugResources(deps, cp, "", debugToolID)
		return nil, fmt.Errorf("no instance id returned from StartSandboxInstance")
	}
	instance, err := waitForInstanceRunning(ctx, cp, instanceID, resourcewait.OptionsFromDeps(deps))
	if err != nil {
		cleanupDebugResources(deps, cp, instanceID, debugToolID)
		cliErr := output.ClassifyError(err)
		if cliErr != nil && cliErr.Failure != nil && strings.HasPrefix(cliErr.Failure.Code, "WAIT_") {
			cliErr.Failure.Hint = "Cleanup was attempted for the temporary debug resources. Re-run the same 'agr instance debug' command to retry."
		}
		return nil, err
	}

	connection := connectionData(instanceID)
	data := map[string]any{
		"SourceToolId":   sourceToolID,
		"SourceToolName": sourceTool.String("ToolName"),
		"ToolId":         debugToolID,
		"ToolName":       debugToolName,
		"InstanceId":     instanceID,
		"Instance":       instance,
		"Status":         instance.String("Status"),
		"Timeout":        instanceTimeout,
		"Connection":     connection,
		"Command":        []string{envdMountPath},
		"AddedMount":     addedMount,
	}
	return &command.Result{
		Data: data,
		Effects: []output.Effect{
			{Kind: "create", Resource: "tool", Id: debugToolID},
			{Kind: "create", Resource: "instance", Id: instanceID},
		},
		Text: func(w io.Writer) {
			fmt.Fprintf(w, "Debug instance ready: %s\n", instanceID)
			instanceview.PrintKV(w, []instanceview.KeyValue{
				{Key: "InstanceID", Value: instanceID},
				{Key: "Status", Value: instance.String("Status")},
				{Key: "ToolID", Value: debugToolID},
				{Key: "ToolName", Value: debugToolName},
				{Key: "SourceToolID", Value: sourceToolID},
				{Key: "Timeout", Value: instanceTimeout},
				{Key: "Login", Value: connection["Login"]},
				{Key: "Proxy", Value: connection["Proxy"]},
				{Key: "Command", Value: envdMountPath},
				{Key: "AddedMount", Value: fmt.Sprintf("%s:%s -> %s", envdImageRef, envdImageSubPath, envdMountPath)},
			})
		},
	}, nil
}

func waitForToolReady(ctx context.Context, cp ControlPlane, toolID string) (apivalue.Object, error) {
	waitCtx, cancel := context.WithTimeout(ctx, debugReadyTimeout)
	defer cancel()
	for {
		tool, err := cp.GetTool(waitCtx, toolID)
		if err != nil {
			return nil, err
		}
		status := strings.ToUpper(tool.String("Status"))
		switch status {
		case "ACTIVE", "READY":
			return tool, nil
		case "FAILED":
			return nil, fmt.Errorf("debug tool %s failed to become ready (status: %s)", toolID, status)
		}
		if err := waitBeforeRetry(waitCtx); err != nil {
			return nil, fmt.Errorf("timed out waiting for debug tool %s to become ready: %w", toolID, err)
		}
	}
}

func waitForInstanceRunning(ctx context.Context, cp ControlPlane, instanceID string, options resourcewait.Options) (apivalue.Object, error) {
	return resourcewait.WaitForInstanceWithPolicy(
		ctx,
		instanceID,
		cp.GetInstance,
		resourcewait.InstancePolicy(resourcewait.OperationCreate),
		options,
	)
}

func waitBeforeRetry(ctx context.Context) error {
	timer := time.NewTimer(debugPollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func cleanupDebugResources(deps command.Deps, cp ControlPlane, instanceID, toolID string) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), debugCleanupWait)
	defer cancel()
	if strings.TrimSpace(instanceID) != "" {
		if err := cp.DeleteInstance(cleanupCtx, instanceID); err != nil {
			fmt.Fprintf(deps.IO.ErrOut, "Warning: failed to cleanup debug instance %s: %v\n", instanceID, err)
		}
	}
	if strings.TrimSpace(toolID) != "" {
		if err := cp.DeleteTool(cleanupCtx, toolID); err != nil {
			fmt.Fprintf(deps.IO.ErrOut, "Warning: failed to cleanup debug tool %s: %v\n", toolID, err)
		}
	}
}

func connectionData(instanceID string) map[string]string {
	return map[string]string{
		"Login": fmt.Sprintf("agr instance login %s --user \"YOUR_USER\"", instanceID),
		"Proxy": fmt.Sprintf("agr instance proxy %s", instanceID),
	}
}

func buildCreateRequest(value any, toolName, description, clientToken string, addedMount map[string]any) (map[string]any, error) {
	sourceTool, err := apivalue.Decode(value)
	if err != nil {
		return nil, err
	}
	req, err := toolcopy.Request(sourceTool)
	if err != nil {
		return nil, err
	}
	customConfig, err := customConfigurationRequest(req["CustomConfiguration"])
	if err != nil {
		return nil, err
	}
	storageMounts, err := storageMountsRequest(req["StorageMounts"], addedMount)
	if err != nil {
		return nil, err
	}
	req["ToolName"] = toolName
	req["Description"] = description
	req["StorageMounts"] = storageMounts
	req["CustomConfiguration"] = customConfig
	if strings.TrimSpace(clientToken) != "" {
		req["ClientToken"] = clientToken
	}
	return req, nil
}

func customConfigurationRequest(source any) (map[string]any, error) {
	var custom map[string]any
	if source != nil {
		if err := jsonRoundTrip(source, &custom); err != nil {
			return nil, err
		}
	}
	if custom == nil {
		custom = map[string]any{}
	}
	delete(custom, "ImageDigest")
	custom["Command"] = []string{envdMountPath}
	custom["Args"] = []string{}
	custom["Ports"] = debugPorts()
	custom["Probe"] = debugProbe()
	return custom, nil
}

func debugPorts() []map[string]any {
	return []map[string]any{{
		"Name":     debugPortName,
		"Port":     debugPort,
		"Protocol": debugPortProtocol,
	}}
}

func debugProbe() map[string]any {
	return map[string]any{
		"HttpGet": map[string]any{
			"Path":   debugHealthPath,
			"Port":   debugPort,
			"Scheme": debugProbeScheme,
		},
		"ReadyTimeoutMs":   30000,
		"ProbeTimeoutMs":   2000,
		"ProbePeriodMs":    1000,
		"SuccessThreshold": 1,
		"FailureThreshold": 30,
	}
}

func storageMountsRequest(value any, addedMount map[string]any) ([]map[string]any, error) {
	wrapper, err := apivalue.Decode(map[string]any{"Mounts": value})
	if err != nil {
		return nil, err
	}
	source := wrapper.Objects("Mounts")
	mounts := make([]map[string]any, 0, len(source)+1)
	for _, mount := range source {
		if mount == nil {
			continue
		}
		var item map[string]any
		if err := jsonRoundTrip(mount, &item); err != nil {
			return nil, err
		}
		if storageSource, ok := item["StorageSource"].(map[string]any); ok {
			if image, ok := storageSource["Image"].(map[string]any); ok {
				delete(image, "Digest")
			}
		}
		mounts = append(mounts, item)
	}
	mounts = append(mounts, addedMount)
	return mounts, nil
}

func validateDebugMountAvailable(value any) error {
	wrapper, err := apivalue.Decode(map[string]any{"Mounts": value})
	if err != nil {
		return err
	}
	mounts := wrapper.Objects("Mounts")
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		if strings.EqualFold(mount.String("Name"), envdMountName) || mount.String("MountPath") == envdMountPath {
			return output.NewUsageError(
				"DEBUG_MOUNT_CONFLICT",
				"source tool already uses the debug mount name or path",
				"Choose a source tool that does not define mount name envd or mount path /envd.",
			)
		}
	}
	return nil
}

func envdMount() map[string]any {
	return map[string]any{
		"Name":      envdMountName,
		"MountPath": envdMountPath,
		"ReadOnly":  true,
		"StorageSource": map[string]any{
			"Image": map[string]any{
				"Reference":         envdImageRef,
				"ImageRegistryType": envdRegistryType,
				"SubPath":           envdImageSubPath,
			},
		},
	}
}

// resolveToolIDFromList extracts the ToolId of the first tool matching toolName
// from a DescribeSandboxToolList response.
func resolveToolIDFromList(value any, toolName string) string {
	response, _ := apivalue.Decode(value)
	for _, tool := range response.Objects("SandboxToolSet") {
		if tool.String("ToolName") == toolName {
			return tool.String("ToolId")
		}
	}
	return ""
}

func defaultDebugToolName(sourceName, sourceToolID string, now time.Time) string {
	base := strings.TrimSpace(sourceName)
	if base == "" {
		base = strings.TrimSpace(sourceToolID)
	}
	if base == "" {
		base = "tool"
	}
	suffix := "-debug-" + now.UTC().Format("20060102150405")
	if len(base)+len(suffix) > defaultNameMaxLen {
		base = base[:max(1, defaultNameMaxLen-len(suffix))]
		base = strings.TrimRight(base, "-_")
		if base == "" {
			base = "tool"
		}
	}
	return base + suffix
}

func displayToolName(value any, fallbackID string) string {
	tool, _ := apivalue.Decode(value)
	if name := tool.String("ToolName"); name != "" {
		return name
	}
	return fallbackID
}

func responseToolID(value any) string {
	response, _ := apivalue.Decode(value)
	return response.String("ToolId")
}

func responseInstance(value any) (string, apivalue.Object) {
	response, _ := apivalue.Decode(value)
	instance := response.Object("Instance")
	return instance.String("InstanceId"), instance
}

func jsonRoundTrip(src any, dst any) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return requestio.DecodeJSON(raw, dst)
}

func stringFlag(req command.Request, name string) string {
	flag, ok := req.Flags[name]
	if !ok {
		return ""
	}
	return flag.String
}
