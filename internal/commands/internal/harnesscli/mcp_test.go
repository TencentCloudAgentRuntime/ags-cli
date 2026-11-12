//go:build preview

package harnesscli

import (
	"encoding/json"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

func mcpRequest(values map[string][]string) command.Request {
	flags := map[string]command.FlagValue{}
	for name, strings := range values {
		flags[name] = command.FlagValue{Name: name, Strings: strings}
	}
	return request(flags)
}

func TestMCPServersBuildsRegistryRemoteHeadersAndCredential(t *testing.T) {
	servers, ok, err := MCPServers(mcpRequest(map[string][]string{
		"mcp":            {"github=MARKETPLACE/rec-a/rv-a", "search=https://mcp.example.com/mcp"},
		"mcp-header":     {"github:Authorization=Bearer ${credential}", "search:X-Team=agents"},
		"mcp-credential": {"github=agc-1/user-1/default"},
	}))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	got, _ := json.Marshal(servers)
	want := `[{"Credential":{"CredentialProviderId":"agc-1","Scope":"default","UserId":"user-1"},"Headers":[{"Name":"Authorization","Value":"Bearer ${credential}"}],"Name":"github","Source":{"Registry":{"RecordId":"rec-a","RegistryId":"MARKETPLACE","VersionId":"rv-a"}}},` +
		`{"Headers":[{"Name":"X-Team","Value":"agents"}],"Name":"search","Source":{"Remote":{"URL":"https://mcp.example.com/mcp"}}}]`
	if string(got) != want {
		t.Fatalf("servers:\n got %s\nwant %s", got, want)
	}
}

func TestMCPServersClearAndAbsent(t *testing.T) {
	if _, ok, err := MCPServers(mcpRequest(map[string][]string{})); ok || err != nil {
		t.Fatalf("absent: ok=%v err=%v", ok, err)
	}
	servers, ok, err := MCPServers(mcpRequest(map[string][]string{"mcp": {""}}))
	if err != nil || !ok || servers == nil || len(servers) != 0 {
		t.Fatalf("clear: servers=%#v ok=%v err=%v", servers, ok, err)
	}
}

func TestMCPServersRejectsInvalidInput(t *testing.T) {
	for name, tc := range map[string]struct {
		values map[string][]string
		code   string
	}{
		"missing name":           {map[string][]string{"mcp": {"MARKETPLACE/r/v"}}, "INVALID_MCP"},
		"bad reference":          {map[string][]string{"mcp": {"a=MARKETPLACE/r"}}, "INVALID_MCP"},
		"bad url":                {map[string][]string{"mcp": {"a=ftp://host/mcp"}}, "INVALID_MCP"},
		"url with userinfo":      {map[string][]string{"mcp": {"a=https://u:p@host/mcp"}}, "INVALID_MCP"},
		"engine-unsafe name":     {map[string][]string{"mcp": {"my.docs=MARKETPLACE/r/v"}}, "INVALID_MCP_NAME"},
		"name too long":          {map[string][]string{"mcp": {"abcdefghijklmnopqrstuvwxyz0123456=MARKETPLACE/r/v"}}, "INVALID_MCP_NAME"},
		"duplicate name":         {map[string][]string{"mcp": {"a=MARKETPLACE/r/v", "a=MARKETPLACE/r/w"}}, "DUPLICATE_MCP_NAME"},
		"pi folded collision":    {map[string][]string{"mcp": {"my-docs=MARKETPLACE/r/v", "my_docs=MARKETPLACE/r/w"}}, "DUPLICATE_MCP_NAME"},
		"header without mcp":     {map[string][]string{"mcp-header": {"a:X=1"}}, "MISSING_REQUIRED_FLAG"},
		"header unknown server":  {map[string][]string{"mcp": {"a=MARKETPLACE/r/v"}, "mcp-header": {"b:X=1"}}, "UNKNOWN_MCP_NAME"},
		"header syntax":          {map[string][]string{"mcp": {"a=MARKETPLACE/r/v"}, "mcp-header": {"a=X"}}, "INVALID_MCP_HEADER"},
		"header name":            {map[string][]string{"mcp": {"a=MARKETPLACE/r/v"}, "mcp-header": {"a:Bad Name=1"}}, "INVALID_MCP_HEADER"},
		"header newline":         {map[string][]string{"mcp": {"a=MARKETPLACE/r/v"}, "mcp-header": {"a:X=1\n2"}}, "INVALID_MCP_HEADER"},
		"header duplicate":       {map[string][]string{"mcp": {"a=MARKETPLACE/r/v"}, "mcp-header": {"a:X-Key=1", "a:x-key=2"}}, "INVALID_MCP_HEADER"},
		"credential syntax":      {map[string][]string{"mcp": {"a=MARKETPLACE/r/v"}, "mcp-credential": {"a=p/u"}}, "INVALID_MCP_CREDENTIAL"},
		"two credentials":        {map[string][]string{"mcp": {"a=MARKETPLACE/r/v"}, "mcp-header": {"a:X=${credential}"}, "mcp-credential": {"a=p/u/s", "a=q/u/s"}}, "INVALID_MCP_CREDENTIAL"},
		"placeholder no cred":    {map[string][]string{"mcp": {"a=MARKETPLACE/r/v"}, "mcp-header": {"a:Authorization=${credential}"}}, "MISSING_REQUIRED_FLAG"},
		"credential unused":      {map[string][]string{"mcp": {"a=MARKETPLACE/r/v"}, "mcp-credential": {"a=p/u/s"}}, "UNUSED_MCP_CREDENTIAL"},
		"clear with header":      {map[string][]string{"mcp": {""}, "mcp-header": {"a:X=1"}}, "FLAG_CONFLICT"},
		"conflicts with servers": {map[string][]string{"mcp": {"a=MARKETPLACE/r/v"}, "mcp-servers": nil}, "FLAG_CONFLICT"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := MCPServers(mcpRequest(tc.values))
			if code := usageCode(t, err); code != tc.code {
				t.Fatalf("code = %s, want %s", code, tc.code)
			}
		})
	}
}
