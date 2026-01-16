package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
)

// Formatter handles output formatting
type Formatter struct {
	format    string
	writer    io.Writer
	errWriter io.Writer
}

// NewFormatter creates a new formatter
func NewFormatter() *Formatter {
	return &Formatter{
		format:    config.GetOutput(),
		writer:    os.Stdout,
		errWriter: os.Stderr,
	}
}

// IsJSON returns true if output format is JSON
func (f *Formatter) IsJSON() bool {
	return f.format == "json"
}

// SetWriter sets the output writer
func (f *Formatter) SetWriter(w io.Writer) {
	f.writer = w
}

// PrintJSON outputs data as JSON
func (f *Formatter) PrintJSON(data any) error {
	encoder := json.NewEncoder(f.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// PrintTiming prints timing info to stderr (text mode only)
func (f *Formatter) PrintTiming(timing *Timing) {
	if timing != nil && f.format != "json" {
		fmt.Fprintf(f.errWriter, "Time: %dms\n", timing.TotalMs)
	}
}

// PrintExecResult prints code execution result
func (f *Formatter) PrintExecResult(result *ExecResult) error {
	if f.format == "json" {
		return f.PrintJSON(result)
	}

	// Text mode: print stdout/stderr
	if len(result.Stdout) > 0 {
		for _, line := range result.Stdout {
			fmt.Fprint(f.writer, line)
		}
	}

	if len(result.Stderr) > 0 {
		fmt.Fprintln(f.writer, "\n--- stderr ---")
		for _, line := range result.Stderr {
			fmt.Fprint(f.writer, line)
		}
	}

	if result.Error != nil {
		fmt.Fprintln(f.writer, "\n--- error ---")
		fmt.Fprintf(f.writer, "%s: %s\n", result.Error.Name, result.Error.Value)
		if result.Error.Traceback != "" {
			fmt.Fprintln(f.writer, result.Error.Traceback)
		}
	}

	return nil
}

// PrintMultiTaskResult prints multiple task execution results
func (f *Formatter) PrintMultiTaskResult(result *MultiTaskResult) error {
	if f.format == "json" {
		return f.PrintJSON(result)
	}

	// Text mode: grouped output
	for _, t := range result.Tasks {
		header := f.formatTaskHeader(t)
		fmt.Fprintln(f.writer, header)

		if len(t.Stdout) > 0 {
			for _, line := range t.Stdout {
				fmt.Fprint(f.writer, line)
			}
		}

		if len(t.Stderr) > 0 {
			fmt.Fprintln(f.writer, "--- stderr ---")
			for _, line := range t.Stderr {
				fmt.Fprint(f.writer, line)
			}
		}

		if t.Error != nil {
			fmt.Fprintln(f.writer, "--- error ---")
			fmt.Fprintf(f.writer, "%s: %s\n", t.Error.Name, t.Error.Value)
			if t.Error.Traceback != "" {
				fmt.Fprintln(f.writer, t.Error.Traceback)
			}
		} else if t.ErrorMsg != "" {
			fmt.Fprintln(f.writer, "--- error ---")
			fmt.Fprintln(f.writer, t.ErrorMsg)
		}

		fmt.Fprintln(f.writer) // Empty line between tasks
	}

	// Print summary
	f.PrintSummary(result.Summary)
	return nil
}

// formatTaskHeader formats the task header line
func (f *Formatter) formatTaskHeader(t TaskResult) string {
	var status string
	if !t.Success {
		status = " [FAILED]"
	}

	if t.TotalInst > 1 {
		return fmt.Sprintf("━━━ Task %d: %s (%d/%d)%s ━━━",
			t.ID, t.Source, t.Instance, t.TotalInst, status)
	}
	return fmt.Sprintf("━━━ Task %d: %s%s ━━━", t.ID, t.Source, status)
}

// PrintSummary prints task summary
func (f *Formatter) PrintSummary(summary TaskSummary) {
	fmt.Fprintln(f.writer, "━━━ Summary ━━━")
	statusMark := "✓"
	if summary.Failed > 0 {
		statusMark = "✗"
	}
	timeStr := ""
	if summary.Timing != nil {
		timeStr = fmt.Sprintf(" | Time: %.2fs", float64(summary.Timing.TotalMs)/1000)
	}
	fmt.Fprintf(f.writer, "Total: %d | %s %d | ✗ %d%s\n",
		summary.Total, statusMark, summary.Success, summary.Failed, timeStr)
}

// PrintSummaryToStderr prints task summary to stderr (for streaming mode)
func (f *Formatter) PrintSummaryToStderr(summary TaskSummary) {
	fmt.Fprintln(f.errWriter, "\n━━━ Summary ━━━")
	timeStr := ""
	if summary.Timing != nil {
		timeStr = fmt.Sprintf(" | Time: %.2fs", float64(summary.Timing.TotalMs)/1000)
	}
	fmt.Fprintf(f.errWriter, "Total: %d | ✓ %d | ✗ %d%s\n",
		summary.Total, summary.Success, summary.Failed, timeStr)
}

// PrintCommandResult prints shell command result
func (f *Formatter) PrintCommandResult(result *CommandResult) error {
	if f.format == "json" {
		return f.PrintJSON(result)
	}

	// Text mode
	if result.Stdout != "" {
		fmt.Fprint(f.writer, result.Stdout)
	}

	if result.Stderr != "" {
		fmt.Fprintln(f.errWriter, "--- stderr ---")
		fmt.Fprint(f.errWriter, result.Stderr)
	}

	if result.Error != "" {
		fmt.Fprintf(f.errWriter, "--- error ---\n%s\n", result.Error)
	}

	return nil
}

// PrintTable outputs data as a table with optional pagination
func (f *Formatter) PrintTable(headers []string, rows [][]string, pagination *Pagination) error {
	if f.format == "json" {
		result := &ListResult{
			Items:      make([]map[string]string, len(rows)),
			Pagination: pagination,
		}
		for i, row := range rows {
			item := make(map[string]string)
			for j, header := range headers {
				if j < len(row) {
					item[header] = row[j]
				}
			}
			result.Items[i] = item
		}
		return f.PrintJSON(result)
	}

	// Text mode: use tabwriter
	w := tabwriter.NewWriter(f.writer, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	if err := w.Flush(); err != nil {
		return err
	}

	// Show pagination info
	if pagination != nil && pagination.Total > len(rows) {
		fmt.Fprintf(f.writer, "\nShowing %d of %d items (use --offset and --limit for pagination)\n",
			len(rows), pagination.Total)
	}

	return nil
}

// PrintTableNoHeader outputs data as a table without headers
func (f *Formatter) PrintTableNoHeader(rows [][]string) error {
	if f.format == "json" {
		return f.PrintJSON(rows)
	}

	w := tabwriter.NewWriter(f.writer, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	return w.Flush()
}

// PrintKeyValue prints key-value pairs in order
func (f *Formatter) PrintKeyValue(pairs []KeyValue) error {
	if f.format == "json" {
		m := make(map[string]string, len(pairs))
		for _, kv := range pairs {
			m[kv.Key] = kv.Value
		}
		return f.PrintJSON(m)
	}

	maxKeyLen := 0
	for _, kv := range pairs {
		if len(kv.Key) > maxKeyLen {
			maxKeyLen = len(kv.Key)
		}
	}

	for _, kv := range pairs {
		fmt.Fprintf(f.writer, "%-*s  %s\n", maxKeyLen, kv.Key+":", kv.Value)
	}
	return nil
}

// PrintSuccess prints a success message
func (f *Formatter) PrintSuccess(message string) {
	if f.format == "json" {
		_ = f.PrintJSON(&OperationResult{
			Status:  "success",
			Message: message,
		})
		return
	}
	fmt.Fprintln(f.writer, "✓", message)
}

// PrintSuccessWithData prints a success message with additional data
func (f *Formatter) PrintSuccessWithData(message string, data map[string]any, timing *Timing) {
	if f.format == "json" {
		_ = f.PrintJSON(&OperationResult{
			Status:  "success",
			Message: message,
			Data:    data,
			Timing:  timing,
		})
		return
	}
	fmt.Fprintln(f.writer, "✓", message)
}

// PrintError prints an error message
func (f *Formatter) PrintError(err error) {
	if f.format == "json" {
		_ = f.PrintJSON(&OperationResult{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}
	fmt.Fprintln(f.writer, "✗", err.Error())
}

// PrintInfo prints an info message
func (f *Formatter) PrintInfo(message string) {
	if f.format == "json" {
		_ = f.PrintJSON(map[string]any{
			"status":  "info",
			"message": message,
		})
		return
	}
	fmt.Fprintln(f.writer, "ℹ", message)
}

// PrintWarning prints a warning message
func (f *Formatter) PrintWarning(message string) {
	if f.format == "json" {
		_ = f.PrintJSON(map[string]any{
			"status":  "warning",
			"message": message,
		})
		return
	}
	fmt.Fprintln(f.writer, "⚠", message)
}

// PrintFileOperation prints file operation result
func (f *Formatter) PrintFileOperation(op *FileOperation) error {
	if f.format == "json" {
		return f.PrintJSON(op)
	}

	switch op.Operation {
	case "upload":
		fmt.Fprintf(f.writer, "✓ Uploaded %s -> %s", op.LocalPath, op.Path)
	case "download":
		fmt.Fprintf(f.writer, "✓ Downloaded %s -> %s", op.Path, op.LocalPath)
		if op.Size > 0 {
			fmt.Fprintf(f.writer, " (%s)", FormatSize(op.Size))
		}
	case "remove":
		fmt.Fprintf(f.writer, "✓ Removed: %s", op.Path)
	case "mkdir":
		fmt.Fprintf(f.writer, "✓ Created directory: %s", op.Path)
	}
	fmt.Fprintln(f.writer)
	return nil
}

// PrintFileContent prints file content
func (f *Formatter) PrintFileContent(content *FileContent) error {
	if f.format == "json" {
		return f.PrintJSON(content)
	}
	fmt.Fprint(f.writer, content.Content)
	return nil
}

// PrintStreamPrefix prints a line with task prefix for streaming mode
func PrintStreamPrefix(taskID int, source string, instanceNo int, isStderr bool, line string) {
	prefix := formatStreamPrefix(taskID, source, instanceNo, isStderr)
	if isStderr {
		fmt.Fprintf(os.Stderr, "%s%s", prefix, line)
	} else {
		fmt.Printf("%s%s", prefix, line)
	}
}

// formatStreamPrefix formats the prefix for streaming output
func formatStreamPrefix(taskID int, source string, instanceNo int, isStderr bool) string {
	errMark := ""
	if isStderr {
		errMark = "|err"
	}
	if instanceNo > 0 {
		return fmt.Sprintf("[%d:%s#%d%s] ", taskID, source, instanceNo, errMark)
	}
	return fmt.Sprintf("[%d:%s%s] ", taskID, source, errMark)
}

// TruncateString truncates a string to the specified length
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// FormatSize formats bytes to human readable string
func FormatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// Global helper functions for convenience

// IsJSON returns true if global output format is JSON
func IsJSON() bool {
	return config.GetOutput() == "json"
}

// PrintSuccess prints a success message using default formatter
func PrintSuccess(message string) {
	NewFormatter().PrintSuccess(message)
}

// PrintError prints an error using default formatter
func PrintError(err error) {
	NewFormatter().PrintError(err)
}

// PrintInfo prints an info message using default formatter
func PrintInfo(message string) {
	NewFormatter().PrintInfo(message)
}

// PrintWarning prints a warning message using default formatter
func PrintWarning(message string) {
	NewFormatter().PrintWarning(message)
}

// PrintTable outputs a table using default formatter (no pagination)
func PrintTable(headers []string, rows [][]string) error {
	return NewFormatter().PrintTable(headers, rows, nil)
}

// PrintTableNoHeader outputs a table without headers using default formatter
func PrintTableNoHeader(rows [][]string) error {
	return NewFormatter().PrintTableNoHeader(rows)
}

// PrintKeyValue prints key-value pairs using default formatter
func PrintKeyValue(pairs []KeyValue) error {
	return NewFormatter().PrintKeyValue(pairs)
}

// Print outputs data as JSON using default formatter
func Print(data any) error {
	return NewFormatter().PrintJSON(data)
}
