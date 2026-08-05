package get

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// ControlPlane supplies the tool lookup used by the get workflow.
type ControlPlane interface {
	GetTool(ctx context.Context, toolID string) (*ags.SandboxTool, error)
}

// Module returns this package's command module.
func Module() command.Module {
	spec := command.Spec{
		ID:           "tool.get",
		Path:         []string{"tool", "get"},
		Use:          "get <tool-id>",
		Short:        "Get tool details",
		SupportsJSON: true,
		Args: []command.ArgSpec{
			{Name: "tool-id", Required: true, Description: "Sandbox tool ID."},
		},
		Flags: []command.FlagSpec{resourcewait.Flag()},
		Output: command.OutputSpec{
			DataType:    "SandboxTool",
			Description: "Sandbox tool details.",
		},
	}
	groups := []command.GroupSpec{
		{
			Path:    []string{"tool"},
			Use:     "tool",
			Short:   "Manage sandbox tools",
			Long:    "Manage sandbox tools (templates). Tools define the type and capabilities of sandbox instances.",
			Aliases: []string{"t"},
		},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec:   spec,
			Groups: groups,
			Source: "workflow",
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			cp, ok := deps.ControlPlane.(ControlPlane)
			if !ok {
				return command.Runtime{}, fmt.Errorf("tool.get requires command.Deps.ControlPlane implementing tool/get.ControlPlane")
			}
			return command.Runtime{
				Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
					toolID := req.ArgValues["tool-id"]
					if toolID == "" && len(req.Args) > 0 {
						toolID = req.Args[0]
					}
					if strings.TrimSpace(toolID) == "" {
						return nil, output.NewUsageError("MISSING_REQUIRED_ARG", "missing tool id", "Provide <tool-id>.")
					}
					var tool *ags.SandboxTool
					var err error
					if resourcewait.Requested(req) {
						tool, err = resourcewait.WaitForTool(ctx, toolID, cp.GetTool, resourcewait.OptionsFromDeps(deps))
					} else {
						tool, err = cp.GetTool(ctx, toolID)
					}
					if err != nil {
						return nil, err
					}
					return Result(tool), nil
				}),
			}, nil
		},
	}
}

// Result returns the canonical command result for a Tool. Lifecycle mutation
// commands reuse it after --wait reaches ACTIVE.
func Result(tool *ags.SandboxTool) *command.Result {
	return &command.Result{
		Data: canonicalToolData(tool),
		Text: func(w io.Writer) {
			renderToolDetails(w, tool)
		},
	}
}

func renderToolDetails(w io.Writer, tool *ags.SandboxTool) {
	tagsStr := strings.Join(sortedTagStrings(tool.Tags), ", ")
	if tagsStr == "" {
		tagsStr = "-"
	}
	networkMode := "-"
	if tool.NetworkConfiguration != nil && tool.NetworkConfiguration.NetworkMode != nil && *tool.NetworkConfiguration.NetworkMode != "" {
		networkMode = *tool.NetworkConfiguration.NetworkMode
	}
	kvs := []keyValue{
		{key: "ID", value: derefString(tool.ToolId)},
		{key: "Name", value: derefString(tool.ToolName)},
		{key: "Type", value: derefString(tool.ToolType)},
		{key: "Status", value: derefString(tool.Status)},
		{key: "NetworkMode", value: networkMode},
		{key: "Description", value: derefString(tool.Description)},
		{key: "Tags", value: tagsStr},
		{key: "Created", value: formatShortTime(derefString(tool.CreateTime))},
	}
	if tool.StatusReason != nil && *tool.StatusReason != "" {
		kvs = append(kvs, keyValue{key: "StatusReason", value: *tool.StatusReason})
	}
	if tool.RoleArn != nil && *tool.RoleArn != "" {
		kvs = append(kvs, keyValue{key: "RoleArn", value: *tool.RoleArn})
	}
	if mounts := formatStorageMountsDetail(tool.StorageMounts); mounts != "" {
		kvs = append(kvs, keyValue{key: "StorageMounts", value: mounts})
	}
	printKV(w, kvs)
}

func canonicalToolData(t *ags.SandboxTool) map[string]any {
	return map[string]any{
		"ToolId":                derefString(t.ToolId),
		"ToolName":              derefString(t.ToolName),
		"ToolType":              derefString(t.ToolType),
		"Status":                derefString(t.Status),
		"StatusReason":          derefString(t.StatusReason),
		"Persistent":            t.Persistent,
		"DefaultTimeoutSeconds": t.DefaultTimeoutSeconds,
		"NetworkConfiguration":  t.NetworkConfiguration,
		"Description":           derefString(t.Description),
		"Tags":                  sdkTagsToMap(t.Tags),
		"CreateTime":            derefString(t.CreateTime),
		"UpdateTime":            derefString(t.UpdateTime),
		"RoleArn":               derefString(t.RoleArn),
		"StorageMounts":         t.StorageMounts,
		"CustomConfiguration":   t.CustomConfiguration,
		"LogConfiguration":      t.LogConfiguration,
	}
}

type keyValue struct {
	key   string
	value string
}

func printKV(w io.Writer, pairs []keyValue) {
	maxLen := 0
	for _, kv := range pairs {
		if len(kv.key) > maxLen {
			maxLen = len(kv.key)
		}
	}
	for _, kv := range pairs {
		fmt.Fprintf(w, "%-*s  %s\n", maxLen, kv.key+":", kv.value)
	}
}

func sdkTagsToMap(tags []*ags.Tag) map[string]string {
	result := make(map[string]string, len(tags))
	for _, tag := range tags {
		if tag != nil && tag.Key != nil && tag.Value != nil {
			result[*tag.Key] = *tag.Value
		}
	}
	return result
}

func sortedTagStrings(tags []*ags.Tag) []string {
	tagMap := sdkTagsToMap(tags)
	keys := make([]string, 0, len(tagMap))
	for key := range tagMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, fmt.Sprintf("%s=%s", key, tagMap[key]))
	}
	return out
}

func formatStorageMountsDetail(mounts []*ags.StorageMount) string {
	if len(mounts) == 0 {
		return ""
	}
	var lines []string
	for i, mount := range mounts {
		lines = append(lines, fmt.Sprintf("\n  [%d] %s", i+1, derefString(mount.Name)))
		if mount.StorageSource != nil && mount.StorageSource.Cos != nil {
			lines = append(lines, fmt.Sprintf("      Bucket: %s", derefString(mount.StorageSource.Cos.BucketName)))
			lines = append(lines, fmt.Sprintf("      Path:   %s", derefString(mount.StorageSource.Cos.BucketPath)))
		}
		lines = append(lines, fmt.Sprintf("      Mount:  %s", derefString(mount.MountPath)))
		readOnly := false
		if mount.ReadOnly != nil {
			readOnly = *mount.ReadOnly
		}
		lines = append(lines, fmt.Sprintf("      RO:     %t", readOnly))
	}
	return strings.Join(lines, "\n")
}

func formatShortTime(isoTime string) string {
	if isoTime == "" {
		return "-"
	}
	if parsed, err := time.Parse(time.RFC3339, isoTime); err == nil {
		return parsed.Format("01-02 15:04")
	}
	if len(isoTime) >= 16 {
		return isoTime[5:16]
	}
	return isoTime
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
