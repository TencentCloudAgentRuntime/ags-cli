package list

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

func previewToolResponse(t *testing.T) apivalue.Object {
	t.Helper()
	var response apivalue.Object
	if err := json.Unmarshal([]byte(`{
		"SandboxToolSet":[{
			"ToolId":"sdt-preview","ToolName":"preview","ToolType":"code-interpreter","Status":"ACTIVE",
			"KeepaliveConfiguration":{"KeepaliveMode":"MIXED","KeepaliveTimeout":"600s"},
			"AutoPause":false,"AutoResume":true,
			"LogConfiguration":{"ContentFormat":"auto"}
		}],
		"TotalCount":1,"RequestId":"req-1"
	}`), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestToolListPreservesKeepaliveAndLogFormat(t *testing.T) {
	result, err := toolListResult(previewToolResponse(t), 0, nil, &command.Result{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := result.Data.(map[string]any)
	items, _ := data["Items"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("items=%v", data["Items"])
	}
	if value, ok := items[0]["AutoPause"]; !ok || value != false {
		t.Fatalf("AutoPause=%v present=%v", value, ok)
	}
	keepalive, _ := items[0]["KeepaliveConfiguration"].(map[string]any)
	if keepalive["KeepaliveTimeout"] != "600s" {
		t.Fatalf("KeepaliveConfiguration=%v", items[0]["KeepaliveConfiguration"])
	}
	logConfiguration, _ := items[0]["LogConfiguration"].(map[string]any)
	if logConfiguration["ContentFormat"] != "auto" {
		t.Fatalf("LogConfiguration=%v", items[0]["LogConfiguration"])
	}

	var text bytes.Buffer
	result.Text(&text)
	if apimeta.BuildChannel == apimeta.Preview {
		if !strings.Contains(text.String(), "LOG FORMAT") || !strings.Contains(text.String(), "KEEPALIVE MODE") {
			t.Fatalf("preview table is missing the configuration columns:\n%s", text.String())
		}
		if !strings.Contains(text.String(), "MIXED") || !strings.Contains(text.String(), "auto") {
			t.Fatalf("preview table is missing the configuration values:\n%s", text.String())
		}
		return
	}
	if strings.Contains(text.String(), "KEEPALIVE") || strings.Contains(text.String(), "LOG FORMAT") {
		t.Fatalf("stable table must keep its layout:\n%s", text.String())
	}
}

func TestToolListRendersMissingConfigurationAsDash(t *testing.T) {
	response := apivalue.Object{
		"SandboxToolSet": []any{map[string]any{"ToolId": "sdt-preview", "Status": "ACTIVE"}},
		"TotalCount":     json.Number("1"),
	}
	result, err := toolListResult(response, 0, nil, &command.Result{})
	if err != nil {
		t.Fatal(err)
	}
	var text bytes.Buffer
	result.Text(&text)
	if apimeta.BuildChannel != apimeta.Preview {
		return
	}
	// One dash per missing configuration cell.
	if strings.Count(text.String(), "-") < 5 {
		t.Fatalf("missing configuration was not rendered as dashes:\n%s", text.String())
	}
}
