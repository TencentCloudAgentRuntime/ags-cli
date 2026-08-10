package call

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	requestio "github.com/TencentCloudAgentRuntime/ags-cli/internal/cli/request"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/controlplane"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

type rawCaller interface {
	RawCall(ctx context.Context, action string, request []byte) (*controlplane.RawCallResult, error)
}

// RawCallResult re-exports the control-plane raw response shape used by this
// command's JSON output.
type RawCallResult = controlplane.RawCallResult

// Module returns this package's command module.
func Module() command.Module {
	spec := command.Spec{
		ID:    "api.call",
		Path:  []string{"api", "call"},
		Use:   "call <Action>",
		Short: "Send a raw API request",
		Args: []command.ArgSpec{
			{Name: "Action", Required: true, Description: "Tencent Cloud AGS action name."},
		},
		Flags: []command.FlagSpec{
			{Name: "request", Usage: "Raw API request body as JSON, @file, or - for stdin (required)", Type: command.FlagString},
		},
		SupportsJSON: true,
		Output: command.OutputSpec{
			DataType:    "RawAPIResponse",
			Description: "Raw control-plane API response.",
		},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Groups: []command.GroupSpec{
				{
					Path:  []string{"api"},
					Use:   "api",
					Short: "Low-level API access",
					Long:  "Raw API access for debugging and unmapped control-plane operations.",
				},
			},
			Source: command.SourceWorkflow,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			caller, _ := deps.ControlPlane.(rawCaller)
			return command.Runtime{
				Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
					action := req.ArgValues["Action"]
					if strings.TrimSpace(action) == "" {
						return nil, output.NewUsageError("MISSING_ACTION", "agr api call requires an <Action> argument", "Run: agr api call <Action> --request '{\"...\":\"...\"}'.")
					}
					flag := req.Flags["request"]
					if !flag.Changed || strings.TrimSpace(flag.String) == "" {
						return nil, output.NewUsageError("MISSING_REQUIRED_FLAG", "--request is required for 'agr api call'", "Provide a JSON object via --request, --request @file, or --request -.")
					}
					raw, err := requestio.ReadFlagFrom(flag.String, req.Stdin)
					if err != nil {
						return nil, err
					}
					var probe map[string]any
					if err := json.Unmarshal(raw, &probe); err != nil {
						return nil, output.NewUsageError("INVALID_REQUEST_JSON", fmt.Sprintf("invalid JSON in --request: %v", err), "Provide a valid JSON object as --request.")
					}
					if probe == nil {
						return nil, output.NewUsageError("INVALID_REQUEST_JSON", "--request must be a JSON object", "The top-level value must be a JSON object, not an array or scalar.")
					}
					callResult := &RawCallResult{
						Response: probe,
						Warnings: []string{
							"agr api call sent as a raw payload; this bypasses resource command mapping.",
						},
					}
					if caller != nil {
						var err error
						callResult, err = caller.RawCall(ctx, action, raw)
						if err != nil {
							return nil, err
						}
					}
					if callResult == nil {
						callResult = &RawCallResult{}
					}
					return &command.Result{
						Data: map[string]any{
							"Action":     action,
							"RequestRaw": json.RawMessage(raw),
							"Response":   callResult.Response,
						},
						Text: func(w io.Writer) {
							endpoint := ""
							if callResult.MetaExtra != nil {
								endpoint, _ = callResult.MetaExtra["CloudEndpoint"].(string)
							}
							if endpoint == "" {
								fmt.Fprintf(w, "Action: %s\n", action)
							} else {
								fmt.Fprintf(w, "Action: %s (raw call via %s)\n", action, endpoint)
							}
							pretty, _ := json.MarshalIndent(callResult.Response, "", "  ")
							fmt.Fprintln(w, string(pretty))
						},
						Warnings:  callResult.Warnings,
						MetaExtra: callResult.MetaExtra,
					}, nil
				}),
			}, nil
		},
	}
}
