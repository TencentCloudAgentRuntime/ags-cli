package delete

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/resourcewait"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

var errDeploymentNotFound = errors.New("ResourceNotFound.Deployment")

type getResult struct {
	deployment *ags.Deployment
	err        error
}

type fakeControlPlane struct {
	callAction  string
	callRequest map[string]any
	response    any
	gets        []getResult
	getCalls    int
}

func (f *fakeControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.callAction, f.callRequest = action, request
	if f.response == nil {
		f.response = &ags.DeleteDeploymentResponseParams{}
	}
	return f.response, nil
}

func (f *fakeControlPlane) GetDeployment(_ context.Context, _ string) (*ags.Deployment, error) {
	index := f.getCalls
	f.getCalls++
	if index >= len(f.gets) {
		index = len(f.gets) - 1
	}
	return f.gets[index].deployment, f.gets[index].err
}

func (f *fakeControlPlane) IsDeploymentNotFound(err error) bool {
	return errors.Is(err, errDeploymentNotFound)
}

func TestModuleUsesSharedOptionalWaitFlag(t *testing.T) {
	module := Module()
	waitIndex := slices.IndexFunc(module.Descriptor.Spec.Flags, func(flag command.FlagSpec) bool { return flag.Name == "wait" })
	timeoutIndex := slices.IndexFunc(module.Descriptor.Spec.Flags, func(flag command.FlagSpec) bool { return flag.Name == "timeout" })
	if waitIndex < 0 || module.Descriptor.Spec.Flags[waitIndex].Default != nil {
		t.Fatalf("wait flag = %#v, want opt-in shared flag", module.Descriptor.Spec.Flags)
	}
	if timeoutIndex >= 0 {
		t.Fatalf("timeout flag = %#v, want shared internal timing", module.Descriptor.Spec.Flags)
	}
	if module.Descriptor.Generated == nil || slices.ContainsFunc(module.Descriptor.Generated.Spec.Flags, func(flag command.FlagSpec) bool {
		return flag.Name == "wait"
	}) {
		t.Fatal("workflow flags leaked into generated descriptor")
	}
}

func TestModuleWaitsUntilExactDeploymentNotFound(t *testing.T) {
	deleting := "DELETING"
	cp := &fakeControlPlane{gets: []getResult{
		{deployment: &ags.Deployment{Status: &deleting}},
		{err: errDeploymentNotFound},
	}}
	runtime := buildRuntime(t, cp)
	result, err := runtime.Handler.Run(context.Background(), deleteRequest(map[string]command.FlagValue{
		"wait": {Name: "wait", Type: command.FlagBool, Bool: true, Changed: true},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cp.callAction != "DeleteDeployment" || cp.callRequest["DeploymentId"] != "dpl-a1b2c3d4" || cp.getCalls != 2 {
		t.Fatalf("action=%q request=%#v getCalls=%d", cp.callAction, cp.callRequest, cp.getCalls)
	}
	var text bytes.Buffer
	result.Text(&text)
	if text.String() != "Deployment deleted: dpl-a1b2c3d4\n" {
		t.Fatalf("text = %q", text.String())
	}
	if result.Data != cp.response {
		t.Fatal("delete JSON response was not preserved")
	}
}

func TestModuleReturnsAfterDeleteRequestByDefault(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime := buildRuntime(t, cp)
	result, err := runtime.Handler.Run(context.Background(), deleteRequest(nil))
	if err != nil || cp.getCalls != 0 {
		t.Fatalf("error=%v getCalls=%d", err, cp.getCalls)
	}
	var text bytes.Buffer
	result.Text(&text)
	if text.String() != "Deployment deletion requested: dpl-a1b2c3d4\n" {
		t.Fatalf("text = %q", text.String())
	}
}

func TestModuleSurfacesDeleteFailedStatusReason(t *testing.T) {
	status, reason := "DELETE_FAILED", "ProviderError: sandbox cleanup failed"
	cp := &fakeControlPlane{gets: []getResult{{deployment: &ags.Deployment{Status: &status, StatusReason: &reason}}}}
	runtime := buildRuntime(t, cp)
	_, err := runtime.Handler.Run(context.Background(), deleteRequest(map[string]command.FlagValue{
		"wait": {Name: "wait", Type: command.FlagBool, Bool: true, Changed: true},
	}))
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("error = %v, want status reason", err)
	}
}

func TestModuleUsesSharedPollingErrorSemantics(t *testing.T) {
	pollError := errors.New("describe failed")
	cp := &fakeControlPlane{gets: []getResult{{err: pollError}, {err: errDeploymentNotFound}}}
	runtime := buildRuntime(t, cp)
	_, err := runtime.Handler.Run(context.Background(), deleteRequest(map[string]command.FlagValue{
		"wait": {Name: "wait", Type: command.FlagBool, Bool: true, Changed: true},
	}))
	if err == nil || !errors.Is(err, pollError) {
		t.Fatalf("error = %v, want polling error", err)
	}
	if cp.getCalls != 1 {
		t.Fatalf("getCalls = %d, want 1", cp.getCalls)
	}
}

func buildRuntime(t *testing.T, cp *fakeControlPlane) command.Runtime {
	t.Helper()
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, Values: map[string]any{
		resourcewait.OptionsKey: resourcewait.Options{Interval: time.Millisecond, Timeout: 100 * time.Millisecond},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func deleteRequest(flags map[string]command.FlagValue) command.Request {
	if flags == nil {
		flags = map[string]command.FlagValue{}
	}
	return command.Request{ArgValues: map[string]string{"deployment-id": "dpl-a1b2c3d4"}, Flags: flags}
}
