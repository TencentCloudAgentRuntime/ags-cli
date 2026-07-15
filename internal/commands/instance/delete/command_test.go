package delete

import (
	"context"
	"errors"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

func TestModuleKeepsGeneratedAPIDescriptorAndWorkflowFlag(t *testing.T) {
	module := Module()
	if module.Descriptor.Generated == nil {
		t.Fatalf("mixed module missing generated descriptor snapshot")
	}
	if module.Descriptor.Generated.Spec.ID != "instance.delete" {
		t.Fatalf("generated id = %q", module.Descriptor.Generated.Spec.ID)
	}
	if got := module.Descriptor.Generated.Spec.Path; len(got) != 2 || got[0] != "instance" || got[1] != "delete" {
		t.Fatalf("generated path = %#v", got)
	}
	api, ok := module.Descriptor.API.(apicli.APIDescriptor)
	if !ok {
		t.Fatalf("API descriptor type = %T", module.Descriptor.API)
	}
	if api.API.Action != "StopSandboxInstance" {
		t.Fatalf("API action = %q, want StopSandboxInstance", api.API.Action)
	}
	if api.API.RequestType != "StopSandboxInstanceRequest" {
		t.Fatalf("request type = %q", api.API.RequestType)
	}
	if api.API.ResponseType != "StopSandboxInstanceResponse" {
		t.Fatalf("response type = %q", api.API.ResponseType)
	}
	if !hasFlag(module.Descriptor.Spec.Flags, "ignore-not-found") {
		t.Fatalf("final spec missing --ignore-not-found")
	}
}

func TestModuleSupportsMultiDeleteWorkflow(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args: []string{"ins-a", "ins-b"},
		Flags: map[string]command.FlagValue{
			"ignore-not-found": {Name: "ignore-not-found", Type: command.FlagBool},
			"request":          {Name: "request", Type: command.FlagString},
		},
		ArgValues: map[string]string{"instance-id": "ins-a"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	summary, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("summary type = %T", result.Data)
	}
	if summary["Deleted"] != 2 || summary["Failed"] != 0 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestModuleIgnoreNotFoundTreatsMissingAsAlreadyAbsent(t *testing.T) {
	cp := &fakeControlPlane{notFound: map[string]bool{"ins-missing": true}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args: []string{"ins-missing"},
		Flags: map[string]command.FlagValue{
			"ignore-not-found": {Name: "ignore-not-found", Type: command.FlagBool, Changed: true, Bool: true},
			"request":          {Name: "request", Type: command.FlagString},
		},
		ArgValues: map[string]string{"instance-id": "ins-missing"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	summary := result.Data.(map[string]any)
	absent := summary["AlreadyAbsent"].([]string)
	if len(absent) != 1 || absent[0] != "ins-missing" {
		t.Fatalf("summary = %#v", summary)
	}
	if summary["Deleted"] != 0 || summary["Failed"] != 0 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestModulePartialFailure(t *testing.T) {
	cp := &fakeControlPlane{fail: map[string]error{"ins-b": errors.New("boom")}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args: []string{"ins-a", "ins-b"},
		Flags: map[string]command.FlagValue{
			"ignore-not-found": {Name: "ignore-not-found", Type: command.FlagBool},
			"request":          {Name: "request", Type: command.FlagString},
		},
		ArgValues: map[string]string{"instance-id": "ins-a"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != output.ExitPartialSuccess || result.Failure == nil {
		t.Fatalf("result = %#v", result)
	}
	summary := result.Data.(map[string]any)
	failed := summary["FailedIds"].([]string)
	if summary["Deleted"] != 1 || summary["Failed"] != 1 || failed[0] != "ins-b" {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestSummaryDataReturnsCopies(t *testing.T) {
	summary := Summary{
		Deleted:       1,
		Failed:        1,
		FailedIDs:     []string{"ins-failed"},
		AlreadyAbsent: []string{"ins-missing"},
	}
	data := summary.Data()
	failed := data["FailedIds"].([]string)
	absent := data["AlreadyAbsent"].([]string)
	failed[0] = "mutated"
	absent[0] = "mutated"
	if summary.FailedIDs[0] != "ins-failed" || summary.AlreadyAbsent[0] != "ins-missing" {
		t.Fatalf("Data leaked backing slices: %#v", summary)
	}
}

func TestModuleRequiresControlPlane(t *testing.T) {
	_, err := Module().Build(command.Deps{})
	if err == nil {
		t.Fatalf("expected missing control plane error")
	}
}

func TestModuleRequestDeletesSingleInstance(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"ins-a"},
		ArgValues: map[string]string{"instance-id": "ins-a"},
		Flags: map[string]command.FlagValue{
			"request":          {Name: "request", Type: command.FlagString, String: `{}`, Changed: true},
			"ignore-not-found": {Name: "ignore-not-found", Type: command.FlagBool},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	summary := result.Data.(map[string]any)
	if summary["Deleted"] != 1 || summary["Failed"] != 0 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestModuleRejectsRequestWithMultipleInstances(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"ins-a", "ins-b"},
		ArgValues: map[string]string{"instance-id": "ins-a"},
		Flags: map[string]command.FlagValue{
			"request": {Name: "request", Type: command.FlagString, String: `{}`, Changed: true},
		},
	})
	if cliErr, ok := err.(*output.CLIError); !ok || cliErr.Failure.Code != "REQUEST_FLAG_CONFLICT" {
		t.Fatalf("error = %#v, want REQUEST_FLAG_CONFLICT", err)
	}
}

func TestIsNotFoundWithoutClassifier(t *testing.T) {
	if isNotFound(struct{}{}, output.NewNotFoundError("INSTANCE_NOT_FOUND", "missing", "hint")) {
		t.Fatalf("isNotFound should require a classifier")
	}
}

func hasFlag(flags []command.FlagSpec, name string) bool {
	for _, flag := range flags {
		if flag.Name == name {
			return true
		}
	}
	return false
}

type fakeControlPlane struct {
	deleted  []string
	fail     map[string]error
	notFound map[string]bool
}

func (f *fakeControlPlane) DeleteInstance(_ context.Context, instanceID string) error {
	if f.notFound[instanceID] {
		return output.NewNotFoundError("INSTANCE_NOT_FOUND", "missing", "hint")
	}
	if err := f.fail[instanceID]; err != nil {
		return err
	}
	f.deleted = append(f.deleted, instanceID)
	return nil
}

func (f *fakeControlPlane) IsNotFound(err error) bool {
	var cliErr *output.CLIError
	return errors.As(err, &cliErr) && cliErr.Failure != nil && cliErr.Failure.Kind == output.KindNotFound
}

func TestModuleDryRunDoesNotDelete(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args: []string{"ins-a", "ins-b"},
		Flags: map[string]command.FlagValue{
			"dry-run":          {Name: "dry-run", Type: command.FlagBool, Bool: true, Changed: true},
			"ignore-not-found": {Name: "ignore-not-found", Type: command.FlagBool},
			"request":          {Name: "request", Type: command.FlagString},
		},
		ArgValues: map[string]string{"instance-id": "ins-a"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(cp.deleted) != 0 {
		t.Fatalf("dry-run should not delete, got deleted = %#v", cp.deleted)
	}
	data := result.Data.(map[string]any)
	if data["DryRun"] != true {
		t.Fatalf("expected DryRun=true, got %#v", data)
	}
	wouldDelete := data["WouldDelete"].([]string)
	if len(wouldDelete) != 2 || wouldDelete[0] != "ins-a" || wouldDelete[1] != "ins-b" {
		t.Fatalf("WouldDelete = %#v", wouldDelete)
	}
}

func TestModuleYesSkipsConfirmation(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args: []string{"ins-a"},
		Flags: map[string]command.FlagValue{
			"yes":              {Name: "yes", Type: command.FlagBool, Bool: true, Changed: true},
			"ignore-not-found": {Name: "ignore-not-found", Type: command.FlagBool},
			"request":          {Name: "request", Type: command.FlagString},
		},
		ArgValues: map[string]string{"instance-id": "ins-a"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(cp.deleted) != 1 || cp.deleted[0] != "ins-a" {
		t.Fatalf("expected deletion with --yes, got deleted = %#v", cp.deleted)
	}
	summary := result.Data.(map[string]any)
	if summary["Deleted"] != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}
