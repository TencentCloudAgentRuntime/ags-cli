package cli

import (
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

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
