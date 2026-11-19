//go:build preview

package harnesscli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
)

func marketVersion(t *testing.T, raw string) apivalue.Object {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

const bearerMCP = `{"DescriptorType":"MCP","Name":"GitHub","RecordId":"rec-gh","VersionId":"rv-gh","VersionName":"1.0","Status":"APPROVED","ContentStatus":"READY",
"Descriptors":"{\"name\":\"com.github/github\",\"remotes\":[{\"type\":\"sse\",\"url\":\"https://old.example.com/sse\"},{\"type\":\"streamable-http\",\"url\":\"https://api.example.com/mcp/\",\"headers\":[{\"name\":\"Authorization\",\"value\":\"Bearer {access_token}\",\"variables\":{\"access_token\":{\"isSecret\":true,\"isRequired\":true}}}]}]}",
"Metadata":[{"Name":"publisher","Value":"GitHub"},{"Name":"requires_credentials","Value":"true"},{"Name":"credential_url","Value":"https://example.com/tokens"}],"SupportedHarnessVersions":[]}`

func TestSummarizeToolMCPWithCredential(t *testing.T) {
	tool := SummarizeTool(marketVersion(t, bearerMCP), Engine{})
	if tool.Type != "mcp" || tool.Reference != "MARKETPLACE/rec-gh/rv-gh" || !tool.Ready || tool.Unusable != "" {
		t.Fatalf("tool = %+v", tool)
	}
	if tool.MCP.URL != "https://api.example.com/mcp/" || !tool.MCP.Supported || !tool.MCP.RequiresCredentials {
		t.Fatalf("mcp = %+v", tool.MCP)
	}
	want := []string{
		"--mcp github=MARKETPLACE/rec-gh/rv-gh",
		"--mcp-credential github=<credential-provider-id>/<user-id>/<scope>",
		"--mcp-header 'github:Authorization=Bearer ${credential}'",
	}
	if !reflect.DeepEqual(tool.Flags, want) {
		t.Fatalf("flags = %q", tool.Flags)
	}
}

func TestSummarizeMCPCredentialRequirements(t *testing.T) {
	for _, tc := range []struct {
		name, headers       string
		metadata            map[string]any
		requiresCredentials bool
		requiredHeaders     []bool
	}{
		{
			name:                "required secret header",
			headers:             `[{"name":"X-Key","isRequired":true,"isSecret":true},{"name":"X-Optional","isSecret":true}]`,
			requiresCredentials: true,
			requiredHeaders:     []bool{true, false},
		},
		{
			name:                "required secret variable",
			headers:             `[{"name":"Authorization","value":"Bearer {token}","variables":{"token":{"isSecret":true,"isRequired":true}}}]`,
			requiresCredentials: true,
			requiredHeaders:     []bool{true},
		},
		{
			name:            "supplied values and defaults",
			headers:         `[{"name":"X-Static","isRequired":true,"isSecret":true,"value":"static"},{"name":"X-Default","isRequired":true,"isSecret":true,"default":"default"},{"name":"X-Template","value":"{specified}-{fallback}","variables":{"specified":{"isSecret":true,"isRequired":true,"value":"static"},"fallback":{"isSecret":true,"isRequired":true,"default":"default"}}}]`,
			requiredHeaders: []bool{false, false, false},
		},
		{
			name:            "optional secret and required public variable",
			headers:         `[{"name":"X-Template","value":"{tenant}-{token}","variables":{"tenant":{"isRequired":true},"token":{"isSecret":true}}}]`,
			requiredHeaders: []bool{true},
		},
		{
			name:                "metadata requires credentials",
			headers:             `[{"name":"X-Optional","isSecret":true}]`,
			metadata:            map[string]any{"requires_credentials": "true", "credential_url": "https://example.com/credentials"},
			requiresCredentials: true,
			requiredHeaders:     []bool{true},
		},
		{
			name:            "metadata hint does not override a configured secret",
			headers:         `[{"name":"X-Key","isRequired":true,"isSecret":true,"default":"configured"}]`,
			metadata:        map[string]any{"requires_credentials": "true"},
			requiredHeaders: []bool{false},
		},
		{
			name:                "metadata can advertise credentials without a header",
			headers:             `[]`,
			metadata:            map[string]any{"requires_credentials": "true"},
			requiresCredentials: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			descriptor := `{"remotes":[{"type":"streamable-http","url":"https://example.com/mcp","headers":` + tc.headers + `}]}`
			mcp := summarizeMCP(descriptor, tc.metadata)
			if mcp.RequiresCredentials != tc.requiresCredentials {
				t.Errorf("RequiresCredentials = %v, want %v", mcp.RequiresCredentials, tc.requiresCredentials)
			}
			if len(mcp.Headers) != len(tc.requiredHeaders) {
				t.Fatalf("headers = %+v, want %d headers", mcp.Headers, len(tc.requiredHeaders))
			}
			for i, required := range tc.requiredHeaders {
				if mcp.Headers[i].Required != required {
					t.Errorf("%s Required = %v, want %v", mcp.Headers[i].Name, mcp.Headers[i].Required, required)
				}
			}
			if url, ok := tc.metadata["credential_url"]; ok && mcp.CredentialURL != url {
				t.Errorf("CredentialURL = %q, want %q", mcp.CredentialURL, url)
			}
		})
	}
}

func TestMarketMCPFlagsOnlyForMissingInputs(t *testing.T) {
	for _, tc := range []struct {
		name, headers string
		want          []string
		credentials   bool
	}{
		{
			name:    "required public header with default",
			headers: `[{"name":"X-Protocol-Version","isRequired":true,"default":"1"}]`,
		},
		{
			name:    "required secret header with explicit empty value",
			headers: `[{"name":"X-API-Key","isRequired":true,"isSecret":true,"value":"","default":"irrelevant"}]`,
		},
		{
			name:    "optional secret header",
			headers: `[{"name":"X-API-Key","isSecret":true}]`,
		},
		{
			name:    "required public header without value",
			headers: `[{"name":"X-Tenant","isRequired":true}]`,
			want:    []string{"--mcp-header 'docs:X-Tenant=<value>'"},
		},
		{
			name:    "required public variable and optional secret",
			headers: `[{"name":"X-Tenant","value":"{tenant}:{token}","variables":{"tenant":{"isRequired":true},"token":{"isSecret":true}}}]`,
			want:    []string{"--mcp-header 'docs:X-Tenant=<value>'"},
		},
		{
			name:        "single missing secret with a configured public variable",
			headers:     `[{"name":"Authorization","default":"{scheme} {access-token}","variables":{"scheme":{"isRequired":true,"default":"Bearer"},"access-token":{"isRequired":true,"isSecret":true}}}]`,
			want:        []string{"--mcp-credential docs=<credential-provider-id>/<user-id>/<scope>", "--mcp-header 'docs:Authorization=Bearer ${credential}'"},
			credentials: true,
		},
		{
			name:        "mixed missing and configured secret variables",
			headers:     `[{"name":"Authorization","value":"{configured}:{missing}","variables":{"configured":{"isSecret":true,"default":"test-private-value"},"missing":{"isSecret":true,"isRequired":true}}}]`,
			want:        []string{"--mcp-header 'docs:Authorization=<value>'"},
			credentials: true,
		},
		{
			name:    "static secret header value",
			headers: `[{"name":"Authorization","value":"test-private-value","isRequired":true,"isSecret":true}]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := marketMCPTool(`{"type":"streamable-http","url":"https://example.com/mcp","headers":` + tc.headers + `}`)
			want := append([]string{"--mcp docs=MARKETPLACE/rec-docs/rv-docs"}, tc.want...)
			if tool.MCP.RequiresCredentials != tc.credentials || !slices.Equal(tool.Flags, want) {
				t.Fatalf("credentials required = %v, flags = %q; want %v, %q", tool.MCP.RequiresCredentials, tool.Flags, tc.credentials, want)
			}
			raw, err := json.Marshal(tool)
			if err != nil {
				t.Fatal(err)
			}
			var rendered bytes.Buffer
			RenderTool(&rendered, tool)
			if bytes.Contains(raw, []byte("test-private-value")) || strings.Contains(rendered.String(), "test-private-value") {
				t.Fatal("configured secret appeared in market output")
			}
			if len(tc.want) == 0 && strings.Contains(rendered.String(), "you must set it") {
				t.Fatalf("market output requires overriding an available header:\n%s", rendered.String())
			}
		})
	}
}

func TestMarketMCPURLResolution(t *testing.T) {
	for _, tc := range []struct {
		name, template, variables string
		wantURL, missing          string
	}{
		{
			name:      "required workspace is missing",
			template:  "https://example.com/{workspace}/mcp",
			variables: `{"workspace":{"isRequired":true}}`,
			missing:   "workspace",
		},
		{
			name:      "required query parameter is missing",
			template:  "https://example.com/mcp?region={region}",
			variables: `{"region":{"isRequired":true}}`,
			missing:   "region",
		},
		{
			name:      "resolved explicit value, default and optional variable",
			template:  "https://example.com/{workspace}/{resource}/mcp{suffix}",
			variables: `{"workspace":{"isRequired":true,"value":"team-a","default":"unused"},"resource":{"isRequired":true,"default":"docs"},"suffix":{}}`,
			wantURL:   "https://example.com/team-a/docs/mcp",
		},
		{
			name:      "configured secret remains a placeholder",
			template:  "https://example.com/mcp?key={key}",
			variables: `{"key":{"isRequired":true,"isSecret":true,"default":"test-private-value"}}`,
			wantURL:   "https://example.com/mcp?key={key}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := marketMCPTool(`{"type":"streamable-http","url":"` + tc.template + `","variables":` + tc.variables + `}`)
			if tc.missing != "" {
				if !strings.Contains(tool.Unusable, tc.missing) || len(tool.Flags) != 0 {
					t.Fatalf("missing URL input %q: reason = %q, flags = %q", tc.missing, tool.Unusable, tool.Flags)
				}
			} else if tool.Unusable != "" || tool.MCP.URL != tc.wantURL || len(tool.Flags) != 1 {
				t.Fatalf("URL = %q, reason = %q, flags = %q; want usable with URL %q", tool.MCP.URL, tool.Unusable, tool.Flags, tc.wantURL)
			}
		})
	}
}

func marketMCPTool(remote string) Tool {
	return SummarizeTool(apivalue.Object{
		"DescriptorType": "MCP", "Name": "Docs", "RecordId": "rec-docs", "VersionId": "rv-docs",
		"Status": "APPROVED", "ContentStatus": "READY", "Descriptors": `{"remotes":[` + remote + `]}`,
	}, Engine{})
}

func TestSummarizeToolMCPWithoutStreamableHTTP(t *testing.T) {
	raw := strings.Replace(bearerMCP, `\"type\":\"streamable-http\"`, `\"type\":\"sse\"`, 1)
	tool := SummarizeTool(marketVersion(t, raw), Engine{})
	if tool.MCP.Supported || tool.Unusable != "has no streamable-http remote" {
		t.Fatalf("tool = %+v", tool)
	}
}

func TestSummarizeToolMCPWithSeveralSecrets(t *testing.T) {
	raw := `{"DescriptorType":"MCP","Name":"WeData","RecordId":"rec-w","VersionId":"rv-w","Status":"APPROVED","ContentStatus":"READY",
"Descriptors":"{\"name\":\"com.tencentcloud/wedata\",\"remotes\":[{\"type\":\"streamable-http\",\"url\":\"https://w.example.com/mcp\",\"headers\":[{\"name\":\"X-Secret-Id\",\"value\":\"{id}\",\"variables\":{\"id\":{\"isRequired\":true}}},{\"name\":\"X-Secret-Key\",\"value\":\"{key}\",\"variables\":{\"key\":{\"isSecret\":true,\"isRequired\":true}}},{\"name\":\"X-Static\",\"value\":\"v1\"}]}]}",
"Metadata":[{"Name":"requires_credentials","Value":"true"}]}`
	tool := SummarizeTool(marketVersion(t, raw), Engine{})
	want := []string{
		"--mcp wedata=MARKETPLACE/rec-w/rv-w",
		"--mcp-header 'wedata:X-Secret-Id=<value>'",
		"--mcp-header 'wedata:X-Secret-Key=<value>'",
	}
	if !reflect.DeepEqual(tool.Flags, want) {
		t.Fatalf("flags = %q", tool.Flags)
	}
	if tool.MCP.Headers[2].Required {
		t.Fatalf("static header must not be required: %+v", tool.MCP.Headers[2])
	}
}

func TestSummarizeToolPluginCompatibility(t *testing.T) {
	raw := `{"DescriptorType":"PLUGIN","Name":"lint","RecordId":"rec-p","VersionId":"rv-p","Status":"APPROVED","ContentStatus":"READY",
"SupportedHarnessVersions":[{"HarnessEngine":"pi","HarnessEngineVersion":"1.3.0"}]}`
	ok := SummarizeTool(marketVersion(t, raw), Engine{"pi", "1.3.0"})
	if ok.Compatible == nil || !*ok.Compatible || ok.Unusable != "" || ok.Flags[0] != "--plugin MARKETPLACE/rec-p/rv-p" {
		t.Fatalf("compatible tool = %+v", ok)
	}
	no := SummarizeTool(marketVersion(t, raw), Engine{"pi", "1.4.0"})
	if no.Compatible == nil || *no.Compatible || no.Unusable != "does not declare support for pi@1.4.0" {
		t.Fatalf("incompatible tool = %+v", no)
	}
	skill := SummarizeTool(marketVersion(t, strings.Replace(raw, `"PLUGIN"`, `"AGENT_SKILLS"`, 1)), Engine{"pi", "1.4.0"})
	if skill.Type != "skill" || skill.Compatible != nil || skill.Unusable != "" {
		t.Fatalf("skills work with every engine: %+v", skill)
	}
}

func TestSummarizeToolNotReadyAndUnsupportedType(t *testing.T) {
	pending := SummarizeTool(marketVersion(t, `{"DescriptorType":"AGENT_SKILLS","RecordId":"r","VersionId":"v","Status":"APPROVED","ContentStatus":"PREPARING"}`), Engine{})
	if pending.Ready || pending.Unusable != "this version is not approved and ready" {
		t.Fatalf("pending = %+v", pending)
	}
	a2a := SummarizeTool(marketVersion(t, `{"DescriptorType":"A2A","RecordId":"r","VersionId":"v","Status":"APPROVED","ContentStatus":"READY"}`), Engine{})
	if a2a.Type != "" || a2a.Unusable == "" || len(a2a.Flags) != 0 {
		t.Fatalf("a2a = %+v", a2a)
	}
}

func TestToolFilters(t *testing.T) {
	filters, err := ToolFilters("plugin", " lint ", "pi", "1.3.0")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(filters)
	want := `[{"Name":"label","Values":["stable"]},{"Name":"descriptor_type","Values":["PLUGIN"]},{"Name":"search","Values":["lint"]},{"Name":"supported_harness_engine","Values":["pi"]},{"Name":"supported_harness_version","Values":["1.3.0"]}]`
	if string(got) != want {
		t.Fatalf("filters = %s", got)
	}
	// MCP servers and Skills declare no engines; an engine filter would hide them.
	filters, _ = ToolFilters("mcp", "", "pi", "1.3.0")
	if len(filters) != 2 {
		t.Fatalf("mcp filters = %v", filters)
	}
	if _, err := ToolFilters("a2a", "", "", ""); usageCode(t, err) != "INVALID_TOOL_TYPE" {
		t.Fatal("want INVALID_TOOL_TYPE")
	}
}

func TestMCPNameFallsBackToRecordID(t *testing.T) {
	tool := Tool{Name: "腾讯文档", RecordId: "rec-docs", MCP: &ToolMCP{}}
	if got := MCPName(tool); got != "rec-docs" {
		t.Fatalf("name = %q", got)
	}
	tool.MCP.ServerName = "com.tencent/docs"
	if got := MCPName(tool); got != "docs" {
		t.Fatalf("name = %q", got)
	}
}

func TestRenderToolShowsFlagsOnlyWhenUsable(t *testing.T) {
	tool := SummarizeTool(marketVersion(t, bearerMCP), Engine{})
	var out bytes.Buffer
	RenderTool(&out, tool)
	if !strings.Contains(out.String(), "agr harness update <harness-id> --mcp github=") || !strings.Contains(out.String(), "agr credential secret set") {
		t.Fatalf("text:\n%s", out.String())
	}
	tool.Unusable = "blocked"
	out.Reset()
	RenderTool(&out, tool)
	if strings.Contains(out.String(), "agr harness update") || !strings.Contains(out.String(), "no: blocked") {
		t.Fatalf("text:\n%s", out.String())
	}
}
