//go:build preview

package list

import (
	"context"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
)

// Module renders model provider revision history.
func Module() command.Module {
	return harnesscli.Mixed(APIDescriptor(), nil, func(ctx context.Context, req command.Request, env harnesscli.Env) (*command.Result, error) {
		request, err := env.Builder.Build(req)
		if err != nil {
			return nil, err
		}
		result, response, err := env.Execute(ctx, request)
		if err != nil {
			return nil, err
		}
		revisions, err := response.ReadObjects("ModelProviderRevisionSet")
		if err != nil {
			return nil, err
		}
		total := int(response.Int64("TotalCount"))
		result.Text = func(w io.Writer) { harnesscli.RenderProviderRevisionList(w, revisions, total) }
		return result, nil
	})
}
