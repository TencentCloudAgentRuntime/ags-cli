package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/spf13/cobra"
)

func contractRoot() *cobra.Command {
	root := cli.RootCmd()
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	if !have["instance"] {
		if err := attachCommands(); err != nil {
			panic(err)
		}
	}
	return root
}

func TestContract_GeneratedFlagsArePresentOnCobraCommands(t *testing.T) {
	for _, f := range loadGeneratedFlags(t) {
		if f.Positional || f.Excluded {
			continue
		}
		cmd, ok := findCobraCommand(contractRoot(), f.Command)
		if !ok {
			t.Errorf("command %s not found in cobra tree (referenced by generated flag --%s)", f.Command, f.Flag)
			continue
		}
		if cmd.Flags().Lookup(f.Flag) == nil {
			t.Errorf("command %q is missing generated long flag --%s (api field %s)", f.Command, f.Flag, f.Field)
		}
		for _, alias := range f.Aliases {
			if cmd.Flags().Lookup(alias) == nil {
				t.Errorf("command %q is missing alias --%s for api field %s", f.Command, alias, f.Field)
			}
		}
	}
}

func TestContract_GeneratedFlagTypesMatchCobraTypes(t *testing.T) {
	for _, f := range loadGeneratedFlags(t) {
		if f.Positional || f.Excluded {
			continue
		}
		cmd, ok := findCobraCommand(contractRoot(), f.Command)
		if !ok {
			t.Errorf("command %s not found in cobra tree (referenced by generated flag --%s)", f.Command, f.Flag)
			continue
		}
		flag := cmd.Flags().Lookup(f.Flag)
		if flag == nil {
			t.Errorf("command %q is missing generated long flag --%s (api field %s)", f.Command, f.Flag, f.Field)
			continue
		}
		want, ok := expectedCobraType(f)
		if !ok {
			continue
		}
		if got := flag.Value.Type(); got != want {
			t.Errorf("command %q flag --%s (api field %s type %s/%s) has cobra type %q, want %q",
				f.Command, f.Flag, f.Field, f.Type, f.NestedType, got, want)
		}
	}
}

func TestContract_RequestFlagOnEveryMappedRequestCommand(t *testing.T) {
	root := filepath.Join("..", "..", "api", "ags", "v20250920")
	spec, err := apimeta.LoadEffectiveSpec(root)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	mapping, err := apimeta.LoadMapping(filepath.Join(root, "mapping.yaml"))
	if err != nil {
		t.Fatalf("load mapping: %v", err)
	}
	for _, name := range mapping.MappedActionNames() {
		a := mapping.Actions[name]
		obj := spec.Object(a.Request)
		if obj == nil {
			continue
		}
		hasField := false
		for _, m := range obj.Members {
			if !m.Disabled {
				hasField = true
				break
			}
		}
		if !hasField {
			continue
		}
		cmd, ok := findCobraCommand(contractRoot(), a.Command)
		if !ok {
			t.Errorf("mapped command %s missing in cobra tree", a.Command)
			continue
		}
		if cmd.Flags().Lookup("request") == nil {
			t.Errorf("mapped command %q (action %s, request %s) is missing --request flag", a.Command, name, a.Request)
		}
	}
}

func expectedCobraType(f apimeta.FieldFlag) (string, bool) {
	switch f.Type {
	case "string", "enum":
		return "string", true
	case "bool":
		return "bool", true
	case "int", "integer":
		return "int", true
	case "object":
		return "string", true
	case "list", "array":
		if f.NestedType == "string" {
			return "stringArray", true
		}
		return "string", true
	default:
		return "", false
	}
}

func loadGeneratedFlags(t *testing.T) []apimeta.FieldFlag {
	t.Helper()
	root := filepath.Join("..", "..", "api", "ags", "v20250920")
	spec, err := apimeta.LoadEffectiveSpec(root)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	mapping, err := apimeta.LoadMapping(filepath.Join(root, "mapping.yaml"))
	if err != nil {
		t.Fatalf("load mapping: %v", err)
	}
	return apimeta.BuildFlags(spec, mapping).Flags
}

func findCobraCommand(root *cobra.Command, cmdID string) (*cobra.Command, bool) {
	parts := strings.Split(cmdID, ".")
	cmd := root
	for _, part := range parts {
		var next *cobra.Command
		for _, child := range cmd.Commands() {
			if child.Name() == part {
				next = child
				break
			}
		}
		if next == nil {
			return nil, false
		}
		cmd = next
	}
	return cmd, true
}
