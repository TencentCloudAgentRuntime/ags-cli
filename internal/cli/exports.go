package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/cobra"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// GenerateSkeletonEnabled reports whether the global --generate-cli-skeleton
// mode is active.
func GenerateSkeletonEnabled() bool { return generateSkeleton }

// ArgsExactUnlessSkeleton returns a Cobra arg validator that is disabled while
// generating request skeletons.
func ArgsExactUnlessSkeleton(n int) func(cmd *cobra.Command, args []string) error {
	return argsExactUnlessSkeleton(n)
}

// SkeletonResult returns the JSON skeleton for commandName without executing the
// command's remote operation.
func SkeletonResult(commandName string) (*CmdResult, error) {
	return skeletonResult(commandName)
}

// RequestConflict reports which flag conflicts with --request for commandID.
func RequestConflict(cmd *cobra.Command, commandID string) string {
	return requestConflict(cmd, commandID)
}

// RequestConflictDetail returns a user-facing explanation of a --request flag
// conflict for commandID.
func RequestConflictDetail(cmd *cobra.Command, commandID string) string {
	return requestConflictDetail(cmd, commandID)
}

// ReadRequestFlag reads a --request value from inline JSON, @file, or stdin.
func ReadRequestFlag(value string) ([]byte, error) {
	return readRequestFlag(value)
}

// ValidateRequestPayload checks raw --request JSON against the generated schema
// for commandID.
func ValidateRequestPayload(commandID string, raw []byte) error {
	return validateRequestPayload(commandID, raw)
}

// RequestParseError wraps request parsing failures in the CLI's structured usage
// error format.
func RequestParseError(commandName string, err error) error {
	return requestParseError(commandName, err)
}

// MergePositionalIntoRequest writes a positional resource id into raw request
// JSON when the request did not already provide a conflicting value.
func MergePositionalIntoRequest(rawRequest, fieldName, positional string) ([]byte, error) {
	return mergePositionalIntoRequest(rawRequest, fieldName, positional)
}

// NewCloudClient constructs the configured TencentCloud AGS SDK client.
func NewCloudClient() (*ags.Client, error) {
	return newCloudClient()
}

// NonInteractive reports whether prompts and other interactive flows are
// disabled by global flags.
func NonInteractive() bool { return nonInteractive }

// IsJSONOutput reports whether the resolved output format is JSON or NDJSON.
func IsJSONOutput() bool { return isJSON() || isNDJSON() }

// NoColor reports whether ANSI color/escape output is disabled.
func NoColor() bool { return noColor }

// CfgFile returns the config file path supplied on the command line.
func CfgFile() string { return cfgFile }

// RegionFlag returns the raw --region value before config defaults are applied.
func RegionFlag() string { return region }

// DomainFlag returns the raw --domain value before config defaults are applied.
func DomainFlag() string { return domain }

// SecretIDFlag returns the raw --secret-id value from global flags.
func SecretIDFlag() string { return secretID }

// SecretKeyFlag returns the raw --secret-key value from global flags.
func SecretKeyFlag() string { return secretKey }

// IsJSON reports whether the current command should render JSON envelopes.
func IsJSON() bool { return isJSON() }

// IsNDJSON reports whether the current command should render newline-delimited
// JSON stream events.
func IsNDJSON() bool { return isNDJSON() }

// Stderr writes formatted text to the configured error stream.
func Stderr(format string, args ...any) {
	if ios == nil {
		initIOStreams()
	}
	fmt.Fprintf(ios.ErrOut, format, args...)
}

// ClassifyCLIError converts arbitrary errors into AGR's structured CLI error
// type and exit code.
func ClassifyCLIError(err error) *output.CLIError {
	return classifyCLIError(err)
}

// RemoteCodeFailure returns the canonical failure payload for remote code
// execution failures.
func RemoteCodeFailure() *output.Failure {
	return remoteCodeFailure()
}

// RemoteCommandFailure returns the canonical failure payload for remote shell
// command failures.
func RemoteCommandFailure() *output.Failure {
	return remoteCommandFailure()
}

// ValidateNDJSONOnlyForStream rejects -o ndjson for commands that do not stream
// incremental events.
func ValidateNDJSONOnlyForStream(hasStream bool) error {
	return validateNDJSONOnlyForStream(hasStream)
}

// ValidateStreamNotJSON rejects incompatible JSON output for interactive stream
// commands.
func ValidateStreamNotJSON() error {
	return validateStreamNotJSON()
}

// RequireTTY returns a usage error when stdin/stdout is not attached to a TTY.
func RequireTTY() error { return requireTTY() }

// IsNotFoundCLIError reports whether err is a structured not-found CLI error.
func IsNotFoundCLIError(err error) bool { return isNotFoundCLIError(err) }

// PrintKV renders key/value rows using the CLI's text table format.
func PrintKV(w io.Writer, pairs []KeyValue) {
	printKV(w, pairs)
}

// PrintTable renders headers and rows in the CLI's text table format.
func PrintTable(w io.Writer, headers []string, rows [][]string) {
	printTable(w, headers, rows)
}

// PrintTableWithPagination renders a table and appends shown/total pagination
// information.
func PrintTableWithPagination(w io.Writer, headers []string, rows [][]string, shown, total int) {
	printTableWithPagination(w, headers, rows, shown, total)
}

// StrPtr returns a pointer to s for TencentCloud SDK request fields.
func StrPtr(s string) *string { return strPtr(s) }

// DerefString returns the string value or an empty string for nil SDK pointers.
func DerefString(s *string) string { return derefString(s) }

// DerefInt64 returns the int value or zero for nil SDK pointers.
func DerefInt64(i *int64) int { return derefInt64(i) }

// IsAuthModeNone reports whether the SDK auth mode pointer is set to "none".
func IsAuthModeNone(authMode *string) bool { return isAuthModeNone(authMode) }

// SDKTagsToMap converts TencentCloud SDK tag slices into a stable map shape for
// JSON output.
func SDKTagsToMap(tags []*ags.Tag) map[string]string {
	return sdkTagsToMap(tags)
}

// ToCanonicalInstanceData converts an SDK SandboxInstance into the CLI's
// canonical instance JSON shape.
func ToCanonicalInstanceData(inst *ags.SandboxInstance) map[string]any {
	return toCanonicalInstanceData(inst)
}

// ToCanonicalToolData converts an SDK SandboxTool into the CLI's canonical tool
// JSON shape.
func ToCanonicalToolData(t *ags.SandboxTool) map[string]any {
	return toCanonicalToolData(t)
}

// ParseRequestFlag decodes a --request object from inline JSON, @file, or stdin.
func ParseRequestFlag(value string) (map[string]any, error) {
	return parseRequestFlag(value)
}

// ParseJSONFlagValue decodes a JSON-valued flag into target and wraps syntax
// errors as CLI usage errors.
func ParseJSONFlagValue(flagName, value string, target any) error {
	return parseJSONFlagValue(flagName, value, target)
}

// ValidateListenAddress rejects unsafe or malformed local bind addresses.
func ValidateListenAddress(address string) error {
	return validateListenAddress(address)
}

// SortedKeys returns map keys in deterministic order for text output.
func SortedKeys(m map[string]bool) []string {
	return sortedKeys(m)
}

// RequireNonEmptyValue returns a structured usage error when a required string
// field is empty.
func RequireNonEmptyValue(value, field, code, hint string) error {
	return requireNonEmptyValue(value, field, code, hint)
}

// ResolveUser returns the requested sandbox user or the configured default.
func ResolveUser(flagValue string) string {
	return resolveUser(flagValue)
}

// AcquireInstanceToken obtains a data-plane access token for an instance.
func AcquireInstanceToken(ctx context.Context, instanceID string) (string, error) {
	return acquireInstanceToken(ctx, instanceID)
}

// OverlayCloudCreate creates the temporary instance used by overlay workflows.
func OverlayCloudCreate(ctx context.Context, toolName, toolID string) (string, error) {
	return overlayCloudCreate(ctx, toolName, toolID)
}

// OverlayCloudDelete deletes the temporary instance used by overlay workflows.
func OverlayCloudDelete(ctx context.Context, instanceID string) error {
	return overlayCloudDelete(ctx, instanceID)
}

// TestDataPlane returns the process-wide data-plane override used by legacy
// tests.
func TestDataPlane() DataPlaneOverride {
	return testDataPlane
}

// SetTestDataPlaneForTest installs a temporary data-plane override and returns a
// restore function for tests.
func SetTestDataPlaneForTest(dp DataPlaneOverride) func() {
	previous := testDataPlane
	testDataPlane = dp
	return func() { testDataPlane = previous }
}

// DataPlaneOverride is the legacy aggregate interface used by data-plane
// commands before command.Module introduced per-command dependency injection.
type DataPlaneOverride interface {
	RunCode(ctx context.Context, instanceID, code, language string) (stdout, stderr string, results any, remoteErr any, executionCount int, err error)
	Exec(ctx context.Context, instanceID string, argv []string) (stdout, stderr string, exitCode int, remoteErr any, err error)
	Upload(ctx context.Context, instanceID, localPath, remotePath string, r io.Reader) (path string, size int64, err error)
	Download(ctx context.Context, instanceID, remotePath string) (io.Reader, int64, error)
}

// CloudStartSandboxInstance calls the injectable StartSandboxInstance function.
func CloudStartSandboxInstance(ctx context.Context, sdk *ags.Client, req *ags.StartSandboxInstanceRequest) (*ags.StartSandboxInstanceResponseParams, error) {
	return cloudStartSandboxInstance(ctx, sdk, req)
}

// SetCloudStartSandboxInstanceForTest replaces StartSandboxInstance and returns
// a restore function for tests.
func SetCloudStartSandboxInstanceForTest(fn func(context.Context, *ags.Client, *ags.StartSandboxInstanceRequest) (*ags.StartSandboxInstanceResponseParams, error)) func() {
	previous := cloudStartSandboxInstance
	cloudStartSandboxInstance = fn
	return func() { cloudStartSandboxInstance = previous }
}

// CloudDescribeSandboxInstanceList calls the injectable list-instances function.
func CloudDescribeSandboxInstanceList(ctx context.Context, sdk *ags.Client, req *ags.DescribeSandboxInstanceListRequest) (*ags.DescribeSandboxInstanceListResponseParams, error) {
	return cloudDescribeSandboxInstanceList(ctx, sdk, req)
}

// CloudUpdateSandboxInstance calls the injectable update-instance function.
func CloudUpdateSandboxInstance(ctx context.Context, sdk *ags.Client, req *ags.UpdateSandboxInstanceRequest) (*ags.UpdateSandboxInstanceResponseParams, error) {
	return cloudUpdateSandboxInstance(ctx, sdk, req)
}

// CloudPauseSandboxInstance calls the injectable pause-instance function.
func CloudPauseSandboxInstance(ctx context.Context, sdk *ags.Client, req *ags.PauseSandboxInstanceRequest) (*ags.PauseSandboxInstanceResponseParams, error) {
	return cloudPauseSandboxInstance(ctx, sdk, req)
}

// CloudResumeSandboxInstance calls the injectable resume-instance function.
func CloudResumeSandboxInstance(ctx context.Context, sdk *ags.Client, req *ags.ResumeSandboxInstanceRequest) (*ags.ResumeSandboxInstanceResponseParams, error) {
	return cloudResumeSandboxInstance(ctx, sdk, req)
}

// CloudStopSandboxInstance calls the injectable stop-instance function.
func CloudStopSandboxInstance(ctx context.Context, sdk *ags.Client, req *ags.StopSandboxInstanceRequest) (*ags.StopSandboxInstanceResponseParams, error) {
	return cloudStopSandboxInstance(ctx, sdk, req)
}

// SetCloudStopSandboxInstanceForTest replaces StopSandboxInstance and returns a
// restore function for tests.
func SetCloudStopSandboxInstanceForTest(fn func(context.Context, *ags.Client, *ags.StopSandboxInstanceRequest) (*ags.StopSandboxInstanceResponseParams, error)) func() {
	previous := cloudStopSandboxInstance
	cloudStopSandboxInstance = fn
	return func() { cloudStopSandboxInstance = previous }
}

// CloudAcquireSandboxInstanceToken calls the injectable acquire-token function.
func CloudAcquireSandboxInstanceToken(ctx context.Context, sdk *ags.Client, req *ags.AcquireSandboxInstanceTokenRequest) (*ags.AcquireSandboxInstanceTokenResponseParams, error) {
	return cloudAcquireSandboxInstanceToken(ctx, sdk, req)
}

// CloudCreateSandboxTool calls the injectable create-tool function.
func CloudCreateSandboxTool(ctx context.Context, sdk *ags.Client, req *ags.CreateSandboxToolRequest) (*ags.CreateSandboxToolResponseParams, error) {
	return cloudCreateSandboxTool(ctx, sdk, req)
}

// CloudDescribeSandboxToolList calls the injectable list-tools function.
func CloudDescribeSandboxToolList(ctx context.Context, sdk *ags.Client, req *ags.DescribeSandboxToolListRequest) (*ags.DescribeSandboxToolListResponseParams, error) {
	return cloudDescribeSandboxToolList(ctx, sdk, req)
}

// CloudUpdateSandboxTool calls the injectable update-tool function.
func CloudUpdateSandboxTool(ctx context.Context, sdk *ags.Client, req *ags.UpdateSandboxToolRequest) (*ags.UpdateSandboxToolResponseParams, error) {
	return cloudUpdateSandboxTool(ctx, sdk, req)
}

// CloudDeleteSandboxTool calls the injectable delete-tool function.
func CloudDeleteSandboxTool(ctx context.Context, sdk *ags.Client, req *ags.DeleteSandboxToolRequest) (*ags.DeleteSandboxToolResponseParams, error) {
	return cloudDeleteSandboxTool(ctx, sdk, req)
}

// CloudCreateAPIKey calls the injectable create-API-key function.
func CloudCreateAPIKey(ctx context.Context, sdk *ags.Client, req *ags.CreateAPIKeyRequest) (*ags.CreateAPIKeyResponseParams, error) {
	return cloudCreateAPIKey(ctx, sdk, req)
}

// CloudDescribeAPIKeyList calls the injectable list-API-keys function.
func CloudDescribeAPIKeyList(ctx context.Context, sdk *ags.Client, req *ags.DescribeAPIKeyListRequest) (*ags.DescribeAPIKeyListResponseParams, error) {
	return cloudDescribeAPIKeyList(ctx, sdk, req)
}

// CloudDeleteAPIKey calls the injectable delete-API-key function.
func CloudDeleteAPIKey(ctx context.Context, sdk *ags.Client, req *ags.DeleteAPIKeyRequest) (*ags.DeleteAPIKeyResponseParams, error) {
	return cloudDeleteAPIKey(ctx, sdk, req)
}

// CloudCreatePreCacheImageTask calls the injectable create-precache-task
// function.
func CloudCreatePreCacheImageTask(ctx context.Context, sdk *ags.Client, req *ags.CreatePreCacheImageTaskRequest) (*ags.CreatePreCacheImageTaskResponseParams, error) {
	return cloudCreatePreCacheImageTask(ctx, sdk, req)
}

// CloudDescribePreCacheImageTask calls the injectable describe-precache-task
// function.
func CloudDescribePreCacheImageTask(ctx context.Context, sdk *ags.Client, req *ags.DescribePreCacheImageTaskRequest) (*ags.DescribePreCacheImageTaskResponseParams, error) {
	return cloudDescribePreCacheImageTask(ctx, sdk, req)
}

// UnsupportedGeneratedCommand returns a placeholder handler for generated
// commands that still need a hand-written workflow hook.
func UnsupportedGeneratedCommand(commandID string) func(*cobra.Command, []string) (*CmdResult, error) {
	return func(*cobra.Command, []string) (*CmdResult, error) {
		return nil, output.NewUsageError("UNIMPLEMENTED_COMMAND", fmt.Sprintf("generated command %s has no hook", commandID), "Add a *_hooks.go implementation for this command.")
	}
}
