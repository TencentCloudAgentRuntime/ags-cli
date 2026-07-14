package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/spf13/cobra"
)

func TestBuildSchemaCatalogUsesRenderedCommandExamples(t *testing.T) {
	root := &cobra.Command{Use: "agr"}
	root.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show a resource",
		Run:   func(*cobra.Command, []string) {},
		Example: exampleBlocks(
			"agr show resource-1",
			"agr show resource-1 -o json\nagr show resource-2 -o json",
		),
	})

	schema, ok := buildSchemaCatalog(root).Lookup(root, "show")
	if !ok {
		t.Fatal("show schema not found")
	}
	want := []string{
		"agr show resource-1",
		"agr show resource-1 -o json\nagr show resource-2 -o json",
	}
	if !reflect.DeepEqual(schema.Examples, want) {
		t.Fatalf("Examples = %#v, want %#v", schema.Examples, want)
	}
}

func TestSchemaCatalogLeafExamplesMatchHelp(t *testing.T) {
	root := RootCmd()
	catalog := buildSchemaCatalog(root)
	helpSchema, ok := catalog.Lookup(root, "help")
	if !ok {
		t.Fatal("help schema not found")
	}
	wantHelpExamples := []string{"agr help instance create", "agr help schema"}
	if !reflect.DeepEqual(helpSchema.Examples, wantHelpExamples) {
		t.Fatalf("help Examples = %#v, want %#v", helpSchema.Examples, wantHelpExamples)
	}

	walkPublicCommands(root, func(cmd *cobra.Command) {
		if cmd.HasAvailableSubCommands() {
			return
		}
		commandID := canonicalCommandID(cmd)
		schema, ok := catalog.Lookup(root, commandID)
		if !ok {
			t.Errorf("schema for %s not found", commandID)
			return
		}

		wantCount := strings.Count(cmd.Example, "Example - ")
		if len(schema.Examples) != wantCount {
			t.Errorf("%s schema has %d examples, help has %d", commandID, len(schema.Examples), wantCount)
			return
		}
		for _, example := range schema.Examples {
			for _, line := range strings.Split(example, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.Contains(cmd.Example, line) {
					t.Errorf("%s schema example line %q is missing from help", commandID, line)
				}
			}
		}
	})
}

func TestSchemaFromDescriptorInfersEffects(t *testing.T) {
	tests := []struct {
		name            string
		effects         []string
		mutation        bool
		createsResource bool
		requiresAuth    bool
	}{
		{
			name:            "create effect",
			effects:         []string{"create:tool"},
			mutation:        true,
			createsResource: true,
			requiresAuth:    true,
		},
		{
			name:         "delete effect",
			effects:      []string{"delete:apikey"},
			mutation:     true,
			requiresAuth: true,
		},
		{
			name: "no effect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := schemaFromDescriptor(command.Descriptor{
				Spec: command.Spec{
					ID:           "test.command",
					Output:       command.OutputSpec{Effects: tt.effects},
					SupportsJSON: true,
				},
			})

			if schema.Mutation != tt.mutation {
				t.Fatalf("Mutation = %v, want %v", schema.Mutation, tt.mutation)
			}
			if schema.CreatesResource != tt.createsResource {
				t.Fatalf("CreatesResource = %v, want %v", schema.CreatesResource, tt.createsResource)
			}
			if schema.RequiresAuth != tt.requiresAuth {
				t.Fatalf("RequiresAuth = %v, want %v", schema.RequiresAuth, tt.requiresAuth)
			}
		})
	}
}
