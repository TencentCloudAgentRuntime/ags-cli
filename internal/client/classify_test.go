package client

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

func TestClassifyErrorNil(t *testing.T) {
	result := ClassifyError(nil)
	if result != nil {
		t.Fatalf("expected nil, got %+v", result)
	}
}

func TestClassifyErrorAlreadyCLIError(t *testing.T) {
	original := output.NewNotFoundError("TEST_NOT_FOUND", "not found", "hint")
	result := ClassifyError(original)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Failure.Code != "TEST_NOT_FOUND" {
		t.Fatalf("Code = %q, want TEST_NOT_FOUND", result.Failure.Code)
	}
	if result.Failure.Fix == "" {
		t.Fatal("expected Fix to be populated for not_found kind")
	}
}

func TestClassifyErrorSDKNotFound(t *testing.T) {
	sdkErr := sdkerrors.NewTencentCloudSDKError("ResourceNotFound.SandboxInstance", "instance not found", "req-123")
	result := ClassifyError(sdkErr)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Failure.Kind != output.KindNotFound {
		t.Fatalf("Kind = %q, want not_found", result.Failure.Kind)
	}
	if result.Failure.Code != "ResourceNotFound.SandboxInstance" {
		t.Fatalf("Code = %q", result.Failure.Code)
	}
	if result.Failure.Fix == "" {
		t.Fatal("expected Fix to be populated")
	}
}

func TestClassifyErrorSDKAuth(t *testing.T) {
	sdkErr := sdkerrors.NewTencentCloudSDKError("AuthFailure.SecretIdNotFound", "id not found", "req-456")
	result := ClassifyError(sdkErr)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Failure.Kind != output.KindAuthOrPermission {
		t.Fatalf("Kind = %q, want auth", result.Failure.Kind)
	}
	if result.Failure.Fix == "" {
		t.Fatal("expected Fix to be populated for auth kind")
	}
}

func TestClassifyErrorSDKRateLimit(t *testing.T) {
	sdkErr := sdkerrors.NewTencentCloudSDKError("RequestLimitExceeded", "too many requests", "req-789")
	result := ClassifyError(sdkErr)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Failure.Kind != output.KindRateLimit {
		t.Fatalf("Kind = %q, want rate_limit", result.Failure.Kind)
	}
	if !result.Failure.Retryable {
		t.Fatal("expected Retryable=true")
	}
	if result.Failure.Fix != "wait and retry" {
		t.Fatalf("Fix = %q, want 'wait and retry'", result.Failure.Fix)
	}
}

func TestClassifyErrorTimeout(t *testing.T) {
	result := ClassifyError(context.DeadlineExceeded)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Failure.Kind != output.KindTimeout {
		t.Fatalf("Kind = %q, want timeout", result.Failure.Kind)
	}
	if result.Failure.Fix == "" {
		t.Fatal("expected Fix to be populated for timeout kind")
	}
}

func TestClassifyErrorNetwork(t *testing.T) {
	netErr := &net.DNSError{Err: "no such host", Name: "example.com"}
	result := ClassifyError(netErr)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Failure.Kind != output.KindNetwork {
		t.Fatalf("Kind = %q, want network", result.Failure.Kind)
	}
	if result.Failure.Fix != "agr doctor" {
		t.Fatalf("Fix = %q, want 'agr doctor'", result.Failure.Fix)
	}
}

func TestClassifyErrorGeneric(t *testing.T) {
	result := ClassifyError(errors.New("something unexpected"))
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Failure.Kind != output.KindGenericError {
		t.Fatalf("Kind = %q, want error", result.Failure.Kind)
	}
	// Generic errors don't get a Fix.
	if result.Failure.Fix != "" {
		t.Fatalf("Fix = %q, want empty for generic error", result.Failure.Fix)
	}
}

func TestClassifyErrorPreservesExistingFix(t *testing.T) {
	original := &output.CLIError{
		Failure: &output.Failure{
			Code: "CUSTOM",
			Kind: output.KindNotFound,
			Fix:  "custom fix already set",
		},
		ExitCode: 1,
	}
	result := ClassifyError(original)
	if result.Failure.Fix != "custom fix already set" {
		t.Fatalf("Fix = %q, expected existing fix to be preserved", result.Failure.Fix)
	}
}

func TestAttachFixNotFound(t *testing.T) {
	cliErr := &output.CLIError{Failure: &output.Failure{Kind: output.KindNotFound}}
	result := attachFix(cliErr)
	if result.Failure.Fix != "agr instance list / agr tool list" {
		t.Fatalf("Fix = %q", result.Failure.Fix)
	}
}

func TestAttachFixAuth(t *testing.T) {
	cliErr := &output.CLIError{Failure: &output.Failure{Kind: output.KindAuthOrPermission}}
	result := attachFix(cliErr)
	if result.Failure.Fix != "agr init / agr doctor" {
		t.Fatalf("Fix = %q", result.Failure.Fix)
	}
}

func TestAttachFixNetwork(t *testing.T) {
	cliErr := &output.CLIError{Failure: &output.Failure{Kind: output.KindNetwork}}
	result := attachFix(cliErr)
	if result.Failure.Fix != "agr doctor" {
		t.Fatalf("Fix = %q", result.Failure.Fix)
	}
}

func TestAttachFixTimeout(t *testing.T) {
	cliErr := &output.CLIError{Failure: &output.Failure{Kind: output.KindTimeout}}
	result := attachFix(cliErr)
	if result.Failure.Fix != "retry with longer --timeout or check agr doctor" {
		t.Fatalf("Fix = %q", result.Failure.Fix)
	}
}

func TestAttachFixRateLimit(t *testing.T) {
	cliErr := &output.CLIError{Failure: &output.Failure{Kind: output.KindRateLimit}}
	result := attachFix(cliErr)
	if result.Failure.Fix != "wait and retry" {
		t.Fatalf("Fix = %q", result.Failure.Fix)
	}
}

func TestAttachFixUsageNoFix(t *testing.T) {
	cliErr := &output.CLIError{Failure: &output.Failure{Kind: output.KindUsage}}
	result := attachFix(cliErr)
	if result.Failure.Fix != "" {
		t.Fatalf("Fix = %q, want empty for usage kind", result.Failure.Fix)
	}
}

func TestAttachFixNilSafe(t *testing.T) {
	if attachFix(nil) != nil {
		t.Fatal("expected nil")
	}
	if attachFix(&output.CLIError{}) != nil {
		// Failure is nil → should not panic.
		t.Log("non-nil CLIError with nil Failure handled")
	}
}

// sliceError contains a slice field, making it uncomparable with ==.
type sliceError struct{ values []string }

func (e sliceError) Error() string { return "slice error" }

func TestClassifyErrorDoesNotPanicOnUncomparableError(t *testing.T) {
	// Regression: using == to compare error interfaces panics if the
	// underlying type contains slices/maps. ClassifyError must handle this.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ClassifyError panicked: %v", r)
		}
	}()
	result := ClassifyError(sliceError{values: []string{"x"}})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Failure.Kind != output.KindGenericError {
		t.Fatalf("Kind = %q, want error", result.Failure.Kind)
	}
}

func TestAttachFixExported(t *testing.T) {
	// Verify the exported AttachFix works on bare Failure (for CmdResult path).
	f := &output.Failure{Kind: output.KindNotFound}
	AttachFix(f)
	if f.Fix != "agr instance list / agr tool list" {
		t.Fatalf("Fix = %q", f.Fix)
	}
}

func TestAttachFixExportedPreservesExisting(t *testing.T) {
	f := &output.Failure{Kind: output.KindNotFound, Fix: "custom"}
	AttachFix(f)
	if f.Fix != "custom" {
		t.Fatalf("Fix = %q, expected preserved 'custom'", f.Fix)
	}
}
