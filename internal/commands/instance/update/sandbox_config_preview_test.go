//go:build preview

package update

import (
	"context"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

func runPreviewInstanceUpdate(t *testing.T, flags map[string]command.FlagValue) (map[string]any, error) {
	t.Helper()
	cp := &fakeMixedControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"ins-preview"},
		ArgValues: map[string]string{"instance-id": "ins-preview"},
		Flags:     flags,
	})
	return cp.request, err
}

func TestPreviewInstanceUpdateKeepaliveFlags(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flags  map[string]command.FlagValue
		absent []string
		check  func(t *testing.T, request map[string]any)
	}{
		{
			name:   "omitted",
			flags:  map[string]command.FlagValue{"timeout": {Name: "timeout", Type: command.FlagString, String: "10m", Changed: true}},
			absent: []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"},
		},
		{
			name: "explicit-false",
			flags: map[string]command.FlagValue{
				"auto-pause":  {Name: "auto-pause", Type: command.FlagBool, Bool: false, Changed: true},
				"auto-resume": {Name: "auto-resume", Type: command.FlagBool, Bool: false, Changed: true},
			},
			check: func(t *testing.T, request map[string]any) {
				for _, name := range []string{"AutoPause", "AutoResume"} {
					if value, ok := request[name]; !ok || value != false {
						t.Fatalf("%s=%v present=%v", name, value, ok)
					}
				}
			},
		},
		{
			name: "single-member",
			flags: map[string]command.FlagValue{
				"keepalive-configuration": {Name: "keepalive-configuration", Type: command.FlagString, String: `{"KeepaliveTimeout":"5m"}`, Changed: true},
			},
			check: func(t *testing.T, request map[string]any) {
				configuration, _ := request["KeepaliveConfiguration"].(map[string]any)
				if configuration["KeepaliveTimeout"] != "5m" {
					t.Fatalf("KeepaliveConfiguration=%v", request["KeepaliveConfiguration"])
				}
				if _, ok := configuration["KeepaliveMode"]; ok {
					t.Fatalf("KeepaliveMode was sent although omitted: %v", configuration)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := runPreviewInstanceUpdate(t, tc.flags)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range tc.absent {
				if _, ok := request[name]; ok {
					t.Fatalf("request sent %s although the flag was omitted: %v", name, request)
				}
			}
			if tc.check != nil {
				tc.check(t, request)
			}
		})
	}
}

func TestPreviewInstanceUpdateRejectsInvalidKeepaliveJSON(t *testing.T) {
	if _, err := runPreviewInstanceUpdate(t, map[string]command.FlagValue{
		"keepalive-configuration": {Name: "keepalive-configuration", Type: command.FlagString, String: `{invalid`, Changed: true},
	}); err == nil {
		t.Fatal("invalid keepalive JSON was accepted")
	}
}
