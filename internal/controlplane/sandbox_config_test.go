package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// sandboxPreviewSDK keeps every call on the preview contract and records the raw
// signed body so a field silently dropped by the presentation SDK is visible.
func sandboxPreviewSDK(t *testing.T, respond func(action string, body map[string]any) string) (*SDK, *[]byte, *int) {
	t.Helper()
	contract, err := apimeta.LoadContract("../../api/ags/v20250920", apimeta.Preview)
	if err != nil {
		t.Fatal(err)
	}
	config.SetSecretID("fake")
	config.SetSecretKey("fake")
	var sent []byte
	calls := 0
	sdk := &SDK{Contract: contract.Spec, RawSender: func(_ context.Context, action, _ string, body []byte) ([]byte, error) {
		calls++
		sent = slices.Clone(body)
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("request body is not JSON: %s", body)
		}
		return []byte(respond(action, request)), nil
	}}
	return sdk, &sent, &calls
}

func TestSandboxPreviewTransportPreservesNewRequestMembers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		action  string
		request map[string]any
		want    map[string]any
		absent  []string
	}{
		{
			name:    "omitted",
			action:  "UpdateSandboxInstance",
			request: map[string]any{"InstanceId": "ins-test"},
			absent:  []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"},
		},
		{
			name:    "empty-object",
			action:  "UpdateSandboxInstance",
			request: map[string]any{"InstanceId": "ins-test", "KeepaliveConfiguration": map[string]any{}},
			want:    map[string]any{"KeepaliveConfiguration": map[string]any{}},
		},
		{
			name:    "single-member",
			action:  "UpdateSandboxInstance",
			request: map[string]any{"InstanceId": "ins-test", "KeepaliveConfiguration": map[string]any{"KeepaliveTimeout": "5m"}},
			want:    map[string]any{"KeepaliveConfiguration": map[string]any{"KeepaliveTimeout": "5m"}},
			absent:  []string{"AutoPause", "AutoResume"},
		},
		{
			name:    "explicit-false",
			action:  "UpdateSandboxInstance",
			request: map[string]any{"InstanceId": "ins-test", "AutoPause": false, "AutoResume": false},
			want:    map[string]any{"AutoPause": false, "AutoResume": false},
		},
		{
			name:    "raw-duration",
			action:  "UpdateSandboxTool",
			request: map[string]any{"ToolId": "sdt-test", "KeepaliveConfiguration": map[string]any{"KeepaliveMode": "MIXED", "KeepaliveTimeout": "600s"}},
			want:    map[string]any{"KeepaliveConfiguration": map[string]any{"KeepaliveMode": "MIXED", "KeepaliveTimeout": "600s"}},
		},
		{
			name:   "log-format-case-preserved",
			action: "CreateSandboxTool",
			request: map[string]any{
				"ToolName": "t", "ToolType": "code-interpreter",
				"NetworkConfiguration": map[string]any{"NetworkMode": "SANDBOX"},
				"LogConfiguration":     map[string]any{"ContentFormat": "AuTo", "CLSConfig": map[string]any{"TopicId": "topic"}},
			},
			want: map[string]any{"LogConfiguration": map[string]any{"ContentFormat": "AuTo", "CLSConfig": map[string]any{"TopicId": "topic"}}},
		},
		{
			name:   "log-format-empty-string",
			action: "CreateSandboxTool",
			request: map[string]any{
				"ToolName": "t", "ToolType": "code-interpreter",
				"NetworkConfiguration": map[string]any{"NetworkMode": "SANDBOX"},
				"LogConfiguration":     map[string]any{"ContentFormat": ""},
			},
			want: map[string]any{"LogConfiguration": map[string]any{"ContentFormat": ""}},
		},
		{
			name:   "start-instance",
			action: "StartSandboxInstance",
			request: map[string]any{
				"ToolId":                 "sdt-test",
				"KeepaliveConfiguration": map[string]any{"KeepaliveMode": "REQUEST", "KeepaliveTimeout": "30m"},
				"AutoPause":              true,
			},
			want: map[string]any{
				"KeepaliveConfiguration": map[string]any{"KeepaliveMode": "REQUEST", "KeepaliveTimeout": "30m"},
				"AutoPause":              true,
			},
			absent: []string{"AutoResume"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sdk, sent, calls := sandboxPreviewSDK(t, func(action string, _ map[string]any) string {
				if action == "StartSandboxInstance" {
					return `{"Response":{"Instance":{"InstanceId":"ins-test","AuthMode":"NONE"},"RequestId":"req-1"}}`
				}
				return `{"Response":{"RequestId":"req-1"}}`
			})
			if _, err := sdk.Call(t.Context(), tc.action, tc.request); err != nil {
				t.Fatal(err)
			}
			if *calls != 1 {
				t.Fatalf("raw sender calls=%d, want 1", *calls)
			}
			var body map[string]any
			if err := json.Unmarshal(*sent, &body); err != nil {
				t.Fatal(err)
			}
			for name, want := range tc.want {
				if got, ok := body[name]; !ok || !reflectJSONEqual(got, want) {
					t.Fatalf("body[%s]=%v, want %v (body=%s)", name, got, want, *sent)
				}
			}
			for _, name := range tc.absent {
				if _, ok := body[name]; ok {
					t.Fatalf("body sent %s although the input omitted it: %s", name, *sent)
				}
			}
		})
	}
}

func TestSandboxPreviewTransportPreservesNewResponseMembers(t *testing.T) {
	sdk, _, _ := sandboxPreviewSDK(t, func(string, map[string]any) string {
		return `{"Response":{"InstanceSet":[{"InstanceId":"ins-1","KeepaliveConfiguration":{"KeepaliveMode":"MIXED","KeepaliveTimeout":"600s","ServerExtension":{"Nested":true}},"AutoPause":false,"AutoResume":true}],"TotalCount":1,"RequestId":"req-2"}}`
	})
	value, err := sdk.Call(t.Context(), "DescribeSandboxInstanceList", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := apivalue.Decode(value)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := response.ReadObjects("InstanceSet")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if value, ok := rows[0]["AutoPause"]; !ok || value != false {
		t.Fatalf("AutoPause=false was lost: %v", rows[0]["AutoPause"])
	}
	keepalive := rows[0].Object("KeepaliveConfiguration")
	if keepalive.String("KeepaliveTimeout") != "600s" || keepalive.Object("ServerExtension") == nil {
		t.Fatalf("keepalive configuration was not preserved: %v", keepalive)
	}
	if response.String("RequestId") != "req-2" {
		t.Fatalf("RequestId was lost: %v", response)
	}
}

func TestSandboxPreviewTransportOmitsMissingConfiguration(t *testing.T) {
	sdk, _, _ := sandboxPreviewSDK(t, func(string, map[string]any) string {
		return `{"Response":{"InstanceSet":[{"InstanceId":"ins-1","AutoPause":false}],"TotalCount":1}}`
	})
	value, err := sdk.Call(t.Context(), "DescribeSandboxInstanceList", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := apivalue.Decode(value)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := response.ReadObjects("InstanceSet")
	if _, exists := rows[0]["KeepaliveConfiguration"]; exists {
		t.Fatalf("a missing configuration must stay missing: %v", rows[0])
	}
	if _, exists := rows[0]["AutoResume"]; exists {
		t.Fatalf("a missing bool must stay missing: %v", rows[0])
	}
}

func TestSandboxPreviewTransportAppliesToolUpdateReadback(t *testing.T) {
	sdk, sent, _ := sandboxPreviewSDK(t, func(string, map[string]any) string {
		return `{"Response":{"RequestId":"req-3"}}`
	})
	if _, err := sdk.Call(t.Context(), "UpdateSandboxTool", map[string]any{
		"ToolId":                 "sdt-1",
		"LogConfiguration":       nil,
		"KeepaliveConfiguration": map[string]any{"KeepaliveMode": "NONE"},
	}); err == nil {
		t.Fatal("UpdateSandboxTool accepted LogConfiguration although the contract has no such request member")
	}
	if len(*sent) != 0 {
		t.Fatalf("invalid request reached the sender: %s", *sent)
	}
}

func TestSandboxPreviewTransportRejectsUnknownMembersBeforeSending(t *testing.T) {
	cases := []struct {
		name    string
		action  string
		request map[string]any
	}{
		{"unknown-top-level-keepalive-mode", "UpdateSandboxInstance", map[string]any{"InstanceId": "ins-1", "KeepaliveMode": "REQUEST"}},
		{"unknown-max-duration", "UpdateSandboxInstance", map[string]any{"InstanceId": "ins-1", "MaxKeepaliveDuration": "600s"}},
		{"wrong-configuration-type", "UpdateSandboxInstance", map[string]any{"InstanceId": "ins-1", "KeepaliveConfiguration": "REQUEST"}},
		{"wrong-bool-type", "UpdateSandboxInstance", map[string]any{"InstanceId": "ins-1", "AutoPause": "false"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, sent, calls := sandboxPreviewSDK(t, func(string, map[string]any) string {
				return `{"Response":{}}`
			})
			_, err := sdk.Call(t.Context(), tc.action, tc.request)
			var classified *output.CLIError
			if !errors.As(err, &classified) || classified.Failure.Kind != output.KindUsage {
				t.Fatalf("err=%v, want a usage error", err)
			}
			if *calls != 0 || len(*sent) != 0 {
				t.Fatalf("invalid request reached the sender: calls=%d body=%s", *calls, *sent)
			}
		})
	}
}

func TestSandboxPreviewTransportDoesNotDisguiseAPIErrors(t *testing.T) {
	sdk, _, calls := sandboxPreviewSDK(t, func(action string, _ map[string]any) string {
		if action == "StartSandboxInstance" {
			return `{"Response":{"Error":{"Code":"InvalidParameter.KeepaliveTimeout","Message":"bad timeout"},"RequestId":"req-4"}}`
		}
		return `{"Response":{"Error":{"Code":"InvalidParameter.ContentFormat","Message":"bad format"},"RequestId":"req-4"}}`
	})
	_, err := sdk.Call(t.Context(), "StartSandboxInstance", map[string]any{"ToolId": "sdt-1"})
	if err == nil {
		t.Fatal("an API error was reported as success")
	}
	if *calls != 1 {
		t.Fatalf("calls=%d, want a single attempt without retry", *calls)
	}
}

func TestSandboxPreviewTransportStartRequiresInstanceID(t *testing.T) {
	sdk, _, _ := sandboxPreviewSDK(t, func(string, map[string]any) string {
		return `{"Response":{"RequestId":"req-5"}}`
	})
	if _, err := sdk.Call(t.Context(), "StartSandboxInstance", map[string]any{"ToolId": "sdt-1"}); err == nil || !strings.Contains(err.Error(), "instance") {
		t.Fatalf("err=%v, want a missing instance error", err)
	}
}

func reflectJSONEqual(got, want any) bool {
	return strings.TrimSpace(renderJSON(got)) == strings.TrimSpace(renderJSON(want))
}

func renderJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
