package create

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// fakeMixedControlPlane implements apicli.ControlPlane for testing the full
// tool create flow (validation → request build → executor → response render).
type fakeMixedControlPlane struct {
	action  string
	request map[string]any
	resp    *ags.CreateSandboxToolResponseParams
}

func (f *fakeMixedControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	if f.resp != nil {
		return f.resp, nil
	}
	return map[string]any{"ok": true}, nil
}

// allToolCreateFlags returns a complete flag set mimicking what Cobra registers
// for `agr tool create`. Unset optional flags have Changed=false.
func allToolCreateFlags() map[string]command.FlagValue {
	return map[string]command.FlagValue{
		"tool-name":             {Name: "tool-name", Type: command.FlagString},
		"tool-type":             {Name: "tool-type", Type: command.FlagString},
		"network-configuration": {Name: "network-configuration", Type: command.FlagString},
		"description":           {Name: "description", Type: command.FlagString},
		"default-timeout":       {Name: "default-timeout", Type: command.FlagString},
		"tags":                  {Name: "tags", Type: command.FlagString},
		"client-token":          {Name: "client-token", Type: command.FlagString},
		"role-arn":              {Name: "role-arn", Type: command.FlagString},
		"storage-mounts":        {Name: "storage-mounts", Type: command.FlagString},
		"custom-configuration":  {Name: "custom-configuration", Type: command.FlagString},
		"log-configuration":     {Name: "log-configuration", Type: command.FlagString},
		"persistent":            {Name: "persistent", Type: command.FlagBool},
		"request":               {Name: "request", Type: command.FlagString},
	}
}

// withChanged returns a copy of flags with the named flag set to the given string value.
func withChanged(flags map[string]command.FlagValue, name, value string) map[string]command.FlagValue {
	out := map[string]command.FlagValue{}
	for k, v := range flags {
		out[k] = v
	}
	f := out[name]
	f.String = value
	f.Changed = true
	out[name] = f
	return out
}

// withBoolChanged returns a copy of flags with the named bool flag set.
func withBoolChanged(flags map[string]command.FlagValue, name string, value bool) map[string]command.FlagValue {
	out := map[string]command.FlagValue{}
	for k, v := range flags {
		out[k] = v
	}
	f := out[name]
	f.Bool = value
	f.Changed = true
	out[name] = f
	return out
}

// minRequiredFlags returns flags with the three required fields set.
func minRequiredFlags() map[string]command.FlagValue {
	flags := allToolCreateFlags()
	flags = withChanged(flags, "tool-name", "my-tool")
	flags = withChanged(flags, "tool-type", "code-interpreter")
	flags = withChanged(flags, "network-configuration", `{"NetworkMode":"SANDBOX"}`)
	return flags
}

func TestModuleBuildsWithoutError(t *testing.T) {
	module := Module()
	if module.Descriptor.Spec.ID == "" {
		t.Fatalf("expected non-empty spec ID")
	}
	if module.Build == nil {
		t.Fatalf("expected non-nil Build func")
	}
}

func TestModuleRequiresToolName(t *testing.T) {
	runtime, err := Module().Build(command.Deps{ControlPlane: &fakeMixedControlPlane{}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Missing --tool-name; other required fields present.
	flags := allToolCreateFlags()
	flags = withChanged(flags, "tool-type", "code-interpreter")
	flags = withChanged(flags, "network-configuration", `{"NetworkMode":"SANDBOX"}`)
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err == nil {
		t.Fatalf("expected missing tool name error")
	}
	if !strings.Contains(err.Error(), "tool-name") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify structured error code (stable contract, not just message text).
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *output.CLIError, got %T", err)
	}
	if cliErr.Failure.Code != "MISSING_REQUIRED_FLAG" {
		t.Fatalf("Failure.Code = %q, want MISSING_REQUIRED_FLAG", cliErr.Failure.Code)
	}
}

func TestModuleRequiresToolType(t *testing.T) {
	runtime, err := Module().Build(command.Deps{ControlPlane: &fakeMixedControlPlane{}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Missing --tool-type; other required fields present.
	flags := allToolCreateFlags()
	flags = withChanged(flags, "tool-name", "my-tool")
	flags = withChanged(flags, "network-configuration", `{"NetworkMode":"SANDBOX"}`)
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err == nil {
		t.Fatalf("expected missing tool type error")
	}
	if !strings.Contains(err.Error(), "tool-type") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify structured error code (stable contract, not just message text).
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *output.CLIError, got %T", err)
	}
	if cliErr.Failure.Code != "MISSING_REQUIRED_FLAG" {
		t.Fatalf("Failure.Code = %q, want MISSING_REQUIRED_FLAG", cliErr.Failure.Code)
	}
}

func TestModuleRequiresNetworkConfiguration(t *testing.T) {
	runtime, err := Module().Build(command.Deps{ControlPlane: &fakeMixedControlPlane{}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Missing --network-configuration; other required fields present.
	flags := allToolCreateFlags()
	flags = withChanged(flags, "tool-name", "my-tool")
	flags = withChanged(flags, "tool-type", "code-interpreter")
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err == nil {
		t.Fatalf("expected missing network-configuration error")
	}
	if !strings.Contains(err.Error(), "network-configuration") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify structured error code (stable contract, not just message text).
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *output.CLIError, got %T", err)
	}
	if cliErr.Failure.Code != "MISSING_REQUIRED_FLAG" {
		t.Fatalf("Failure.Code = %q, want MISSING_REQUIRED_FLAG", cliErr.Failure.Code)
	}
}

func TestModuleCreatesToolAndRendersText(t *testing.T) {
	toolID := "sdt-new123"
	cp := &fakeMixedControlPlane{resp: &ags.CreateSandboxToolResponseParams{
		ToolId: &toolID,
	}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	// Simulate: agr tool create -n my-tool -t code-interpreter --network-configuration '...' -d "A test tool"
	flags := minRequiredFlags()
	flags = withChanged(flags, "description", "A test tool")
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.action != "CreateSandboxTool" {
		t.Fatalf("action = %q, want CreateSandboxTool", cp.action)
	}
	// Verify request fields.
	if cp.request["ToolName"] != "my-tool" {
		t.Fatalf("request ToolName = %v", cp.request["ToolName"])
	}
	if cp.request["ToolType"] != "code-interpreter" {
		t.Fatalf("request ToolType = %v", cp.request["ToolType"])
	}
	if cp.request["Description"] != "A test tool" {
		t.Fatalf("request Description = %v", cp.request["Description"])
	}
	// Verify text output.
	var buf bytes.Buffer
	if result.Text == nil {
		t.Fatalf("expected text renderer, got nil")
	}
	result.Text(&buf)
	text := buf.String()
	if !strings.Contains(text, toolID) {
		t.Fatalf("text output missing tool ID %q: %s", toolID, text)
	}
	if !strings.Contains(text, "my-tool") {
		t.Fatalf("text output missing tool name: %s", text)
	}
	if !strings.Contains(text, "code-interpreter") {
		t.Fatalf("text output missing tool type: %s", text)
	}
	// Verify effects.
	if len(result.Effects) == 0 {
		t.Fatalf("expected at least one effect")
	}
	if result.Effects[0].Kind != "create" || result.Effects[0].Resource != "tool" {
		t.Fatalf("effect = %+v", result.Effects[0])
	}
}

func TestModuleCreatesToolWithAllOptionalFlags(t *testing.T) {
	toolID := "sdt-full"
	cp := &fakeMixedControlPlane{resp: &ags.CreateSandboxToolResponseParams{
		ToolId: &toolID,
	}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	// Simulate a fully-loaded create command with all optional flags.
	flags := minRequiredFlags()
	flags = withChanged(flags, "description", "full tool")
	flags = withChanged(flags, "default-timeout", "30m")
	flags = withChanged(flags, "tags", `[{"Key":"team","Value":"platform"}]`)
	flags = withChanged(flags, "client-token", "tok-abc")
	flags = withBoolChanged(flags, "persistent", true)
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// Verify optional fields in request.
	if cp.request["DefaultTimeout"] != "30m" {
		t.Fatalf("request DefaultTimeout = %v", cp.request["DefaultTimeout"])
	}
	if cp.request["ClientToken"] != "tok-abc" {
		t.Fatalf("request ClientToken = %v", cp.request["ClientToken"])
	}
	if cp.request["Tags"] == nil {
		t.Fatalf("request Tags is nil")
	}
	var buf bytes.Buffer
	result.Text(&buf)
	if !strings.Contains(buf.String(), toolID) {
		t.Fatalf("text output missing tool ID: %s", buf.String())
	}
}

func TestModuleCreatesToolWithStorageMountsRequiresRoleArn(t *testing.T) {
	runtime, err := Module().Build(command.Deps{ControlPlane: &fakeMixedControlPlane{}})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	// --storage-mounts without --role-arn should fail convenience validation.
	flags := minRequiredFlags()
	flags = withChanged(flags, "storage-mounts", `[{"Name":"data","MountPath":"/data","StorageSource":{"Cos":{"Endpoint":"cos.ap-guangzhou.myqcloud.com","BucketName":"b","BucketPath":"/"}}}]`)
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err == nil {
		t.Fatalf("expected role-arn required error")
	}
	if !strings.Contains(err.Error(), "role-arn") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify structured error code (stable contract, not just message text).
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *output.CLIError, got %T", err)
	}
	if cliErr.Failure.Code != "MISSING_REQUIRED_FLAG" {
		t.Fatalf("Failure.Code = %q, want MISSING_REQUIRED_FLAG", cliErr.Failure.Code)
	}
}

func TestModuleCreatesToolWithStorageMountsAndRoleArn(t *testing.T) {
	toolID := "sdt-cos"
	cp := &fakeMixedControlPlane{resp: &ags.CreateSandboxToolResponseParams{
		ToolId: &toolID,
	}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	// Valid: --storage-mounts with --role-arn.
	flags := minRequiredFlags()
	flags = withChanged(flags, "role-arn", "qcs::cam::uin/100000:roleName/my-role")
	flags = withChanged(flags, "storage-mounts", `[{"Name":"data","MountPath":"/data","StorageSource":{"Cos":{"Endpoint":"cos.ap-guangzhou.myqcloud.com","BucketName":"b","BucketPath":"/"}}}]`)
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.request["RoleArn"] != "qcs::cam::uin/100000:roleName/my-role" {
		t.Fatalf("request RoleArn = %v", cp.request["RoleArn"])
	}
	if cp.request["StorageMounts"] == nil {
		t.Fatalf("request StorageMounts is nil")
	}
}

func TestModuleBypassesConvenienceValidationWithRequestFlag(t *testing.T) {
	// --request mode sends raw JSON; convenience validation is skipped.
	toolID := "sdt-raw456"
	cp := &fakeMixedControlPlane{resp: &ags.CreateSandboxToolResponseParams{
		ToolId: &toolID,
	}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	// Simulate: agr tool create --request '{"ToolName":"raw","ToolType":"code-interpreter","NetworkConfiguration":{...}}'
	flags := allToolCreateFlags()
	f := flags["request"]
	f.String = `{"ToolName":"raw","ToolType":"code-interpreter","NetworkConfiguration":{"NetworkMode":"SANDBOX"}}`
	f.Changed = true
	flags["request"] = f
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.action != "CreateSandboxTool" {
		t.Fatalf("action = %q, want CreateSandboxTool", cp.action)
	}
	if cp.request["ToolName"] != "raw" {
		t.Fatalf("request ToolName = %v", cp.request["ToolName"])
	}
	var buf bytes.Buffer
	if result.Text != nil {
		result.Text(&buf)
		if !strings.Contains(buf.String(), toolID) {
			t.Fatalf("text output missing tool ID: %s", buf.String())
		}
	}
}

func TestModuleReturnsResultWhenResponseNotTyped(t *testing.T) {
	// Non-typed response (e.g. API schema change) should not panic.
	cp := &fakeMixedControlPlane{} // returns map[string]any{"ok": true}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: minRequiredFlags()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
}

// stderrIO returns an IOStreams backed by in-memory buffers with stderr forced
// to look like a TTY, for spinner wiring tests.
func stderrIO(stderrTTY bool) (*iostreams.IOStreams, *bytes.Buffer) {
	_, _, _, stderr := iostreams.Test()
	ios := &iostreams.IOStreams{ErrOut: stderr}
	ios.SetStderrTTY(stderrTTY)
	return ios, stderr
}

func TestSpinnerWritesSuccessLineWhenStderrIsTTY(t *testing.T) {
	// Interactive terminal (stderr TTY, text mode, interactive): the spinner
	// runs and emits "✓ Tool created" to stderr; stdout stays pure.
	toolID := "sdt-sp"
	cp := &fakeMixedControlPlane{resp: &ags.CreateSandboxToolResponseParams{ToolId: &toolID}}
	ios, stderrBuf := stderrIO(true)
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, IO: ios})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: minRequiredFlags()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := stderrBuf.String(); !strings.Contains(got, "✓ Tool created") {
		t.Fatalf("stderr missing success status line: %q", got)
	}
	// stdout must not be polluted by spinner frames.
	var stdout bytes.Buffer
	if result.Text == nil {
		t.Fatalf("expected text renderer")
	}
	result.Text(&stdout)
	for _, frame := range []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"} {
		if strings.Contains(stdout.String(), frame) {
			t.Fatalf("stdout polluted with spinner frame %q: %q", frame, stdout.String())
		}
	}
}

func TestSpinnerIsNopWhenStderrIsNotTTY(t *testing.T) {
	// Piped/redirected stderr (non-TTY): spinner is a Nop — no frames, no
	// status line — and the command still succeeds normally.
	toolID := "sdt-nop"
	cp := &fakeMixedControlPlane{resp: &ags.CreateSandboxToolResponseParams{ToolId: &toolID}}
	ios, stderrBuf := stderrIO(false)
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, IO: ios})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: minRequiredFlags()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := stderrBuf.String(); got != "" {
		t.Fatalf("expected empty stderr in non-TTY mode, got: %q", got)
	}
	var stdout bytes.Buffer
	if result.Text == nil {
		t.Fatalf("expected text renderer")
	}
	result.Text(&stdout)
	if !strings.Contains(stdout.String(), toolID) {
		t.Fatalf("text output missing tool ID: %s", stdout.String())
	}
}

func TestModuleDescriptor(t *testing.T) {
	module := Module()
	spec := module.Descriptor.Spec
	if spec.ID != "tool.create" {
		t.Fatalf("spec ID = %q, want tool.create", spec.ID)
	}
	if module.Descriptor.Generated == nil {
		t.Fatalf("expected generated descriptor")
	}
}
