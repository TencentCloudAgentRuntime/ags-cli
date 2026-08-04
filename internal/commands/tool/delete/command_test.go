package delete

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

type fakeControlPlane struct {
	deleted       []string
	fail          map[string]error
	getErr        map[string]error
	notFoundOnGet map[string]bool
	events        []string
}

func TestModuleExposesWorkflowWaitWithoutChangingGeneratedDescriptor(t *testing.T) {
	module := Module()
	if !slices.ContainsFunc(module.Descriptor.Spec.Flags, func(flag command.FlagSpec) bool {
		return flag.Name == "wait"
	}) {
		t.Fatalf("tool.delete must expose workflow --wait")
	}
	if module.Descriptor.Generated == nil {
		t.Fatal("mixed module missing generated descriptor")
	}
	if slices.ContainsFunc(module.Descriptor.Generated.Spec.Flags, func(flag command.FlagSpec) bool {
		return flag.Name == "wait"
	}) {
		t.Fatalf("generated API descriptor must not include workflow --wait")
	}
}

func (f *fakeControlPlane) DeleteTool(_ context.Context, toolID string) error {
	f.events = append(f.events, "delete:"+toolID)
	if err := f.fail[toolID]; err != nil {
		return err
	}
	f.deleted = append(f.deleted, toolID)
	return nil
}

func (f *fakeControlPlane) GetTool(_ context.Context, toolID string) (*ags.SandboxTool, error) {
	f.events = append(f.events, "get:"+toolID)
	if f.notFoundOnGet[toolID] {
		return nil, output.NewNotFoundError("TOOL_NOT_FOUND", "missing", "hint")
	}
	if err := f.getErr[toolID]; err != nil {
		return nil, err
	}
	status := "DELETING"
	return &ags.SandboxTool{ToolId: &toolID, Status: &status}, nil
}

func (f *fakeControlPlane) IsNotFound(err error) bool {
	var cliErr *output.CLIError
	return errors.As(err, &cliErr) && cliErr.Failure != nil && cliErr.Failure.Kind == output.KindNotFound
}

func TestModuleDeletesMultipleTools(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"sdt-a", "sdt-b"},
		ArgValues: map[string]string{"tool-id": "sdt-a"},
		Flags:     map[string]command.FlagValue{},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(cp.deleted) != 2 || cp.deleted[0] != "sdt-a" || cp.deleted[1] != "sdt-b" {
		t.Fatalf("deleted = %#v", cp.deleted)
	}
	summary, ok := result.Data.(map[string]any)
	if !ok || summary["Deleted"] != 2 || summary["Failed"] != 0 {
		t.Fatalf("summary = %#v", result.Data)
	}
}

func TestModuleReturnsPartialSummary(t *testing.T) {
	cp := &fakeControlPlane{fail: map[string]error{"sdt-b": errors.New("boom")}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"sdt-a", "sdt-b"},
		ArgValues: map[string]string{"tool-id": "sdt-a"},
		Flags:     map[string]command.FlagValue{},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	summary := result.Data.(map[string]any)
	failed := summary["FailedIds"].([]string)
	if summary["Deleted"] != 1 || summary["Failed"] != 1 || len(failed) != 1 || failed[0] != "sdt-b" {
		t.Fatalf("summary = %#v", summary)
	}
	if result.ExitCode == 0 || len(result.Warnings) != 1 {
		t.Fatalf("partial result = %#v", result)
	}
	if result.ExitCode != output.ExitPartialSuccess || result.Failure == nil {
		t.Fatalf("partial result = %#v", result)
	}
	if result.Failure.Code != "PARTIAL_DELETE_FAILED" || result.Failure.Kind != output.KindPartialSuccess {
		t.Fatalf("failure = %#v", result.Failure)
	}
}

func TestModuleRequestDeletesSingleTool(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"sdt-a"},
		ArgValues: map[string]string{"tool-id": "sdt-a"},
		Flags: map[string]command.FlagValue{
			"request": {Name: "request", Type: command.FlagString, String: `{}`, Changed: true},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(cp.deleted) != 1 || cp.deleted[0] != "sdt-a" {
		t.Fatalf("deleted = %#v", cp.deleted)
	}
	summary := result.Data.(map[string]any)
	if summary["Deleted"] != 1 || summary["Failed"] != 0 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestSummaryDataIncludesFailedIdsOnlyWhenPresent(t *testing.T) {
	data := Summary{Deleted: 1}.Data()
	if _, ok := data["FailedIds"]; ok {
		t.Fatalf("unexpected FailedIds in %#v", data)
	}
	data = Summary{Deleted: 1, Failed: 1, FailedIDs: []string{"sdt-b"}}.Data()
	failed, ok := data["FailedIds"].([]string)
	if !ok || len(failed) != 1 || failed[0] != "sdt-b" {
		t.Fatalf("FailedIds = %#v", data["FailedIds"])
	}
}

func TestModuleRequiresControlPlane(t *testing.T) {
	_, err := Module().Build(command.Deps{})
	if err == nil {
		t.Fatalf("expected missing control plane error")
	}
}

func TestModuleRequestDeleteReturnsControlPlaneError(t *testing.T) {
	cp := &fakeControlPlane{fail: map[string]error{"sdt-a": errors.New("boom")}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"sdt-a"},
		ArgValues: map[string]string{"tool-id": "sdt-a"},
		Flags: map[string]command.FlagValue{
			"request": {Name: "request", Type: command.FlagString, String: `{}`, Changed: true},
		},
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("error = %v, want boom", err)
	}
}

func TestModuleRejectsRequestWithMultipleTools(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"sdt-a", "sdt-b"},
		ArgValues: map[string]string{"tool-id": "sdt-a"},
		Flags: map[string]command.FlagValue{
			"request": {Name: "request", Type: command.FlagString, String: `{}`, Changed: true},
		},
	})
	if err == nil {
		t.Fatalf("expected request conflict error")
	}
}

func TestResultFromSummaryWritesSuccessToStdoutAndWarningsToStderr(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	result := resultFromSummary(
		Summary{Deleted: 1, DeletedIDs: []string{"sdt-a"}},
		[]string{"failed to delete sdt-b: boom"},
		&stderr,
	)
	if result.Text == nil {
		t.Fatal("expected text renderer")
	}

	result.Text(&stdout)

	if got := stdout.String(); got != "Tool deleted: sdt-a\n" {
		t.Fatalf("stdout = %q", got)
	}
	if got := stderr.String(); got != "Warning: failed to delete sdt-b: boom\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestModuleDryRunDoesNotDelete(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"sdt-a", "sdt-b"},
		ArgValues: map[string]string{"tool-id": "sdt-a"},
		Flags: map[string]command.FlagValue{
			"dry-run": {Name: "dry-run", Type: command.FlagBool, Bool: true, Changed: true},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// No actual deletion should have occurred.
	if len(cp.deleted) != 0 {
		t.Fatalf("dry-run should not delete, got deleted = %#v", cp.deleted)
	}
	data := result.Data.(map[string]any)
	if data["DryRun"] != true {
		t.Fatalf("expected DryRun=true, got %#v", data)
	}
	wouldDelete := data["WouldDelete"].([]string)
	if len(wouldDelete) != 2 || wouldDelete[0] != "sdt-a" || wouldDelete[1] != "sdt-b" {
		t.Fatalf("WouldDelete = %#v", wouldDelete)
	}
}

func TestModuleYesSkipsConfirmation(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"sdt-a"},
		ArgValues: map[string]string{"tool-id": "sdt-a"},
		Flags: map[string]command.FlagValue{
			"yes": {Name: "yes", Type: command.FlagBool, Bool: true, Changed: true},
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(cp.deleted) != 1 || cp.deleted[0] != "sdt-a" {
		t.Fatalf("expected deletion with --yes, got deleted = %#v", cp.deleted)
	}
	summary := result.Data.(map[string]any)
	if summary["Deleted"] != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestModuleWaitSubmitsAllDeletesBeforePollingForNotFound(t *testing.T) {
	cp := &fakeControlPlane{notFoundOnGet: map[string]bool{
		"sdt-a": true,
		"sdt-b": true,
	}}
	runtime, err := Module().Build(command.Deps{
		ControlPlane: cp,
		Values: map[string]any{resourcewait.OptionsKey: resourcewait.Options{
			Interval: time.Millisecond,
			Timeout:  50 * time.Millisecond,
		}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args: []string{"sdt-a", "sdt-b"},
		Flags: map[string]command.FlagValue{
			"wait": {Name: "wait", Type: command.FlagBool, Bool: true},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantEvents := []string{"delete:sdt-a", "delete:sdt-b", "get:sdt-a", "get:sdt-b"}
	if !slices.Equal(cp.events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", cp.events, wantEvents)
	}
	if len(cp.deleted) != 2 {
		t.Fatalf("mutations = %#v, want exactly two", cp.deleted)
	}
	summary := result.Data.(map[string]any)
	if summary["Deleted"] != 2 || summary["Failed"] != 0 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestModuleWaitDoesNotTreatOtherGetErrorsAsDeleted(t *testing.T) {
	cp := &fakeControlPlane{getErr: map[string]error{"sdt-a": errors.New("network down")}}
	runtime, err := Module().Build(command.Deps{
		ControlPlane: cp,
		Values: map[string]any{resourcewait.OptionsKey: resourcewait.Options{
			Interval: time.Millisecond,
			Timeout:  50 * time.Millisecond,
		}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args: []string{"sdt-a"},
		Flags: map[string]command.FlagValue{
			"wait": {Name: "wait", Type: command.FlagBool, Bool: true},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(cp.deleted) != 1 {
		t.Fatalf("mutations = %#v, want exactly one", cp.deleted)
	}
	if result.ExitCode != output.ExitPartialSuccess || result.Failure == nil {
		t.Fatalf("result = %#v", result)
	}
	summary := result.Data.(map[string]any)
	if summary["Deleted"] != 0 || summary["Failed"] != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}
