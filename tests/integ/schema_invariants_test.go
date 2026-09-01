package integ_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands"
)

type schemaCatalogSnapshot struct {
	Commands []schemaCommandSnapshot `json:"Commands"`
}

type schemaCommandSnapshot struct {
	Name            string                 `json:"Name"`
	Kind            string                 `json:"Kind"`
	Mutation        bool                   `json:"Mutation"`
	RequiresAuth    bool                   `json:"RequiresAuth"`
	SupportsRequest bool                   `json:"SupportsRequest"`
	RequestSchema   *schemaRequestSnapshot `json:"RequestSchema"`
	Flags           []schemaFlagSnapshot   `json:"Flags"`
}

type schemaRequestSnapshot struct {
	Properties map[string]schemaPropertySnapshot `json:"Properties"`
}

type schemaPropertySnapshot struct {
	Type    string  `json:"Type"`
	CliFlag *string `json:"CliFlag"`
}

type schemaFlagSnapshot struct {
	Name string `json:"Name"`
	Type string `json:"Type"`
}

func TestSchema_RegistryInvariants(t *testing.T) {
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
	apiCatalog, err := apimeta.Get()
	if err != nil {
		t.Fatalf("load API metadata: %v", err)
	}

	var updateEffectCount, updateNameCount, workflowCount int
	var apiDescriptorCount, descriptorFlagCount, jsonFlagCount, requestPropertyCount, wrapperAPICommandCount int
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
		checkedFlags, jsonFlags := checkDescriptorFlagInvariants(t, descriptor, schema)
		descriptorFlagCount += checkedFlags
		jsonFlagCount += jsonFlags

		api, ok := descriptor.API.(apicli.APIDescriptor)
		if !ok {
			continue
		}
		apiDescriptorCount++
		if descriptor.Source != command.SourceAPICli {
			wrapperAPICommandCount++
		}
		requestPropertyCount += checkAPIContractInvariants(t, descriptor, api, schema, apiCatalog)
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
	if apiDescriptorCount == 0 {
		t.Error("no descriptors with API metadata found")
	}
	if descriptorFlagCount == 0 {
		t.Error("no descriptor flags found")
	}
	if jsonFlagCount == 0 {
		t.Error("no JSON parser flags found")
	}
	if requestPropertyCount == 0 {
		t.Error("no API request properties found")
	}
	if wrapperAPICommandCount == 0 {
		t.Error("no mixed or workflow command with API metadata found")
	}
}

func checkDescriptorFlagInvariants(t *testing.T, descriptor command.Descriptor, schema schemaCommandSnapshot) (int, int) {
	t.Helper()

	flagsByName := make(map[string]schemaFlagSnapshot, len(schema.Flags))
	for _, flag := range schema.Flags {
		if _, exists := flagsByName[flag.Name]; exists {
			t.Errorf("schema %q: duplicate flag %q", descriptor.Spec.ID, flag.Name)
		}
		flagsByName[flag.Name] = flag
	}
	jsonFlags := map[string]bool{}
	if api, ok := descriptor.API.(apicli.APIDescriptor); ok {
		for _, field := range api.Fields {
			if field.Parser != "common.default_json" {
				continue
			}
			for _, input := range field.Inputs {
				if !input.Positional && input.Flag != "" {
					jsonFlags[input.Flag] = true
				}
			}
		}
	}

	checkedCount := 0
	jsonFlagCount := 0
	descriptorFlagsByName := make(map[string]bool, len(descriptor.Spec.Flags))
	for _, descriptorFlag := range descriptor.Spec.Flags {
		if descriptorFlag.Hidden {
			continue
		}
		descriptorFlagsByName[descriptorFlag.Name] = true
		checkedCount++
		flag, ok := flagsByName[descriptorFlag.Name]
		if !ok {
			t.Errorf("schema %q: missing descriptor flag %q", descriptor.Spec.ID, descriptorFlag.Name)
			continue
		}
		wantType := descriptorFlagType(descriptorFlag.Type)
		if jsonFlags[descriptorFlag.Name] {
			jsonFlagCount++
			wantType = "json"
		}
		if flag.Type != wantType && (wantType != "string" || flag.Type != "enum") {
			t.Errorf("schema %q flag %q: Type = %q, want %q", descriptor.Spec.ID, descriptorFlag.Name, flag.Type, wantType)
		}
	}
	for _, schemaFlag := range schema.Flags {
		if descriptorFlagsByName[schemaFlag.Name] || (schemaFlag.Name == "generate-skeleton" && schema.SupportsRequest) {
			continue
		}
		t.Errorf("schema %q: flag %q is not present in the command descriptor", descriptor.Spec.ID, schemaFlag.Name)
	}
	return checkedCount, jsonFlagCount
}

func checkAPIContractInvariants(t *testing.T, descriptor command.Descriptor, api apicli.APIDescriptor, schema schemaCommandSnapshot, catalog *apimeta.Catalog) int {
	t.Helper()

	requestObject, ok := catalog.Object(api.API.RequestType)
	if !ok {
		t.Errorf("descriptor %q: API request type %q not found in metadata", descriptor.Spec.ID, api.API.RequestType)
		return 0
	}
	membersByName := make(map[string]apimeta.CatalogMember, len(requestObject.Members))
	for _, member := range requestObject.Members {
		membersByName[member.Name] = member
	}

	requestPropertyCount := 0
	for _, field := range api.Fields {
		member, ok := membersByName[field.Name]
		if !ok {
			t.Errorf("descriptor %q: API field %q not found in request type %q", descriptor.Spec.ID, field.Name, api.API.RequestType)
			continue
		}
		if schema.RequestSchema == nil {
			t.Errorf("schema %q: RequestSchema is nil for API field %q", descriptor.Spec.ID, field.Name)
			continue
		}
		property, ok := schema.RequestSchema.Properties[field.Name]
		if !ok {
			t.Errorf("schema %q: RequestSchema missing API field %q", descriptor.Spec.ID, field.Name)
			continue
		}
		requestPropertyCount++
		if want := requestPropertyType(member.Type); property.Type != want && (want != "string" || property.Type != "enum") {
			t.Errorf("schema %q request property %q: Type = %q, want %q", descriptor.Spec.ID, field.Name, property.Type, want)
		}

		for _, input := range field.Inputs {
			if input.Positional || input.Flag == "" {
				continue
			}
			if property.CliFlag == nil || *property.CliFlag != input.Flag {
				t.Errorf("schema %q request property %q: CliFlag = %v, want %q", descriptor.Spec.ID, field.Name, property.CliFlag, input.Flag)
			}
		}
	}
	return requestPropertyCount
}

func descriptorFlagType(flagType command.FlagType) string {
	switch flagType {
	case command.FlagInt:
		return "integer"
	case command.FlagStringArray:
		return "string_array"
	default:
		return string(flagType)
	}
}

func requestPropertyType(apiType string) string {
	switch apiType {
	case "list":
		return "array"
	case "int":
		return "integer"
	default:
		return apiType
	}
}
