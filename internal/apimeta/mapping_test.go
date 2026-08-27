package apimeta_test

import (
	"path/filepath"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

func loadCheckedIn(t *testing.T) (*apimeta.Spec, *apimeta.Mapping) {
	t.Helper()
	root := filepath.Join("..", "..")
	spec, err := apimeta.LoadEffectiveSpec(filepath.Join(root, "api", "ags", "v20250920"))
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	mapping, err := apimeta.LoadMapping(filepath.Join(root, "api", "ags", "v20250920", "mapping.yaml"))
	if err != nil {
		t.Fatalf("load mapping: %v", err)
	}
	return spec, mapping
}

// TestCheckedInMappingIsValid runs the mapping invariants against the
// committed spec + mapping. With NextPlan §8 mapping is by-need: it
// must validate clean even when most fields are not listed.
func TestCheckedInMappingIsValid(t *testing.T) {
	spec, mapping := loadCheckedIn(t)
	issues := mapping.Validate(spec)
	if len(issues) > 0 {
		for _, i := range issues {
			t.Errorf("mapping invariant violation: %s", i)
		}
	}
}

// TestRawOnlyAndDeferredRequireReason locks in the contract that
// raw_only / deferred_with_reason actions document why they are not
// exposed as resource commands.
func TestRawOnlyAndDeferredRequireReason(t *testing.T) {
	_, mapping := loadCheckedIn(t)
	for _, n := range mapping.SortedActionNames() {
		a := mapping.Actions[n]
		switch a.Status {
		case apimeta.StatusRawOnly, apimeta.StatusDeferredOnly:
			if a.Reason == "" {
				t.Errorf("%s: status=%s requires reason", n, a.Status)
			}
		}
	}
}

// TestMappingDetectsStaleAction verifies STALE_MAPPING fires when a
// mapping entry references an action absent from the spec.
func TestMappingDetectsStaleAction(t *testing.T) {
	spec, mapping := loadCheckedIn(t)
	mapping.Actions["MadeUpAction"] = &apimeta.ActionMapping{Name: "MadeUpAction", Status: apimeta.StatusMapped, Command: "fake"}
	issues := mapping.Validate(spec)
	if !hasIssueCode(issues, "STALE_MAPPING") {
		t.Fatalf("expected STALE_MAPPING, got %v", issues)
	}
}

// TestMappingDetectsStaleField verifies STALE_FIELD fires when a
// mapping field references a member the spec does not declare. This is
// the drift detector kept after MISSING_FIELD was relaxed.
func TestMappingDetectsStaleField(t *testing.T) {
	spec, mapping := loadCheckedIn(t)
	a := mapping.Actions["StartSandboxInstance"]
	if a.Fields == nil {
		a.Fields = map[string]*apimeta.FieldMapping{}
	}
	a.Fields["NoSuchField"] = &apimeta.FieldMapping{Flag: "no-such-field"}
	issues := mapping.Validate(spec)
	if !hasIssueCode(issues, "STALE_FIELD") {
		t.Fatalf("expected STALE_FIELD, got %v", issues)
	}
}

// TestMappingDetectsAliasShorthandConflict verifies SHORTHAND_CONFLICT
// catches two members claiming the same shorthand within one command.
func TestMappingDetectsAliasShorthandConflict(t *testing.T) {
	spec, mapping := loadCheckedIn(t)
	a := mapping.Actions["StartSandboxInstance"]
	if a.Fields == nil {
		a.Fields = map[string]*apimeta.FieldMapping{}
	}
	if a.Fields["ToolName"] == nil {
		a.Fields["ToolName"] = &apimeta.FieldMapping{}
	}
	a.Fields["ToolName"].Shorthand = "x"
	// Add a second field that uses the same shorthand. ClientToken is a
	// real spec member of StartSandboxInstanceRequest with no default
	// shorthand.
	a.Fields["ClientToken"] = &apimeta.FieldMapping{Shorthand: "x"}
	issues := mapping.Validate(spec)
	if !hasIssueCode(issues, "SHORTHAND_CONFLICT") {
		t.Fatalf("expected SHORTHAND_CONFLICT, got %v", issues)
	}
}

// TestExcludedWithFlagIsRejected verifies EXCLUDED_WITH_FLAG fires
// when a field is marked excluded and still declares flag/shorthand/
// aliases/positional.
func TestExcludedWithFlagIsRejected(t *testing.T) {
	spec, mapping := loadCheckedIn(t)
	a := mapping.Actions["StartSandboxInstance"]
	if a.Fields == nil {
		a.Fields = map[string]*apimeta.FieldMapping{}
	}
	a.Fields["ClientToken"] = &apimeta.FieldMapping{
		Excluded: true,
		Flag:     "client-token",
	}
	issues := mapping.Validate(spec)
	if !hasIssueCode(issues, "EXCLUDED_WITH_FLAG") {
		t.Fatalf("expected EXCLUDED_WITH_FLAG, got %v", issues)
	}
}

// TestFlagConflictWhenAliasShadowsAnotherCanonical verifies that an
// alias colliding with another field's canonical kebab-case flag is a
// hard error.
func TestFlagConflictWhenAliasShadowsAnotherCanonical(t *testing.T) {
	spec, mapping := loadCheckedIn(t)
	a := mapping.Actions["StartSandboxInstance"]
	if a.Fields == nil {
		a.Fields = map[string]*apimeta.FieldMapping{}
	}
	// ToolId already gets canonical "tool-id" from the spec. Force
	// ToolName to declare alias "tool-id" -> conflict.
	if a.Fields["ToolName"] == nil {
		a.Fields["ToolName"] = &apimeta.FieldMapping{}
	}
	a.Fields["ToolName"].Aliases = append(a.Fields["ToolName"].Aliases, "tool-id")
	issues := mapping.Validate(spec)
	if !hasIssueCode(issues, "FLAG_CONFLICT") {
		t.Fatalf("expected FLAG_CONFLICT, got %v", issues)
	}
}

// TestMissingMappingForUnknownAction asserts MISSING_MAPPING fires for
// spec actions absent from mapping.yaml.
func TestMissingMappingForUnknownAction(t *testing.T) {
	spec, mapping := loadCheckedIn(t)
	// Pretend an action exists in spec without a mapping entry by
	// removing one and validating.
	delete(mapping.Actions, "DescribeAPIKeyList")
	issues := mapping.Validate(spec)
	if !hasIssueCode(issues, "MISSING_MAPPING") {
		t.Fatalf("expected MISSING_MAPPING, got %v", issues)
	}
}

// TestOverrideFlagEqualsDefaultWarns verifies that explicitly setting
// the same flag the generator would derive surfaces as a soft warning
// (Severity == "warning") rather than failing CI. NextPlan §8.4 / §8.8
// mandate this advisory behaviour to keep mapping.yaml clean.
func TestOverrideFlagEqualsDefaultWarns(t *testing.T) {
	spec, mapping := loadCheckedIn(t)
	a := mapping.Actions["StartSandboxInstance"]
	if a.Fields == nil {
		a.Fields = map[string]*apimeta.FieldMapping{}
	}
	// 'tool-id' is the canonical kebab-case for ToolId; declaring it
	// explicitly should warn but not fail.
	a.Fields["ToolId"] = &apimeta.FieldMapping{Flag: "tool-id"}
	issues := mapping.Validate(spec)
	var found *apimeta.Issue
	for i := range issues {
		if issues[i].Code == "OVERRIDE_FLAG_EQUALS_DEFAULT" {
			found = &issues[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected OVERRIDE_FLAG_EQUALS_DEFAULT, got %v", issues)
	}
	if found.IsError() {
		t.Errorf("OVERRIDE_FLAG_EQUALS_DEFAULT must be a warning, not an error: %#v", *found)
	}
	// Sanity: no real error issues should appear from this scenario alone.
	for _, i := range issues {
		if i.IsError() {
			t.Errorf("unexpected error severity issue: %s", i.String())
		}
	}
}

func hasIssueCode(issues []apimeta.Issue, code string) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}
