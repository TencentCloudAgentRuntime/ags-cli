//go:build preview

package list

import (
	"context"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
)

var filters = []harnesscli.FilterFlag{
	{Flag: "name", Filter: "model-provider-name", Usage: "Exact model provider name"},
	{Flag: "name-like", Filter: "model-provider-name-like", Usage: "Literal substring of the model provider name"},
	{Flag: "type", Filter: "type", Usage: "Source type", Values: []string{"OPENAI_COMPATIBLE", "TOKENHUB"}},
}

// Module wraps DescribeModelProviderList with filter shortcuts and --all.
func Module() command.Module {
	return harnesscli.Mixed(APIDescriptor(), func(spec *command.Spec) {
		spec.Flags = append(spec.Flags, harnesscli.FilterFlags(filters)...)
	}, func(ctx context.Context, req command.Request, env harnesscli.Env) (*command.Result, error) {
		return harnesscli.List(ctx, req, env, harnesscli.ListOptions{
			Filters: filters, SetField: "ModelProviderSet", Exclusive: [2]string{"name", "name-like"},
			Render: func(w io.Writer, items []harnesscli.Item, total int) { harnesscli.RenderProviderList(w, items, total) },
		})
	})
}
