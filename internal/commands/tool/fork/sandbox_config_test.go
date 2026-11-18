package fork

import (
	"reflect"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

func TestForkCopiesSandboxConfiguration(t *testing.T) {
	cp := &fakeControlPlane{sourceTool: previewSourceTool("sdt-source")}
	runFork(t, cp, command.Request{
		Args: []string{"sdt-source"},
		Flags: map[string]command.FlagValue{
			"tool-name": {Name: "tool-name", Type: command.FlagString, String: "copy", Changed: true},
		},
	})
	if apimeta.BuildChannel != apimeta.Preview {
		for _, name := range []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"} {
			if _, ok := cp.request[name]; ok {
				t.Fatalf("stable fork must not copy %s: %#v", name, cp.request)
			}
		}
		return
	}
	if !reflect.DeepEqual(cp.request["KeepaliveConfiguration"], map[string]any{"KeepaliveMode": "MIXED", "KeepaliveTimeout": "600s"}) {
		t.Fatalf("KeepaliveConfiguration = %#v", cp.request["KeepaliveConfiguration"])
	}
	if cp.request["AutoPause"] != false || cp.request["AutoResume"] != true {
		t.Fatalf("AutoPause=%#v AutoResume=%#v", cp.request["AutoPause"], cp.request["AutoResume"])
	}
	logConfiguration, _ := cp.request["LogConfiguration"].(map[string]any)
	if logConfiguration["ContentFormat"] != "auto" {
		t.Fatalf("LogConfiguration = %#v", cp.request["LogConfiguration"])
	}
}

func TestForkOverridesSandboxConfiguration(t *testing.T) {
	cp := &fakeControlPlane{sourceTool: previewSourceTool("sdt-source")}
	runFork(t, cp, command.Request{
		Args: []string{"sdt-source"},
		Flags: map[string]command.FlagValue{
			"tool-name":               {Name: "tool-name", Type: command.FlagString, String: "copy", Changed: true},
			"keepalive-configuration": {Name: "keepalive-configuration", Type: command.FlagString, String: `{}`, Changed: true},
			"auto-pause":              {Name: "auto-pause", Type: command.FlagBool, Bool: false, Changed: true},
		},
	})
	if apimeta.BuildChannel != apimeta.Preview {
		return
	}
	configuration, ok := cp.request["KeepaliveConfiguration"].(map[string]any)
	if !ok || len(configuration) != 0 {
		t.Fatalf("an explicit empty object must replace the source configuration: %#v", cp.request["KeepaliveConfiguration"])
	}
	if value, ok := cp.request["AutoPause"]; !ok || value != false {
		t.Fatalf("explicit false override was lost: %#v present=%v", value, ok)
	}
	if cp.request["AutoResume"] != true {
		t.Fatalf("an unmentioned member must keep the source value: %#v", cp.request["AutoResume"])
	}
}
