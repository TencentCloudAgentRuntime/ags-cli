package list

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// Module returns this package's command module.
func Module() command.Module {
	api := APIDescriptor()
	spec := api.CommandSpec()
	spec.Output = command.OutputSpec{
		DataType:    "APIKeyListData",
		Description: "API key list with normalized items.",
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Generated: &command.Descriptor{
				Spec:   api.CommandSpec(),
				Groups: api.Groups,
				API:    api,
				Source: command.SourceAPICli,
			},
			Groups: api.Groups,
			API:    api,
			Source: command.SourceMixedAPI,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			builder := apicli.NewRequestBuilder(api)
			executor := apicli.NewExecutor(api, deps.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				apiReq, err := builder.Build(req)
				if err != nil {
					return nil, err
				}
				result, err := executor.Execute(ctx, apiReq)
				if err != nil {
					return nil, err
				}
				response, ok := result.Data.(*ags.DescribeAPIKeyListResponseParams)
				if !ok {
					return result, nil
				}
				result.Data = apiKeyListData(response)
				result.Text = func(w io.Writer) {
					renderAPIKeyList(w, response)
				}
				return result, nil
			})}, nil
		},
	}
}

func apiKeyListData(result *ags.DescribeAPIKeyListResponseParams) map[string]any {
	keys := result.APIKeySet
	items := make([]map[string]any, len(keys))
	for i, k := range keys {
		items[i] = map[string]any{
			"KeyId":     derefString(k.KeyId),
			"Name":      derefString(k.Name),
			"Status":    derefString(k.Status),
			"MaskedKey": derefString(k.MaskedKey),
			"CreatedAt": derefString(k.CreatedAt),
		}
	}
	return map[string]any{"Items": items}
}

func renderAPIKeyList(w io.Writer, result *ags.DescribeAPIKeyListResponseParams) {
	keys := result.APIKeySet
	if len(keys) == 0 {
		fmt.Fprintln(w, "No API keys found")
		return
	}
	headers := []string{"KEY ID", "NAME", "STATUS", "MASKED KEY", "CREATED"}
	rows := make([][]string, len(keys))
	for i, k := range keys {
		rows[i] = []string{derefString(k.KeyId), derefString(k.Name), derefString(k.Status), derefString(k.MaskedKey), derefString(k.CreatedAt)}
	}
	printTable(w, headers, rows)
}

func printTable(w io.Writer, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
