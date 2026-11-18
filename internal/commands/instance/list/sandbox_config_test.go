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

func previewInstanceResponse(t *testing.T) apivalue.Object {
	t.Helper()
	var response apivalue.Object
	if err := json.Unmarshal([]byte(`{
		"InstanceSet":[{
			"InstanceId":"ins-preview","ToolName":"preview","Status":"RUNNING",
			"KeepaliveConfiguration":{"KeepaliveMode":"REQUEST","KeepaliveTimeout":"30m"},
			"AutoPause":false,"AutoResume":true
		}],
		"TotalCount":1,"RequestId":"req-1"
	}`), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestInstanceListPreservesKeepaliveConfiguration(t *testing.T) {
	result, err := instanceListResult(previewInstanceResponse(t), 0, nil, &command.Result{})
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
	if keepalive["KeepaliveMode"] != "REQUEST" || keepalive["KeepaliveTimeout"] != "30m" {
		t.Fatalf("KeepaliveConfiguration=%v", items[0]["KeepaliveConfiguration"])
	}

	var text bytes.Buffer
	result.Text(&text)
	if apimeta.BuildChannel == apimeta.Preview {
		if !strings.Contains(text.String(), "KEEPALIVE MODE") || !strings.Contains(text.String(), "REQUEST") {
			t.Fatalf("preview table is missing keepalive columns:\n%s", text.String())
		}
		return
	}
	if strings.Contains(text.String(), "KEEPALIVE") {
		t.Fatalf("stable table must keep its layout:\n%s", text.String())
	}
}

func TestInstanceListAllPreservesKeepaliveWithRegion(t *testing.T) {
	result, err := instanceListResult(previewInstanceResponse(t), 0, nil, &command.Result{}, listRenderOptions{All: true, Region: "ap-guangzhou"})
	if err != nil {
		t.Fatal(err)
	}
	var text bytes.Buffer
	result.Text(&text)
	if apimeta.BuildChannel != apimeta.Preview {
		return
	}
	header := strings.SplitN(text.String(), "\n", 2)[0]
	if !strings.HasPrefix(header, "REGION") || !strings.Contains(header, "KEEPALIVE MODE") {
		t.Fatalf("region column must stay first and configuration columns must follow: %q", header)
	}
}
