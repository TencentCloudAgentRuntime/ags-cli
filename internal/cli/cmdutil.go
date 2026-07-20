package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	requestio "github.com/TencentCloudAgentRuntime/ags-cli/internal/cli/request"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/client"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/cobra"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// ═══════════════════════════════════════════════════════════════════════════
// Global IOStreams — set once in Execute(), overridable for tests
// ═══════════════════════════════════════════════════════════════════════════

var ios *iostreams.IOStreams

// cloud SDK factory; production returns a thin Tencent Cloud SDK holder.
var newCloudClient = client.NewCloudClient

func initIOStreams() {
	if ios == nil {
		ios = iostreams.System()
	}
}

// SetIOStreams allows tests to inject a custom IOStreams.
func SetIOStreams(s *iostreams.IOStreams) {
	ios = s
}

// IO returns the current IOStreams.
func IO() *iostreams.IOStreams { return ios }

// ═══════════════════════════════════════════════════════════════════════════
// KeyValue — used by text-mode renderers
// ═══════════════════════════════════════════════════════════════════════════

// KeyValue is one row in the CLI's simple text key/value renderer.
type KeyValue struct {
	Key   string
	Value string
}

// ═══════════════════════════════════════════════════════════════════════════
// CmdResult
// ═══════════════════════════════════════════════════════════════════════════

// CmdResult is the command-layer result consumed by Wrap. It separates machine
// data from optional text rendering so the same command can support text and
// JSON output.
type CmdResult struct {
	Data       any
	Warnings   []string
	Effects    []output.Effect
	ExitCode   int
	Failure    *output.Failure
	RenderText func(w io.Writer) // receives ios.Out from Wrap
	Handled    bool
	// MetaExtra is merged into the envelope Meta object. It carries
	// command-specific metadata that should ride alongside Backend /
	// DurationMs / Effects, e.g. raw API call metadata
	// (RequestMode, Action, Service, ApiVersion, CloudEndpoint).
	MetaExtra map[string]any
}

// OK returns a successful command result with optional text rendering.
func OK(data any, text func(w io.Writer)) *CmdResult {
	return &CmdResult{Data: data, RenderText: text}
}

// OKWithEffects returns a successful command result annotated with resource
// side effects for the JSON envelope.
func OKWithEffects(data any, text func(w io.Writer), effects ...output.Effect) *CmdResult {
	return &CmdResult{Data: data, RenderText: text, Effects: effects}
}

// PartialResult returns data plus warnings using the partial-success exit code.
func PartialResult(data any, warnings []string, text func(w io.Writer)) *CmdResult {
	return &CmdResult{Data: data, Warnings: warnings, ExitCode: output.ExitPartialSuccess, RenderText: text}
}

// FailedData returns a structured failure while preserving any response data
// that should still be emitted.
func FailedData(data any, failure *output.Failure, exitCode int, text func(w io.Writer)) *CmdResult {
	return &CmdResult{Data: data, Failure: failure, ExitCode: exitCode, RenderText: text}
}

// StreamDone marks commands that have already streamed their output and only
// need exit-code propagation.
func StreamDone(exitCode int) *CmdResult {
	return &CmdResult{Handled: true, ExitCode: exitCode}
}

// FromCommandResult adapts the registry command.Result type to the legacy
// CmdResult wrapper used by older cli package code.
func FromCommandResult(result *command.Result) *CmdResult {
	if result == nil {
		return nil
	}
	if result.StreamDone {
		return &CmdResult{Handled: true, ExitCode: result.ExitCode}
	}
	return &CmdResult{
		Data:       result.Data,
		Warnings:   result.Warnings,
		Effects:    result.Effects,
		ExitCode:   result.ExitCode,
		Failure:    result.Failure,
		RenderText: result.Text,
		MetaExtra:  result.MetaExtra,
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// CmdFunc + Wrap
// ═══════════════════════════════════════════════════════════════════════════

// CmdFunc is the legacy command handler signature used by cli package commands.
type CmdFunc func(cmd *cobra.Command, args []string) (*CmdResult, error)

// Wrap adds uniform timing, JSON envelope rendering, text rendering, and
// structured error classification around a command handler.
func Wrap(commandID string, fn CmdFunc) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		start := time.Now()
		debugCommand(commandID)
		result, err := fn(cmd, args)
		dm := time.Since(start).Milliseconds()

		if err != nil {
			cliErr := classifyCLIError(err)
			if isJSON() {
				if jqErr := writeEnvelope(ios.Out, commandID, "failed", nil, cliErr.Failure, nil, nil, dm, nil); jqErr != nil {
					return output.NewUsageError("INVALID_JQ_EXPRESSION", jqErr.Error(), "Check your --jq expression syntax.")
				}
				return &envelopeAlreadyWritten{code: cliErr.ExitCode}
			}
			return cliErr
		}

		if result == nil {
			return nil
		}

		if result.Handled {
			if result.ExitCode != 0 {
				return &envelopeAlreadyWritten{code: result.ExitCode}
			}
			return nil
		}

		// Ensure Fix field is populated for result-path failures too.
		if result.Failure != nil {
			client.AttachFix(result.Failure)
		}

		status := "succeeded"
		if result.ExitCode == output.ExitPartialSuccess || (result.Failure != nil && result.Failure.Kind == output.KindPartialSuccess) {
			status = "partial"
		} else if result.Failure != nil {
			status = "failed"
		} else if result.ExitCode != 0 {
			status = "failed"
		}

		if isJSON() {
			if jqErr := writeEnvelope(ios.Out, commandID, status, result.Data, result.Failure,
				result.Warnings, result.Effects, dm, result.MetaExtra); jqErr != nil {
				return output.NewUsageError("INVALID_JQ_EXPRESSION", jqErr.Error(), "Check your --jq expression syntax.")
			}
			if result.ExitCode != 0 {
				return &envelopeAlreadyWritten{code: result.ExitCode}
			}
			return nil
		}

		if result.RenderText != nil {
			result.RenderText(ios.Out)
		}
		if result.ExitCode != 0 {
			if result.Failure == nil {
				return &envelopeAlreadyWritten{code: result.ExitCode}
			}
			return resultFailureError(result)
		}
		return nil
	}
}

// WrapNoJSON rejects JSON/NDJSON output modes before running text-only commands.
func WrapNoJSON(fn func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if isJSON() || isNDJSON() {
			return output.NewUsageError(
				"UNSUPPORTED_OUTPUT",
				"this command does not support -o json or -o ndjson",
				"Use text output for this command, or run 'agr schema -o json' for machine-readable command metadata.",
			)
		}
		return fn(cmd, args)
	}
}

type envelopeAlreadyWritten struct{ code int }

// Error satisfies the error interface for the sentinel returned after an
// envelope has already been written.
func (e *envelopeAlreadyWritten) Error() string { return "envelope already written" }

func resultFailureError(result *CmdResult) error {
	return &output.CLIError{Failure: result.Failure, ExitCode: result.ExitCode}
}

// ═══════════════════════════════════════════════════════════════════════════
// writeEnvelope — writes to the given writer (ios.Out or test buffer)
// ═══════════════════════════════════════════════════════════════════════════

func writeEnvelope(
	w io.Writer,
	commandID, status string,
	data any, failure *output.Failure,
	warnings []string, effects []output.Effect,
	dm int64, extra map[string]any,
) error {
	failure = withIdempotencyHint(commandID, failure)
	if warnings == nil {
		warnings = []string{}
	}
	env := &output.Envelope{
		SchemaVersion: "agr.v1",
		Command:       commandID,
		Status:        status,
		Data:          data,
		Failure:       failure,
		Warnings:      warnings,
		Meta: &output.Meta{
			Backend:    config.GetBackend(),
			DurationMs: dm,
			Effects:    effects,
			Extra:      extra,
		},
	}
	if commandID == "schema" {
		env.ExitCodes = exitCodeTable()
	}
	return output.RenderEnvelope(w, env, jqExpr)
}

// ═══════════════════════════════════════════════════════════════════════════
// Format helpers
// ═══════════════════════════════════════════════════════════════════════════

func withIdempotencyHint(commandID string, failure *output.Failure) *output.Failure {
	if failure == nil || !failure.Retryable {
		return failure
	}
	for _, s := range getAllSchemas() {
		if s.Name == commandID && strings.Contains(s.Idempotency, "client_token") {
			copyFailure := *failure
			if !strings.Contains(copyFailure.Hint, "--client-token") {
				if copyFailure.Hint != "" {
					copyFailure.Hint += " "
				}
				copyFailure.Hint += "Safe to retry with --client-token <stable-uuid> for duplicate creation protection."
			}
			return &copyFailure
		}
	}
	return failure
}

func isJSON() bool   { return effectiveOutput() == "json" }
func isNDJSON() bool { return effectiveOutput() == "ndjson" }

func remoteCodeFailure() *output.Failure {
	return &output.Failure{Code: "REMOTE_CODE_FAILED", Kind: output.KindRemoteExecFailed, Message: "remote code execution failed", Hint: "Inspect Data.Error in -o json output.", Retryable: false}
}

func remoteCommandFailure() *output.Failure {
	return &output.Failure{Code: "REMOTE_COMMAND_FAILED", Kind: output.KindRemoteExecFailed, Message: "remote command failed", Hint: "Inspect Data.ExitCode, Data.Stdout, and Data.Stderr in -o json output.", Retryable: false}
}

func stderr(format string, args ...any) {
	fmt.Fprintf(ios.ErrOut, format, args...)
}

func debugf(format string, args ...any) {
	if !debugFlag {
		return
	}
	if ios == nil {
		initIOStreams()
	}
	fmt.Fprintf(ios.ErrOut, format, args...)
}

func debugCommand(commandID string) {
	debugf("Debug: command=%s output=%s region=%s domain=%s cloud_endpoint=%s config=%s loaded=%t\n",
		commandID,
		effectiveOutput(),
		config.GetRegion(),
		config.GetDomain(),
		config.GetCloudEndpoint(),
		config.ConfigFilePath(),
		config.ConfigFileLoaded(),
	)
}

func requireTTY() error {
	if !ios.IsStdinTTY() {
		return output.NewUsageError("TTY_REQUIRED", "this command requires a TTY", "Run the command from an interactive terminal.")
	}
	return nil
}

func validateNDJSONOnlyForStream(hasStream bool) error {
	if isNDJSON() && !hasStream {
		return output.NewUsageError("NDJSON_REQUIRES_STREAM", "-o ndjson can only be used with --stream", "Add --stream, or use -o json for a single envelope.")
	}
	return nil
}

func validateStreamNotJSON() error {
	if isJSON() {
		return output.NewUsageError("STREAM_JSON_CONFLICT", "--stream cannot be used with -o json", "Use -o ndjson for machine-readable stream events, or remove --stream to get a JSON envelope.")
	}
	return nil
}

// ═══════════════════════════════════════════════════════════════════════════
// Text rendering helpers — receive io.Writer from Wrap
// ═══════════════════════════════════════════════════════════════════════════

func printKV(w io.Writer, pairs []KeyValue) {
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

func printTable(w io.Writer, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
}

func printTableWithPagination(w io.Writer, headers []string, rows [][]string, shown, total int) {
	printTable(w, headers, rows)
	if total > shown {
		fmt.Fprintf(w, "\nShowing %d of %d items (use --offset and --limit for pagination)\n", shown, total)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// PascalCase converters
// ═══════════════════════════════════════════════════════════════════════════

func strPtr(s string) *string { return &s }
func int64Ptr(i int64) *int64 { return &i }

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt64(i *int64) int {
	if i == nil {
		return 0
	}
	return int(*i)
}

func isAuthModeNone(authMode *string) bool {
	return authMode != nil && *authMode == "NONE"
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

// ═══════════════════════════════════════════════════════════════════════════
// Canonical resource converters — shared by list and get commands
// ═══════════════════════════════════════════════════════════════════════════

func toCanonicalInstanceData(inst *ags.SandboxInstance) map[string]any {
	data := map[string]any{
		"InstanceId":          derefString(inst.InstanceId),
		"ToolId":              derefString(inst.ToolId),
		"ToolName":            derefString(inst.ToolName),
		"Status":              derefString(inst.Status),
		"Persistent":          inst.Persistent,
		"TimeoutSeconds":      inst.TimeoutSeconds,
		"ExpiresAt":           derefString(inst.ExpiresAt),
		"StopReason":          derefString(inst.StopReason),
		"CreateTime":          derefString(inst.CreateTime),
		"MountOptions":        inst.MountOptions,
		"CustomConfiguration": inst.CustomConfiguration,
		"NetworkMode":         derefString(inst.NetworkMode),
		"Metadata":            inst.Metadata,
		"AuthMode":            derefString(inst.AuthMode),
	}
	if inst.UpdateTime != nil {
		data["UpdateTime"] = derefString(inst.UpdateTime)
	}
	if inst.TimeoutSeconds != nil {
		data["TimeoutSeconds"] = *inst.TimeoutSeconds
	}
	return data
}

func toCanonicalToolData(t *ags.SandboxTool) map[string]any {
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

// ═══════════════════════════════════════════════════════════════════════════
// --request flag parser
// ═══════════════════════════════════════════════════════════════════════════

func readRequestFlag(value string) ([]byte, error) {
	return requestio.ReadFlag(value)
}

func parseRequestFlag(value string) (map[string]any, error) {
	return requestio.ParseFlag(value)
}

func parseJSONFlagValue(flagName, value string, target any) error {
	return requestio.ParseJSONFlagValue(flagName, value, target)
}
