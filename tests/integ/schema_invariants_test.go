package integ_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands"
)

type schemaCatalogSnapshot struct {
	Commands []schemaCommandSnapshot `json:"Commands"`
}

type schemaCommandSnapshot struct {
	Name         string `json:"Name"`
	Kind         string `json:"Kind"`
	Mutation     bool   `json:"Mutation"`
	RequiresAuth bool   `json:"RequiresAuth"`
}

func TestSchema_MetadataInvariants(t *testing.T) {
	registry, err := commands.Registry()
	if err != nil {
		t.Fatalf("build command registry: %v", err)
	}
	descriptors := registry.Descriptors()

	r := run(t, "schema", "-o", "json")
	if r.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s\nstdout: %s", r.exitCode, r.stderr, r.stdout)
	}
	env := parseEnvelope(t, r.stdout)
	rawData, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("marshal schema data: %v", err)
	}
	var catalog schemaCatalogSnapshot
	if err := json.Unmarshal(rawData, &catalog); err != nil {
		t.Fatalf("decode schema catalog: %v", err)
	}

	schemasByName := make(map[string]schemaCommandSnapshot, len(catalog.Commands))
	for i, schema := range catalog.Commands {
		if schema.Name == "" {
			t.Fatalf("schema entry %d has an empty Name", i)
		}
		if _, exists := schemasByName[schema.Name]; exists {
			t.Fatalf("duplicate schema Name %q", schema.Name)
		}
		schemasByName[schema.Name] = schema
	}

	knownSources := map[string]struct{}{
		command.SourceAPICli:   {},
		command.SourceWorkflow: {},
		command.SourceMixedAPI: {},
	}
	var updateEffectCount, updateNameCount, workflowCount int
	for _, descriptor := range descriptors {
		if _, ok := knownSources[descriptor.Source]; !ok {
			t.Errorf("descriptor %q: unknown Source %q", descriptor.Spec.ID, descriptor.Source)
		}
		if descriptor.Spec.Hidden {
			continue
		}

		schema, ok := schemasByName[descriptor.Spec.ID]
		if !ok {
			t.Errorf("descriptor %q not found in schema output", descriptor.Spec.ID)
			continue
		}
		if schema.Kind != "command" {
			t.Errorf("schema %q: Kind = %q, want command", descriptor.Spec.ID, schema.Kind)
		}

		hasUpdateEffect := false
		for _, effect := range descriptor.Spec.Output.Effects {
			if strings.HasPrefix(effect, "update:") {
				hasUpdateEffect = true
				break
			}
		}
		if hasUpdateEffect {
			updateEffectCount++
			if !schema.Mutation {
				t.Errorf("schema %q: Mutation = false for update effect", descriptor.Spec.ID)
			}
		}
		// Keep the command-name invariant while generated instance/tool update
		// descriptors do not yet declare update:* effects.
		if strings.HasSuffix(descriptor.Spec.ID, ".update") {
			updateNameCount++
			if !schema.Mutation {
				t.Errorf("schema %q: Mutation = false for .update command", descriptor.Spec.ID)
			}
		}
		if descriptor.Source == command.SourceWorkflow {
			workflowCount++
			if !schema.RequiresAuth {
				t.Errorf("schema %q: RequiresAuth = false for workflow command", descriptor.Spec.ID)
			}
		}
	}

	if updateEffectCount == 0 {
		t.Error("no non-hidden descriptors with update:* effects found")
	}
	if updateNameCount == 0 {
		t.Error("no non-hidden .update descriptors found")
	}
	if workflowCount == 0 {
		t.Error("no non-hidden workflow descriptors found")
	}
}
