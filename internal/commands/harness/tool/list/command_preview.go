//go:build preview

// Package list browses the official tool market for harness tools.
package list

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/harness/get"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module returns the read-only tool market list workflow.
func Module() command.Module {
	spec := command.Spec{
		ID:    "harness.tool.list",
		Path:  []string{"harness", "tool", "list"},
		Use:   "list",
		Short: "List official MCP servers, Skills and Plugins for harnesses",
		Long: "List the stable version of every tool in the tool market of the current region. Each row shows the reference to pass to harness create or update, whether an MCP server needs your own credential, and which engines a Plugin supports.\n\n" +
			"--compatible-with <harness-id> (or --engine with --engine-version) marks which tools that harness can use. Plugins must declare the exact engine and version; Skills and MCP servers work with every engine. " +
			"With --type plugin the incompatible Plugins are filtered out on the server. Available in preview builds.",
		Examples: []string{
			"Example - Browse every tool:\n  agr harness tool list -o text",
			"Example - Search MCP servers:\n  agr harness tool list --type mcp --search github -o text",
			"Example - Plugins an existing harness can use:\n  agr harness tool list --type plugin --compatible-with hns-a1b2c3d4 -o text",
			"Example - References of MCP servers that need no credential:\n  agr harness tool list --type mcp --all -o json --jq '.Data.Tools[] | select(.MCP.RequiresCredentials | not) | .Reference'",
		},
		PreserveFlagOrder: true,
		SupportsJSON:      true,
		Flags: []command.FlagSpec{
			harnesscli.EnumFlag("type", "Tool type: mcp, skill or plugin", harnesscli.ToolTypeNames...),
			harnesscli.StringFlag("search", "One keyword matched against tool names and descriptions"),
			harnesscli.StringFlag("compatible-with", "Harness ID whose engine decides which tools are usable"),
			harnesscli.StringFlag("engine", "Harness Engine to check compatibility against; use with --engine-version"),
			harnesscli.StringFlag("engine-version", "Harness Engine version to check compatibility against"),
			{Name: "offset", Usage: "Pagination offset", Type: command.FlagInt, Default: 0, Workflow: true},
			{Name: "limit", Usage: "Page size, 1-100", Type: command.FlagInt, Default: 100, Workflow: true},
			harnesscli.BoolFlag("all", "Fetch every page instead of one page"),
		},
		Output: command.OutputSpec{DataType: "HarnessToolList", Description: "Tools with reference, type, compatibility, MCP credential needs and the flags to add them.", Effects: []string{"read:registry-record-version"}},
	}
	return command.Module{
		Descriptor: command.Descriptor{Spec: spec, Groups: harnesscli.ToolGroups(harnesscli.GroupsOf(get.APIDescriptor(), 1)), Source: command.SourceWorkflow},
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
	engine, err := targetEngine(ctx, client, req)
	if err != nil {
		return nil, err
	}
	all := harnesscli.Bool(req, "all")
	if name := harnesscli.ChangedAny(req, "offset", "limit"); all && name != "" {
		return nil, output.NewUsageError("INVALID_PAGINATION", fmt.Sprintf("--all cannot be combined with --%s", name), "Use --all for every page, or --offset/--limit for one page.")
	}
	offset, limit := req.Flags["offset"].Int, req.Flags["limit"].Int
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, output.NewUsageError("INVALID_PAGINATION", "--offset must be >= 0 and --limit between 1 and 100", "Example: --offset 0 --limit 50")
	}
	kind := harnesscli.String(req, "type")
	filters, err := harnesscli.ToolFilters(kind, harnesscli.String(req, "search"), engine.Name, engine.Version)
	if err != nil {
		return nil, err
	}
	versions, total, err := client.ToolVersions(ctx, filters, offset, limit, all)
	if err != nil {
		return nil, err
	}
	tools := make([]harnesscli.Tool, 0, len(versions))
	for _, version := range versions {
		tools = append(tools, harnesscli.SummarizeTool(version, engine))
	}
	data := map[string]any{"Tools": tools, "TotalCount": total}
	if engine.Name != "" {
		data["HarnessEngine"] = engine.Name
		data["HarnessEngineVersion"] = engine.Version
	}
	return &command.Result{Data: data, Text: func(w io.Writer) { harnesscli.RenderTools(w, tools, total, engine) }}, nil
}

func targetEngine(ctx context.Context, client harnesscli.Client, req command.Request) (harnesscli.Engine, error) {
	harnessID := harnesscli.String(req, "compatible-with")
	engine := harnesscli.Engine{Name: harnesscli.String(req, "engine"), Version: harnesscli.String(req, "engine-version")}
	switch {
	case harnessID != "" && (engine.Name != "" || engine.Version != ""):
		return engine, output.NewUsageError("FLAG_CONFLICT", "--compatible-with cannot be combined with --engine or --engine-version", "Use a harness ID, or an engine and version.")
	case harnessID != "":
		harness, err := client.Harness(ctx, harnessID)
		if err != nil {
			return engine, err
		}
		return harnesscli.HarnessEngine(harness), nil
	case (engine.Name == "") != (engine.Version == ""):
		return engine, output.NewUsageError("MISSING_REQUIRED_FLAG", "--engine and --engine-version must be used together", "List values with 'agr harness engine list'.")
	}
	return engine, nil
}
