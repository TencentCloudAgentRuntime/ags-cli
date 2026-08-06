package controlplane

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

func TestRawAPIClientUsesInjectedSender(t *testing.T) {
	config.SetSecretID("sid")
	config.SetSecretKey("skey")
	var gotAction, gotEndpoint string
	var gotPayload []byte
	client := RawAPIClient{Sender: func(_ context.Context, action, cloudEndpoint string, payload []byte) ([]byte, error) {
		gotAction = action
		gotEndpoint = cloudEndpoint
		gotPayload = append([]byte(nil), payload...)
		return []byte(`{"Response":{"ok":true}}`), nil
	}}

	result, err := client.RawCall(context.Background(), "DescribeSandboxInstanceList", []byte(`{"Limit":1}`))
	if err != nil {
		t.Fatalf("RawCall returned error: %v", err)
	}
	if gotAction != "DescribeSandboxInstanceList" || gotEndpoint != "ags.tencentcloudapi.com" || string(gotPayload) != `{"Limit":1}` {
		t.Fatalf("sender got action=%q endpoint=%q payload=%q", gotAction, gotEndpoint, gotPayload)
	}
	if len(result.Warnings) != 1 || result.MetaExtra["Action"] != "DescribeSandboxInstanceList" {
		t.Fatalf("result = %#v", result)
	}
	response := result.Response.(map[string]any)
	if response["Response"] == nil {
		t.Fatalf("response = %#v", result.Response)
	}
}

func TestRawAPIClientKeepsNonJSONResponseAsString(t *testing.T) {
	config.SetSecretID("sid")
	config.SetSecretKey("skey")
	client := RawAPIClient{Sender: func(context.Context, string, string, []byte) ([]byte, error) {
		return []byte("plain text"), nil
	}}
	result, err := client.RawCall(context.Background(), "Action", []byte(`{}`))
	if err != nil {
		t.Fatalf("RawCall returned error: %v", err)
	}
	if result.Response != "plain text" {
		t.Fatalf("response = %#v", result.Response)
	}
}

func TestRawAPIClientReturnsSenderError(t *testing.T) {
	config.SetSecretID("sid")
	config.SetSecretKey("skey")
	client := RawAPIClient{Sender: func(context.Context, string, string, []byte) ([]byte, error) {
		return nil, errors.New("boom")
	}}
	_, err := client.RawCall(context.Background(), "Action", []byte(`{}`))
	if err == nil || err.Error() != "api call Action: boom" {
		t.Fatalf("error = %v, want wrapped sender error", err)
	}
}

func TestFillRequestUsesCanonicalPrecacheCommandIDInHint(t *testing.T) {
	req := ags.NewCreatePreCacheImageTaskRequest()
	err := fillRequest("pre-cache-image-task.create", map[string]any{
		"Image":             123,
		"ImageRegistryType": "personal",
	}, req)
	if err == nil {
		t.Fatalf("expected parse error")
	}
	cliErr := output.ClassifyError(err)
	if cliErr == nil || cliErr.Failure == nil {
		t.Fatalf("expected CLI error, got %v", err)
	}
	if !strings.Contains(cliErr.Failure.Hint, "agr schema pre-cache-image-task.create -o json") {
		t.Fatalf("hint = %q", cliErr.Failure.Hint)
	}
}

func TestFillRequestAcceptsLatestPauseAndResumeFields(t *testing.T) {
	tests := []struct {
		name      string
		commandID string
		request   map[string]any
		target    jsonRequest
	}{
		{
			name:      "pause memory",
			commandID: "instance.pause",
			request:   map[string]any{"InstanceId": "ins-unit", "Memory": true},
			target:    ags.NewPauseSandboxInstanceRequest(),
		},
		{
			name:      "resume timeout",
			commandID: "instance.resume",
			request:   map[string]any{"InstanceId": "ins-unit", "Timeout": "10m"},
			target:    ags.NewResumeSandboxInstanceRequest(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := fillRequest(tt.commandID, tt.request, tt.target); err != nil {
				t.Fatalf("fillRequest returned error: %v", err)
			}
		})
	}
}

// TestSDKCallFallbackClassifiesSDKError guards the default-case fallback:
// actions not covered by typed wrappers (identity/credential modules in
// workflow-adapter mode) must classify TencentCloud SDK errors into typed
// CLIErrors, mirroring the typed call wrappers. Otherwise an AuthFailure /
// ResourceNotFound surfaces as a generic INTERNAL_ERROR with no code or hint.
func TestSDKCallFallbackClassifiesSDKError(t *testing.T) {
	config.SetSecretID("sid")
	config.SetSecretKey("skey")

	sdkErr := sdkerrors.NewTencentCloudSDKError("AuthFailure", "invalid secret key", "req-123")
	sdk := &SDK{
		NewClient: func() (*ags.Client, error) { return &ags.Client{}, nil },
		RawSender: func(context.Context, string, string, []byte) ([]byte, error) {
			return nil, sdkErr
		},
	}

	_, err := sdk.Call(context.Background(), "CreateWorkloadIdentity", map[string]any{"Name": "x"})
	if err == nil {
		t.Fatal("expected error from fallback")
	}
	cliErr, ok := err.(*output.CLIError)
	if !ok {
		t.Fatalf("expected *output.CLIError, got %T: %v", err, err)
	}
	if cliErr.Failure == nil {
		t.Fatal("expected non-nil Failure")
	}
	if cliErr.Failure.Kind != output.KindAuthOrPermission {
		t.Fatalf("Kind = %q, want %q (AuthFailure should classify as auth)", cliErr.Failure.Kind, output.KindAuthOrPermission)
	}
	if cliErr.Failure.Code != "AuthFailure" {
		t.Fatalf("Code = %q, want AuthFailure", cliErr.Failure.Code)
	}
	if cliErr.Failure.Details["RequestId"] != "req-123" {
		t.Fatalf("RequestId detail = %v, want req-123", cliErr.Failure.Details["RequestId"])
	}
}

// TestSDKCallFallbackUnwrapsInnerResponse verifies the fallback returns the
// inner Response object (with RequestId stripped) on success, matching the
// shape typed callers expect.
func TestSDKCallFallbackUnwrapsInnerResponse(t *testing.T) {
	config.SetSecretID("sid")
	config.SetSecretKey("skey")

	sdk := &SDK{
		NewClient: func() (*ags.Client, error) { return &ags.Client{}, nil },
		RawSender: func(context.Context, string, string, []byte) ([]byte, error) {
			return []byte(`{"Response":{"WorkloadIdentityId":"wi-1","RequestId":"rid-omit"}}`), nil
		},
	}

	got, err := sdk.Call(context.Background(), "CreateWorkloadIdentity", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	inner, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", got)
	}
	if inner["WorkloadIdentityId"] != "wi-1" {
		t.Fatalf("WorkloadIdentityId = %v, want wi-1", inner["WorkloadIdentityId"])
	}
	if _, present := inner["RequestId"]; present {
		t.Fatalf("RequestId should be stripped from inner Response, got %v", inner["RequestId"])
	}
}
