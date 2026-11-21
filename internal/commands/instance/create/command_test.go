package create

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// fakeMixedControlPlane implements apicli.ControlPlane for testing the full
// create flow (parameter validation → request build → executor → response render).
type fakeMixedControlPlane struct {
	action        string
	request       map[string]any
	resp          *ags.StartSandboxInstanceResponseParams
	calls         int
	getCalls      int
	finalInstance *ags.SandboxInstance
}

func (f *fakeMixedControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action = action
	f.request = request
	f.calls++
	if f.resp != nil {
		return f.resp, nil
	}
	return map[string]any{"ok": true}, nil
}

func (f *fakeMixedControlPlane) GetInstance(_ context.Context, _ string) (apivalue.Object, error) {
	f.getCalls++
	return apivalue.Decode(f.finalInstance)
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
		"storage-mounts":       {Name: "storage-mounts", Type: command.FlagString},
		"source-instance-id":   {Name: "source-instance-id", Type: command.FlagString},
		"snapshot-id":          {Name: "snapshot-id", Type: command.FlagString},
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

func TestModuleWaitsAfterCreatingExactlyOnce(t *testing.T) {
	id := "ins-created"
	starting := "STARTING"
	running := "RUNNING"
	cp := &fakeMixedControlPlane{
		resp:          &ags.StartSandboxInstanceResponseParams{Instance: &ags.SandboxInstance{InstanceId: &id, Status: &starting}},
		finalInstance: &ags.SandboxInstance{InstanceId: &id, Status: &running},
	}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, Values: map[string]any{
		resourcewait.OptionsKey: resourcewait.Options{Interval: time.Millisecond, Timeout: 50 * time.Millisecond},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	flags := withChanged(allInstanceCreateFlags(), "tool-name", "tool-name")
	flags = withChanged(flags, "start-mode", "WAIT_UNTIL_HEALTHY")
	flags["wait"] = command.FlagValue{Name: "wait", Type: command.FlagBool, Bool: true}
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cp.calls != 1 || cp.getCalls != 1 {
		t.Fatalf("Call = %d, GetInstance = %d", cp.calls, cp.getCalls)
	}
	if cp.request["StartMode"] != "WAIT_UNTIL_HEALTHY" {
		t.Fatalf("StartMode = %v", cp.request["StartMode"])
	}
	if result.Data.(map[string]any)["Status"] != running || len(result.Effects) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestModuleWaitDoesNotTreatStoppedInstanceAsCreated(t *testing.T) {
	id := "ins-created"
	starting := "STARTING"
	stopped := "STOPPED"
	cp := &fakeMixedControlPlane{
		resp:          &ags.StartSandboxInstanceResponseParams{Instance: &ags.SandboxInstance{InstanceId: &id, Status: &starting}},
		finalInstance: &ags.SandboxInstance{InstanceId: &id, Status: &stopped},
	}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, Values: map[string]any{
		resourcewait.OptionsKey: resourcewait.Options{Interval: time.Millisecond, Timeout: 50 * time.Millisecond},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	flags := withChanged(allInstanceCreateFlags(), "tool-name", "tool-name")
	flags["wait"] = command.FlagValue{Name: "wait", Type: command.FlagBool, Bool: true}
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) || cliErr.Failure.Code != "WAIT_PREEMPTED" {
		t.Fatalf("error = %#v, want WAIT_PREEMPTED", err)
	}
	if cp.calls != 1 || cp.getCalls != 1 {
		t.Fatalf("Call = %d, GetInstance = %d", cp.calls, cp.getCalls)
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
	// Simulate: agr instance create --tool-name my-tool --metadata '[{"Name":"env","Value":"test"}]'
	flags := withChanged(allInstanceCreateFlags(), "tool-name", "my-tool")
	flags = withChanged(flags, "metadata", `[{"Name":"env","Value":"test"}]`)
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

func TestModuleRejectsMalformedResponse(t *testing.T) {
	// When the fake CP returns a non-typed response, the handler should not panic.
	cp := &fakeMixedControlPlane{} // no resp → returns map[string]any{"ok": true}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	flags := withChanged(allInstanceCreateFlags(), "tool-name", "t")
	result, err := runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	if err == nil || result != nil {
		t.Fatalf("malformed response: result=%#v err=%v", result, err)
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

func TestModuleBuildsStorageMountsForEveryStartMode(t *testing.T) {
	const mounts = `[{"Name":"runtime","MountPath":"/mnt/agr-start","ReadOnly":true,"StorageSource":{"Image":{"Reference":"ccr.ccs.tencentyun.com/ags-image/envd:v0.5.14","ImageRegistryType":"personal","SubPath":"/usr/bin"}}}]`
	var want []any
	if err := json.Unmarshal([]byte(mounts), &want); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(t.TempDir(), "mounts.json")
	if err := os.WriteFile(filePath, []byte(mounts), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		flags map[string]command.FlagValue
		stdin string
	}{
		{
			name:  "tool inline",
			flags: withChanged(withChanged(allInstanceCreateFlags(), "tool-id", "sdt-unit"), "storage-mounts", mounts),
		},
		{
			name:  "tool file",
			flags: withChanged(withChanged(allInstanceCreateFlags(), "tool-id", "sdt-unit"), "storage-mounts", "@"+filePath),
		},
		{
			name:  "tool stdin",
			flags: withChanged(withChanged(allInstanceCreateFlags(), "tool-id", "sdt-unit"), "storage-mounts", "-"),
			stdin: mounts,
		},
		{
			name:  "tool request",
			flags: withChanged(allInstanceCreateFlags(), "request", `{"ToolId":"sdt-unit","StorageMounts":`+mounts+`}`),
		},
		{
			name:  "clone inline",
			flags: withChanged(withChanged(allInstanceCreateFlags(), "source-instance-id", "ins-source"), "storage-mounts", mounts),
		},
		{
			name:  "snapshot inline",
			flags: withChanged(withChanged(allInstanceCreateFlags(), "snapshot-id", "snapshot-unit"), "storage-mounts", mounts),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp := &fakeMixedControlPlane{resp: createdInstanceResponse("ins-unit")}
			runtime, err := Module().Build(command.Deps{ControlPlane: cp})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			req := command.Request{Flags: tc.flags}
			if tc.stdin != "" {
				req.Stdin = strings.NewReader(tc.stdin)
			}
			result, err := runtime.Handler.Run(context.Background(), req)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got, ok := cp.request["StorageMounts"].([]any)
			if !ok || !reflect.DeepEqual(got, want) {
				t.Fatalf("StorageMounts = %#v, want %#v", cp.request["StorageMounts"], want)
			}
			if cp.action != "StartSandboxInstance" || result.Data.(map[string]any)["InstanceId"] != "ins-unit" {
				t.Fatalf("unexpected result: %s %#v", cp.action, result.Data)
			}
		})
	}
}

func TestModuleOmitsAbsentStorageMounts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  []any
	}{
		{"absent", "", nil},
		{"empty array", "[]", []any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp := &fakeMixedControlPlane{resp: createdInstanceResponse("ins-unit")}
			runtime, err := Module().Build(command.Deps{ControlPlane: cp})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			flags := withChanged(allInstanceCreateFlags(), "tool-id", "sdt-unit")
			if tc.value != "" {
				flags = withChanged(flags, "storage-mounts", tc.value)
			}
			if _, err := runtime.Handler.Run(context.Background(), command.Request{Flags: flags}); err != nil {
				t.Fatalf("Run: %v", err)
			}
			got, present := cp.request["StorageMounts"]
			if tc.want == nil {
				if present {
					t.Fatalf("invented StorageMounts: %#v", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("StorageMounts = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestModuleRejectsInvalidStorageMountsLocally(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags map[string]command.FlagValue
		code  string
	}{
		{
			name:  "invalid json flag",
			flags: withChanged(withChanged(allInstanceCreateFlags(), "tool-id", "sdt-unit"), "storage-mounts", `[{"Name":`),
			code:  "INVALID_JSON_FLAG",
		},
		{
			name:  "request and flag conflict",
			flags: withChanged(withChanged(allInstanceCreateFlags(), "request", `{"ToolId":"sdt-unit"}`), "storage-mounts", `[]`),
			code:  "REQUEST_FLAG_CONFLICT",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp := &fakeMixedControlPlane{resp: createdInstanceResponse("ins-unit")}
			runtime, err := Module().Build(command.Deps{ControlPlane: cp})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: tc.flags})
			var cliErr *output.CLIError
			if !errors.As(err, &cliErr) || cliErr.Failure.Code != tc.code {
				t.Fatalf("error = %v, want %s", err, tc.code)
			}
			if cp.calls != 0 {
				t.Fatal("invalid mounts caused an API call")
			}
		})
	}
}

func createdInstanceResponse(id string) *ags.StartSandboxInstanceResponseParams {
	status := "RUNNING"
	return &ags.StartSandboxInstanceResponseParams{
		Instance:  &ags.SandboxInstance{InstanceId: &id, Status: &status},
		RequestId: ptr("request-create"),
	}
}

func ptr(value string) *string { return &value }
