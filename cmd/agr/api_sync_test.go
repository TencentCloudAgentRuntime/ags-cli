package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestSyncedCommandContracts(t *testing.T) {
	for _, tc := range []struct {
		command, field, flag string
		values               []string
	}{
		{"pre-cache-image-task.get", "PreCacheType", "pre-cache-type", []string{"image", "image_volume"}},
		{"instance.create", "StartMode", "start-mode", []string{"DEFAULT", "WAIT_UNTIL_HEALTHY"}},
	} {
		t.Run(tc.command, func(t *testing.T) {
			schema := schemaForCommand(t, tc.command)
			property, ok := schema.RequestSchema[tc.field]
			if !ok || property.CliFlag == nil || *property.CliFlag != tc.flag {
				t.Fatalf("request property %s = %+v", tc.field, property)
			}
			cmd, ok := findCobraCommand(contractRoot(), tc.command)
			if !ok {
				t.Fatal("command missing")
			}
			help := commandHelp(t, cmd)
			for _, value := range tc.values {
				if !slices.Contains(schema.Flags[tc.flag].Values, value) || !strings.Contains(help, value) {
					t.Errorf("schema/help missing %q", value)
				}
			}
		})
	}
	out, err := runAGR(t, "schema", "registry.record.version.list", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			RequestSchema struct{ Required []string }
		}
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	required := envelope.Data.RequestSchema.Required
	if slices.Contains(required, "RecordId") || !slices.Contains(required, "RegistryId") {
		t.Fatalf("registry required properties = %v", required)
	}
}

func TestInstanceCommandsDoNotAdvertiseResizing(t *testing.T) {
	for _, id := range []string{"instance.create", "instance.get", "instance.list", "instance.update", "instance.pause", "instance.resume", "instance.delete"} {
		cmd, ok := findCobraCommand(contractRoot(), id)
		if !ok {
			t.Fatalf("command %s missing", id)
		}
		if strings.Contains(commandHelp(t, cmd), "RESIZING") {
			t.Errorf("%s advertises unsupported RESIZING status", id)
		}
		schema := schemaForCommand(t, id)
		for _, flag := range schema.Flags {
			if strings.Contains(strings.Join(flag.Values, " "), "RESIZING") {
				t.Errorf("%s schema advertises unsupported RESIZING status", id)
			}
		}
	}
}
