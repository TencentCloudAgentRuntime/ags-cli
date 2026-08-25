package delete

import (
	"bytes"
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
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

func TestModuleDefaultsToWaitingWithTenMinuteTimeout(t *testing.T) {
	module := Module()
	waitIndex := slices.IndexFunc(module.Descriptor.Spec.Flags, func(flag command.FlagSpec) bool { return flag.Name == "wait" })
	timeoutIndex := slices.IndexFunc(module.Descriptor.Spec.Flags, func(flag command.FlagSpec) bool { return flag.Name == "timeout" })
	if waitIndex < 0 || module.Descriptor.Spec.Flags[waitIndex].Default != true {
		t.Fatalf("wait flag = %#v, want default true", module.Descriptor.Spec.Flags)
	}
	if timeoutIndex < 0 || module.Descriptor.Spec.Flags[timeoutIndex].Default != "10m" {
		t.Fatalf("timeout flag = %#v, want default 10m", module.Descriptor.Spec.Flags)
	}
	if module.Descriptor.Generated == nil || slices.ContainsFunc(module.Descriptor.Generated.Spec.Flags, func(flag command.FlagSpec) bool {
		return flag.Name == "wait" || flag.Name == "timeout"
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
	result, err := runtime.Handler.Run(context.Background(), deleteRequest(nil))
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

func TestModuleWaitFalseReturnsAfterDeleteRequest(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime := buildRuntime(t, cp)
	result, err := runtime.Handler.Run(context.Background(), deleteRequest(map[string]command.FlagValue{
		"wait": {Name: "wait", Type: command.FlagBool, Bool: false, Changed: true},
	}))
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
	_, err := runtime.Handler.Run(context.Background(), deleteRequest(nil))
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("error = %v, want status reason", err)
	}
}

func TestModuleRetriesOnlyRetryablePollingErrors(t *testing.T) {
	retryable := output.NewCLIError(&output.Failure{Code: "RequestLimitExceeded", Kind: output.KindRateLimit, Message: "slow down", Retryable: true})
	cp := &fakeControlPlane{gets: []getResult{{err: retryable}, {err: errDeploymentNotFound}}}
	runtime := buildRuntime(t, cp)
	if _, err := runtime.Handler.Run(context.Background(), deleteRequest(nil)); err != nil {
		t.Fatalf("retryable polling error should recover: %v", err)
	}
	if cp.getCalls != 2 {
		t.Fatalf("getCalls = %d, want 2", cp.getCalls)
	}

	cp = &fakeControlPlane{gets: []getResult{{err: &net.DNSError{Err: "temporary", Name: "ags.tencentcloudapi.com", IsTemporary: true}}, {err: errDeploymentNotFound}}}
	runtime = buildRuntime(t, cp)
	if _, err := runtime.Handler.Run(context.Background(), deleteRequest(nil)); err != nil {
		t.Fatalf("classified retryable network error should recover: %v", err)
	}
	if cp.getCalls != 2 {
		t.Fatalf("network getCalls = %d, want 2", cp.getCalls)
	}

	nonRetryable := output.NewCLIError(&output.Failure{Code: "AuthFailure", Kind: output.KindAuthOrPermission, Message: "denied"})
	cp = &fakeControlPlane{gets: []getResult{{err: nonRetryable}, {err: errDeploymentNotFound}}}
	runtime = buildRuntime(t, cp)
	if _, err := runtime.Handler.Run(context.Background(), deleteRequest(nil)); err == nil || err.Error() != "denied" {
		t.Fatalf("error = %v, want denied", err)
	}
	if cp.getCalls != 1 {
		t.Fatalf("getCalls = %d, want 1", cp.getCalls)
	}
}

func TestParseTimeoutAcceptsZeroAsInfinite(t *testing.T) {
	got, err := parseTimeout("0")
	if err != nil || got != 0 {
		t.Fatalf("parseTimeout(0) = (%s, %v), want zero", got, err)
	}
	if _, err := parseTimeout("-1s"); err == nil {
		t.Fatal("negative timeout should fail")
	}
}

func TestInvalidTimeoutDoesNotSubmitDelete(t *testing.T) {
	cp := &fakeControlPlane{}
	runtime := buildRuntime(t, cp)
	_, err := runtime.Handler.Run(context.Background(), deleteRequest(map[string]command.FlagValue{
		"timeout": {Name: "timeout", Type: command.FlagString, String: "later", Changed: true},
	}))
	if err == nil || cp.callAction != "" {
		t.Fatalf("error=%v action=%q, want validation before mutation", err, cp.callAction)
	}
}

func buildRuntime(t *testing.T, cp *fakeControlPlane) command.Runtime {
	t.Helper()
	runtime, err := Module().Build(command.Deps{ControlPlane: cp, Values: map[string]any{
		waitIntervalKey: time.Millisecond,
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
