package get

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
)

func previewInstance(t *testing.T) apivalue.Object {
	t.Helper()
	var instance apivalue.Object
	if err := json.Unmarshal([]byte(`{
		"InstanceId":"ins-preview","ToolId":"sdt-preview","Status":"RUNNING",
		"KeepaliveConfiguration":{"KeepaliveMode":"REQUEST","KeepaliveTimeout":"30m"},
		"AutoPause":false,"AutoResume":true,
		"ServerExtension":{"Nested":true}
	}`), &instance); err != nil {
		t.Fatal(err)
	}
	return instance
}

func TestResultPreservesKeepaliveConfiguration(t *testing.T) {
	result := Result(previewInstance(t))
	data, _ := result.Data.(map[string]any)
	if value, ok := data["AutoPause"]; !ok || value != false {
		t.Fatalf("AutoPause=%v present=%v", value, ok)
	}
	keepalive, _ := data["KeepaliveConfiguration"].(map[string]any)
	if keepalive["KeepaliveMode"] != "REQUEST" || keepalive["KeepaliveTimeout"] != "30m" {
		t.Fatalf("KeepaliveConfiguration=%v", data["KeepaliveConfiguration"])
	}
	if _, ok := data["ServerExtension"]; !ok {
		t.Fatalf("unknown response fields must stay: %v", data)
	}

	var text bytes.Buffer
	result.Text(&text)
	if apimeta.BuildChannel == apimeta.Preview {
		for _, fragment := range []string{"KeepaliveMode:", "REQUEST", "KeepaliveTimeout:", "30m", "AutoPause:", "false"} {
			if !strings.Contains(text.String(), fragment) {
				t.Fatalf("text output is missing %q:\n%s", fragment, text.String())
			}
		}
		return
	}
	for _, fragment := range []string{"KeepaliveMode", "KeepaliveTimeout", "AutoPause"} {
		if strings.Contains(text.String(), fragment) {
			t.Fatalf("stable text output must not add %q:\n%s", fragment, text.String())
		}
	}
}

func TestResultKeepsMissingConfigurationMissing(t *testing.T) {
	result := Result(apivalue.Object{"InstanceId": "ins-preview", "Status": "RUNNING"})
	data, _ := result.Data.(map[string]any)
	for _, name := range []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"} {
		if _, ok := data[name]; ok {
			t.Fatalf("missing %s was invented: %v", name, data)
		}
	}
}
