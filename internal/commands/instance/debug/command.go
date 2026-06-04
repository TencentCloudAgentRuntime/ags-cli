package debug

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	instanceview "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/instance/internal/instanceview"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/tooltags"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
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
	GetTool(ctx context.Context, toolID string) (*ags.SandboxTool, error)
	GetInstance(ctx context.Context, instanceID string) (*ags.SandboxInstance, error)
	DeleteTool(ctx context.Context, toolID string) error
	DeleteInstance(ctx context.Context, instanceID string) error
	Call(ctx context.Context, action string, request map[string]any) (any, error)
}

// Module returns this package's command module.
func Module() command.Module {
	spec := command.Spec{
		ID:           "instance.debug",
		Path:         []string{"instance", "debug"},
		Use:          "debug <tool-id>",
		Short:        "Create a debug instance from an existing tool",
		Long:         "Create a temporary debug tool from an existing tool, wait for it to be ready, then start a debug instance.",
		SupportsJSON: true,
		Args: []command.ArgSpec{
			{Name: "tool-id", Required: true, Description: "Source sandbox tool ID."},
		},
		Flags: []command.FlagSpec{
			{Name: "timeout", Usage: "Instance lifetime timeout for the created debug instance", Type: command.FlagString, Default: defaultTimeout, Workflow: true},
			{Name: "auth-mode", Usage: "Auth mode for the debug instance: DEFAULT, TOKEN, NONE, PUBLIC", Values: []string{"DEFAULT", "TOKEN", "NONE", "PUBLIC"}, Type: command.FlagString, Workflow: true},
			{
				Name: "mount-options", Usage: "MountOptions as JSON array, @file, or - for stdin",
				Format:   `[{"Name":"<name>","MountPath":"<path>"}]`,
				Examples: []string{`agr instance debug sdt-xxxx --mount-options '[{"Name":"data","MountPath":"/workspace"}]'`},
				Type:     command.FlagString, Workflow: true,
			},
			{
				Name:     "custom-configuration",
				Usage:    "CustomConfiguration JSON object for the debug instance, @file, or - for stdin",
				Format:   `{"Env":[{"Name":"KEY","Value":"VAL"}]}`,
				Examples: []string{`agr instance debug sdt-xxxx --custom-configuration '{"Env":[{"Name":"MY_VAR","Value":"hello"}]}'`},
				Type:     command.FlagString, Workflow: true,
			},
			{
				Name:     "metadata",
				Usage:    "Metadata as JSON array, @file, or - for stdin",
				Format:   `[{"Key":"<key>","Value":"<value>"}]`,
				Examples: []string{`agr instance debug sdt-xxxx --metadata '[{"Key":"env","Value":"debug"}]'`},
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
			Source: "workflow",
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
	sourceToolID := req.ArgValues["tool-id"]
	if sourceToolID == "" && len(req.Args) > 0 {
		sourceToolID = req.Args[0]
	}
	if strings.TrimSpace(sourceToolID) == "" {
		return nil, output.NewUsageError("MISSING_REQUIRED_ARG", "missing tool id", "Provide <tool-id>.")
	}

	sourceTool, err := cp.GetTool(ctx, sourceToolID)
	if err != nil {
		return nil, err
	}
	if err := validateDebugMountAvailable(sourceTool.StorageMounts); err != nil {
		return nil, err
	}
	if sourceTool.RoleArn == nil || strings.TrimSpace(*sourceTool.RoleArn) == "" {
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
	if v := stringFlag(req, "mount-options"); v != "" {
		var parsed any
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			return nil, fmt.Errorf("invalid JSON for --mount-options: %w", err)
		}
		extraInstanceParams["MountOptions"] = parsed
	}
	if v := stringFlag(req, "custom-configuration"); v != "" {
		var parsed any
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			return nil, fmt.Errorf("invalid JSON for --custom-configuration: %w", err)
		}
		extraInstanceParams["CustomConfiguration"] = parsed
	}
	if v := stringFlag(req, "metadata"); v != "" {
		var parsed any
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			return nil, fmt.Errorf("invalid JSON for --metadata: %w", err)
		}
		extraInstanceParams["Metadata"] = parsed
	}

	debugToolName := defaultDebugToolName(derefString(sourceTool.ToolName), sourceToolID, deps.Now())
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

	toolID := responseToolID(resp)
	if strings.TrimSpace(toolID) == "" {
		return nil, fmt.Errorf("no tool id returned from CreateSandboxTool")
	}
	if _, err := waitForToolReady(ctx, cp, toolID); err != nil {
		cleanupDebugResources(deps, cp, "", toolID)
		return nil, err
	}

	startResp, err := cp.Call(ctx, "StartSandboxInstance", func() map[string]any {
		r := map[string]any{
			"ToolId":  toolID,
			"Timeout": instanceTimeout,
		}
		for k, v := range extraInstanceParams {
			r[k] = v
		}
		return r
	}())
	if err != nil {
		cleanupDebugResources(deps, cp, "", toolID)
		return nil, err
	}
	instanceID, _ := responseInstance(startResp)
	if strings.TrimSpace(instanceID) == "" {
		cleanupDebugResources(deps, cp, "", toolID)
		return nil, fmt.Errorf("no instance id returned from StartSandboxInstance")
	}
	instance, err := waitForInstanceRunning(ctx, cp, instanceID)
	if err != nil {
		cleanupDebugResources(deps, cp, instanceID, toolID)
		return nil, err
	}

	connection := connectionData(instanceID)
	data := map[string]any{
		"SourceToolId":   sourceToolID,
		"SourceToolName": derefString(sourceTool.ToolName),
		"ToolId":         toolID,
		"ToolName":       debugToolName,
		"InstanceId":     instanceID,
		"Instance":       instance,
		"Status":         derefString(instance.Status),
		"Timeout":        instanceTimeout,
		"Connection":     connection,
		"Command":        []string{envdMountPath},
		"AddedMount":     addedMount,
	}
	return &command.Result{
		Data: data,
		Effects: []output.Effect{
			{Kind: "create", Resource: "tool", Id: toolID},
			{Kind: "create", Resource: "instance", Id: instanceID},
		},
		Text: func(w io.Writer) {
			fmt.Fprintf(w, "Debug instance ready: %s\n", instanceID)
			instanceview.PrintKV(w, []instanceview.KeyValue{
				{Key: "InstanceID", Value: instanceID},
				{Key: "Status", Value: derefString(instance.Status)},
				{Key: "ToolID", Value: toolID},
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

func waitForToolReady(ctx context.Context, cp ControlPlane, toolID string) (*ags.SandboxTool, error) {
	waitCtx, cancel := context.WithTimeout(ctx, debugReadyTimeout)
	defer cancel()
	for {
		tool, err := cp.GetTool(waitCtx, toolID)
		if err != nil {
			return nil, err
		}
		status := strings.ToUpper(derefString(tool.Status))
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

func waitForInstanceRunning(ctx context.Context, cp ControlPlane, instanceID string) (*ags.SandboxInstance, error) {
	waitCtx, cancel := context.WithTimeout(ctx, debugReadyTimeout)
	defer cancel()
	for {
		instance, err := cp.GetInstance(waitCtx, instanceID)
		if err != nil {
			return nil, err
		}
		status := strings.ToUpper(derefString(instance.Status))
		switch status {
		case "RUNNING":
			return instance, nil
		case "FAILED", "STOPPED", "STOP_FAILED":
			return nil, fmt.Errorf("debug instance %s failed to become ready (status: %s)", instanceID, status)
		}
		if err := waitBeforeRetry(waitCtx); err != nil {
			return nil, fmt.Errorf("timed out waiting for debug instance %s to become ready: %w", instanceID, err)
		}
	}
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

func buildCreateRequest(sourceTool *ags.SandboxTool, toolName, description, clientToken string, addedMount map[string]any) (map[string]any, error) {
	customConfig, err := customConfigurationRequest(sourceTool.CustomConfiguration)
	if err != nil {
		return nil, err
	}
	storageMounts, err := storageMountsRequest(sourceTool.StorageMounts, addedMount)
	if err != nil {
		return nil, err
	}

	req := map[string]any{
		"ToolName":             toolName,
		"ToolType":             derefString(sourceTool.ToolType),
		"NetworkConfiguration": sourceTool.NetworkConfiguration,
		"Description":          description,
		"StorageMounts":        storageMounts,
		"CustomConfiguration":  customConfig,
	}
	if tags := tooltags.FilterInheritedTags(sourceTool.Tags); len(tags) > 0 {
		req["Tags"] = tags
	}
	if sourceTool.RoleArn != nil && *sourceTool.RoleArn != "" {
		req["RoleArn"] = *sourceTool.RoleArn
	}
	if sourceTool.LogConfiguration != nil {
		req["LogConfiguration"] = sourceTool.LogConfiguration
	}
	if sourceTool.Persistent != nil {
		req["Persistent"] = *sourceTool.Persistent
	}
	if sourceTool.DefaultTimeoutSeconds != nil {
		req["DefaultTimeout"] = fmt.Sprintf("%ds", *sourceTool.DefaultTimeoutSeconds)
	}
	if strings.TrimSpace(clientToken) != "" {
		req["ClientToken"] = clientToken
	}
	return req, nil
}

func customConfigurationRequest(source *ags.CustomConfigurationDetail) (map[string]any, error) {
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

func storageMountsRequest(source []*ags.StorageMount, addedMount map[string]any) ([]map[string]any, error) {
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

func validateDebugMountAvailable(mounts []*ags.StorageMount) error {
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		if strings.EqualFold(derefString(mount.Name), envdMountName) || derefString(mount.MountPath) == envdMountPath {
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

func displayToolName(tool *ags.SandboxTool, fallbackID string) string {
	if name := derefString(tool.ToolName); name != "" {
		return name
	}
	return fallbackID
}

func responseToolID(resp any) string {
	switch value := resp.(type) {
	case *ags.CreateSandboxToolResponseParams:
		return derefString(value.ToolId)
	case map[string]any:
		return fmt.Sprint(value["ToolId"])
	default:
		return ""
	}
}

func responseInstance(resp any) (string, *ags.SandboxInstance) {
	switch value := resp.(type) {
	case *ags.StartSandboxInstanceResponseParams:
		if value.Instance == nil {
			return "", nil
		}
		return derefString(value.Instance.InstanceId), value.Instance
	case map[string]any:
		instanceValue, ok := value["Instance"]
		if !ok {
			if id, ok := value["InstanceId"]; ok && id != nil {
				return fmt.Sprint(id), nil
			}
			return "", nil
		}
		var instance ags.SandboxInstance
		if err := jsonRoundTrip(instanceValue, &instance); err != nil {
			return "", nil
		}
		return derefString(instance.InstanceId), &instance
	default:
		return "", nil
	}
}

func jsonRoundTrip(src any, dst any) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

func stringFlag(req command.Request, name string) string {
	flag, ok := req.Flags[name]
	if !ok {
		return ""
	}
	return flag.String
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
