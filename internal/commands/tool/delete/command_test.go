package delete

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

type fakeControlPlane struct {
	deleted []string
	fail    map[string]error
}

func (f *fakeControlPlane) DeleteTool(_ context.Context, toolID string) error {
	if err := f.fail[toolID]; err != nil {
		return err
	}
	f.deleted = append(f.deleted, toolID)
	return nil
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
