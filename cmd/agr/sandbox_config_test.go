//go:build !preview

package main

import (
	"strings"
	"testing"
)

var sandboxConfigCommands = []string{
	"tool.create",
	"tool.update",
	"instance.create",
	"instance.update",
	"tool.fork",
}

// The keepalive and log format members stay behind the preview channel: the
// stable contract, help and schema must not advertise flags they cannot send.
func TestSandboxConfigStableHidesPreviewMembers(t *testing.T) {
	for _, commandID := range sandboxConfigCommands {
		schema := schemaForCommand(t, commandID)
		for _, property := range []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"} {
			if _, ok := schema.RequestSchema[property]; ok {
				t.Errorf("%s exposes %s in the stable channel", commandID, property)
			}
		}
		for _, flag := range []string{"keepalive-configuration", "auto-pause", "auto-resume"} {
			if entry, ok := schema.Flags[flag]; ok {
				t.Errorf("%s exposes --%s in the stable channel: %#v", commandID, flag, entry)
			}
		}
	}
}

func TestSandboxConfigStableLogHelpOmitsContentFormat(t *testing.T) {
	schema := schemaForCommand(t, "tool.create")
	flag, ok := schema.Flags["log-configuration"]
	if !ok {
		t.Fatal("tool.create lost its existing --log-configuration flag")
	}
	for _, text := range append([]string{flag.Description, flag.Format}, flag.Examples...) {
		if strings.Contains(text, "ContentFormat") {
			t.Errorf("stable --log-configuration mentions ContentFormat: %q", text)
		}
	}
	if _, ok := schemaForCommand(t, "tool.update").Flags["log-configuration"]; ok {
		t.Error("tool.update must not accept --log-configuration")
	}
}
