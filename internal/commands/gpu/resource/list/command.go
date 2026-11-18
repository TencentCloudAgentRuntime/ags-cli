package list

import (
	"cmp"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/gpu"
)

// Module lists individual GPU resources and their sandbox bindings.
func Module() command.Module {
	return gpu.ListModule(APIDescriptor(), "Resources", "No GPU resources found",
		[]string{"RESOURCE ID", "GROUP ID", "STATE", "ALLOCATION STATE", "SANDBOX INSTANCE ID", "EXPIRES AT"},
		func(item apivalue.Object) []string {
			return []string{
				item.String("ResourceId"), item.String("ResourceGroupId"), item.String("State"),
				item.String("AllocationState"), cmp.Or(item.String("InstanceId"), "-"),
				cmp.Or(item.String("ExpireAt"), "-"),
			}
		}, nil)
}
