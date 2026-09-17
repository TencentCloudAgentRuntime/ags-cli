package apimeta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

func contractFixture(t *testing.T, overrides map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"api.json":       effectiveTestBase,
		"api.patch.json": "[]", "mapping.patch.json": "[]", "help.patch.json": "[]",
		"mapping.yaml": "api_version: v1\nactions:\n  ExistingAction:\n    status: mapped\n    command: test.get\n    request: ExistingRequest\n    response: ExistingResponse\n    fields: {}\n",
		"help.json":    `{"api_version":"v1","commands":{"test.get":{"short":"Stable help","fields":{"Id":{"inputs":{"id":{"usage":"ID"}}}}}}}`,
	}
	for name, value := range overrides {
		files[name] = value
	}
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestContractIndependentOverlays(t *testing.T) {
	dir := contractFixture(t, map[string]string{
		"mapping.patch.json": `[{"op":"add","path":"/actions/ExistingAction/fields/Id","value":{"flag":"identifier"}}]`,
		"help.patch.json":    `[{"op":"test","path":"/commands/test.get/short","value":"Stable help"},{"op":"replace","path":"/commands/test.get/short","value":"Preview help"},{"op":"test","path":"/commands/test.get/fields/Id/inputs","value":{"id":{"usage":"ID"}}},{"op":"replace","path":"/commands/test.get/fields/Id/inputs","value":{"identifier":{"usage":"Preview ID"}}}]`,
	})
	for _, channel := range []apimeta.Channel{apimeta.Stable, apimeta.Preview} {
		c, err := apimeta.LoadContract(dir, channel)
		if err != nil {
			t.Fatal(err)
		}
		if channel == apimeta.Stable {
			if c.Mapping.Actions["ExistingAction"].Fields["Id"] != nil || c.Help.Commands["test.get"].Short != "Stable help" {
				t.Fatal("overlay leaked into stable")
			}
		} else if c.Mapping.Actions["ExistingAction"].Fields["Id"].Flag != "identifier" || c.Help.Commands["test.get"].Short != "Preview help" {
			t.Fatal("independent mapping/help overlays were not loaded")
		}
	}
}

func TestContractNewActionAndCommandRetention(t *testing.T) {
	dir := contractFixture(t, map[string]string{
		"api.patch.json":     `[{"op":"add","path":"/actions/PreviewAction","value":{"input":"ExistingRequest","output":"ExistingResponse"}}]`,
		"mapping.patch.json": `[{"op":"add","path":"/actions/PreviewAction","value":{"status":"mapped","command":"preview.get","request":"ExistingRequest","response":"ExistingResponse"}}]`,
	})
	stable, err := apimeta.LoadContract(dir, apimeta.Stable)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := apimeta.LoadContract(dir, apimeta.Preview)
	if err != nil {
		t.Fatal(err)
	}
	if len(stable.Spec.Actions) != 1 || len(preview.Spec.Actions) != 2 {
		t.Fatal("incorrect action projections")
	}
	if err := apimeta.ValidateCommandRetention(stable, preview); err != nil {
		t.Fatal(err)
	}
	preview.Mapping.Actions["ExistingAction"].Command = "renamed.get"
	if err := apimeta.ValidateCommandRetention(stable, preview); err == nil {
		t.Fatal("renaming a stable command was accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "mapping.patch.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := apimeta.LoadContract(dir, apimeta.Preview); err == nil || !strings.Contains(err.Error(), "MISSING_MAPPING") {
		t.Fatalf("missing preview mapping not rejected: %v", err)
	}
}

func TestContractRejectsInvalidMetadataPatches(t *testing.T) {
	for _, patch := range []string{
		`[{"op":"replace","path":"/api_version","value":"v2"}]`,
		`[{"op":"test","path":"/api_version","value":"wrong"},{"op":"replace","path":"/api_version","value":"v2"}]`,
		`[{"op":"add","path":"/api_version","value":"v2"}]`,
		`[{"op":"move","from":"/commands","path":"/other"}]`,
		`[{"op":"add","path":"/commands/unknown.command","value":{}}]`,
	} {
		dir := contractFixture(t, map[string]string{"help.patch.json": patch})
		if _, err := apimeta.LoadContract(dir, apimeta.Preview); err == nil {
			t.Errorf("invalid patch accepted: %s", patch)
		}
	}
	dir := contractFixture(t, nil)
	if _, err := apimeta.LoadContract(dir, "dev"); err == nil {
		t.Fatal("invalid channel accepted")
	}
	if err := os.Remove(filepath.Join(dir, "mapping.patch.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := apimeta.LoadContract(dir, apimeta.Stable); err == nil {
		t.Fatal("missing patch silently ignored")
	}
}

func TestCheckedInContractsBothChannels(t *testing.T) {
	for _, channel := range []apimeta.Channel{apimeta.Stable, apimeta.Preview} {
		if _, err := apimeta.LoadContract(filepath.Join("..", "..", "api", "ags", "v20250920"), channel); err != nil {
			t.Fatalf("%s: %v", channel, err)
		}
	}
}
