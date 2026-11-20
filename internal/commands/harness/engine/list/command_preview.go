//go:build preview

package list

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/textview"
)

// Module renders engine and version options as a table.
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
		engines, err := response.ReadObjects("HarnessEngineSet")
		if err != nil {
			return nil, err
		}
		result.Text = func(w io.Writer) {
			if len(engines) == 0 {
				fmt.Fprintln(w, "No Harness Engines are offered in this region")
				return
			}
			rows := make([][]string, 0, len(engines))
			for _, engine := range engines {
				rows = append(rows, []string{engine.String("HarnessEngine"), strings.Join(harnesscli.StringList(engine["HarnessEngineVersion"]), ", ")})
			}
			textview.PrintTable(w, []string{"ENGINE", "VERSIONS"}, rows)
		}
		return result, nil
	})
}
