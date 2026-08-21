package apimeta_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

const effectiveTestBase = `{
  "version": "1.0",
  "metadata": {"preserved": true},
  "actions": {
    "ExistingAction": {
      "name": "Existing action",
      "input": "ExistingRequest",
      "output": "ExistingResponse",
      "status": "online"
    }
  },
  "objects": {
    "ExistingRequest": {
      "type": "object",
      "members": [
        {"name": "Id", "type": "string", "member": "string", "required": true, "value_allowed_null": false}
      ]
    },
    "ExistingResponse": {"type": "object", "members": []}
  }
}`

func TestApplyAPIPatch_AddsCompleteActionBeforeTypedParsing(t *testing.T) {
	patch := `[
      {"op":"add","path":"/actions/NewAction","value":{"name":"New action","input":"NewRequest","output":"NewResponse","status":"online"}},
      {"op":"add","path":"/objects/NewRequest","value":{"type":"object","members":[]}},
      {"op":"add","path":"/objects/NewResponse","value":{"type":"object","members":[]}}
    ]`

	effective, err := apimeta.ApplyAPIPatch([]byte(effectiveTestBase), []byte(patch))
	if err != nil {
		t.Fatalf("ApplyAPIPatch returned error: %v", err)
	}
	spec, err := apimeta.ParseSpec(effective)
	if err != nil {
		t.Fatalf("ParseSpec returned error: %v", err)
	}
	if _, ok := spec.Actions["NewAction"]; !ok {
		t.Fatal("effective spec is missing NewAction")
	}
	var raw map[string]any
	if err := json.Unmarshal(effective, &raw); err != nil {
		t.Fatalf("unmarshal effective JSON: %v", err)
	}
	metadata, _ := raw["metadata"].(map[string]any)
	if metadata["preserved"] != true {
		t.Fatalf("raw metadata was not preserved: %#v", metadata)
	}
	objects := raw["objects"].(map[string]any)
	existing := objects["ExistingRequest"].(map[string]any)
	members := existing["members"].([]any)
	id := members[0].(map[string]any)
	if id["value_allowed_null"] != false {
		t.Fatalf("raw member fields were not preserved: %#v", id)
	}
}

func TestApplyAPIPatch_AppendsUniqueMember(t *testing.T) {
	patch := `[
      {"op":"add","path":"/objects/ExistingRequest/members/-","value":{"name":"Optional","type":"string","member":"string","required":false}}
    ]`
	effective, err := apimeta.ApplyAPIPatch([]byte(effectiveTestBase), []byte(patch))
	if err != nil {
		t.Fatalf("ApplyAPIPatch returned error: %v", err)
	}
	spec, err := apimeta.ParseSpec(effective)
	if err != nil {
		t.Fatalf("ParseSpec returned error: %v", err)
	}
	if got := len(spec.Object("ExistingRequest").Members); got != 2 {
		t.Fatalf("members=%d, want 2", got)
	}
}

func TestApplyAPIPatch_RejectsUnsafeOrObsoleteAdd(t *testing.T) {
	tests := []struct {
		name  string
		patch string
		want  string
	}{
		{
			name:  "conflict",
			patch: `[{"op":"add","path":"/actions/ExistingAction/status","value":"preview"}]`,
			want:  "conflicts with upstream",
		},
		{
			name:  "obsolete",
			patch: `[{"op":"add","path":"/actions/ExistingAction/status","value":"online"}]`,
			want:  "is obsolete",
		},
		{
			name:  "duplicate member",
			patch: `[{"op":"add","path":"/objects/ExistingRequest/members/-","value":{"name":"Id","type":"int","member":"int64","required":true}}]`,
			want:  "member Id already exists with a different value",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := apimeta.ApplyAPIPatch([]byte(effectiveTestBase), []byte(tt.patch))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestApplyAPIPatch_RequiresGuardedReplaceAndRemove(t *testing.T) {
	unguarded := []string{
		`[{"op":"replace","path":"/actions/ExistingAction/status","value":"preview"}]`,
		`[{"op":"remove","path":"/actions/ExistingAction/status"}]`,
	}
	for _, patch := range unguarded {
		if _, err := apimeta.ApplyAPIPatch([]byte(effectiveTestBase), []byte(patch)); err == nil || !strings.Contains(err.Error(), "must be preceded by test") {
			t.Fatalf("unguarded patch error=%v", err)
		}
	}

	guarded := `[
      {"op":"test","path":"/actions/ExistingAction/status","value":"online"},
      {"op":"replace","path":"/actions/ExistingAction/status","value":"preview"}
    ]`
	effective, err := apimeta.ApplyAPIPatch([]byte(effectiveTestBase), []byte(guarded))
	if err != nil {
		t.Fatalf("guarded replace returned error: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(effective, &raw); err != nil {
		t.Fatalf("unmarshal effective JSON: %v", err)
	}
	action := raw["actions"].(map[string]any)["ExistingAction"].(map[string]any)
	if action["status"] != "preview" {
		t.Fatalf("status=%v, want preview", action["status"])
	}

	guardedRemove := `[
      {"op":"test","path":"/metadata/preserved","value":true},
      {"op":"remove","path":"/metadata/preserved"}
    ]`
	if _, err := apimeta.ApplyAPIPatch([]byte(effectiveTestBase), []byte(guardedRemove)); err != nil {
		t.Fatalf("guarded remove returned error: %v", err)
	}
}

func TestApplyAPIPatch_RejectsNumericArrayAddAndMissingReferences(t *testing.T) {
	numeric := `[{"op":"add","path":"/objects/ExistingRequest/members/0","value":{"name":"Other","type":"string","member":"string"}}]`
	if _, err := apimeta.ApplyAPIPatch([]byte(effectiveTestBase), []byte(numeric)); err == nil || !strings.Contains(err.Error(), "numeric array index") {
		t.Fatalf("numeric add error=%v", err)
	}

	missing := `[{"op":"add","path":"/actions/Broken","value":{"name":"Broken","input":"MissingRequest","output":"ExistingResponse","status":"online"}}]`
	if _, err := apimeta.ApplyAPIPatch([]byte(effectiveTestBase), []byte(missing)); err == nil || !strings.Contains(err.Error(), "missing input object") {
		t.Fatalf("missing reference error=%v", err)
	}
}

func TestApplyAPIPatch_RestrictsArrayAppendToObjectMembers(t *testing.T) {
	patch := `[{"op":"add","path":"/metadata/members/-","value":{"name":"Other"}}]`
	if _, err := apimeta.ApplyAPIPatch([]byte(effectiveTestBase), []byte(patch)); err == nil || !strings.Contains(err.Error(), "unsupported array") {
		t.Fatalf("unsupported append error=%v", err)
	}
}

func TestApplyAPIPatch_AllowsPrimitiveLists(t *testing.T) {
	patch := `[{"op":"add","path":"/objects/ExistingRequest/members/-","value":{"name":"Values","type":"list","member":"double","required":false}}]`
	if _, err := apimeta.ApplyAPIPatch([]byte(effectiveTestBase), []byte(patch)); err != nil {
		t.Fatalf("primitive list patch returned error: %v", err)
	}
}

func TestLoadEffectiveSpec_UsesTwoFileModel(t *testing.T) {
	dir := t.TempDir()
	writeEffectiveTestFile(t, filepath.Join(dir, "api.json"), effectiveTestBase)
	writeEffectiveTestFile(t, filepath.Join(dir, "api.patch.json"), `[
      {"op":"add","path":"/actions/NewAction","value":{"name":"New action","input":"NewRequest","output":"NewResponse","status":"online"}},
      {"op":"add","path":"/objects/NewRequest","value":{"type":"object","members":[]}},
      {"op":"add","path":"/objects/NewResponse","value":{"type":"object","members":[]}}
    ]`)
	raw, err := apimeta.LoadSpec(filepath.Join(dir, "api.json"))
	if err != nil {
		t.Fatalf("LoadSpec returned error: %v", err)
	}
	if len(raw.Actions) != 1 {
		t.Fatalf("raw actions=%d, want 1", len(raw.Actions))
	}
	spec, err := apimeta.LoadEffectiveSpec(dir)
	if err != nil {
		t.Fatalf("LoadEffectiveSpec returned error: %v", err)
	}
	if len(spec.Actions) != 2 {
		t.Fatalf("effective actions=%d, want 2", len(spec.Actions))
	}
}

func TestEvaluateAPIPatch_Statuses(t *testing.T) {
	tests := []struct {
		name     string
		upstream string
		patch    string
		want     apimeta.PatchStatus
	}{
		{
			name:     "active",
			upstream: effectiveTestBase,
			patch:    `[{"op":"add","path":"/metadata/new","value":"value"}]`,
			want:     apimeta.PatchStatusActive,
		},
		{
			name:     "obsolete",
			upstream: addMetadataFields(t, map[string]any{"new": "value"}),
			patch:    `[{"op":"add","path":"/metadata/new","value":"value"}]`,
			want:     apimeta.PatchStatusObsolete,
		},
		{
			name:     "partial",
			upstream: addMetadataFields(t, map[string]any{"first": "one"}),
			patch:    `[{"op":"add","path":"/metadata/first","value":"one"},{"op":"add","path":"/metadata/second","value":"two"}]`,
			want:     apimeta.PatchStatusPartial,
		},
		{
			name:     "conflict",
			upstream: addMetadataFields(t, map[string]any{"new": "different"}),
			patch:    `[{"op":"add","path":"/metadata/new","value":"value"}]`,
			want:     apimeta.PatchStatusConflict,
		},
		{
			name:     "empty",
			upstream: effectiveTestBase,
			patch:    `[]`,
			want:     apimeta.PatchStatusActive,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := apimeta.EvaluateAPIPatch([]byte(tt.upstream), []byte(tt.patch))
			if err != nil {
				t.Fatalf("EvaluateAPIPatch returned error: %v", err)
			}
			if report.Status != tt.want {
				t.Fatalf("status=%s, want %s; operations=%+v", report.Status, tt.want, report.Operations)
			}
		})
	}
}

func TestEvaluateAPIPatch_GuardedReplaceBecomesObsolete(t *testing.T) {
	upstream := strings.Replace(effectiveTestBase, `"status": "online"`, `"status": "preview"`, 1)
	patch := `[
      {"op":"test","path":"/actions/ExistingAction/status","value":"online"},
      {"op":"replace","path":"/actions/ExistingAction/status","value":"preview"}
    ]`
	report, err := apimeta.EvaluateAPIPatch([]byte(upstream), []byte(patch))
	if err != nil {
		t.Fatalf("EvaluateAPIPatch returned error: %v", err)
	}
	if report.Status != apimeta.PatchStatusObsolete {
		t.Fatalf("status=%s, want OBSOLETE", report.Status)
	}
}

func addMetadataFields(t *testing.T, fields map[string]any) string {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(effectiveTestBase), &raw); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	metadata := raw["metadata"].(map[string]any)
	for key, value := range fields {
		metadata[key] = value
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return string(data)
}

func writeEffectiveTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
