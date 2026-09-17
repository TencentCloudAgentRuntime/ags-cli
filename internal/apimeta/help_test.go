package apimeta

import (
	"reflect"
	"testing"
)

func TestGeneratedHelpLookupsAreIsolated(t *testing.T) {
	baseline := GeneratedHelp()
	if len(baseline.Commands) == 0 {
		t.Fatal("expected embedded help commands")
	}
	for commandID, expected := range baseline.Commands {
		t.Run(commandID, func(t *testing.T) {
			t.Parallel()
			got := CommandHelpFor(commandID)
			if !reflect.DeepEqual(got, expected) {
				t.Fatal("command lookup differs from embedded JSON")
			}
			for fieldName, field := range expected.Fields {
				wantDescription := field.Description
				if wantDescription == "" {
					wantDescription = "fallback"
				}
				if description := FieldDescription(commandID, fieldName, "fallback"); description != wantDescription {
					t.Fatalf("%s description = %q, want %q", fieldName, description, wantDescription)
				}
				for flagName, input := range field.Inputs {
					want := input
					if want.Usage == "" {
						want.Usage = wantDescription
					}
					actual := InputHelpFor(commandID, fieldName, flagName, "fallback")
					if !reflect.DeepEqual(actual, want) {
						t.Fatalf("%s/%s input differs from embedded JSON", fieldName, flagName)
					}
					mutateHelpStrings(actual.Fields, actual.Examples, actual.Values)
					if !reflect.DeepEqual(InputHelpFor(commandID, fieldName, flagName, "fallback"), want) {
						t.Fatal("input lookup leaked a mutable slice")
					}
				}
			}
			mutateHelpStrings(got.Examples)
			for name, field := range got.Fields {
				for flag, input := range field.Inputs {
					mutateHelpStrings(input.Fields, input.Examples, input.Values)
					delete(field.Inputs, flag)
				}
				delete(got.Fields, name)
			}
			if !reflect.DeepEqual(CommandHelpFor(commandID), expected) {
				t.Fatal("command lookup leaked mutable metadata")
			}
		})
	}
}

func TestGeneratedHelpFallback(t *testing.T) {
	if got := FieldDescription("missing", "missing", "fallback"); got != "fallback" {
		t.Fatalf("description = %q", got)
	}
	if got := InputHelpFor("missing", "missing", "missing", "fallback"); !reflect.DeepEqual(got, InputHelp{Usage: "fallback"}) {
		t.Fatalf("input = %#v", got)
	}
	copy := GeneratedHelp()
	clear(copy.Commands)
	if len(GeneratedHelp().Commands) == 0 || len(embeddedHelp().Commands) == 0 {
		t.Fatal("GeneratedHelp leaked its command map")
	}
}

func mutateHelpStrings(groups ...[]string) {
	for _, group := range groups {
		for i := range group {
			group[i] = "mutated"
		}
	}
}

func BenchmarkGeneratedFieldHelp(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		description := FieldDescription("tool.create", "ToolType", "fallback")
		InputHelpFor("tool.create", "ToolType", "tool-type", description)
	}
}
