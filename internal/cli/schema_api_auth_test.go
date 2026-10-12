package cli

import (
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

func TestReadOnlyAuthComesFromModuleMetadata(t *testing.T) {
	for _, source := range []string{command.SourceAPICli, command.SourceMixedAPI} {
		desc := command.Descriptor{
			Spec:   command.Spec{ID: "future.get", SupportsJSON: true, RequiresAuth: true},
			Source: source,
		}
		if schema := schemaFromDescriptor(desc); !schema.RequiresAuth || schema.Mutation {
			t.Fatalf("read API auth was inferred from mutation: %+v", schema)
		}
	}
}
