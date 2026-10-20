package controlplane

import (
	"encoding/json"
	"reflect"
	"testing"

	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func TestFillRequestSupportsStorageSecurityContract(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request interface {
			jsonRequest
			ToJsonString() string
		}
	}{
		{"tool.create", ags.NewCreateSandboxToolRequest()},
		{"tool.update", ags.NewUpdateSandboxToolRequest()},
		{"instance.create", ags.NewStartSandboxInstanceRequest()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var payload map[string]any
			if err := json.Unmarshal([]byte(`{"CustomConfiguration":{"SecurityContext":{"Privileged":false,"Capabilities":{"Add":["NET_BIND_SERVICE"],"Drop":["ALL"]}}}}`), &payload); err != nil {
				t.Fatal(err)
			}
			if tc.name == "tool.create" {
				var mounts []any
				if err := json.Unmarshal([]byte(`[{"Name":"existing","Volume":{"VolumeId":"vol-test"}},{"Name":"named","Volume":{"VolumeName":"workspace"}},{"Name":"derived","VolumeTemplate":{"VolumeTemplateName":"sessions","ReuseKey":"${metadata.session_id}"}}]`), &mounts); err != nil {
					t.Fatal(err)
				}
				payload["StorageMounts"] = mounts
			}
			if err := fillRequest(tc.name, payload, tc.request); err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(tc.request.ToJsonString()), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, payload) {
				t.Fatalf("SDK dropped nested fields or explicit false: got %#v, want %#v", got, payload)
			}
		})
	}
}

func TestFillRequestSupportsToolComputerConfiguration(t *testing.T) {
	payload := map[string]any{
		"ComputerConfiguration": map[string]any{
			"WAAConfiguration":     map[string]any{"ImageId": "img-unit"},
			"OSWorldConfiguration": map[string]any{"Version": "osworld2"},
		},
	}
	tests := []struct {
		name    string
		request jsonRequest
	}{
		{
			name:    "create",
			request: ags.NewCreateSandboxToolRequest(),
		},
		{
			name:    "update",
			request: ags.NewUpdateSandboxToolRequest(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := fillRequest("tool."+tc.name, payload, tc.request); err != nil {
				t.Fatalf("fillRequest: %v", err)
			}
			var imageID, osWorldVersion *string
			switch request := tc.request.(type) {
			case *ags.CreateSandboxToolRequest:
				if request.ComputerConfiguration != nil && request.ComputerConfiguration.WAAConfiguration != nil {
					imageID = request.ComputerConfiguration.WAAConfiguration.ImageId
				}
				if request.ComputerConfiguration != nil && request.ComputerConfiguration.OSWorldConfiguration != nil {
					osWorldVersion = request.ComputerConfiguration.OSWorldConfiguration.Version
				}
			case *ags.UpdateSandboxToolRequest:
				if request.ComputerConfiguration != nil && request.ComputerConfiguration.WAAConfiguration != nil {
					imageID = request.ComputerConfiguration.WAAConfiguration.ImageId
				}
				if request.ComputerConfiguration != nil && request.ComputerConfiguration.OSWorldConfiguration != nil {
					osWorldVersion = request.ComputerConfiguration.OSWorldConfiguration.Version
				}
			}
			if imageID == nil || *imageID != "img-unit" {
				t.Fatalf("ImageId = %v, want img-unit", imageID)
			}
			if osWorldVersion == nil || *osWorldVersion != "osworld2" {
				t.Fatalf("OSWorld Version = %v, want osworld2", osWorldVersion)
			}
		})
	}
}

func TestSDKSupportsInstanceTokenPaginationContract(t *testing.T) {
	request := ags.NewDescribeSandboxInstanceListRequest()
	if err := request.FromJsonString(`{"MaxResults":25,"NextToken":"current-page","NeedTotalCount":true}`); err != nil {
		t.Fatalf("FromJsonString: %v", err)
	}
	if request.MaxResults == nil || *request.MaxResults != 25 {
		t.Fatalf("MaxResults = %v, want 25", request.MaxResults)
	}
	if request.NextToken == nil || *request.NextToken != "current-page" {
		t.Fatalf("NextToken = %v, want current-page", request.NextToken)
	}
	if request.NeedTotalCount == nil || !*request.NeedTotalCount {
		t.Fatalf("NeedTotalCount = %v, want true", request.NeedTotalCount)
	}

	var response ags.DescribeSandboxInstanceListResponseParams
	if err := json.Unmarshal([]byte(`{"InstanceSet":[],"TotalCount":1,"NextToken":"next-page"}`), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.NextToken == nil || *response.NextToken != "next-page" {
		t.Fatalf("response NextToken = %v, want next-page", response.NextToken)
	}
}

func TestSDKSupportsPromotedPrecacheContract(t *testing.T) {
	request := ags.NewDescribePreCacheImageTaskRequest()
	if err := request.FromJsonString(`{"PreCacheImageId":"cache-test"}`); err != nil {
		t.Fatalf("FromJsonString: %v", err)
	}
	if request.PreCacheImageId == nil || *request.PreCacheImageId != "cache-test" {
		t.Fatalf("PreCacheImageId = %v, want cache-test", request.PreCacheImageId)
	}

	var created ags.CreatePreCacheImageTaskResponseParams
	if err := json.Unmarshal([]byte(`{"PreCacheImageId":"cache-test"}`), &created); err != nil {
		t.Fatal(err)
	}
	if created.PreCacheImageId == nil || *created.PreCacheImageId != "cache-test" {
		t.Fatalf("created PreCacheImageId = %v, want cache-test", created.PreCacheImageId)
	}

	var response ags.DescribePreCacheImageTaskResponseParams
	if err := json.Unmarshal([]byte(`{"PreCacheImageId":"cache-test","SourceType":"EXPLICIT","CachedImageSizeBytes":9007199254740993,"CreateTime":"created","LastUsedTime":"used"}`), &response); err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct {
		name, want string
		value      *string
	}{
		{"PreCacheImageId", "cache-test", response.PreCacheImageId},
		{"SourceType", "EXPLICIT", response.SourceType},
		{"CreateTime", "created", response.CreateTime},
		{"LastUsedTime", "used", response.LastUsedTime},
	} {
		if field.value == nil || *field.value != field.want {
			t.Errorf("%s = %v, want %s", field.name, field.value, field.want)
		}
	}
	if response.CachedImageSizeBytes == nil || *response.CachedImageSizeBytes != 9007199254740993 {
		t.Fatalf("CachedImageSizeBytes = %v, precision lost", response.CachedImageSizeBytes)
	}
}

func TestSDKSupportsSnapshotContract(t *testing.T) {
	start := ags.NewStartSandboxInstanceRequest()
	if err := start.FromJsonString(`{"SourceInstanceId":"ins-source"}`); err != nil {
		t.Fatal(err)
	}
	if start.SourceInstanceId == nil || *start.SourceInstanceId != "ins-source" {
		t.Fatalf("SourceInstanceId = %v, want ins-source", start.SourceInstanceId)
	}
	create := ags.NewCreateSandboxSnapshotRequest()
	if err := create.FromJsonString(`{"InstanceId":"ins-source","Memory":false,"ClientToken":"snapshot-retry"}`); err != nil {
		t.Fatal(err)
	}
	if create.Memory == nil || *create.Memory {
		t.Fatalf("Memory = %v, want explicit false", create.Memory)
	}
	if create.ClientToken == nil || *create.ClientToken != "snapshot-retry" {
		t.Fatalf("ClientToken = %v, want snapshot-retry", create.ClientToken)
	}
	list := ags.NewDescribeSandboxSnapshotListRequest()
	if err := list.FromJsonString(`{"SnapshotIds":["snapshot-test"],"MaxResults":25,"NextToken":"current-page"}`); err != nil {
		t.Fatal(err)
	}
	if list.MaxResults == nil || *list.MaxResults != 25 || list.NextToken == nil || *list.NextToken != "current-page" {
		t.Fatalf("pagination fields lost: %s", list.ToJsonString())
	}
	deleteRequest := ags.NewDeleteSandboxSnapshotRequest()
	if err := deleteRequest.FromJsonString(`{"SnapshotId":"snapshot-test"}`); err != nil {
		t.Fatal(err)
	}
	if deleteRequest.SnapshotId == nil || *deleteRequest.SnapshotId != "snapshot-test" {
		t.Fatalf("SnapshotId = %v, want snapshot-test", deleteRequest.SnapshotId)
	}
	var response ags.DescribeSandboxSnapshotListResponseParams
	if err := json.Unmarshal([]byte(`{"SnapshotSet":[{"SnapshotId":"snapshot-test","MemoryIncluded":false}],"TotalCount":1,"NextToken":"next-page"}`), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.SnapshotSet) != 1 || response.SnapshotSet[0].MemoryIncluded == nil || *response.SnapshotSet[0].MemoryIncluded {
		t.Fatalf("snapshot response lost explicit false: %+v", response)
	}
	if response.NextToken == nil || *response.NextToken != "next-page" {
		t.Fatalf("response NextToken = %v, want next-page", response.NextToken)
	}
}
