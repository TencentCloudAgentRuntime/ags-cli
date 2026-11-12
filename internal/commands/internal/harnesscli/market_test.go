//go:build preview

package harnesscli

import (
	"bytes"
	"encoding/json"
	"reflect"
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
