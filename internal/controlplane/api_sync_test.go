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
		{"describe pre-cache type", ags.NewDescribePreCacheImageTaskRequest(), `{"PreCacheType":"image_volume","Image":"example:latest","ImageDigest":"sha256:test","ImageRegistryType":"personal"}`},
		{"create pre-cache type", ags.NewCreatePreCacheImageTaskRequest(), `{"PreCacheType":"image_volume","Image":"example:latest","ImageRegistryType":"personal"}`},
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

// StorageMounts is a nested array with an optional string. The typed SDK must
// keep explicit false, an omitted SubPath, and an explicit empty SubPath
// distinct so tool updates can select "keep", "image root", and a directory.
func TestSyncedSDKStorageMounts(t *testing.T) {
	subPath := func(value string) *string { return &value }
	for _, tc := range []struct {
		name      string
		request   interface{ FromJsonString(string) error }
		body      string
		mountPath string
		subPath   *string
	}{
		{
			name:      "start instance directory",
			request:   ags.NewStartSandboxInstanceRequest(),
			body:      `{"ToolId":"sdt-test","StorageMounts":[{"Name":"runtime","MountPath":"/mnt/runtime","ReadOnly":false,"StorageSource":{"Image":{"Reference":"repo/image:v1","ImageRegistryType":"personal","SubPath":"usr/bin"}}}]}`,
			mountPath: "/mnt/runtime",
			subPath:   subPath("usr/bin"),
		},
		{
			name:      "start instance image root",
			request:   ags.NewStartSandboxInstanceRequest(),
			body:      `{"ToolId":"sdt-test","StorageMounts":[{"Name":"runtime","MountPath":"/mnt/runtime","ReadOnly":false,"StorageSource":{"Image":{"Reference":"repo/image:v1","ImageRegistryType":"personal","SubPath":""}}}]}`,
			mountPath: "/mnt/runtime",
			subPath:   subPath(""),
		},
		{
			name:    "update tool omitted subpath",
			request: ags.NewUpdateSandboxToolRequest(),
			body:    `{"ToolId":"sdt-test","StorageMounts":[{"Name":"runtime","ReadOnly":false,"StorageSource":{"Image":{"Reference":"repo/image:v1","ImageRegistryType":"personal"}}}]}`,
		},
		{
			name:    "update tool explicit empty subpath",
			request: ags.NewUpdateSandboxToolRequest(),
			body:    `{"ToolId":"sdt-test","StorageMounts":[{"Name":"runtime","ReadOnly":false,"StorageSource":{"Image":{"Reference":"repo/image:v1","ImageRegistryType":"personal","SubPath":""}}}]}`,
			subPath: subPath(""),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.request.FromJsonString(tc.body); err != nil {
				t.Fatalf("SDK rejected the public request: %v", err)
			}
			encoded, err := json.Marshal(tc.request)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			mounts, ok := got["StorageMounts"].([]any)
			if !ok || len(mounts) != 1 {
				t.Fatalf("SDK lost StorageMounts: %s", encoded)
			}
			mount, _ := mounts[0].(map[string]any)
			if mount["ReadOnly"] != false {
				t.Fatalf("SDK lost explicit ReadOnly false: %v", mount)
			}
			mountPath, hasMountPath := mount["MountPath"]
			if tc.mountPath == "" {
				if hasMountPath {
					t.Fatalf("SDK invented MountPath: %v", mount)
				}
			} else if mountPath != tc.mountPath {
				t.Fatalf("SDK changed MountPath: %v", mount)
			}
			source, _ := mount["StorageSource"].(map[string]any)
			image, _ := source["Image"].(map[string]any)
			if image["Reference"] != "repo/image:v1" || image["ImageRegistryType"] != "personal" {
				t.Fatalf("SDK lost the image source: %v", image)
			}
			value, present := image["SubPath"]
			if tc.subPath == nil {
				if present {
					t.Fatalf("SDK invented SubPath: %v", image)
				}
				return
			}
			if !present || value != *tc.subPath {
				t.Fatalf("SubPath = %v (present %v), want %q", value, present, *tc.subPath)
			}
		})
	}
}
