package apicli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

func TestRequestBuilderBuildsFlagRequest(t *testing.T) {
	builder := NewRequestBuilder(APIDescriptor{
		Spec: command.Spec{ID: "tool.list"},
		API:  APISpec{Action: "DescribeSandboxToolList"},
		Fields: []FieldSpec{
			{
				Name:   "Limit",
				Parser: "common.default_int",
				Inputs: []InputSpec{
					{Name: "limit", Flag: "limit", Type: command.FlagInt},
				},
			},
			{
				Name:   "Status",
				Parser: "common.default_string",
				Inputs: []InputSpec{
					{Name: "status", Flag: "status", Type: command.FlagString},
				},
			},
		},
	})
	req, err := builder.Build(command.Request{Flags: map[string]command.FlagValue{
		"limit":  {Name: "limit", Type: command.FlagInt, Int: 7, Changed: true},
		"status": {Name: "status", Type: command.FlagString, String: "ACTIVE", Changed: true},
	}})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if req["Limit"] != 7 {
		t.Fatalf("Limit = %#v, want 7", req["Limit"])
	}
	if req["Status"] != "ACTIVE" {
		t.Fatalf("Status = %#v, want ACTIVE", req["Status"])
	}
}

func TestRequestBuilderBuildsStringArrayWithoutControlPlane(t *testing.T) {
	builder := NewRequestBuilder(APIDescriptor{
		Spec: command.Spec{ID: "tool.list"},
		API:  APISpec{Action: "DescribeSandboxToolList"},
		Fields: []FieldSpec{
			{
				Name:   "ToolIds",
				Parser: "common.default_string_array",
				Inputs: []InputSpec{
					{Name: "tool-ids", Flag: "tool-ids", Type: command.FlagStringArray},
				},
			},
		},
	})
	req, err := builder.Build(command.Request{Flags: map[string]command.FlagValue{
		"tool-ids": {Name: "tool-ids", Type: command.FlagStringArray, Strings: []string{"sdt-a", "sdt-b"}, Changed: true},
	}})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	values, ok := req["ToolIds"].([]string)
	if !ok {
		t.Fatalf("ToolIds = %#v, want []string", req["ToolIds"])
	}
	if strings.Join(values, ",") != "sdt-a,sdt-b" {
		t.Fatalf("ToolIds = %#v", values)
	}
}

func TestRequestBuilderSendsDefaultWhenRequested(t *testing.T) {
	builder := NewRequestBuilder(APIDescriptor{
		Spec: command.Spec{ID: "tool.list"},
		API:  APISpec{Action: "DescribeSandboxToolList"},
		Fields: []FieldSpec{
			{
				Name:   "Limit",
				Parser: "common.default_int",
				Inputs: []InputSpec{
					{Name: "limit", Flag: "limit", Type: command.FlagInt, Default: 20, SendDefault: true},
				},
			},
		},
	})
	req, err := builder.Build(command.Request{Flags: map[string]command.FlagValue{
		"limit": {Name: "limit", Type: command.FlagInt, Int: 20},
	}})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if req["Limit"] != 20 {
		t.Fatalf("Limit = %#v, want 20", req["Limit"])
	}
}

func TestRequestBuilderReadsJSONFlagFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filters.json")
	if err := os.WriteFile(path, []byte(`[{"Name":"Status","Values":["RUNNING"]}]`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	builder := NewRequestBuilder(APIDescriptor{
		Spec: command.Spec{ID: "instance.list"},
		API:  APISpec{Action: "DescribeSandboxInstanceList"},
		Fields: []FieldSpec{
			{
				Name:   "Filters",
				Parser: "common.default_json",
				Inputs: []InputSpec{
					{Name: "filters", Flag: "filters", Type: command.FlagString},
				},
			},
		},
	})
	req, err := builder.Build(command.Request{Flags: map[string]command.FlagValue{
		"filters": {Name: "filters", Type: command.FlagString, String: "@" + path, Changed: true},
	}})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	filters, ok := req["Filters"].([]any)
	if !ok || len(filters) != 1 {
		t.Fatalf("Filters = %#v", req["Filters"])
	}
	filter, ok := filters[0].(map[string]any)
	if !ok || filter["Name"] != "Status" {
		t.Fatalf("Filters = %#v", req["Filters"])
	}
}

func TestRequestBuilderRejectsRequestFlagConflict(t *testing.T) {
	builder := NewRequestBuilder(APIDescriptor{
		Spec: command.Spec{ID: "tool.list"},
		API:  APISpec{Action: "DescribeSandboxToolList"},
	})
	_, err := builder.Build(command.Request{Flags: map[string]command.FlagValue{
		"request": {Name: "request", Type: command.FlagString, String: `{"Limit":1}`, Changed: true},
		"limit":   {Name: "limit", Type: command.FlagInt, Int: 7, Changed: true},
	}})
	if err == nil || !strings.Contains(err.Error(), "--request cannot be combined") {
		t.Fatalf("expected request conflict error, got %v", err)
	}
	if cliErr, ok := err.(*output.CLIError); !ok || cliErr.Failure.Code != "REQUEST_FLAG_CONFLICT" {
		t.Fatalf("error = %#v, want REQUEST_FLAG_CONFLICT usage error", err)
	}
}

func TestRequestBuilderMergesPositionalIntoRequestFlag(t *testing.T) {
	builder := NewRequestBuilder(APIDescriptor{
		Spec: command.Spec{ID: "apikey.delete"},
		API:  APISpec{Action: "DeleteAPIKey"},
		Fields: []FieldSpec{
			{
				Name: "KeyId",
				Inputs: []InputSpec{
					{Name: "key-id", Positional: true},
				},
			},
		},
	})
	req, err := builder.Build(command.Request{
		ArgValues: map[string]string{"key-id": "ak-unit"},
		Flags: map[string]command.FlagValue{
			"request": {Name: "request", Type: command.FlagString, String: `{}`, Changed: true},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if req["KeyId"] != "ak-unit" {
		t.Fatalf("KeyId = %#v, want ak-unit", req["KeyId"])
	}
}

func TestRequestBuilderRejectsPositionalRequestMismatch(t *testing.T) {
	builder := NewRequestBuilder(APIDescriptor{
		Spec: command.Spec{ID: "apikey.delete"},
		API:  APISpec{Action: "DeleteAPIKey"},
		Fields: []FieldSpec{
			{
				Name: "KeyId",
				Inputs: []InputSpec{
					{Name: "key-id", Positional: true},
				},
			},
		},
	})
	_, err := builder.Build(command.Request{
		ArgValues: map[string]string{"key-id": "ak-unit"},
		Flags: map[string]command.FlagValue{
			"request": {Name: "request", Type: command.FlagString, String: `{"KeyId":"ak-other"}`, Changed: true},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "does not match positional") {
		t.Fatalf("expected positional mismatch error, got %v", err)
	}
}

func TestRequestBuilderRejectsMissingRequiredFlagField(t *testing.T) {
	builder := NewRequestBuilder(APIDescriptor{
		Spec: command.Spec{ID: "pre-cache-image-task.create"},
		API:  APISpec{Action: "CreatePreCacheImageTask"},
		Fields: []FieldSpec{
			{
				Name:     "Image",
				Required: true,
				Parser:   "common.default_string",
				Inputs: []InputSpec{
					{Name: "image", Flag: "image", Type: command.FlagString},
				},
			},
		},
	})
	_, err := builder.Build(command.Request{Flags: map[string]command.FlagValue{}})
	if err == nil || !strings.Contains(err.Error(), "--image is required") {
		t.Fatalf("expected missing required flag error, got %v", err)
	}
	if cliErr, ok := err.(*output.CLIError); !ok || cliErr.Failure.Code != "MISSING_REQUIRED_FLAG" {
		t.Fatalf("error = %#v, want MISSING_REQUIRED_FLAG usage error", err)
	}
}

func TestRequestBuilderRejectsMissingRequiredFieldInRawRequest(t *testing.T) {
	builder := NewRequestBuilder(APIDescriptor{
		Spec: command.Spec{ID: "pre-cache-image-task.create"},
		API:  APISpec{Action: "CreatePreCacheImageTask"},
		Fields: []FieldSpec{
			{
				Name:     "Image",
				Required: true,
				Parser:   "common.default_string",
				Inputs: []InputSpec{
					{Name: "image", Flag: "image", Type: command.FlagString},
				},
			},
		},
	})
	_, err := builder.Build(command.Request{Flags: map[string]command.FlagValue{
		"request": {Name: "request", Type: command.FlagString, String: `{}`, Changed: true},
	}})
	if err == nil || !strings.Contains(err.Error(), "--image is required") {
		t.Fatalf("expected missing required flag error, got %v", err)
	}
	if cliErr, ok := err.(*output.CLIError); !ok || cliErr.Failure.Code != "MISSING_REQUIRED_FLAG" {
		t.Fatalf("error = %#v, want MISSING_REQUIRED_FLAG usage error", err)
	}
}

func TestRequestBuilderRejectsEmptyRequiredStringInRawRequest(t *testing.T) {
	builder := NewRequestBuilder(APIDescriptor{
		Spec: command.Spec{ID: "pre-cache-image-task.create"},
		API:  APISpec{Action: "CreatePreCacheImageTask"},
		Fields: []FieldSpec{
			{
				Name:     "Image",
				Required: true,
				Parser:   "common.default_string",
				Inputs: []InputSpec{
					{Name: "image", Flag: "image", Type: command.FlagString},
				},
			},
		},
	})
	for _, raw := range []string{`{"Image":""}`, `{"Image":"   "}`, `{"Image":null}`} {
		_, err := builder.Build(command.Request{Flags: map[string]command.FlagValue{
			"request": {Name: "request", Type: command.FlagString, String: raw, Changed: true},
		}})
		if err == nil || !strings.Contains(err.Error(), "--image is required") {
			t.Fatalf("raw=%s expected missing required flag error, got %v", raw, err)
		}
		if cliErr, ok := err.(*output.CLIError); !ok || cliErr.Failure.Code != "MISSING_REQUIRED_FLAG" {
			t.Fatalf("raw=%s error = %#v, want MISSING_REQUIRED_FLAG usage error", raw, err)
		}
	}
}

func TestRequestBuilderRejectsMissingRequiredPositionalField(t *testing.T) {
	builder := NewRequestBuilder(APIDescriptor{
		Spec: command.Spec{ID: "pre-cache-image-task.get"},
		API:  APISpec{Action: "DescribePreCacheImageTask"},
		Fields: []FieldSpec{
			{
				Name:     "ImageDigest",
				Required: true,
				Inputs: []InputSpec{
					{Name: "image-digest", Positional: true},
				},
			},
		},
	})
	_, err := builder.Build(command.Request{Flags: map[string]command.FlagValue{}})
	if err == nil || !strings.Contains(err.Error(), "missing required argument <image-digest>") {
		t.Fatalf("expected missing required arg error, got %v", err)
	}
	if cliErr, ok := err.(*output.CLIError); !ok || cliErr.Failure.Code != "MISSING_REQUIRED_ARG" {
		t.Fatalf("error = %#v, want MISSING_REQUIRED_ARG usage error", err)
	}
}

func TestRequestBuilderRejectsInvalidRequestJSON(t *testing.T) {
	builder := NewRequestBuilder(APIDescriptor{
		Spec: command.Spec{ID: "tool.create"},
		API:  APISpec{Action: "CreateSandboxTool"},
	})
	_, err := builder.Build(command.Request{Flags: map[string]command.FlagValue{
		"request": {Name: "request", Type: command.FlagString, String: `[]`, Changed: true},
	}})
	if err == nil || !strings.Contains(err.Error(), "--request must be a JSON object") {
		t.Fatalf("expected invalid request JSON error, got %v", err)
	}
	if cliErr, ok := err.(*output.CLIError); !ok || cliErr.Failure.Code != "INVALID_REQUEST_JSON" {
		t.Fatalf("error = %#v, want INVALID_REQUEST_JSON usage error", err)
	}
}

func TestRequestBuilderRejectsMissingRequestFile(t *testing.T) {
	builder := NewRequestBuilder(APIDescriptor{
		Spec: command.Spec{ID: "tool.list"},
		API:  APISpec{Action: "DescribeSandboxToolList"},
	})
	_, err := builder.Build(command.Request{Flags: map[string]command.FlagValue{
		"request": {Name: "request", Type: command.FlagString, String: `@/no/such/file`, Changed: true},
	}})
	if err == nil || !strings.Contains(err.Error(), "failed to read request file") {
		t.Fatalf("expected invalid request input error, got %v", err)
	}
	if cliErr, ok := err.(*output.CLIError); !ok || cliErr.Failure.Code != "INVALID_REQUEST_INPUT" {
		t.Fatalf("error = %#v, want INVALID_REQUEST_INPUT usage error", err)
	}
}

func TestRequestBuilderRejectsInvalidJSONFlag(t *testing.T) {
	builder := NewRequestBuilder(APIDescriptor{
		Spec: command.Spec{ID: "tool.create"},
		API:  APISpec{Action: "CreateSandboxTool"},
		Fields: []FieldSpec{
			{
				Name:   "NetworkConfiguration",
				Parser: "common.default_json",
				Inputs: []InputSpec{
					{Name: "network-configuration", Flag: "network-configuration", Type: command.FlagString},
				},
			},
		},
	})
	_, err := builder.Build(command.Request{Flags: map[string]command.FlagValue{
		"network-configuration": {Name: "network-configuration", Type: command.FlagString, String: `bad`, Changed: true},
	}})
	if err == nil || !strings.Contains(err.Error(), "invalid JSON for --network-configuration") {
		t.Fatalf("expected invalid JSON flag error, got %v", err)
	}
	if cliErr, ok := err.(*output.CLIError); !ok || cliErr.Failure.Code != "INVALID_JSON_FLAG" {
		t.Fatalf("error = %#v, want INVALID_JSON_FLAG usage error", err)
	}
}

func TestRequiredStringAllowEmptyIsExplicit(t *testing.T) {
	for _, allow := range []bool{false, true} {
		for _, raw := range []bool{false, true} {
			builder := NewRequestBuilder(APIDescriptor{Fields: []FieldSpec{{Name: "Description", Required: true, AllowEmpty: allow, Parser: "common.default_string", Inputs: []InputSpec{{Name: "description", Flag: "description", Type: command.FlagString}}}}})
			flags := map[string]command.FlagValue{"description": {Type: command.FlagString, Changed: true, String: ""}}
			if raw {
				flags = map[string]command.FlagValue{"request": {Type: command.FlagString, Changed: true, String: `{"Description":""}`}}
			}
			got, err := builder.Build(command.Request{Flags: flags})
			if allow {
				if err != nil || got["Description"] != "" {
					t.Fatalf("raw=%v got=%v err=%v", raw, got, err)
				}
			} else if err == nil {
				t.Fatal("required nonempty field accepted empty value")
			}
			if _, err := builder.Build(command.Request{}); err == nil {
				t.Fatal("allow-empty must not allow an absent field")
			}
		}
	}
}

func TestRequestBuilderPreservesStringPresence(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		changed, sendDefault bool
		value                string
		present              bool
	}{
		{"omitted", false, false, "", false},
		{"explicit-empty", true, false, "", true},
		{"explicit-value", true, false, "value", true},
		{"unsent-default", false, false, "default", false},
		{"empty-default", false, true, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			builder := NewRequestBuilder(APIDescriptor{
				Spec:   command.Spec{ID: "test.update"},
				Fields: []FieldSpec{{Name: "Value", Parser: "common.default_string", Inputs: []InputSpec{{Name: "value", Flag: "value", Type: command.FlagString, SendDefault: tc.sendDefault}}}},
			})
			request, err := builder.Build(command.Request{Flags: map[string]command.FlagValue{"value": {Type: command.FlagString, String: tc.value, Changed: tc.changed}}})
			if err != nil {
				t.Fatal(err)
			}
			value, present := request["Value"]
			if present != tc.present || (present && value != tc.value) {
				t.Fatalf("Value = %#v, present=%v; want %q, present=%v", value, present, tc.value, tc.present)
			}
		})
	}
}
