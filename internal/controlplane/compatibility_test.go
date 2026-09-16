package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/token"
)

func testContract(t *testing.T) *apimeta.Spec {
	t.Helper()
	contract, err := apimeta.LoadContract("../../api/ags/v20250920", apimeta.Stable)
	if err != nil {
		t.Fatal(err)
	}
	return contract.Spec
}

func TestSDKCompatibilityIncludesNestedRequestsAndResponses(t *testing.T) {
	for _, tc := range []struct{ object, action string }{
		{"StartSandboxInstanceRequest", "StartSandboxInstance"},
		{"SandboxInstance", "StartSandboxInstance"},
		{"ComputerConfiguration", "DescribeSandboxInstanceList"},
		{"CustomConfiguration", "CreateSandboxTool"},
	} {
		t.Run(tc.object, func(t *testing.T) {
			spec := testContract(t)
			if needsDynamic(spec, tc.action) {
				t.Fatalf("baseline unexpectedly incompatible: %s", tc.action)
			}
			spec.Objects[tc.object].Members = append(spec.Objects[tc.object].Members, apimeta.Member{Name: "Future", Type: "string", Member: "string"})
			if !needsDynamic(spec, tc.action) {
				t.Fatal("SDK silently accepts an unrepresentable contract")
			}
		})
	}
	spec := testContract(t)
	for i := range spec.Objects["StartSandboxInstanceRequest"].Members {
		m := &spec.Objects["StartSandboxInstanceRequest"].Members[i]
		if m.Name == "ToolId" {
			m.Type = "object"
			m.Member = "ComputerConfiguration"
		}
	}
	if !needsDynamic(spec, "StartSandboxInstance") {
		t.Fatal("changed field type routed to typed SDK")
	}
}

func TestDynamicValidationAndTokenCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	config.SetSecretID("fake")
	config.SetSecretKey("fake")
	spec := testContract(t)
	spec.Objects["StartSandboxInstanceRequest"].Members = append(spec.Objects["StartSandboxInstanceRequest"].Members, apimeta.Member{Name: "Future", Type: "object", Member: "ComputerConfiguration"})
	// Force token acquisition through the same dynamic route.
	spec.Objects["AcquireSandboxInstanceTokenResponse"].Members = append(spec.Objects["AcquireSandboxInstanceTokenResponse"].Members, apimeta.Member{Name: "Future", Type: "string", Member: "string"})
	cache, err := token.NewCache()
	if err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	sdk := &SDK{Contract: spec, TokenCache: cache, TokenCacheReady: true, RawSender: func(ctx context.Context, action, endpoint string, payload []byte) ([]byte, error) {
		calls = append(calls, action)
		if action == "StartSandboxInstance" {
			if !strings.Contains(string(payload), `"ClientToken":"unchanged"`) {
				t.Errorf("lost idempotency token: %s", payload)
			}
			return []byte(`{"Response":{"Instance":{"InstanceId":"ssi-test","Status":"RUNNING","AuthMode":"TOKEN","Future":{"N":9007199254740993}},"RequestId":"rid"}}`), nil
		}
		return []byte(`{"Response":{"Token":"cached-token","Future":"kept"}}`), nil
	}}
	for _, payload := range []map[string]any{
		{"Unknown": true}, {"Future": map[string]any{"Unknown": true}}, {"Future": "wrong-type"},
		{"MountOptions": []any{map[string]any{"Unknown": "bad"}}},
	} {
		if _, err := sdk.Call(t.Context(), "StartSandboxInstance", payload); err == nil {
			t.Fatalf("accepted %#v", payload)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("invalid requests sent: %v", calls)
	}
	result, err := sdk.Call(t.Context(), "StartSandboxInstance", map[string]any{"Future": map[string]any{}, "ClientToken": "unchanged"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := apivalue.Decode(result)
	if err != nil {
		t.Fatal(err)
	}
	if response.Object("Instance").Object("Future")["N"] != json.Number("9007199254740993") {
		t.Fatalf("lost response: %#v", response)
	}
	if strings.Join(calls, ",") != "StartSandboxInstance,AcquireSandboxInstanceToken" {
		t.Fatalf("unexpected calls/retry: %v", calls)
	}
	if got, ok := cache.Get("ssi-test"); !ok || got != "cached-token" {
		t.Fatalf("cache: %q %v", got, ok)
	}
	// Malformed successful envelopes must never reach a resource success result.
	for _, body := range []string{`{}`, `{"Response":{}}`, `{"Response":{"Instance":{}}}`} {
		sdk.RawSender = func(context.Context, string, string, []byte) ([]byte, error) { return []byte(body), nil }
		if _, err := sdk.Call(t.Context(), "StartSandboxInstance", map[string]any{}); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	sdk.RawSender = func(ctx context.Context, _ string, _ string, _ []byte) ([]byte, error) { return nil, ctx.Err() }
	_, err = sdk.Call(ctx, "StartSandboxInstance", map[string]any{})
	// Classification may wrap cancellation; it must still be an error.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
