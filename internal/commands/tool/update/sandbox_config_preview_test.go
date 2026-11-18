//go:build preview

package update

import (
	"context"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

func runPreviewToolUpdate(t *testing.T, flags map[string]command.FlagValue) (map[string]any, error) {
	t.Helper()
	cp := &fakeControlPlane{}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"sdt-preview"},
		ArgValues: map[string]string{"tool-id": "sdt-preview"},
		Flags:     flags,
	})
	return cp.request, err
}

func TestPreviewToolUpdateKeepaliveFlags(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flags  map[string]command.FlagValue
		absent []string
		check  func(t *testing.T, request map[string]any)
	}{
		{
			name:   "omitted",
			flags:  map[string]command.FlagValue{"description": {Name: "description", Type: command.FlagString, String: "updated", Changed: true}},
			absent: []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"},
		},
		{
			name: "inline-json",
			flags: map[string]command.FlagValue{
				"keepalive-configuration": {Name: "keepalive-configuration", Type: command.FlagString, String: `{"KeepaliveMode":"MIXED","KeepaliveTimeout":"900s"}`, Changed: true},
				"auto-pause":              {Name: "auto-pause", Type: command.FlagBool, Bool: false, Changed: true},
			},
			check: func(t *testing.T, request map[string]any) {
				configuration, _ := request["KeepaliveConfiguration"].(map[string]any)
				if configuration["KeepaliveMode"] != "MIXED" || configuration["KeepaliveTimeout"] != "900s" {
					t.Fatalf("KeepaliveConfiguration=%v", request["KeepaliveConfiguration"])
				}
				if value, ok := request["AutoPause"]; !ok || value != false {
					t.Fatalf("AutoPause=%v present=%v", value, ok)
				}
				if _, ok := request["AutoResume"]; ok {
					t.Fatalf("AutoResume was sent although omitted: %v", request)
				}
			},
		},
		{
			name: "empty-object",
			flags: map[string]command.FlagValue{
				"keepalive-configuration": {Name: "keepalive-configuration", Type: command.FlagString, String: `{}`, Changed: true},
			},
			check: func(t *testing.T, request map[string]any) {
				configuration, ok := request["KeepaliveConfiguration"].(map[string]any)
				if !ok || len(configuration) != 0 {
					t.Fatalf("KeepaliveConfiguration=%v present=%v", request["KeepaliveConfiguration"], ok)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := runPreviewToolUpdate(t, tc.flags)
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

func TestPreviewToolUpdateRejectsInvalidKeepaliveJSON(t *testing.T) {
	if _, err := runPreviewToolUpdate(t, map[string]command.FlagValue{
		"keepalive-configuration": {Name: "keepalive-configuration", Type: command.FlagString, String: `{invalid`, Changed: true},
	}); err == nil {
		t.Fatal("invalid keepalive JSON was accepted")
	}
}
