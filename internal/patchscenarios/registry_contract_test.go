package patchscenarios

import (
	"encoding/json"
	"strings"
	"testing"
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
