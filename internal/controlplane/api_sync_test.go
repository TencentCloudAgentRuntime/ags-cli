package controlplane

import (
	"encoding/json"
	"testing"

	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func TestSyncedSDKRequestDecoding(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request interface{ FromJsonString(string) error }
		body    string
	}{
		{"pre-cache type", ags.NewDescribePreCacheImageTaskRequest(), `{"PreCacheType":"image_volume","Image":"example:latest","ImageDigest":"sha256:test","ImageRegistryType":"personal"}`},
		{"start mode", ags.NewStartSandboxInstanceRequest(), `{"ToolId":"sdt-test","StartMode":"WAIT_UNTIL_HEALTHY","CustomConfiguration":{"Resources":{"GPUResourceGroupId":"gpug-test"}}}`},
		{"create GPU tool", ags.NewCreateSandboxToolRequest(), `{"ToolName":"gpu-test","ToolType":"custom","NetworkConfiguration":{"NetworkMode":"SANDBOX"},"CustomConfiguration":{"Resources":{"GPUResourceGroupId":"gpug-test"}}}`},
		{"update GPU tool", ags.NewUpdateSandboxToolRequest(), `{"ToolId":"sdt-test","CustomConfiguration":{"Resources":{"GPUResourceGroupId":"gpug-test"}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.request.FromJsonString(tc.body); err != nil {
				t.Fatalf("SDK rejected the public request: %v", err)
			}
			encoded, err := json.Marshal(tc.request)
			if err != nil {
				t.Fatal(err)
			}
			var want, got map[string]any
			if err := json.Unmarshal([]byte(tc.body), &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			assertRequestSubset(t, got, want)
		})
	}
}

func assertRequestSubset(t *testing.T, got, want map[string]any) {
	t.Helper()
	for key, value := range want {
		if nested, ok := value.(map[string]any); ok {
			actual, ok := got[key].(map[string]any)
			if !ok {
				t.Fatalf("SDK lost object %s: %v", key, got)
			}
			assertRequestSubset(t, actual, nested)
		} else if got[key] != value {
			t.Errorf("SDK changed %s: got %v, want %v", key, got[key], value)
		}
	}
}
