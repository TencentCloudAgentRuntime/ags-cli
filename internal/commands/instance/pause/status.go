package pause

import (
	"fmt"

	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func pauseResultText(data any, instanceID string) string {
	response, ok := data.(*ags.PauseSandboxInstanceResponseParams)
	if !ok || response == nil || response.InstanceStatus == nil || *response.InstanceStatus == "" {
		return fmt.Sprintf("Pause requested: %s", instanceID)
	}
	if *response.InstanceStatus == "PAUSED" {
		return fmt.Sprintf("Instance paused: %s", instanceID)
	}
	return fmt.Sprintf("Instance pause status: %s (%s)", *response.InstanceStatus, instanceID)
}
