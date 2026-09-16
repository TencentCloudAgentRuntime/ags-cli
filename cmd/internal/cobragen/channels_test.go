package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

func TestGenerationSeparatesPreviewFields(t *testing.T) {
	apiDir := filepath.Join(t.TempDir(), "api")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"api.json", "mapping.yaml", "help.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "ags", "v20250920", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(apiDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	patch := `[{"op":"add","path":"/objects/StartSandboxInstanceRequest/members/-","value":{"name":"PreviewProbe","type":"string","member":"string","required":false}}]`
	for name, content := range map[string]string{"api.patch.json": patch, "mapping.patch.json": "[]", "help.patch.json": "[]"} {
		if err := os.WriteFile(filepath.Join(apiDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(t.TempDir())
	if err := run(apiDir, false); err != nil {
		t.Fatal(err)
	}
	stable, err := os.ReadFile("internal/apimeta/generated_model.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stable, []byte("PreviewProbe")) {
		t.Fatal("preview field leaked into stable catalog")
	}
	preview, err := os.ReadFile("internal/apimeta/generated_model_preview.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(preview, []byte("PreviewProbe")) {
		t.Fatal("preview catalog omitted preview field")
	}
	if err := run(apiDir, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, "api.patch.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(apiDir, true); err == nil {
		t.Fatal("check accepted stale preview outputs")
	}
	if err := run(apiDir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("internal/apimeta/generated_model_preview.go"); !os.IsNotExist(err) {
		t.Fatalf("stale preview catalog retained: %v", err)
	}
	stable, err = os.ReadFile("internal/apimeta/generated_model.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stable, []byte("go:build")) {
		t.Fatal("shared catalog retained channel constraint")
	}
}

func TestHandwrittenModuleBuildConstraints(t *testing.T) {
	dir := t.TempDir()
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("command_preview.go", "//go:build preview\n\npackage fixture\nfunc Module() {}\n")
	for _, tc := range []struct {
		channel apimeta.Channel
		symbol  string
	}{{apimeta.Stable, "GeneratedModule"}, {apimeta.Preview, "Module"}} {
		got, err := registrySymbolForCommandDir(dir, tc.channel)
		if err != nil || got != tc.symbol {
			t.Fatalf("%s: %s %v", tc.channel, got, err)
		}
	}
	write("command.go", "//go:build !preview\n\npackage fixture\n// Module() in a comment is not a declaration.\nfunc Module() {}\n")
	for _, channel := range []apimeta.Channel{apimeta.Stable, apimeta.Preview} {
		if got, err := registrySymbolForCommandDir(dir, channel); err != nil || got != "Module" {
			t.Fatal(got, err)
		}
	}
	write("duplicate_preview.go", "//go:build preview\n\npackage fixture\nfunc Module() {}\n")
	if _, err := registrySymbolForCommandDir(dir, apimeta.Preview); err == nil {
		t.Fatal("duplicate active Module accepted")
	}
	write("command.go", "package fixture\n// func Module() {}\n")
	if _, err := registrySymbolForCommandDir(dir, apimeta.Stable); err == nil {
		t.Fatal("comment mistaken for Module declaration")
	}
}

func TestGeneratorNeverDeletesUnmarkedFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("internal/commands/fixture", 0755); err != nil {
		t.Fatal(err)
	}
	path := "internal/commands/fixture/api_preview.generated.go"
	if err := os.WriteFile(path, []byte("package fixture\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := obsoleteOutputs(map[string][]byte{}); err == nil {
		t.Fatal("unmarked file accepted for deletion")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
