package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/token"
)

func TestSDKNumericCompatibility(t *testing.T) {
	for _, kind := range []string{"int", "int64", "integer", "uint", "uint64", "float", "double"} {
		for _, typ := range []reflect.Type{reflect.TypeFor[int8](), reflect.TypeFor[int32](), reflect.TypeFor[int64](), reflect.TypeFor[uint8](), reflect.TypeFor[uint32](), reflect.TypeFor[uint64](), reflect.TypeFor[float32](), reflect.TypeFor[float64]()} {
			t.Run(kind+"/"+typ.String(), func(t *testing.T) {
				want := typ == reflect.TypeFor[int64]()
				if kind == "uint" || kind == "uint64" {
					want = typ == reflect.TypeFor[uint64]()
				}
				if kind == "float" || kind == "double" {
					want = typ == reflect.TypeFor[float64]()
				}
				if got := sdkValueSupports(nil, kind, kind, reflect.PointerTo(typ), nil); got != want {
					t.Fatalf("compatible=%v, want %v", got, want)
				}
			})
		}
	}
}

func TestUnsignedContractRoutesRequestAndResponseDynamically(t *testing.T) {
	config.SetSecretID("fake")
	config.SetSecretKey("fake")
	const large = "9223372036854775808"
	for _, tc := range []struct{ object, field string }{
		{"DescribeSandboxInstanceListRequest", "Limit"},
		{"DescribeSandboxInstanceListResponse", "TotalCount"},
	} {
		t.Run(tc.object, func(t *testing.T) {
			spec := testContract(t)
			for i := range spec.Objects[tc.object].Members {
				field := &spec.Objects[tc.object].Members[i]
				if field.Name == tc.field {
					field.Type, field.Member = "uint64", "uint64"
				}
			}
			if !needsDynamic(spec, "DescribeSandboxInstanceList") {
				t.Fatal("unsigned contract incorrectly selects signed SDK transport")
			}
			calls := 0
			sdk := &SDK{Contract: spec, RawSender: func(_ context.Context, _, _ string, body []byte) ([]byte, error) {
				calls++
				if tc.field == "Limit" && !strings.Contains(string(body), large) {
					t.Errorf("request lost unsigned value: %s", body)
				}
				return []byte(`{"Response":{"TotalCount":` + large + `,"InstanceSet":[]}}`), nil
			}}
			request := map[string]any{}
			if tc.field == "Limit" {
				request["Limit"] = json.Number(large)
			}
			value, err := sdk.Call(t.Context(), "DescribeSandboxInstanceList", request)
			if err != nil {
				t.Fatal(err)
			}
			response, err := apivalue.Decode(value)
			if err != nil || calls != 1 || response["TotalCount"] != json.Number(large) {
				t.Fatalf("response=%v calls=%d err=%v", response, calls, err)
			}
		})
	}
}

func testContract(t *testing.T) *apimeta.Spec {
	t.Helper()
	contract, err := apimeta.LoadContract("../../api/ags/v20250920", apimeta.Stable)
	if err != nil {
		t.Fatal(err)
	}
	return contract.Spec
}

func TestSDKCompatibilityIncludesNestedRequestsAndResponses(t *testing.T) {
	type leaf struct {
		Value *string `json:"Value"`
	}
	type shape struct {
		Nested *leaf   `json:"Nested"`
		Items  []*leaf `json:"Items"`
	}
	const action = "CompatibilityFixture"
	sdkShapes[action] = sdkShape{reflect.TypeFor[shape](), reflect.TypeFor[shape]()}
	t.Cleanup(func() { delete(sdkShapes, action) })
	// Use an SDK-compatible fixture: the real base contract already contains
	// signed integers backed by unsigned SDK fields, which require dynamic calls.
	for _, object := range []string{"Request", "Response", "RequestLeaf", "ResponseLeaf"} {
		t.Run(object, func(t *testing.T) {
			spec := &apimeta.Spec{
				Actions: map[string]apimeta.Action{action: {Input: "Request", Output: "Response"}},
				Objects: map[string]*apimeta.Object{},
			}
			for _, name := range []string{"Request", "Response"} {
				spec.Objects[name] = &apimeta.Object{Members: []apimeta.Member{
					{Name: "Nested", Type: "object", Member: name + "Leaf"},
					{Name: "Items", Type: "list", Member: name + "Leaf"},
				}}
				spec.Objects[name+"Leaf"] = &apimeta.Object{Members: []apimeta.Member{{Name: "Value", Type: "string", Member: "string"}}}
			}
			if needsDynamic(spec, action) {
				t.Fatal("fixture unexpectedly incompatible")
			}
			spec.Objects[object].Members = append(spec.Objects[object].Members, apimeta.Member{Name: "Future", Type: "string", Member: "string"})
			if !needsDynamic(spec, action) {
				t.Fatal("SDK silently accepts an unrepresentable contract")
			}
			spec.Objects[object].Members = spec.Objects[object].Members[:len(spec.Objects[object].Members)-1]
			field := &spec.Objects[object].Members[0]
			field.Type, field.Member = "int", "int"
			if !needsDynamic(spec, action) {
				t.Fatal("changed field type routed to typed SDK")
			}
		})
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
