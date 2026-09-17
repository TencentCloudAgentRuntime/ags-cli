package controlplane

import (
	"encoding/json"
	"testing"

	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

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
