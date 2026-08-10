package cli

import (
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

func TestSchemaFromDescriptorInfersEffects(t *testing.T) {
	tests := []struct {
		name            string
		effects         []string
		source          string
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
			name:         "update effect",
			effects:      []string{"update:identity"},
			mutation:     true,
			requiresAuth: true,
		},
		{
			name: "no effect",
		},
		{
			// Workflow commands always hit the cloud control plane, so they
			// require auth even with no declared side-effects.
			name:         "workflow source without effects",
			source:       command.SourceWorkflow,
			requiresAuth: true,
		},
		{
			// A non-workflow source with no effects must stay auth-free.
			name:   "apicli source without effects",
			source: command.SourceAPICli,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := schemaFromDescriptor(command.Descriptor{
				Source: tt.source,
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
