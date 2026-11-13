//go:build preview

package harnesscli

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// MCPShortcuts are the repeatable flags that build MCPServers.
var MCPShortcuts = []string{"mcp", "mcp-header", "mcp-credential"}

const credentialPlaceholder = "${credential}"

// mcpHeaderWarnings inspects the assembled request so shortcuts, JSON, files and
// stdin use the same advisory check. Header values are never put in warnings.
func mcpHeaderWarnings(request map[string]any) []string {
	var warnings []string
	for _, server := range (apivalue.Object(request)).Objects("MCPServers") {
		for _, header := range server.Objects("Headers") {
			name, value := header.String("Name"), header.String("Value")
			lower := strings.ToLower(name)
			sensitive := strings.Contains(lower, "authorization") || strings.Contains(lower, "key") ||
				strings.Contains(lower, "token") || strings.Contains(lower, "secret")
			if sensitive && strings.TrimSpace(value) != "" && !strings.Contains(value, credentialPlaceholder) {
				warnings = append(warnings, fmt.Sprintf("MCP header %q for server %q uses a plaintext value stored in Harness configuration; prefer --mcp-credential with ${credential} where possible.", name, server.String("Name")))
			}
		}
	}
	return warnings
}

// pi and dsh reject other characters inside the instance, long after save;
// dsh also caps names at 32 characters.
var mcpNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// RFC 9110 token characters, as validated by the service.
var headerNamePattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

// MCPFlags returns the MCP shortcut flags shared by create and update.
func MCPFlags(update bool) []command.FlagSpec {
	replace := ""
	if update {
		replace = " Replaces all current MCP servers; pass '' once to remove all."
	}
	return []command.FlagSpec{
		StringArrayFlag("mcp", "MCP server as <name>=<registry-id>/<record-id>/<version-id> or <name>=<https-url>; repeat for more. Names use letters, digits, '_' and '-' (at most 32)."+replace,
			"--mcp docs=MARKETPLACE/rec-0123abcd/rv-0123abcd", "--mcp search=https://mcp.example.com/mcp"),
		StringArrayFlag("mcp-header", "Header for an --mcp server as <name>:<Header>=<value>; repeat for more. Overrides the Registry header with the same name (case-insensitive). Use ${credential} in the value together with --mcp-credential.",
			"--mcp-header github:Authorization='Bearer ${credential}'"),
		StringArrayFlag("mcp-credential", "Managed SecretMultiUser credential for an --mcp server as <name>=<credential-provider-id>/<user-id>/<scope>. Its value replaces ${credential} in that server's --mcp-header values.",
			"--mcp-credential github=agc-a1b2c3d4/github-user/default"),
	}
}

// MCPServers builds the MCPServers list from the shortcut flags. ok is false
// when none of them was set.
func MCPServers(req command.Request) (servers []map[string]any, ok bool, err error) {
	if ChangedAny(req, MCPShortcuts...) == "" {
		return nil, false, nil
	}
	if Changed(req, "mcp-servers") {
		return nil, false, output.NewUsageError("FLAG_CONFLICT", "--mcp, --mcp-header and --mcp-credential cannot be combined with --mcp-servers",
			"Use either the --mcp shortcuts or a complete --mcp-servers JSON array.")
	}
	if !Changed(req, "mcp") {
		return nil, false, output.NewUsageError("MISSING_REQUIRED_FLAG", "--mcp-header and --mcp-credential require --mcp",
			"Declare the server with --mcp <name>=<source>, then attach headers or a credential to that name.")
	}
	values := listOrClear(Strings(req, "mcp"))
	if len(values) == 0 {
		if ChangedAny(req, "mcp-header", "mcp-credential") != "" {
			return nil, false, output.NewUsageError("FLAG_CONFLICT", "--mcp '' removes all MCP servers and cannot take headers or credentials", "Drop --mcp-header and --mcp-credential.")
		}
		return []map[string]any{}, true, nil
	}
	servers = make([]map[string]any, 0, len(values))
	byName := map[string]map[string]any{}
	foldedNames := map[string]string{}
	for _, value := range values {
		name, source, found := strings.Cut(strings.TrimSpace(value), "=")
		name = strings.TrimSpace(name)
		if !found || name == "" || strings.TrimSpace(source) == "" {
			return nil, false, invalidMCP(fmt.Sprintf("--mcp %q must be <name>=<registry-id>/<record-id>/<version-id> or <name>=<https-url>", value))
		}
		if !mcpNamePattern.MatchString(name) {
			return nil, false, output.NewUsageError("INVALID_MCP_NAME", fmt.Sprintf("MCP name %q is invalid", name),
				"Use 1-32 ASCII letters, digits, '_' or '-'; the pi and dsh engines reject other names when an instance starts.")
		}
		if _, exists := byName[name]; exists {
			return nil, false, output.NewUsageError("DUPLICATE_MCP_NAME", fmt.Sprintf("MCP name %q is used more than once", name), "Give every --mcp server a unique name.")
		}
		// pi maps '-' to '_' in tool prefixes, so these names collide at startup.
		folded := strings.ReplaceAll(name, "-", "_")
		if other, exists := foldedNames[folded]; exists {
			return nil, false, output.NewUsageError("DUPLICATE_MCP_NAME", fmt.Sprintf("MCP names %q and %q collide because '-' and '_' are treated alike", other, name), "Rename one of the --mcp servers.")
		}
		foldedNames[folded] = name
		parsed, err := mcpSource(strings.TrimSpace(source), value)
		if err != nil {
			return nil, false, err
		}
		server := map[string]any{"Name": name, "Source": parsed}
		byName[name] = server
		servers = append(servers, server)
	}
	if err := attachHeaders(byName, Strings(req, "mcp-header")); err != nil {
		return nil, false, err
	}
	if err := attachCredentials(byName, Strings(req, "mcp-credential")); err != nil {
		return nil, false, err
	}
	for _, server := range servers {
		if err := checkCredentialUse(server); err != nil {
			return nil, false, err
		}
	}
	return servers, true, nil
}

func mcpSource(source, original string) (map[string]any, error) {
	if strings.Contains(source, "://") {
		parsed, err := url.Parse(source)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil || strings.ContainsAny(source, " \t") {
			return nil, invalidMCP(fmt.Sprintf("--mcp %q has an invalid URL; use an absolute http(s) Streamable HTTP endpoint without credentials", original))
		}
		return map[string]any{"Remote": map[string]any{"URL": source}}, nil
	}
	refs, err := RegistryRefs("mcp", []string{source})
	if err != nil {
		return nil, invalidMCP(fmt.Sprintf("--mcp %q must reference <registry-id>/<record-id>/<version-id>", original))
	}
	return refs[0]["Source"].(map[string]any), nil
}

func invalidMCP(message string) error {
	return output.NewUsageError("INVALID_MCP", message,
		"Example: --mcp docs=MARKETPLACE/rec-0123abcd/rv-0123abcd. Find official MCP servers with 'agr harness tool list --type mcp'.")
}

func serverFor(byName map[string]map[string]any, flag, name, value string) (map[string]any, error) {
	server, ok := byName[name]
	if !ok {
		return nil, output.NewUsageError("UNKNOWN_MCP_NAME", fmt.Sprintf("--%s %q names MCP server %q, which no --mcp declares", flag, value, name),
			"Use the name given before '=' in --mcp.")
	}
	return server, nil
}

func attachHeaders(byName map[string]map[string]any, values []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		name, rest, okName := strings.Cut(value, ":")
		header, headerValue, okValue := strings.Cut(rest, "=")
		name, header = strings.TrimSpace(name), strings.TrimSpace(header)
		if !okName || !okValue || name == "" || header == "" {
			return output.NewUsageError("INVALID_MCP_HEADER", fmt.Sprintf("--mcp-header %q must be <name>:<Header>=<value>", value),
				"Example: --mcp-header github:Authorization='Bearer ${credential}'")
		}
		server, err := serverFor(byName, "mcp-header", name, value)
		if err != nil {
			return err
		}
		if !headerNamePattern.MatchString(header) {
			return output.NewUsageError("INVALID_MCP_HEADER", fmt.Sprintf("header name %q is not a valid HTTP header name", header), "Use letters, digits and '-', such as Authorization or X-Api-Key.")
		}
		if strings.ContainsAny(headerValue, "\r\n\x00") {
			return output.NewUsageError("INVALID_MCP_HEADER", fmt.Sprintf("header %q value contains a line break or NUL", header), "Remove control characters from the value.")
		}
		key := name + "\x00" + strings.ToLower(header)
		if seen[key] {
			return output.NewUsageError("INVALID_MCP_HEADER", fmt.Sprintf("header %q is set more than once for MCP server %q", header, name), "Header names are case-insensitive; set each once.")
		}
		seen[key] = true
		headers, _ := server["Headers"].([]map[string]any)
		server["Headers"] = append(headers, map[string]any{"Name": header, "Value": headerValue})
	}
	return nil
}

func attachCredentials(byName map[string]map[string]any, values []string) error {
	for _, value := range values {
		name, ref, found := strings.Cut(value, "=")
		name = strings.TrimSpace(name)
		parts := strings.Split(strings.TrimSpace(ref), "/")
		if !found || name == "" || len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return output.NewUsageError("INVALID_MCP_CREDENTIAL", fmt.Sprintf("--mcp-credential %q must be <name>=<credential-provider-id>/<user-id>/<scope>", value),
				"Create a SecretMultiUser provider with 'agr credential provider create --type SecretMultiUser' and store the secret with 'agr credential secret set'.")
		}
		server, err := serverFor(byName, "mcp-credential", name, value)
		if err != nil {
			return err
		}
		if _, exists := server["Credential"]; exists {
			return output.NewUsageError("INVALID_MCP_CREDENTIAL", fmt.Sprintf("MCP server %q has more than one --mcp-credential", name), "Each MCP server takes one credential.")
		}
		server["Credential"] = map[string]any{"CredentialProviderId": parts[0], "UserId": parts[1], "Scope": parts[2]}
	}
	return nil
}

// checkCredentialUse mirrors the service rules: ${credential} needs a
// credential, and a credential without ${credential} is never read.
func checkCredentialUse(server map[string]any) error {
	name := server["Name"].(string)
	headers, _ := server["Headers"].([]map[string]any)
	used := false
	for _, header := range headers {
		if strings.Contains(header["Value"].(string), credentialPlaceholder) {
			used = true
		}
	}
	_, hasCredential := server["Credential"]
	switch {
	case used && !hasCredential:
		return output.NewUsageError("MISSING_REQUIRED_FLAG", fmt.Sprintf("MCP server %q uses ${credential} without --mcp-credential", name),
			fmt.Sprintf("Add --mcp-credential %s=<credential-provider-id>/<user-id>/<scope>.", name))
	case hasCredential && !used:
		return output.NewUsageError("UNUSED_MCP_CREDENTIAL", fmt.Sprintf("MCP server %q has a credential but no --mcp-header uses ${credential}", name),
			fmt.Sprintf("Add a header such as --mcp-header %s:Authorization='Bearer ${credential}'; Registry default headers never receive the credential.", name))
	}
	return nil
}
