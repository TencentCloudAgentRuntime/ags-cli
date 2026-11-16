//go:build preview

package main

import (
	"strings"
	"testing"
)

// Same command set as the stable-channel guard, kept local because the two
// channel files are compiled separately.
var sandboxConfigCommands = []string{
	"tool.create",
	"tool.update",
	"instance.create",
	"instance.update",
	"tool.fork",
}

var sandboxConfigPreviewProperties = []struct {
	property string
	flag     string
	schema   string
	flagType string
}{
	{property: "KeepaliveConfiguration", flag: "keepalive-configuration", schema: "object", flagType: "json"},
	{property: "AutoPause", flag: "auto-pause", schema: "bool", flagType: "bool"},
	{property: "AutoResume", flag: "auto-resume", schema: "bool", flagType: "bool"},
}

func TestSandboxConfigPreviewSchemaMatchesHelp(t *testing.T) {
	root := contractRoot()
	for _, commandID := range sandboxConfigCommands {
		schema := schemaForCommand(t, commandID)
		command, ok := findCobraCommand(root, commandID)
		if !ok {
			t.Fatalf("command %s not found", commandID)
		}
		help := commandHelp(t, command)
		for _, want := range sandboxConfigPreviewProperties {
			property, ok := schema.RequestSchema[want.property]
			if !ok {
				t.Fatalf("%s schema is missing %s", commandID, want.property)
			}
			if property.Type != want.schema {
				t.Errorf("%s %s type = %q, want %q", commandID, want.property, property.Type, want.schema)
			}
			if property.CliFlag == nil || *property.CliFlag != want.flag {
				t.Errorf("%s %s cli flag = %v, want %q", commandID, want.property, property.CliFlag, want.flag)
			}
			flag, ok := schema.Flags[want.flag]
			if !ok {
				t.Fatalf("%s schema has no --%s flag", commandID, want.flag)
			}
			if flag.Type != want.flagType {
				t.Errorf("%s --%s type = %q, want %q", commandID, want.flag, flag.Type, want.flagType)
			}
			if flag.Description == "" || !strings.Contains(help, "--"+want.flag) {
				t.Errorf("%s help does not document --%s:\n%s", commandID, want.flag, help)
			}
		}
		if !strings.Contains(help, "no default") {
			t.Errorf("%s help must state that the CLI applies no keepalive default:\n%s", commandID, help)
		}
	}
}

// The override flag is optional but must accept an explicit false, and the
// inherited sentinel must not be presented as a client default.
func TestSandboxConfigPreviewAutoFlagsDocumentExplicitFalse(t *testing.T) {
	for _, commandID := range sandboxConfigCommands {
		schema := schemaForCommand(t, commandID)
		for _, name := range []string{"auto-pause", "auto-resume"} {
			flag := schema.Flags[name]
			if !strings.Contains(flag.Description, "false") {
				t.Errorf("%s --%s must document the explicit false: %q", commandID, name, flag.Description)
			}
		}
	}
}

func TestSandboxConfigPreviewLogHelpCoversContentFormat(t *testing.T) {
	schema := schemaForCommand(t, "tool.create")
	flag, ok := schema.Flags["log-configuration"]
	if !ok {
		t.Fatal("tool.create is missing --log-configuration")
	}
	if !strings.Contains(flag.Format, "ContentFormat") {
		t.Errorf("--log-configuration format does not mention ContentFormat: %q", flag.Format)
	}
	if _, ok := schemaForCommand(t, "tool.update").Flags["log-configuration"]; ok {
		t.Error("tool.update must not accept --log-configuration")
	}
}

// Rejections that belong to the CLI contract must happen before any request is
// attempted, so they stay provable without credentials.
func TestSandboxConfigPreviewRejectsBadInputLocally(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		marker string
	}{
		{
			name:   "malformed keepalive json",
			args:   []string{"tool", "create", "-n", "sandbox-config", "-t", "code-interpreter", "--keepalive-configuration", `{"KeepaliveMode":`},
			marker: "INVALID_JSON",
		},
		{
			name:   "unknown top-level member",
			args:   []string{"instance", "update", "ins-xxxx", "--request", `{"KeepaliveMode":"REQUEST"}`},
			marker: "INVALID_REQUEST_JSON",
		},
		{
			name:   "missing selector",
			args:   []string{"instance", "update", "--auto-pause"},
			marker: "requires at least 1 arg",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := runAGR(t, append(tc.args, "-o", "json")...)
			if err == nil {
				t.Fatalf("command unexpectedly succeeded:\n%s", output)
			}
			if !strings.Contains(output, tc.marker) {
				t.Fatalf("output does not report %s:\n%s", tc.marker, output)
			}
		})
	}
}
