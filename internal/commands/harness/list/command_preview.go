//go:build preview

package list

import (
	"context"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
)

var filters = []harnesscli.FilterFlag{
	{Flag: "name", Filter: "harness-name", Usage: "Exact harness name"},
	{Flag: "name-like", Filter: "harness-name-like", Usage: "Literal substring of the harness name"},
	{Flag: "status", Filter: "status", Usage: "Harness status", Values: []string{"ACTIVE", "DELETING", "DELETE_FAILED"}},
	{Flag: "tool-id", Filter: "tool-id", Usage: "Exact Sandbox Tool ID"},
	{Flag: "deployment-id", Filter: "deployment-id", Usage: "Exact Deployment ID"},
}

// Module wraps DescribeHarnessList with filter shortcuts and --all.
func Module() command.Module {
	return harnesscli.Mixed(APIDescriptor(), func(spec *command.Spec) {
		spec.Flags = append(spec.Flags, harnesscli.FilterFlags(filters)...)
	}, func(ctx context.Context, req command.Request, env harnesscli.Env) (*command.Result, error) {
		return harnesscli.List(ctx, req, env, harnesscli.ListOptions{
			Filters: filters, SetField: "HarnessSet", Exclusive: [2]string{"name", "name-like"},
			Render: func(w io.Writer, items []harnesscli.Item, total int) { harnesscli.RenderHarnessList(w, items, total) },
		})
	})
}
