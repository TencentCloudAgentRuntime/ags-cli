package apicli

import (
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

func TestRequiredHintIsMetadataDriven(t *testing.T) {
	for _, field := range []FieldSpec{
		{Name: "FutureId", Inputs: []InputSpec{{Flag: "future-id"}}, RequiredHint: "Find the ID with the future list command."},
		{Name: "FutureId", Inputs: []InputSpec{{Name: "future-id", Positional: true}}, RequiredHint: "Choose an existing ID."},
	} {
		err := missingRequiredFieldError(field)
		if err.Failure.Hint != field.RequiredHint || err.ExitCode != 2 {
			t.Fatalf("metadata hint not preserved: %+v", err)
		}
	}
	desc := APIDescriptor{Spec: command.Spec{ID: "future.get"}, Fields: []FieldSpec{{Name: "FutureId", Required: true, Inputs: []InputSpec{{Flag: "future-id"}}, RequiredHint: "Custom lookup guidance."}}}
	for _, req := range []command.Request{{}, {Flags: map[string]command.FlagValue{"request": {Changed: true, String: `{}`}}}} {
		_, err := NewRequestBuilder(desc).Build(req)
		if err == nil || err.Error() != "--future-id is required" {
			t.Fatalf("missing input classification changed: %v", err)
		}
	}
}
