package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistrySymbolForCommandDir_UsesModuleWhenWrapperExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "command.go"), []byte("package x\n\nfunc Module() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(command.go) returned error: %v", err)
	}

	got, err := registrySymbolForCommandDir(dir)
	if err != nil {
		t.Fatalf("registrySymbolForCommandDir returned error: %v", err)
	}
	if got != "Module" {
		t.Fatalf("symbol = %q, want %q", got, "Module")
	}
}

func TestRegistrySymbolForCommandDir_UsesGeneratedModuleWithoutWrapper(t *testing.T) {
	dir := t.TempDir()

	got, err := registrySymbolForCommandDir(dir)
	if err != nil {
		t.Fatalf("registrySymbolForCommandDir returned error: %v", err)
	}
	if got != "GeneratedModule" {
		t.Fatalf("symbol = %q, want %q", got, "GeneratedModule")
	}
}

func TestRegistrySymbolForCommandDir_ErrsWhenWrapperHasNoModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "command.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(command.go) returned error: %v", err)
	}

	if _, err := registrySymbolForCommandDir(dir); err == nil {
		t.Fatal("registrySymbolForCommandDir returned nil error, want error")
	}
}

func TestOptionalPositionalIsCommandIndependent(t *testing.T) {
	for _, optional := range []bool{false, true} {
		args := argSpecs(commandModel{Command: "future.get", Fields: []fieldModel{{Name: "ResourceId", Positional: true, OptionalPositional: optional}}})
		if len(args) != 1 || args[0].Required == optional {
			t.Fatalf("optional=%v args=%+v", optional, args)
		}
	}
}
