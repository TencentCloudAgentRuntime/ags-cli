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
// create flow (parameter validation → request build → executor → response render).
type fakeMixedControlPlane struct {
	action  string
	request map[string]any
	resp    *ags.StartSandboxInstanceResponseParams
}

func (f *fakeMixedControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	if f.resp != nil {
		return f.resp, nil
	}
	return map[string]any{"ok": true}, nil
}

// allInstanceCreateFlags returns a complete flag set mimicking what Cobra registers
// for `agr instance create`. Unset optional flags have Changed=false.
func allInstanceCreateFlags() map[string]command.FlagValue {
	return map[string]command.FlagValue{
		"tool-id":              {Name: "tool-id", Type: command.FlagString},
		"tool-name":            {Name: "tool-name", Type: command.FlagString},
		"timeout":              {Name: "timeout", Type: command.FlagString},
		"client-token":         {Name: "client-token", Type: command.FlagString},
		"mount-options":        {Name: "mount-options", Type: command.FlagString},
		"custom-configuration": {Name: "custom-configuration", Type: command.FlagString},
		"auth-mode":            {Name: "auth-mode", Type: command.FlagString},
		"metadata":             {Name: "metadata", Type: command.FlagString},
		"request":              {Name: "request", Type: command.FlagString},
	}
}

// withChanged returns a copy of flags with the named flag set to the given value.
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

func TestModuleBuildsWithoutError(t *testing.T) {
	module := Module()
	if module.Descriptor.Spec.ID == "" {
		t.Fatalf("expected non-empty spec ID")
	}
	if module.Build == nil {
		t.Fatalf("expected non-nil Build func")
	}
}

func TestModuleRequiresToolSelection(t *testing.T) {
	module := Module()
	runtime, err := module.Build(command.Deps{ControlPlane: &fakeMixedControlPlane{}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// No --tool-name or --tool-id set → should fail.
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		Flags: allInstanceCreateFlags(),
	})
	if err == nil {
		t.Fatalf("expected missing tool selection error")
	}
	if !strings.Contains(err.Error(), "tool-name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestModuleRejectsConflictingToolFlags(t *testing.T) {
	module := Module()
	runtime, err := module.Build(command.Deps{ControlPlane: &fakeMixedControlPlane{}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Both --tool-name and --tool-id set → should fail.
	flags := withChanged(allInstanceCreateFlags(), "tool-name", "my-tool")
	flags = withChanged(flags, "tool-id", "sdt-123")
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err == nil {
		t.Fatalf("expected conflicting flags error")
	}
	if !strings.Contains(err.Error(), "both") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestModuleCreatesInstanceAndRendersText(t *testing.T) {
	id := "ins-created"
	name := "tool-name"
	status := "RUNNING"
	created := "2026-05-21T10:00:00Z"
	mountName := "workspace"
	cp := &fakeMixedControlPlane{resp: &ags.StartSandboxInstanceResponseParams{
		Instance: &ags.SandboxInstance{
			InstanceId:   &id,
			ToolName:     &name,
			Status:       &status,
			CreateTime:   &created,
			MountOptions: []*ags.MountOption{{Name: &mountName}},
		},
	}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	// Simulate: agr instance create --tool-name tool-name --timeout 6m --mount-options '[...]'
	flags := withChanged(allInstanceCreateFlags(), "tool-name", "tool-name")
	flags = withChanged(flags, "timeout", "6m")
	flags = withChanged(flags, "mount-options", `[{"Name":"workspace","MountPath":"/workspace"}]`)
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.action != "StartSandboxInstance" {
		t.Fatalf("action = %q, want StartSandboxInstance", cp.action)
	}
	// Verify request fields were sent.
	if cp.request["ToolName"] != "tool-name" {
		t.Fatalf("request ToolName = %v", cp.request["ToolName"])
	}
	if cp.request["Timeout"] != "6m" {
		t.Fatalf("request Timeout = %v", cp.request["Timeout"])
	}
	// Verify text output contains instance ID and mount info.
	var buf bytes.Buffer
	if result.Text == nil {
		t.Fatalf("expected text renderer, got nil")
	}
	result.Text(&buf)
	text := buf.String()
	if !strings.Contains(text, id) {
		t.Fatalf("text output missing instance ID %q: %s", id, text)
	}
	if !strings.Contains(text, "workspace") {
		t.Fatalf("text output missing mount name: %s", text)
	}
	if !strings.Contains(text, "RUNNING") {
		t.Fatalf("text output missing status: %s", text)
	}
}

func TestModuleCreatesInstanceWithToolID(t *testing.T) {
	id := "ins-byid"
	toolID := "sdt-abc123"
	status := "RUNNING"
	created := "2026-05-21T10:00:00Z"
	cp := &fakeMixedControlPlane{resp: &ags.StartSandboxInstanceResponseParams{
		Instance: &ags.SandboxInstance{
			InstanceId: &id,
			ToolId:     &toolID,
			Status:     &status,
			CreateTime: &created,
		},
	}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	// Simulate: agr instance create --tool-id sdt-abc123 --auth-mode TOKEN
	flags := withChanged(allInstanceCreateFlags(), "tool-id", toolID)
	flags = withChanged(flags, "auth-mode", "TOKEN")
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.action != "StartSandboxInstance" {
		t.Fatalf("action = %q, want StartSandboxInstance", cp.action)
	}
	if got := cp.request["ToolId"]; got != toolID {
		t.Fatalf("request ToolId = %v, want %q", got, toolID)
	}
	if got := cp.request["AuthMode"]; got != "TOKEN" {
		t.Fatalf("request AuthMode = %v, want TOKEN", got)
	}
	var buf bytes.Buffer
	result.Text(&buf)
	if !strings.Contains(buf.String(), id) {
		t.Fatalf("text output missing instance ID: %s", buf.String())
	}
}

func TestModuleCreatesInstanceWithMetadata(t *testing.T) {
	id := "ins-meta"
	status := "RUNNING"
	created := "2026-05-21T10:00:00Z"
	cp := &fakeMixedControlPlane{resp: &ags.StartSandboxInstanceResponseParams{
		Instance: &ags.SandboxInstance{
			InstanceId: &id,
			Status:     &status,
			CreateTime: &created,
		},
	}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	// Simulate: agr instance create --tool-name my-tool --metadata '[{"Key":"env","Value":"test"}]'
	flags := withChanged(allInstanceCreateFlags(), "tool-name", "my-tool")
	flags = withChanged(flags, "metadata", `[{"Key":"env","Value":"test"}]`)
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// Metadata should be in request.
	if cp.request["Metadata"] == nil {
		t.Fatalf("request Metadata is nil")
	}
	if result.Text == nil {
		t.Fatalf("expected text renderer")
	}
}

func TestModuleBypassesValidationWithRequestFlag(t *testing.T) {
	// --request mode sends raw JSON and skips tool-name/tool-id validation.
	id := "ins-raw"
	status := "RUNNING"
	created := "2026-05-21T10:00:00Z"
	cp := &fakeMixedControlPlane{resp: &ags.StartSandboxInstanceResponseParams{
		Instance: &ags.SandboxInstance{
			InstanceId: &id,
			Status:     &status,
			CreateTime: &created,
		},
	}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	// Simulate: agr instance create --request '{"ToolId":"sdt-xyz","Timeout":"10m"}'
	flags := allInstanceCreateFlags()
	f := flags["request"]
	f.String = `{"ToolId":"sdt-xyz","Timeout":"10m"}`
	f.Changed = true
	flags["request"] = f
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if cp.request["ToolId"] != "sdt-xyz" {
		t.Fatalf("request ToolId = %v", cp.request["ToolId"])
	}
	var buf bytes.Buffer
	result.Text(&buf)
	if !strings.Contains(buf.String(), id) {
		t.Fatalf("text output missing instance ID: %s", buf.String())
	}
}

func TestModuleReturnsResultWhenResponseNotTyped(t *testing.T) {
	// When the fake CP returns a non-typed response, the handler should not panic.
	cp := &fakeMixedControlPlane{} // no resp → returns map[string]any{"ok": true}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	flags := withChanged(allInstanceCreateFlags(), "tool-name", "t")
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
}

func TestModuleReturnsErrorWhenInstanceIsNil(t *testing.T) {
	// Regression: when API returns a typed response but Instance is nil,
	// the spinner must NOT print "✓ Instance created".
	cp := &fakeMixedControlPlane{resp: &ags.StartSandboxInstanceResponseParams{
		Instance: nil, // no instance in response
	}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	flags := withChanged(allInstanceCreateFlags(), "tool-name", "t")
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err == nil {
		t.Fatal("expected error for nil Instance")
	}
	if !strings.Contains(err.Error(), "no instance returned from API") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify structured error code is preserved.
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatal("expected *output.CLIError")
	}
	if cliErr.Failure.Code != "INTERNAL_ERROR" {
		t.Fatalf("Failure.Code = %q, want INTERNAL_ERROR", cliErr.Failure.Code)
	}
}

// stderrIO returns an IOStreams backed by in-memory buffers with stderr forced
// to look like a TTY (or not), for spinner wiring tests.
func stderrIO(stderrTTY bool) (*iostreams.IOStreams, *bytes.Buffer) {
	_, _, _, stderr := iostreams.Test()
	ios := &iostreams.IOStreams{ErrOut: stderr}
	ios.SetStderrTTY(stderrTTY)
	return ios, stderr
}

func TestSpinnerWritesSuccessLineWhenStderrIsTTY(t *testing.T) {
	// In a real interactive terminal (stderr is a TTY, text mode, interactive),
	// the spinner must run and emit the final "✓ Instance created" status line
	// to stderr — while stdout stays pure (no spinner noise on the data stream).
	id := "ins-sp"
	status := "RUNNING"
	created := "2026-05-21T10:00:00Z"
	cp := &fakeMixedControlPlane{resp: &ags.StartSandboxInstanceResponseParams{
		Instance: &ags.SandboxInstance{
			InstanceId: &id, Status: &status, CreateTime: &created,
		},
	}}
	ios, stderrBuf := stderrIO(true)
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, IO: ios})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	flags := withChanged(allInstanceCreateFlags(), "tool-name", "t")
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Final status line reached stderr.
	if got := stderrBuf.String(); !strings.Contains(got, "✓ Instance created") {
		t.Fatalf("stderr missing success status line: %q", got)
	}
	// stdout (the text renderer) must not be polluted by spinner frames.
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
	// Piped/redirected stderr (non-TTY): the spinner is a Nop. No frames, no
	// status line, and the command still succeeds normally.
	id := "ins-nop"
	status := "RUNNING"
	created := "2026-05-21T10:00:00Z"
	cp := &fakeMixedControlPlane{resp: &ags.StartSandboxInstanceResponseParams{
		Instance: &ags.SandboxInstance{
			InstanceId: &id, Status: &status, CreateTime: &created,
		},
	}}
	ios, stderrBuf := stderrIO(false)
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, IO: ios})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	flags := withChanged(allInstanceCreateFlags(), "tool-name", "t")
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
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
	if !strings.Contains(stdout.String(), id) {
		t.Fatalf("text output missing instance ID: %s", stdout.String())
	}
}
