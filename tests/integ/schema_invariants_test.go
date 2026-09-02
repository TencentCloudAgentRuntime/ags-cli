package integ_test

import (
	"encoding/json"
	"maps"
	"slices"
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

// apiDescriptorFieldExclusions is the only escape hatch for API request
// members that an API-backed command intentionally omits from its descriptor.
// It is keyed by command ID because multiple wrappers may share one API action
// while exposing different contracts. Every exclusion must carry a reason;
// the coverage check rejects empty, stale, and overlapping entries.
var apiDescriptorFieldExclusions = map[string]map[string]string{}

// workflowAPIContracts makes API-backed hand-written workflows explicit.
// Generated and mixed API commands already carry generator-owned metadata;
// workflows need this declaration so removing Descriptor.API cannot silently
// opt the command out of the API contract checks.
var workflowAPIContracts = map[string]string{
	"tool.fork": "CreateSandboxTool",
}

func TestAPIFieldCoverageIssues(t *testing.T) {
	members := []apimeta.CatalogMember{{Name: "A"}, {Name: "B"}}
	tests := []struct {
		name       string
		fields     []apicli.FieldSpec
		exclusions map[string]string
		want       []string
	}{
		{
			name:   "complete descriptor",
			fields: []apicli.FieldSpec{{Name: "A"}, {Name: "B"}},
		},
		{
			name:   "missing request member",
			fields: []apicli.FieldSpec{{Name: "A"}},
			want:   []string{`API request member "B" is missing from descriptor fields and has no explicit exclusion`},
		},
		{
			name:       "reasoned exclusion",
			fields:     []apicli.FieldSpec{{Name: "A"}},
			exclusions: map[string]string{"B": "not exposed by this wrapper"},
		},
		{
			name:       "invalid exclusions",
			fields:     []apicli.FieldSpec{{Name: "A"}, {Name: "B"}},
			exclusions: map[string]string{"A": "legacy wrapper behavior", "B": "  ", "C": "legacy field"},
			want: []string{
				`API request member "A" is both covered by the descriptor and explicitly excluded`,
				`exclusion for API request member "B" must include a non-empty reason`,
				`API request member "B" is both covered by the descriptor and explicitly excluded`,
				`exclusion field "C" is not present in the API request`,
			},
		},
		{
			name:       "invalid descriptor fields",
			fields:     []apicli.FieldSpec{{Name: "A"}, {Name: "A"}, {Name: "C"}},
			exclusions: map[string]string{"B": "not exposed by this wrapper"},
			want: []string{
				`descriptor field "A" appears more than once`,
				`descriptor field "C" is not present in the API request`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := apiFieldCoverageIssues(tt.fields, members, tt.exclusions)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("issues = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestAPIFieldExclusionCommandIssues(t *testing.T) {
	got := apiFieldExclusionCommandIssues(
		map[string]bool{"known": true},
		map[string]map[string]string{"known": {}, "unknown": {"Field": "reason"}},
	)
	want := []string{`API field exclusions reference unknown API-backed command "unknown"`}
	if !slices.Equal(got, want) {
		t.Fatalf("issues = %#v, want %#v", got, want)
	}
}

func apiFieldCoverageIssues(fields []apicli.FieldSpec, members []apimeta.CatalogMember, exclusions map[string]string) []string {
	membersByName := make(map[string]bool, len(members))
	for _, member := range members {
		membersByName[member.Name] = true
	}

	var issues []string
	fieldsByName := make(map[string]bool, len(fields))
	for _, field := range fields {
		if fieldsByName[field.Name] {
			issues = append(issues, `descriptor field "`+field.Name+`" appears more than once`)
		}
		fieldsByName[field.Name] = true
		if !membersByName[field.Name] {
			issues = append(issues, `descriptor field "`+field.Name+`" is not present in the API request`)
		}
	}

	for _, member := range members {
		if fieldsByName[member.Name] {
			continue
		}
		if _, excluded := exclusions[member.Name]; !excluded {
			issues = append(issues, `API request member "`+member.Name+`" is missing from descriptor fields and has no explicit exclusion`)
		}
	}

	for _, fieldName := range slices.Sorted(maps.Keys(exclusions)) {
		if strings.TrimSpace(exclusions[fieldName]) == "" {
			issues = append(issues, `exclusion for API request member "`+fieldName+`" must include a non-empty reason`)
		}
		if !membersByName[fieldName] {
			issues = append(issues, `exclusion field "`+fieldName+`" is not present in the API request`)
		}
		if fieldsByName[fieldName] {
			issues = append(issues, `API request member "`+fieldName+`" is both covered by the descriptor and explicitly excluded`)
		}
	}
	return issues
}

func apiFieldExclusionCommandIssues(seenAPICommands map[string]bool, exclusions map[string]map[string]string) []string {
	var issues []string
	for _, commandID := range slices.Sorted(maps.Keys(exclusions)) {
		if !seenAPICommands[commandID] {
			issues = append(issues, `API field exclusions reference unknown API-backed command "`+commandID+`"`)
		}
	}
	return issues
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
	seenAPICommands := map[string]bool{}
	for _, descriptor := range descriptors {
		if _, ok := knownSources[descriptor.Source]; !ok {
			t.Errorf("descriptor %q: unknown Source %q", descriptor.Spec.ID, descriptor.Source)
		}
		api, hasAPI := apiDescriptorFrom(t, descriptor)
		if hasAPI {
			apiDescriptorCount++
			seenAPICommands[descriptor.Spec.ID] = true
			if descriptor.Source != command.SourceAPICli {
				wrapperAPICommandCount++
			}
			expectedAction, declaredWorkflowAPI := workflowAPIContracts[descriptor.Spec.ID]
			if descriptor.Source == command.SourceWorkflow && !declaredWorkflowAPI {
				t.Errorf("API-backed workflow %q has no explicit workflow API contract", descriptor.Spec.ID)
			}
			if declaredWorkflowAPI {
				if descriptor.Source != command.SourceWorkflow {
					t.Errorf("workflow API contract %q belongs to source %q, want %q", descriptor.Spec.ID, descriptor.Source, command.SourceWorkflow)
				}
				if api.API.Action != expectedAction {
					t.Errorf("API-backed workflow %q: action = %q, want %q", descriptor.Spec.ID, api.API.Action, expectedAction)
				}
			}
			checkAPIFieldCoverageInvariants(t, descriptor, api, apiCatalog)
		} else if descriptor.Source == command.SourceAPICli || descriptor.Source == command.SourceMixedAPI {
			t.Errorf("descriptor %q with source %q is missing APIDescriptor metadata", descriptor.Spec.ID, descriptor.Source)
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
		checkedFlags, jsonFlags := checkDescriptorFlagInvariants(t, descriptor, schema, api, hasAPI)
		descriptorFlagCount += checkedFlags
		jsonFlagCount += jsonFlags

		if !hasAPI {
			continue
		}
		requestPropertyCount += checkAPISchemaInvariants(t, descriptor, api, schema, apiCatalog)
	}
	for _, issue := range apiFieldExclusionCommandIssues(seenAPICommands, apiDescriptorFieldExclusions) {
		t.Error(issue)
	}
	for _, commandID := range slices.Sorted(maps.Keys(workflowAPIContracts)) {
		if !seenAPICommands[commandID] {
			t.Errorf("workflow API contract %q is not backed by an APIDescriptor", commandID)
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

func apiDescriptorFrom(t *testing.T, descriptor command.Descriptor) (apicli.APIDescriptor, bool) {
	t.Helper()

	switch api := descriptor.API.(type) {
	case apicli.APIDescriptor:
		return api, true
	case *apicli.APIDescriptor:
		if api == nil {
			t.Errorf("descriptor %q: API metadata is a nil *apicli.APIDescriptor", descriptor.Spec.ID)
			return apicli.APIDescriptor{}, false
		}
		return *api, true
	default:
		if descriptor.API != nil {
			t.Errorf("descriptor %q: unsupported API metadata type %T", descriptor.Spec.ID, descriptor.API)
		}
		return apicli.APIDescriptor{}, false
	}
}

func checkDescriptorFlagInvariants(t *testing.T, descriptor command.Descriptor, schema schemaCommandSnapshot, api apicli.APIDescriptor, hasAPI bool) (int, int) {
	t.Helper()

	flagsByName := make(map[string]schemaFlagSnapshot, len(schema.Flags))
	for _, flag := range schema.Flags {
		if _, exists := flagsByName[flag.Name]; exists {
			t.Errorf("schema %q: duplicate flag %q", descriptor.Spec.ID, flag.Name)
		}
		flagsByName[flag.Name] = flag
	}
	jsonFlags := map[string]bool{}
	if hasAPI {
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

func checkAPIFieldCoverageInvariants(t *testing.T, descriptor command.Descriptor, api apicli.APIDescriptor, catalog *apimeta.Catalog) {
	t.Helper()

	action, ok := catalog.Action(api.API.Action)
	if !ok {
		t.Errorf("descriptor %q: API action %q not found in metadata", descriptor.Spec.ID, api.API.Action)
		return
	}
	if api.API.RequestType != action.Request {
		t.Errorf("descriptor %q: RequestType = %q, want %q from API action %q", descriptor.Spec.ID, api.API.RequestType, action.Request, api.API.Action)
	}
	if api.API.ResponseType != action.Response {
		t.Errorf("descriptor %q: ResponseType = %q, want %q from API action %q", descriptor.Spec.ID, api.API.ResponseType, action.Response, api.API.Action)
	}

	requestObject, ok := catalog.Object(action.Request)
	if !ok {
		t.Errorf("descriptor %q: API request type %q not found in metadata", descriptor.Spec.ID, action.Request)
		return
	}
	for _, issue := range apiFieldCoverageIssues(api.Fields, requestObject.Members, apiDescriptorFieldExclusions[descriptor.Spec.ID]) {
		t.Errorf("descriptor %q action %q: %s", descriptor.Spec.ID, api.API.Action, issue)
	}
}

func checkAPISchemaInvariants(t *testing.T, descriptor command.Descriptor, api apicli.APIDescriptor, schema schemaCommandSnapshot, catalog *apimeta.Catalog) int {
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
			// API field coverage already reports the contract error.
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

		hasFlagInput := false
		for _, input := range field.Inputs {
			if input.Positional || input.Flag == "" {
				continue
			}
			hasFlagInput = true
			if property.CliFlag == nil || *property.CliFlag != input.Flag {
				t.Errorf("schema %q request property %q: CliFlag = %v, want %q", descriptor.Spec.ID, field.Name, property.CliFlag, input.Flag)
			}
		}
		if !hasFlagInput && property.CliFlag != nil {
			t.Errorf("schema %q request property %q: CliFlag = %q, want nil", descriptor.Spec.ID, field.Name, *property.CliFlag)
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
