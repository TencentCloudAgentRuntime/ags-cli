//go:build preview

package get

import (
	"context"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
)

// Module renders DescribeHarness as a readable draft summary.
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
		harness := response.Object("Harness")
		result.Text = func(w io.Writer) { harnesscli.RenderHarness(w, harness) }
		return result, nil
	})
}
