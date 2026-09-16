// Package instanceview formats sandbox instance SDK objects for instance
// commands without coupling each command to the SDK pointer-heavy shape.
package instanceview

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// KeyValue is one row in the text details view.
type KeyValue struct {
	Key   string
	Value string
}

// CanonicalData converts an SDK SandboxInstance into the JSON shape shared by
// instance get/list commands.
func CanonicalData(value any) map[string]any {
	var inst ags.SandboxInstance
	if err := apivalue.Project(value, &inst); err != nil {
		raw, _ := apivalue.Decode(value)
		return map[string]any(raw)
	}
	data := map[string]any{
		"InstanceId":          DerefString(inst.InstanceId),
		"ToolId":              DerefString(inst.ToolId),
		"ToolName":            DerefString(inst.ToolName),
		"Status":              DerefString(inst.Status),
		"Persistent":          inst.Persistent,
		"TimeoutSeconds":      inst.TimeoutSeconds,
		"ExpiresAt":           DerefString(inst.ExpiresAt),
		"StopReason":          DerefString(inst.StopReason),
		"CreateTime":          DerefString(inst.CreateTime),
		"MountOptions":        inst.MountOptions,
		"CustomConfiguration": inst.CustomConfiguration,
		"NetworkMode":         DerefString(inst.NetworkMode),
		"Metadata":            inst.Metadata,
		"AuthMode":            DerefString(inst.AuthMode),
	}
	if inst.UpdateTime != nil {
		data["UpdateTime"] = DerefString(inst.UpdateTime)
	}
	if inst.TimeoutSeconds != nil {
		data["TimeoutSeconds"] = *inst.TimeoutSeconds
	}
	return apivalue.Extend(data, value)
}

// PrintKV renders aligned key/value rows for text output.
func PrintKV(w io.Writer, pairs []KeyValue) {
	maxLen := 0
	for _, kv := range pairs {
		if len(kv.Key) > maxLen {
			maxLen = len(kv.Key)
		}
	}
	for _, kv := range pairs {
		fmt.Fprintf(w, "%-*s  %s\n", maxLen, kv.Key+":", kv.Value)
	}
}

// PrintTableWithPagination renders tabular rows and appends a pagination hint
// when not all rows are shown.
func PrintTableWithPagination(w io.Writer, headers []string, rows [][]string, shown, total int) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
	if total > shown {
		fmt.Fprintf(w, "\nShowing %d of %d items (use --offset and --limit for pagination)\n", shown, total)
	}
}

// MountOptionsSummary returns a compact comma-separated mount name list.
func MountOptionsSummary(opts []*ags.MountOption) string {
	if len(opts) == 0 {
		return "-"
	}
	var parts []string
	for _, opt := range opts {
		parts = append(parts, DerefString(opt.Name))
	}
	return strings.Join(parts, ", ")
}

// MountOptionsDetail returns the multi-line mount detail block used by text
// output.
func MountOptionsDetail(opts []*ags.MountOption) string {
	if len(opts) == 0 {
		return ""
	}

	var lines []string
	for i, opt := range opts {
		lines = append(lines, fmt.Sprintf("\n  [%d] %s", i+1, DerefString(opt.Name)))
		lines = append(lines, fmt.Sprintf("      MountPath: %s", valueOrDefault(DerefString(opt.MountPath), "(default)")))
		if opt.SubPath != nil && *opt.SubPath != "" {
			lines = append(lines, fmt.Sprintf("      SubPath:   %s", *opt.SubPath))
		}
		readOnly := "(default)"
		if opt.ReadOnly != nil {
			readOnly = fmt.Sprintf("%t", *opt.ReadOnly)
		}
		lines = append(lines, fmt.Sprintf("      ReadOnly:  %s", readOnly))
	}
	return strings.Join(lines, "\n")
}

// Timeout formats a timeout in seconds as s/m/h when evenly divisible.
func Timeout(seconds uint64) string {
	if seconds >= 3600 && seconds%3600 == 0 {
		return fmt.Sprintf("%dh", seconds/3600)
	}
	if seconds >= 60 && seconds%60 == 0 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	return fmt.Sprintf("%ds", seconds)
}

// TimeShort formats an RFC3339 timestamp for compact text tables.
func TimeShort(isoTime string) string {
	if isoTime == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, isoTime)
	if err != nil {
		return isoTime
	}
	return t.Local().Format("01-02 15:04")
}

// DerefString returns the SDK string pointer value or an empty string.
func DerefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// DerefInt64 returns the SDK int64 pointer value as int, or zero.
func DerefInt64(i *int64) int {
	if i == nil {
		return 0
	}
	return int(*i)
}

func valueOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}
