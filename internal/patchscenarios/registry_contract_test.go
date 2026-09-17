package patchscenarios

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

func TestRegistryResponseContract(t *testing.T) {
	var schema registryWireSchema
	if err := json.Unmarshal([]byte(`{"objects":{"Reply":{"members":[{"name":"Items","type":"list","member":"Item","output_required":true}]},"Item":{"members":[{"name":"Count","type":"int64","member":"int64","output_required":true},{"name":"Note","type":"string","member":"string","value_allowed_null":true}]}}}`), &schema); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"valid", `{"Items":[{"Count":9007199254740993,"Note":null,"Future":true}]}`, true},
		{"empty", `{"Items":[]}`, true},
		{"missing", `{}`, false},
		{"nested missing", `{"Items":[{}]}`, false},
		{"fractional integer", `{"Items":[{"Count":1.5}]}`, false},
		{"null array", `{"Items":null}`, false},
		{"wrong element", `{"Items":[false]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var data map[string]any
			dec := json.NewDecoder(strings.NewReader(tc.raw))
			dec.UseNumber()
			if err := dec.Decode(&data); err != nil {
				t.Fatal(err)
			}
			if err := validateRegistryObject(schema, "Reply", data); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestRegistryTagsReadback(t *testing.T) {
	want := registryTag{Key: "cli-regression", Value: "disposable"}
	for _, tc := range []struct {
		name, registry string
		valid          bool
	}{
		{"persisted", `{"Tags":[{"Key":"cli-regression","Value":"disposable"}]}`, true},
		{"missing", `{}`, false},
		{"empty", `{"Tags":[]}`, false},
		{"null", `{"Tags":null}`, false},
		{"wrong key", `{"Tags":[{"Key":"other","Value":"disposable"}]}`, false},
		{"wrong value", `{"Tags":[{"Key":"cli-regression","Value":"other"}]}`, false},
		{"unrelated tags", `{"Tags":[{"Key":"other","Value":"extra"},{"Key":"cli-regression","Value":"disposable"}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var response registryResponse
			if err := json.Unmarshal([]byte(`{"Data":{"Registry":`+tc.registry+`}}`), &response); err != nil {
				t.Fatal(err)
			}
			if got := response.hasRegistryTag(want); got != tc.valid {
				t.Fatalf("Tags readback accepted=%v, want %v", got, tc.valid)
			}
		})
	}
}

// Derive the response graph from the effective Registry actions, not a second
// list of response fields. Every new member must classify presence and nullability.
func TestRegistryEffectiveResponseContract(t *testing.T) {
	contract, err := apimeta.LoadContract("../../api/ags/v20250920", apimeta.Preview)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := apimeta.LoadEffectiveJSON("../../api/ags/v20250920")
	if err != nil {
		t.Fatal(err)
	}
	var schema registryWireSchema
	var declarations struct {
		Objects map[string]struct{ Members []map[string]json.RawMessage }
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &declarations); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var check func(string)
	check = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		object, ok := schema.Objects[name]
		if !ok {
			t.Fatalf("missing object %s", name)
		}
		t.Run(name, func(t *testing.T) {
			for _, m := range declarations.Objects[name].Members {
				for _, attribute := range []string{"output_required", "value_allowed_null"} {
					if _, ok := m[attribute]; !ok {
						t.Errorf("%s.%s missing explicit %s", name, m["name"], attribute)
					}
				}
			}
			data := registryResponseSample(schema, name)
			if err := validateRegistryObject(schema, name, data); err != nil {
				t.Fatal(err)
			}
			for _, member := range object.Members {
				t.Run(member.Name, func(t *testing.T) {
					value := data[member.Name]
					delete(data, member.Name)
					err := validateRegistryObject(schema, name, data)
					if (err != nil) != member.OutputRequired {
						t.Errorf("missing field: required=%v err=%v", member.OutputRequired, err)
					}
					data[member.Name] = nil
					err = validateRegistryObject(schema, name, data)
					if (err == nil) != member.ValueAllowedNull {
						t.Errorf("null field: nullable=%v err=%v", member.ValueAllowedNull, err)
					}
					data[member.Name] = value
				})
			}
		})
		for _, member := range object.Members {
			if _, ok := schema.Objects[member.Member]; ok {
				check(member.Member)
			}
		}
	}
	for _, mapping := range contract.Mapping.Actions {
		if strings.HasPrefix(mapping.Command, "registry.") {
			check(mapping.Response)
		}
	}
	// Regression anchor: the wire DTO always emits this non-null number.
	version := registryResponseSample(schema, "CloudRecordVersion")
	for _, null := range []bool{false, true} {
		if null {
			version["Revision"] = nil
		} else {
			delete(version, "Revision")
		}
		if err := validateRegistryObject(schema, "CloudRecordVersion", version); err == nil {
			t.Fatal("missing/null Revision accepted")
		}
	}
}

func registryResponseSample(schema registryWireSchema, name string) map[string]any {
	data := map[string]any{}
	for _, member := range schema.Objects[name].Members {
		var value any
		switch member.Member {
		case "string":
			value = "fixture"
		case "int", "int64", "uint64":
			value = json.Number("1")
		case "bool", "boolean":
			value = true
		default:
			value = registryResponseSample(schema, member.Member)
		}
		if member.Type == "list" {
			value = []any{value}
		}
		data[member.Name] = value
	}
	return data
}

func TestRegistryFixtureWindow(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", time.Minute}, {"30s", 30 * time.Second}, {"2m", 2 * time.Minute}, {"3m", 3 * time.Minute},
		{"0s", 0}, {"-1s", 0}, {"20s", 0}, {"3m1ns", 0}, {"5m", 0}, {"6m", 0}, {"invalid", 0},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("AGR_REGISTRY_FIXTURE_WINDOW", tc.value)
			got, err := registryFixtureWindow()
			if tc.want == 0 {
				if err == nil {
					t.Fatal("invalid window accepted")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("window=%v err=%v want=%v", got, err, tc.want)
			}
		})
	}
}
