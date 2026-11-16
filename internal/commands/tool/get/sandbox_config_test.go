package get

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
)

func previewTool(t *testing.T) apivalue.Object {
	t.Helper()
	var tool apivalue.Object
	if err := json.Unmarshal([]byte(`{
		"ToolId":"sdt-preview","ToolName":"preview","Status":"ACTIVE",
		"KeepaliveConfiguration":{"KeepaliveMode":"MIXED","KeepaliveTimeout":"600s"},
		"AutoPause":false,"AutoResume":true,
		"LogConfiguration":{"ContentFormat":"auto","CLSConfig":{"TopicId":"topic"}},
		"ServerExtension":{"Nested":true}
	}`), &tool); err != nil {
		t.Fatal(err)
	}
	return tool
}

func TestResultPreservesKeepaliveAndLogFormat(t *testing.T) {
	result := Result(previewTool(t))
	data, _ := result.Data.(map[string]any)
	if value, ok := data["AutoPause"]; !ok || value != false {
		t.Fatalf("AutoPause=%v present=%v", value, ok)
	}
	keepalive, _ := data["KeepaliveConfiguration"].(map[string]any)
	if keepalive["KeepaliveMode"] != "MIXED" || keepalive["KeepaliveTimeout"] != "600s" {
		t.Fatalf("KeepaliveConfiguration=%v", data["KeepaliveConfiguration"])
	}
	logConfiguration, _ := data["LogConfiguration"].(map[string]any)
	if logConfiguration["ContentFormat"] != "auto" {
		t.Fatalf("LogConfiguration=%v", data["LogConfiguration"])
	}
	if _, ok := data["ServerExtension"]; !ok {
		t.Fatalf("unknown response fields must stay: %v", data)
	}

	var text bytes.Buffer
	result.Text(&text)
	if apimeta.BuildChannel == apimeta.Preview {
		for _, fragment := range []string{"KeepaliveMode:", "MIXED", "KeepaliveTimeout:", "600s", "AutoPause:", "false", "LogConfiguration.ContentFormat:", "auto"} {
			if !strings.Contains(text.String(), fragment) {
				t.Fatalf("text output is missing %q:\n%s", fragment, text.String())
			}
		}
		return
	}
	for _, fragment := range []string{"KeepaliveMode", "AutoPause", "LogConfiguration.ContentFormat"} {
		if strings.Contains(text.String(), fragment) {
			t.Fatalf("stable text output must not add %q:\n%s", fragment, text.String())
		}
	}
}

func TestResultKeepsMissingConfigurationMissing(t *testing.T) {
	result := Result(apivalue.Object{"ToolId": "sdt-preview", "Status": "ACTIVE"})
	data, _ := result.Data.(map[string]any)
	for _, name := range []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"} {
		if _, ok := data[name]; ok {
			t.Fatalf("missing %s was invented: %v", name, data)
		}
	}
	var text bytes.Buffer
	result.Text(&text)
	if strings.Contains(text.String(), "Keepalive") {
		t.Fatalf("text output invented keepalive data:\n%s", text.String())
	}
}
