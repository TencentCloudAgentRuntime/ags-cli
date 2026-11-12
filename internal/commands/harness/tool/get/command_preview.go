//go:build preview

// Package get shows one tool market item and how to add it to a harness.
package get

import (
	"context"
	"io"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	harnessget "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/harness/get"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module returns the read-only tool market detail workflow.
func Module() command.Module {
	spec := command.Spec{
		ID:    "harness.tool.get",
		Path:  []string{"harness", "tool", "get"},
		Use:   "get <record-id>[/<version-id>]",
		Short: "Show a tool market item and the flags that add it to a harness",
		Long: "Show one tool from the tool market: its fixed reference, the engines a Plugin supports, and for an MCP server the endpoint, the headers it needs and whether you must supply your own credential. " +
			"Without a version ID the stable version is shown. A MARKETPLACE/ prefix is accepted.\n\n" +
			"Data.Flags holds the harness create/update flags. On update, --skill, --plugin and --mcp replace the whole current list, so repeat the items you want to keep. " +
			"For an MCP server that needs a credential, store the secret first ('agr credential provider create --type SecretMultiUser' and 'agr credential secret set') and fill in the --mcp-credential placeholder. Available in preview builds.",
		Examples: []string{
			"Example - Inspect the stable version:\n  agr harness tool get rec-a1b2c3d4 -o text",
			"Example - Check a fixed version against a harness:\n  agr harness tool get rec-a1b2c3d4/rv-a1b2c3d4 --compatible-with hns-a1b2c3d4 -o text",
			"Example - Read the flags for scripts:\n  agr harness tool get rec-a1b2c3d4 -o json --jq '.Data.Tool.Flags[]'",
		},
		SupportsJSON: true,
		Args:         []command.ArgSpec{{Name: "tool", Required: true, Description: "Record ID, optionally followed by /<version-id>, such as rec-a1b2c3d4/rv-a1b2c3d4."}},
		Flags: []command.FlagSpec{
			harnesscli.StringFlag("compatible-with", "Harness ID to check this tool against"),
		},
		Output: command.OutputSpec{DataType: "HarnessTool", Description: "Tool reference, compatibility, MCP endpoint and credential needs, and the flags to add it.", Effects: []string{"read:registry-record-version"}},
	}
	return command.Module{
		Descriptor: command.Descriptor{Spec: spec, Groups: harnesscli.ToolGroups(harnesscli.GroupsOf(harnessget.APIDescriptor(), 1)), Source: command.SourceWorkflow},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			client, err := harnesscli.NewClient(deps, spec.ID)
			if err != nil {
				return command.Runtime{}, err
			}
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				return run(ctx, client, req)
			})}, nil
		},
	}
}

func run(ctx context.Context, client harnesscli.Client, req command.Request) (*command.Result, error) {
	recordID, versionID, err := parseTool(req.ArgValues["tool"])
	if err != nil {
		return nil, err
	}
	var engine harnesscli.Engine
	if harnessID := harnesscli.String(req, "compatible-with"); harnessID != "" {
		harness, err := client.Harness(ctx, harnessID)
		if err != nil {
			return nil, err
		}
		engine = harnesscli.HarnessEngine(harness)
	}
	version, err := client.ToolVersion(ctx, recordID, versionID)
	if err != nil {
		return nil, err
	}
	tool := harnesscli.SummarizeTool(version, engine)
	return &command.Result{Data: map[string]any{"Tool": tool}, Text: func(w io.Writer) { harnesscli.RenderTool(w, tool) }}, nil
}

func parseTool(value string) (recordID, versionID string, err error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), harnesscli.MarketRegistry+"/")
	parts := strings.Split(value, "/")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] == "") {
		return "", "", output.NewUsageError("INVALID_TOOL_REFERENCE", "tool must be <record-id> or <record-id>/<version-id>",
			"Find IDs with 'agr harness tool list'.")
	}
	if len(parts) == 2 {
		versionID = parts[1]
	}
	return parts[0], versionID, nil
}
