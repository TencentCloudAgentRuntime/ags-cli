// Package gpu provides the shared table output for GPU list commands.
package gpu

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

// ListModule keeps the generated API response intact for JSON and adds a table
// for text output. validate can check relationships among assembled request fields.
func ListModule(api apicli.APIDescriptor, collection, empty string, headers []string, row func(apivalue.Object) []string, validate func(map[string]any) error) command.Module {
	spec := api.CommandSpec()
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec, Groups: api.Groups, API: api, Source: command.SourceMixedAPI,
			Generated: &command.Descriptor{Spec: spec, Groups: api.Groups, API: api, Source: command.SourceAPICli},
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			builder := apicli.NewRequestBuilder(api)
			executor := apicli.NewExecutor(api, deps.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				request, err := builder.Build(req)
				if err != nil {
					return nil, err
				}
				if validate != nil {
					if err := validate(request); err != nil {
						return nil, err
					}
				}
				result, err := executor.Execute(ctx, request)
				if err != nil {
					return nil, err
				}
				response, err := apivalue.Decode(result.Data)
				if err != nil {
					return nil, err
				}
				items, err := response.ReadObjects(collection)
				if err != nil {
					return nil, err
				}
				result.Text = func(w io.Writer) {
					if len(items) == 0 {
						fmt.Fprintln(w, empty)
					} else {
						table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
						fmt.Fprintln(table, strings.Join(headers, "\t"))
						for _, item := range items {
							fmt.Fprintln(table, strings.Join(row(item), "\t"))
						}
						_ = table.Flush()
					}
					if total := response.Display("TotalCount"); total != "" {
						fmt.Fprintf(w, "Total: %s\n", total)
					}
				}
				return result, nil
			})}, nil
		},
	}
}
