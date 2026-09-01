package controlplane

import (
	"testing"

	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func TestFillRequestSupportsToolComputerConfiguration(t *testing.T) {
	payload := map[string]any{
		"ComputerConfiguration": map[string]any{
			"WAAConfiguration": map[string]any{"ImageId": "img-unit"},
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
			var imageID *string
			switch request := tc.request.(type) {
			case *ags.CreateSandboxToolRequest:
				if request.ComputerConfiguration != nil && request.ComputerConfiguration.WAAConfiguration != nil {
					imageID = request.ComputerConfiguration.WAAConfiguration.ImageId
				}
			case *ags.UpdateSandboxToolRequest:
				if request.ComputerConfiguration != nil && request.ComputerConfiguration.WAAConfiguration != nil {
					imageID = request.ComputerConfiguration.WAAConfiguration.ImageId
				}
			}
			if imageID == nil || *imageID != "img-unit" {
				t.Fatalf("ImageId = %v, want img-unit", imageID)
			}
		})
	}
}
