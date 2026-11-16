//go:build preview

package create

import (
	"context"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func previewInstanceCreateFlags() map[string]command.FlagValue {
	flags := allInstanceCreateFlags()
	flags["keepalive-configuration"] = command.FlagValue{Name: "keepalive-configuration", Type: command.FlagString}
	flags["auto-pause"] = command.FlagValue{Name: "auto-pause", Type: command.FlagBool}
	flags["auto-resume"] = command.FlagValue{Name: "auto-resume", Type: command.FlagBool}
	flags["source-instance-id"] = command.FlagValue{Name: "source-instance-id", Type: command.FlagString}
	flags["snapshot-id"] = command.FlagValue{Name: "snapshot-id", Type: command.FlagString}
	return flags
}

func runPreviewInstanceCreate(t *testing.T, flags map[string]command.FlagValue) (map[string]any, error) {
	t.Helper()
	id := "ins-preview"
	cp := &fakeMixedControlPlane{resp: &ags.StartSandboxInstanceResponseParams{Instance: &ags.SandboxInstance{InstanceId: &id}}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{Flags: flags})
	return cp.request, err
}

func TestPreviewInstanceCreateKeepaliveForEverySelector(t *testing.T) {
	keepalive := command.FlagValue{Name: "keepalive-configuration", Type: command.FlagString,
		String: `{"KeepaliveMode":"REQUEST","KeepaliveTimeout":"30m"}`, Changed: true}
	for _, tc := range []struct {
		name    string
		flags   map[string]command.FlagValue
		wantKey string
		wantVal string
	}{
		{"tool-id", withChanged(previewInstanceCreateFlags(), "tool-id", "sdt-preview"), "ToolId", "sdt-preview"},
		{"source-instance-id", withChanged(previewInstanceCreateFlags(), "source-instance-id", "ins-source"), "SourceInstanceId", "ins-source"},
		{"snapshot-id", withChanged(previewInstanceCreateFlags(), "snapshot-id", "snap-preview"), "SnapshotId", "snap-preview"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := tc.flags
			flags["keepalive-configuration"] = keepalive
			flags["auto-pause"] = command.FlagValue{Name: "auto-pause", Type: command.FlagBool, Bool: false, Changed: true}
			request, err := runPreviewInstanceCreate(t, flags)
			if err != nil {
				t.Fatal(err)
			}
			if request[tc.wantKey] != tc.wantVal {
				t.Fatalf("%s=%v", tc.wantKey, request[tc.wantKey])
			}
			configuration, _ := request["KeepaliveConfiguration"].(map[string]any)
			if configuration["KeepaliveMode"] != "REQUEST" || configuration["KeepaliveTimeout"] != "30m" {
				t.Fatalf("KeepaliveConfiguration=%v", request["KeepaliveConfiguration"])
			}
			if value, ok := request["AutoPause"]; !ok || value != false {
				t.Fatalf("AutoPause=%v present=%v", value, ok)
			}
			if _, ok := request["AutoResume"]; ok {
				t.Fatalf("AutoResume was sent although omitted: %v", request)
			}
		})
	}
}

func TestPreviewInstanceCreateOmitsOmittedKeepalive(t *testing.T) {
	request, err := runPreviewInstanceCreate(t, withChanged(previewInstanceCreateFlags(), "tool-id", "sdt-preview"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"} {
		if _, ok := request[name]; ok {
			t.Fatalf("request sent %s although the flag was omitted: %v", name, request)
		}
	}
}
