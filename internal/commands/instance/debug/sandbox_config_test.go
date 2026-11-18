package debug

import (
	"context"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

func previewDebugSourceTool(t *testing.T) map[string]any {
	t.Helper()
	value, err := apivalue.Decode(sourceTool())
	if err != nil {
		t.Fatal(err)
	}
	value["KeepaliveConfiguration"] = map[string]any{"KeepaliveMode": "MIXED", "KeepaliveTimeout": "600s"}
	value["AutoPause"] = false
	value["AutoResume"] = true
	// The SDK has no ContentFormat yet, so the raw source object carries the
	// preview log configuration directly.
	value["LogConfiguration"] = map[string]any{
		"CLSConfig":     map[string]any{"TopicId": "topic-fixture"},
		"ContentFormat": "auto",
	}
	return map[string]any(value)
}

func TestDebugCopiesSandboxConfigurationToTheDebugTool(t *testing.T) {
	cp := &fakeControlPlane{sourceTool: previewDebugSourceTool(t)}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if _, err := runtime.Handler.Run(context.Background(), command.Request{
		Flags: map[string]command.FlagValue{
			"tool-id": {Name: "tool-id", Type: command.FlagString, String: "sdt-source", Changed: true},
		},
	}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(cp.requests) < 2 {
		t.Fatalf("requests = %#v", cp.requests)
	}
	createReq := cp.requests[0]
	startReq := cp.requests[1]
	if apimeta.BuildChannel != apimeta.Preview {
		for _, name := range []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"} {
			if _, ok := createReq[name]; ok {
				t.Fatalf("stable debug must not copy %s", name)
			}
		}
		return
	}
	configuration, _ := createReq["KeepaliveConfiguration"].(map[string]any)
	if configuration["KeepaliveTimeout"] != "600s" {
		t.Fatalf("debug tool did not inherit the keepalive configuration: %#v", createReq["KeepaliveConfiguration"])
	}
	if value, ok := createReq["AutoPause"]; !ok || value != false {
		t.Fatalf("debug tool did not inherit AutoPause=false: %#v", createReq["AutoPause"])
	}
	logConfiguration, _ := createReq["LogConfiguration"].(map[string]any)
	if logConfiguration["ContentFormat"] != "auto" {
		t.Fatalf("debug tool did not inherit LogConfiguration: %#v", createReq["LogConfiguration"])
	}
	// The debug start request deliberately keeps its existing fields and lets the
	// freshly created tool supply the defaults.
	for _, name := range []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"} {
		if _, ok := startReq[name]; ok {
			t.Fatalf("debug start request unexpectedly sends %s: %#v", name, startReq)
		}
	}
}
