package list

import (
	"cmp"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/gpu"
)

// Module lists resource groups with their fixed specification and current counts.
func Module() command.Module {
	return gpu.ListModule(APIDescriptor(), "ResourceGroups", "No GPU resource groups found",
		[]string{"GROUP ID", "NAME", "GPU TYPE", "GPUS/MACHINE", "AVAILABLE", "ALLOCATED", "TOTAL"},
		func(item apivalue.Object) []string {
			spec := item.Object("Spec")
			counts := item.Object("Statistics")
			return []string{
				item.String("ResourceGroupId"), item.String("Name"),
				cmp.Or(spec.String("GPUTypeAlias"), spec.String("GPUType")),
				spec.Display("GPUNum"), counts.Display("AvailableCount"),
				counts.Display("AllocatedCount"), counts.Display("TotalCount"),
			}
		}, nil)
}
