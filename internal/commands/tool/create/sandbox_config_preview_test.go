//go:build preview

package create

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func previewToolCreateFlags() map[string]command.FlagValue {
	flags := minRequiredFlags()
	flags["keepalive-configuration"] = command.FlagValue{Name: "keepalive-configuration", Type: command.FlagString}
	flags["auto-pause"] = command.FlagValue{Name: "auto-pause", Type: command.FlagBool}
	flags["auto-resume"] = command.FlagValue{Name: "auto-resume", Type: command.FlagBool}
	return flags
}

func runPreviewToolCreate(t *testing.T, flags map[string]command.FlagValue, stdin string) (map[string]any, error) {
	t.Helper()
	cp := &fakeMixedControlPlane{resp: &ags.CreateSandboxToolResponseParams{ToolId: stringPtr("sdt-preview")}}
	runtime, err := Module().Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	req := command.Request{Flags: flags}
	if stdin != "" {
		req.Stdin = strings.NewReader(stdin)
	}
	if _, err := runtime.Handler.Run(context.Background(), req); err != nil {
		return cp.request, err
	}
	return cp.request, nil
}

func stringPtr(value string) *string { return &value }

func TestPreviewKeepaliveInputsReachTheRequest(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "keepalive.json")
	if err := os.WriteFile(filePath, []byte(`{"KeepaliveMode":"CONNECTION","KeepaliveTimeout":"30m"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		flags  map[string]command.FlagValue
		stdin  string
		absent []string
		check  func(t *testing.T, request map[string]any)
	}{
		{
			name:   "omitted",
			flags:  previewToolCreateFlags(),
			absent: []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"},
		},
		{
			name: "inline-json",
			flags: withBoolChanged(withBoolChanged(withChanged(previewToolCreateFlags(), "keepalive-configuration",
				`{"KeepaliveMode":"REQUEST","KeepaliveTimeout":"600s"}`), "auto-pause", true), "auto-resume", true),
			check: func(t *testing.T, request map[string]any) {
				configuration, _ := request["KeepaliveConfiguration"].(map[string]any)
				if configuration["KeepaliveMode"] != "REQUEST" || configuration["KeepaliveTimeout"] != "600s" {
					t.Fatalf("KeepaliveConfiguration=%v", request["KeepaliveConfiguration"])
				}
				if request["AutoPause"] != true || request["AutoResume"] != true {
					t.Fatalf("AutoPause=%v AutoResume=%v", request["AutoPause"], request["AutoResume"])
				}
			},
		},
		{
			name:  "file",
			flags: withChanged(previewToolCreateFlags(), "keepalive-configuration", "@"+filePath),
			check: func(t *testing.T, request map[string]any) {
				configuration, _ := request["KeepaliveConfiguration"].(map[string]any)
				if configuration["KeepaliveMode"] != "CONNECTION" || configuration["KeepaliveTimeout"] != "30m" {
					t.Fatalf("KeepaliveConfiguration=%v", request["KeepaliveConfiguration"])
				}
			},
		},
		{
			name:  "stdin",
			flags: withChanged(previewToolCreateFlags(), "keepalive-configuration", "-"),
			stdin: `{"KeepaliveTimeout":"5m"}`,
			check: func(t *testing.T, request map[string]any) {
				configuration, _ := request["KeepaliveConfiguration"].(map[string]any)
				if configuration["KeepaliveTimeout"] != "5m" {
					t.Fatalf("KeepaliveConfiguration=%v", request["KeepaliveConfiguration"])
				}
			},
		},
		{
			name:  "explicit-false",
			flags: withBoolChanged(withBoolChanged(previewToolCreateFlags(), "auto-pause", false), "auto-resume", false),
			check: func(t *testing.T, request map[string]any) {
				if value, ok := request["AutoPause"]; !ok || value != false {
					t.Fatalf("AutoPause=%v present=%v", value, ok)
				}
				if value, ok := request["AutoResume"]; !ok || value != false {
					t.Fatalf("AutoResume=%v present=%v", value, ok)
				}
			},
		},
		{
			name:  "empty-object",
			flags: withChanged(previewToolCreateFlags(), "keepalive-configuration", `{}`),
			check: func(t *testing.T, request map[string]any) {
				configuration, ok := request["KeepaliveConfiguration"].(map[string]any)
				if !ok || len(configuration) != 0 {
					t.Fatalf("KeepaliveConfiguration=%v present=%v", request["KeepaliveConfiguration"], ok)
				}
			},
		},
		{
			name: "log-content-format",
			flags: withChanged(previewToolCreateFlags(), "log-configuration",
				`{"CLSConfig":{"TopicId":"topic"},"ContentFormat":"auto"}`),
			check: func(t *testing.T, request map[string]any) {
				configuration, _ := request["LogConfiguration"].(map[string]any)
				if configuration["ContentFormat"] != "auto" {
					t.Fatalf("LogConfiguration=%v", request["LogConfiguration"])
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := runPreviewToolCreate(t, tc.flags, tc.stdin)
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

func TestPreviewKeepaliveInputErrorsFailBeforeSending(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags map[string]command.FlagValue
	}{
		{"invalid-json", withChanged(previewToolCreateFlags(), "keepalive-configuration", `{invalid`)},
		{"empty-json", withChanged(previewToolCreateFlags(), "keepalive-configuration", "")},
		{"missing-file", withChanged(previewToolCreateFlags(), "keepalive-configuration", "@"+filepath.Join(t.TempDir(), "absent.json"))},
		{"conflicting-request", withChanged(withChanged(previewToolCreateFlags(), "keepalive-configuration", `{"KeepaliveMode":"NONE"}`), "request",
			`{"ToolName":"t","ToolType":"code-interpreter","NetworkConfiguration":{"NetworkMode":"SANDBOX"}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := runPreviewToolCreate(t, tc.flags, "")
			if err == nil {
				t.Fatalf("expected a structured failure, got request %v", request)
			}
		})
	}
}
