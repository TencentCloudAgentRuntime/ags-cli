package cli

import (
	"slices"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

func TestRequestSchemaRetainsFieldsWithoutFlags(t *testing.T) {
	previous := registryAPIDescriptors
	previousSeeds := registrySchemaSeedsByID
	t.Cleanup(func() { registryAPIDescriptors = previous; registrySchemaSeedsByID = previousSeeds })
	registrySchemaSeedsByID = map[string]CommandSchema{"fixture": {Flags: []FlagSchema{{Name: "request", Type: "string"}, {Name: "wait", Type: "bool"}}}}
	catalog, err := apimeta.Get()
	if err != nil {
		t.Fatal(err)
	}
	// A fully excluded flag set must not erase the --request contract. Derive
	// expectations from every API member so another excluded field is covered.
	registryAPIDescriptors = map[string]apicli.APIDescriptor{
		"fixture": {API: apicli.APISpec{RequestType: "StartSandboxInstanceRequest"}},
	}
	schemas := []CommandSchema{{Name: "fixture", SupportsRequest: true, Flags: []FlagSchema{{Name: "client-token", Type: "string"}, {Name: "wait", Type: "bool"}}}}
	refreshAPIRequestSchemas(schemas)
	if slices.ContainsFunc(schemas[0].Flags, func(flag FlagSchema) bool { return flag.Name == "client-token" }) {
		t.Fatal("excluded flag retained in schema")
	}
	if !slices.ContainsFunc(schemas[0].Flags, func(flag FlagSchema) bool { return flag.Name == "wait" }) {
		t.Fatal("wrapper flag lost")
	}
	for _, member := range catalog.Spec.Object("StartSandboxInstanceRequest").Members {
		if member.Disabled {
			continue
		}
		if slices.Contains(schemas[0].RequestSchema.Required, member.Name) != member.Required {
			t.Errorf("request-only required status lost for %s", member.Name)
		}
		property, ok := schemas[0].RequestSchema.Properties[member.Name]
		if !ok || property.Type != requestPropertyType(member.Type) || property.CliFlag != nil {
			t.Errorf("request-only %s: property=%+v present=%v", member.Name, property, ok)
		}
	}
	api := registryAPIDescriptors["fixture"]
	api.DisableRequestFlag = true
	registryAPIDescriptors["fixture"] = api
	refreshAPIRequestSchemas(schemas)
	if len(schemas[0].RequestSchema.Properties) != 0 {
		t.Fatal("wrapper without --request exposed fields it cannot accept")
	}
}
