package list

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

// Module lists image pre-cache resources with a human-readable table while
// preserving the complete API response for JSON consumers.
func Module() command.Module {
	api := APIDescriptor()
	spec := api.CommandSpec()
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Generated: &command.Descriptor{
				Spec: spec, Groups: api.Groups, API: api, Source: command.SourceAPICli,
			},
			Groups: api.Groups, API: api, Source: command.SourceMixedAPI,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			builder := apicli.NewRequestBuilder(api)
			executor := apicli.NewExecutor(api, deps.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				request, err := builder.Build(req)
				if err != nil {
					return nil, err
				}
				result, err := executor.Execute(ctx, request)
				if err != nil {
					return nil, err
				}
				response, err := apivalue.Decode(result.Data)
				if err != nil {
					return nil, err
				}
				items, err := response.ReadObjects("PreCacheImageList")
				if err != nil {
					return nil, err
				}
				result.Text = func(w io.Writer) { renderList(w, response, items) }
				return result, nil
			})}, nil
		},
	}
}

func renderList(w io.Writer, response apivalue.Object, items []apivalue.Object) {
	if len(items) == 0 {
		fmt.Fprintln(w, "No image pre-cache tasks found")
	} else {
		table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(table, "ID\tIMAGE\tSTATUS\tREADY TIME")
		for _, item := range items {
			readyTime := item.String("ReadyTime")
			if readyTime == "" {
				readyTime = "-"
			}
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", item.String("PreCacheImageId"), item.String("Image"), item.String("Status"), readyTime)
		}
		_ = table.Flush()
	}
	if total := response.Display("TotalCount"); total != "" {
		fmt.Fprintf(w, "Total: %s\n", total)
	}
	if next := response.String("NextToken"); next != "" {
		fmt.Fprintf(w, "Next token: %s\n", next)
	}
}
