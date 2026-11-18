package list

import (
	"cmp"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/gpu"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module lists GPU specifications and the number of machines available for purchase.
func Module() command.Module {
	return gpu.ListModule(APIDescriptor(), "GPUResourceSpecSet", "No GPU specifications found",
		[]string{"GPU TYPE", "CPU CORES", "HOST MEMORY (GiB)", "GPUS/MACHINE", "TOTAL GPU MEMORY (GiB)", "PURCHASABLE MACHINES"},
		func(item apivalue.Object) []string {
			return []string{
				item.String("GPUType"), item.Display("CPU"), item.Display("Memory"),
				item.Display("GPUNum"), item.Display("GPUMemoryGiB"),
				cmp.Or(item.Display("AvailableCount"), "unknown"),
			}
		}, validateExactMatch)
}

func validateExactMatch(request map[string]any) error {
	count := 0
	fields := []string{"GPUType", "CPU", "Memory", "GPUNum"}
	for _, field := range fields {
		if request[field] != nil {
			count++
		}
	}
	if count == 0 || count == len(fields) {
		return nil
	}
	return output.NewUsageError("INVALID_REQUEST_JSON",
		"GPUType, CPU, Memory, and GPUNum must all be supplied or omitted together.",
		"Use --gpu-type 5090d --cpu 32 --memory 96 --gpu-num 1 for an exact match, or omit all four filters.")
}
