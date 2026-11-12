//go:build preview

package harnesscli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/textview"
)

// MarketRegistry is the read-only selector the service resolves to the
// official tool market Registries of the current region.
const MarketRegistry = "MARKETPLACE"

// ToolGroups returns the harness group from a generated descriptor plus the
// workflow-only "harness tool" group.
func ToolGroups(harness []command.GroupSpec) []command.GroupSpec {
	return append(slices.Clone(harness[:1]), command.GroupSpec{
		Path:  []string{"harness", "tool"},
		Use:   "tool",
		Short: "Browse official MCP servers, Skills and Plugins from the tool market",
		Long: "The tool market lists official MCP servers, Skills and Plugins published for managed harnesses in the current region. " +
			"Every item is referenced by a fixed version as MARKETPLACE/<record-id>/<version-id> in harness create and update.\n\n" +
			"Restrictions: Plugins must declare the exact engine and version of the harness; Skills are only downloaded and checked when a revision publishes; " +
			"MCP servers need a streamable-http remote, and many need your own API key passed with --mcp-credential and --mcp-header.",
		Examples: []string{
			"Example - Browse MCP servers:\n  agr harness tool list --type mcp -o text",
			"Example - Plugins usable by an existing harness:\n  agr harness tool list --type plugin --compatible-with hns-a1b2c3d4 -o text",
			"Example - Inspect one tool and copy its flags:\n  agr harness tool get rec-a1b2c3d4 -o text",
		},
	})
}

// Tool types offered by the market and the Harness field each one fills.
var toolTypes = map[string]struct{ descriptor, flag, field string }{
	"mcp":    {"MCP", "mcp", "MCPServers"},
	"skill":  {"AGENT_SKILLS", "skill", "Skills"},
	"plugin": {"PLUGIN", "plugin", "Plugins"},
}

// ToolTypeNames lists the --type values in display order.
var ToolTypeNames = []string{"mcp", "skill", "plugin"}

func toolType(descriptor string) string {
	for name, kind := range toolTypes {
		if kind.descriptor == descriptor {
			return name
		}
	}
	return ""
}

// ToolFilters builds DescribeRegistryRecordVersionList filters for market
// browsing: always the stable label, plus optional type, search and engine.
func ToolFilters(kind, search, engine, version string) ([]map[string]any, error) {
	filters := []map[string]any{{"Name": "label", "Values": []string{"stable"}}}
	if kind != "" {
		t, ok := toolTypes[kind]
		if !ok {
			return nil, output.NewUsageError("INVALID_TOOL_TYPE", fmt.Sprintf("--type %q is not supported", kind), "Use mcp, skill or plugin.")
		}
		filters = append(filters, map[string]any{"Name": "descriptor_type", "Values": []string{t.descriptor}})
	}
	if search = strings.TrimSpace(search); search != "" {
		filters = append(filters, map[string]any{"Name": "search", "Values": []string{search}})
	}
	if engine != "" && version != "" && kind == "plugin" {
		// Only Plugins declare engine compatibility; filtering MCP or Skills by
		// engine would hide every item.
		filters = append(filters,
			map[string]any{"Name": "supported_harness_engine", "Values": []string{engine}},
			map[string]any{"Name": "supported_harness_version", "Values": []string{version}})
	}
	return filters, nil
}

// ToolVersions lists market versions. With all it follows every page.
func (c Client) ToolVersions(ctx context.Context, filters []map[string]any, offset, limit int, all bool) ([]apivalue.Object, int64, error) {
	var items []apivalue.Object
	for {
		value, err := c.cp.Call(ctx, "DescribeRegistryRecordVersionList", map[string]any{
			"RegistryId": MarketRegistry, "Offset": offset, "Limit": limit, "Filters": filters,
		})
		if err != nil {
			return nil, 0, err
		}
		response, err := apivalue.Decode(value)
		if err != nil {
			return nil, 0, err
		}
		page, err := response.ReadObjects("VersionSet")
		if err != nil {
			return nil, 0, err
		}
		items = append(items, page...)
		total := response.Int64("TotalCount")
		offset += len(page)
		if !all || len(page) == 0 || int64(offset) >= total {
			return items, total, nil
		}
	}
}

// ToolVersion reads one market record at a fixed version, or at its stable
// label when versionID is empty.
func (c Client) ToolVersion(ctx context.Context, recordID, versionID string) (apivalue.Object, error) {
	request := map[string]any{"RegistryId": MarketRegistry, "RecordId": recordID}
	if versionID != "" {
		request["VersionId"] = versionID
	}
	value, err := c.cp.Call(ctx, "DescribeRegistryRecord", request)
	if err != nil {
		return nil, err
	}
	response, err := apivalue.Decode(value)
	if err != nil {
		return nil, err
	}
	version := response.Object("Version")
	if version == nil {
		return nil, fmt.Errorf("DescribeRegistryRecord response is missing Version")
	}
	record := response.Object("Record")
	for _, key := range []string{"Name", "Description", "DescriptorType"} {
		if version.String(key) == "" && record.String(key) != "" {
			version[key] = record[key]
		}
	}
	return version, nil
}

// Engine is a Harness Engine and version a tool must support.
type Engine struct{ Name, Version string }

func (e Engine) String() string { return e.Name + "@" + e.Version }

// HarnessEngine reads the engine a harness draft uses.
func HarnessEngine(harness apivalue.Object) Engine {
	spec := harness.Object("Spec")
	return Engine{spec.String("HarnessEngine"), spec.String("HarnessEngineVersion")}
}

// Tool is the agent-facing summary of one market version.
type Tool struct {
	Type        string         `json:"Type"`
	Name        string         `json:"Name"`
	Description string         `json:"Description"`
	RecordId    string         `json:"RecordId"`
	VersionId   string         `json:"VersionId"`
	VersionName string         `json:"VersionName"`
	Reference   string         `json:"Reference"`
	Publisher   string         `json:"Publisher,omitempty"`
	Category    string         `json:"Category,omitempty"`
	Ready       bool           `json:"Ready"`
	Engines     []string       `json:"SupportedHarnessVersions"`
	Compatible  *bool          `json:"Compatible,omitempty"`
	Unusable    string         `json:"UnusableReason,omitempty"`
	MCP         *ToolMCP       `json:"MCP,omitempty"`
	Flags       []string       `json:"Flags"`
	Metadata    map[string]any `json:"Metadata,omitempty"`
}

// ToolMCP describes what an MCP needs beyond its reference.
type ToolMCP struct {
	ServerName          string          `json:"ServerName,omitempty"`
	URL                 string          `json:"URL,omitempty"`
	Supported           bool            `json:"StreamableHTTP"`
	RequiresCredentials bool            `json:"RequiresCredentials"`
	CredentialURL       string          `json:"CredentialURL,omitempty"`
	Headers             []ToolMCPHeader `json:"Headers"`
}

// ToolMCPHeader is one Registry header and whether the user must supply it.
type ToolMCPHeader struct {
	Name     string `json:"Name"`
	Template string `json:"Template,omitempty"`
	Required bool   `json:"Required"`
	Secret   bool   `json:"Secret"`
}

var templateVar = regexp.MustCompile(`\{[A-Za-z0-9_]+\}`)

// SummarizeTool derives type, readiness, compatibility, MCP needs and the
// flags to paste. engine may be zero when no harness is targeted.
func SummarizeTool(version apivalue.Object, engine Engine) Tool {
	metadata := map[string]any{}
	for _, entry := range version.Objects("Metadata") {
		metadata[entry.String("Name")] = entry["Value"]
	}
	tool := Tool{
		Type:        toolType(version.String("DescriptorType")),
		Name:        version.String("Name"),
		Description: version.String("Description"),
		RecordId:    version.String("RecordId"),
		VersionId:   version.String("VersionId"),
		VersionName: version.String("VersionName"),
		Publisher:   fmt.Sprint(orEmpty(metadata["publisher"])),
		Category:    fmt.Sprint(orEmpty(metadata["category"])),
		Engines:     []string{},
		Flags:       []string{},
		Metadata:    metadata,
	}
	tool.Reference = MarketRegistry + "/" + tool.RecordId + "/" + tool.VersionId
	tool.Ready = version.String("Status") == "APPROVED" && version.String("ContentStatus") == "READY"
	for _, pair := range version.Objects("SupportedHarnessVersions") {
		tool.Engines = append(tool.Engines, Engine{pair.String("HarnessEngine"), pair.String("HarnessEngineVersion")}.String())
	}
	switch {
	case tool.Type == "":
		tool.Unusable = fmt.Sprintf("type %s cannot be referenced by a harness", version.String("DescriptorType"))
	case !tool.Ready:
		tool.Unusable = "this version is not approved and ready"
	}
	if tool.Type == "plugin" && engine.Name != "" {
		compatible := slices.Contains(tool.Engines, engine.String())
		tool.Compatible = &compatible
		if !compatible && tool.Unusable == "" {
			tool.Unusable = fmt.Sprintf("does not declare support for %s", engine)
		}
	}
	if tool.Type == "mcp" {
		tool.MCP = summarizeMCP(version.String("Descriptors"), metadata)
		if !tool.MCP.Supported && tool.Unusable == "" {
			tool.Unusable = "has no streamable-http remote"
		}
	}
	tool.Flags = toolFlags(tool)
	return tool
}

func orEmpty(value any) any {
	if value == nil {
		return ""
	}
	return value
}

func summarizeMCP(descriptors string, metadata map[string]any) *ToolMCP {
	mcp := &ToolMCP{Headers: []ToolMCPHeader{}}
	mcp.RequiresCredentials = fmt.Sprint(metadata["requires_credentials"]) == "true"
	mcp.CredentialURL = fmt.Sprint(orEmpty(metadata["credential_url"]))
	var server struct {
		Name    string `json:"name"`
		Remotes []struct {
			Type    string `json:"type"`
			URL     string `json:"url"`
			Headers []struct {
				Name       string                     `json:"name"`
				Value      *string                    `json:"value"`
				Default    *string                    `json:"default"`
				IsRequired bool                       `json:"isRequired"`
				IsSecret   bool                       `json:"isSecret"`
				Variables  map[string]json.RawMessage `json:"variables"`
			} `json:"headers"`
		} `json:"remotes"`
	}
	if json.Unmarshal([]byte(descriptors), &server) != nil {
		return mcp
	}
	mcp.ServerName = server.Name
	for _, remote := range server.Remotes {
		// The service uses the first streamable-http remote and ignores others.
		if remote.Type != "streamable-http" {
			continue
		}
		mcp.Supported, mcp.URL = true, remote.URL
		for _, header := range remote.Headers {
			entry := ToolMCPHeader{Name: header.Name, Required: header.IsRequired, Secret: header.IsSecret}
			if header.Value != nil {
				entry.Template = *header.Value
			}
			for _, raw := range header.Variables {
				var variable struct {
					IsRequired bool    `json:"isRequired"`
					IsSecret   bool    `json:"isSecret"`
					Value      *string `json:"value"`
					Default    *string `json:"default"`
				}
				if json.Unmarshal(raw, &variable) == nil {
					entry.Secret = entry.Secret || variable.IsSecret
					entry.Required = entry.Required || (variable.IsRequired && variable.Value == nil && variable.Default == nil)
				}
			}
			// Without a value or default the service cannot fill the header itself.
			if header.Value == nil && header.Default == nil && header.IsRequired {
				entry.Required = true
			}
			if mcp.RequiresCredentials && entry.Secret {
				entry.Required = true
			}
			mcp.Headers = append(mcp.Headers, entry)
		}
		break
	}
	return mcp
}

// toolFlags renders the harness create/update flags that reference the tool.
// For an MCP whose headers carry a user secret it also shows the credential
// and header flags, with the Registry template rewritten to ${credential}.
func toolFlags(tool Tool) []string {
	switch tool.Type {
	case "skill", "plugin":
		return []string{fmt.Sprintf("--%s %s", tool.Type, tool.Reference)}
	case "mcp":
		name := MCPName(tool)
		flags := []string{fmt.Sprintf("--mcp %s=%s", name, tool.Reference)}
		var secret []ToolMCPHeader
		for _, header := range tool.MCP.Headers {
			if header.Required || header.Secret {
				secret = append(secret, header)
			}
		}
		if len(secret) == 1 {
			value := "${credential}"
			if matches := templateVar.FindAllString(secret[0].Template, -1); len(matches) == 1 {
				value = strings.Replace(secret[0].Template, matches[0], "${credential}", 1)
			}
			flags = append(flags,
				fmt.Sprintf("--mcp-credential %s=<credential-provider-id>/<user-id>/<scope>", name),
				fmt.Sprintf("--mcp-header '%s:%s=%s'", name, secret[0].Name, value))
		} else {
			for _, header := range secret {
				flags = append(flags, fmt.Sprintf("--mcp-header '%s:%s=<value>'", name, header.Name))
			}
		}
		return flags
	}
	return nil
}

var nameUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// MCPName suggests a connection name that satisfies the engine rules: the last
// segment of the server.json name ("com.github/github" -> "github"), else the
// display name, else the record ID.
func MCPName(tool Tool) string {
	candidates := []string{tool.Name}
	if tool.MCP != nil && tool.MCP.ServerName != "" {
		segments := strings.Split(tool.MCP.ServerName, "/")
		candidates = append([]string{segments[len(segments)-1]}, candidates...)
	}
	for _, candidate := range candidates {
		name := strings.Trim(strings.ToLower(nameUnsafe.ReplaceAllString(candidate, "-")), "-")
		if mcpNamePattern.MatchString(name) {
			return name
		}
	}
	return tool.RecordId
}

// RenderTools prints the market list.
func RenderTools(w io.Writer, tools []Tool, total int64, engine Engine) {
	if len(tools) == 0 {
		fmt.Fprintln(w, "No tools found")
		return
	}
	headers := []string{"TYPE", "NAME", "REFERENCE", "VERSION", "AUTH", "ENGINES"}
	if engine.Name != "" {
		headers = append(headers, "USABLE")
	}
	rows := make([][]string, 0, len(tools))
	for _, tool := range tools {
		auth := "-"
		if tool.MCP != nil {
			auth = "none"
			if tool.MCP.RequiresCredentials {
				auth = "required"
			}
		}
		engines := "all"
		if tool.Type == "plugin" {
			engines = orDash(strings.Join(tool.Engines, ","))
		}
		row := []string{tool.Type, tool.Name, tool.Reference, orDash(tool.VersionName), auth, engines}
		if engine.Name != "" {
			usable := "yes"
			if tool.Unusable != "" {
				usable = "no"
			}
			row = append(row, usable)
		}
		rows = append(rows, row)
	}
	textview.PrintTableWithPagination(w, headers, rows, len(tools), int(total))
}

// RenderTool prints one market tool and how to use it.
func RenderTool(w io.Writer, tool Tool) {
	rows := []textview.KeyValue{
		{Key: "Name", Value: tool.Name},
		{Key: "Type", Value: tool.Type},
		{Key: "Reference", Value: tool.Reference},
		{Key: "Version", Value: orDash(tool.VersionName)},
		{Key: "Publisher", Value: orDash(tool.Publisher)},
		{Key: "Description", Value: orDash(tool.Description)},
	}
	if tool.Type == "plugin" {
		rows = append(rows, textview.KeyValue{Key: "Engines", Value: orDash(strings.Join(tool.Engines, ", "))})
	}
	if tool.MCP != nil {
		rows = append(rows, textview.KeyValue{Key: "Endpoint", Value: orDash(tool.MCP.URL)})
		auth := "not required"
		if tool.MCP.RequiresCredentials {
			auth = "required"
			if tool.MCP.CredentialURL != "" {
				auth += " (get one at " + tool.MCP.CredentialURL + ")"
			}
		}
		rows = append(rows, textview.KeyValue{Key: "Credentials", Value: auth})
		for _, header := range tool.MCP.Headers {
			label := header.Name
			if header.Template != "" {
				label += ": " + header.Template
			}
			if header.Required {
				label += "  (you must set it)"
			}
			rows = append(rows, textview.KeyValue{Key: "Header", Value: label})
		}
	}
	if tool.Compatible != nil || tool.Unusable != "" {
		status := "yes"
		if tool.Unusable != "" {
			status = "no: " + tool.Unusable
		}
		rows = append(rows, textview.KeyValue{Key: "Usable", Value: status})
	}
	textview.PrintKV(w, rows)
	if len(tool.Flags) > 0 && tool.Unusable == "" {
		fmt.Fprintln(w, "\nAdd to a harness (on update the list replaces the current one; repeat the items to keep):")
		fmt.Fprintf(w, "  agr harness update <harness-id> %s\n", strings.Join(tool.Flags, " "))
		if note := toolNote(tool); note != "" {
			fmt.Fprintln(w, note)
		}
	}
}

// toolNote explains what the placeholders in the suggested flags need.
func toolNote(tool Tool) string {
	for _, flag := range tool.Flags {
		if strings.Contains(flag, "<value>") {
			return "Each <value> is stored in the harness as plain text; a managed credential fills only one ${credential} per MCP server."
		}
		if strings.Contains(flag, "<credential-provider-id>") {
			return "Store the secret first: 'agr credential provider create --type SecretMultiUser', then 'agr credential secret set'."
		}
	}
	return ""
}
