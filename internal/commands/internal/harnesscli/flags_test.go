//go:build preview

package harnesscli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

func usageCode(t *testing.T, err error) string {
	t.Helper()
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) || cliErr.Failure.Kind != output.KindUsage {
		t.Fatalf("want usage error, got %v", err)
	}
	return cliErr.Failure.Code
}

func request(flags map[string]command.FlagValue) command.Request {
	for name, flag := range flags {
		flag.Changed = true
		flags[name] = flag
	}
	return command.Request{Flags: flags}
}

func TestParseModelRefSplitsOnFirstColonOnly(t *testing.T) {
	ref, err := ParseModelRef("model", "mp-a1:org/model:v2")
	if err != nil || ref != (ModelRef{ProviderID: "mp-a1", Model: "org/model:v2"}) {
		t.Fatalf("ref=%+v err=%v", ref, err)
	}
	for _, value := range []string{"", "mp-a1", ":hy3", "mp-a1:", "  :  "} {
		if _, err := ParseModelRef("model", value); usageCode(t, err) != "INVALID_MODEL_REFERENCE" {
			t.Fatalf("%q: %v", value, err)
		}
	}
}

func TestModelProvidersGroupsInFirstSeenOrder(t *testing.T) {
	providers, refs, err := ModelProviders([]string{"mp-b:x", "mp-a:y", "mp-b:z"})
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"ModelProviderId": "mp-b", "Models": []string{"x", "z"}},
		{"ModelProviderId": "mp-a", "Models": []string{"y"}},
	}
	if !reflect.DeepEqual(providers, want) || len(refs) != 3 {
		t.Fatalf("providers=%v refs=%v", providers, refs)
	}
	if _, _, err := ModelProviders([]string{"mp-a:x", "mp-a:x"}); usageCode(t, err) != "DUPLICATE_MODEL_REFERENCE" {
		t.Fatal(err)
	}
}

func TestDefaultModel(t *testing.T) {
	one := []ModelRef{{ProviderID: "mp-a", Model: "x"}}
	two := append(one, ModelRef{ProviderID: "mp-b", Model: "y"})
	if got, ok, err := DefaultModel(request(map[string]command.FlagValue{}), one); !ok || err != nil || got["Model"] != "x" {
		t.Fatalf("single model must be the implicit default: %v %v %v", got, ok, err)
	}
	if _, ok, err := DefaultModel(request(map[string]command.FlagValue{}), two); ok || err != nil {
		t.Fatal("multiple models must not pick a default implicitly")
	}
	req := request(map[string]command.FlagValue{"default-model": {String: "mp-b:y"}})
	if got, ok, err := DefaultModel(req, two); !ok || err != nil || got["ModelProviderId"] != "mp-b" {
		t.Fatalf("explicit default: %v %v %v", got, ok, err)
	}
	req = request(map[string]command.FlagValue{"default-model": {String: "mp-c:z"}})
	if _, _, err := DefaultModel(req, two); usageCode(t, err) != "DEFAULT_MODEL_NOT_SELECTED" {
		t.Fatal(err)
	}
	// On update without --model the default may name any already selected model.
	if _, ok, err := DefaultModel(req, nil); !ok || err != nil {
		t.Fatalf("update default without --model: %v %v", ok, err)
	}
}

func TestRegistryRefs(t *testing.T) {
	refs, err := RegistryRefs("skill", []string{"MARKETPLACE/rec-1/rv-1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{{"Source": map[string]any{"Registry": map[string]any{"RegistryId": "MARKETPLACE", "RecordId": "rec-1", "VersionId": "rv-1"}}}}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs=%v", refs)
	}
	for _, value := range []string{"a/b", "a//c", "a/b/c/d"} {
		if _, err := RegistryRefs("skill", []string{value}); usageCode(t, err) != "INVALID_REGISTRY_REFERENCE" {
			t.Fatalf("%q: %v", value, err)
		}
	}
	if refs, err := RegistryRefs("skill", listOrClear([]string{""})); err != nil || len(refs) != 0 || refs == nil {
		t.Fatalf("single empty value must clear to an explicit []: %v %v", refs, err)
	}
}

func TestTags(t *testing.T) {
	tags, err := Tags([]string{"env=prod", "note=a=b", "empty="})
	if err != nil || len(tags) != 3 || tags[1]["Value"] != "a=b" || tags[2]["Value"] != "" {
		t.Fatalf("tags=%v err=%v", tags, err)
	}
	if tags, err := Tags([]string{""}); err != nil || len(tags) != 0 {
		t.Fatalf("'' clears tags: %v %v", tags, err)
	}
	for value, code := range map[string]string{"novalue": "INVALID_TAG", "=x": "INVALID_TAG"} {
		if _, err := Tags([]string{value}); usageCode(t, err) != code {
			t.Fatalf("%q: %v", value, err)
		}
	}
	if _, err := Tags([]string{"a=1", "a=2"}); usageCode(t, err) != "DUPLICATE_TAG" {
		t.Fatal(err)
	}
}

func TestRegisteredModels(t *testing.T) {
	models, err := RegisteredModels([]string{"hy3", "qwen-vl:TEXT,IMAGE", "org/m:v2", "vision:image"})
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"ID": "hy3", "Input": []string{"TEXT"}},
		{"ID": "qwen-vl", "Input": []string{"TEXT", "IMAGE"}},
		// A suffix that is not an input list belongs to the model ID.
		{"ID": "org/m:v2", "Input": []string{"TEXT"}},
		{"ID": "vision", "Input": []string{"IMAGE"}},
	}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models=%v", models)
	}
	if _, err := RegisteredModels([]string{"a", "a:TEXT"}); usageCode(t, err) != "DUPLICATE_MODEL" {
		t.Fatal(err)
	}
	if _, err := RegisteredModels([]string{":TEXT"}); usageCode(t, err) != "INVALID_MODEL" {
		t.Fatal(err)
	}
}

func TestApplyProviderShortcuts(t *testing.T) {
	for name, tc := range map[string]struct {
		flags map[string]command.FlagValue
		code  string
	}{
		"both sources":            {map[string]command.FlagValue{"base-url": {String: "https://x"}, "tokenhub-api-key-id": {String: "k"}}, "FLAG_CONFLICT"},
		"credential without url":  {map[string]command.FlagValue{"tokenhub-api-key-id": {String: "k"}, "credential-scope": {String: "s"}}, "FLAG_CONFLICT"},
		"partial credential":      {map[string]command.FlagValue{"base-url": {String: "https://x"}, "credential-scope": {String: "s"}}, "MISSING_REQUIRED_FLAG"},
		"model and models":        {map[string]command.FlagValue{"tokenhub-api-key-id": {String: "k"}, "model": {Strings: []string{"a"}}, "models": {String: "[]"}}, "FLAG_CONFLICT"},
		"no source on create":     {map[string]command.FlagValue{"model": {Strings: []string{"a"}}}, "MISSING_REQUIRED_FLAG"},
		"two flags reading stdin": {map[string]command.FlagValue{"tokenhub-api-key-id": {String: "k"}, "models": {String: "-"}, "request": {String: "-"}}, "STDIN_CONFLICT"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ApplyProviderShortcuts(request(tc.flags), map[string]any{}, false); usageCode(t, err) != tc.code {
				t.Fatalf("got %v, want %s", err, tc.code)
			}
		})
	}
	out := map[string]any{}
	req := request(map[string]command.FlagValue{
		"base-url": {String: "https://llm/v1"}, "credential-provider-id": {String: "agc-1"}, "credential-user-id": {String: "u"}, "credential-scope": {String: "s"},
	})
	if err := ApplyProviderShortcuts(req, out, false); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"OpenAICompatible": map[string]any{"BaseURL": "https://llm/v1", "Credential": map[string]any{"CredentialProviderId": "agc-1", "UserId": "u", "Scope": "s"}}}
	if !reflect.DeepEqual(out["Source"], want) {
		t.Fatalf("source=%v", out["Source"])
	}
	if err := ApplyProviderShortcuts(request(map[string]command.FlagValue{}), map[string]any{}, true); err != nil {
		t.Fatalf("update keeps the source when omitted: %v", err)
	}
}

func TestModelsJSONTransports(t *testing.T) {
	file := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(file, []byte(`[{"ID":"f","Input":["TEXT"]}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		value, stdin, id string
	}{
		"inline": {value: `[{"ID":"i","Input":["TEXT"]}]`, id: "i"},
		"file":   {value: "@" + file, id: "f"},
		"stdin":  {value: "-", stdin: `[{"ID":"s","Input":["TEXT"]}]`, id: "s"},
	} {
		t.Run(name, func(t *testing.T) {
			req := request(map[string]command.FlagValue{"tokenhub-api-key-id": {String: "k"}, "models": {String: tc.value}})
			req.Stdin = strings.NewReader(tc.stdin)
			out := map[string]any{}
			if err := ApplyProviderShortcuts(req, out, false); err != nil {
				t.Fatal(err)
			}
			models, _ := out["Models"].([]any)
			if first, _ := models[0].(map[string]any); len(models) != 1 || first["ID"] != tc.id {
				t.Fatalf("models=%v", out["Models"])
			}
		})
	}
	for name, value := range map[string]string{"missing file": "@" + filepath.Join(t.TempDir(), "none"), "invalid": "[{", "trailing": "[] []"} {
		t.Run(name, func(t *testing.T) {
			req := request(map[string]command.FlagValue{"tokenhub-api-key-id": {String: "k"}, "models": {String: value}})
			if err := ApplyProviderShortcuts(req, map[string]any{}, false); err == nil {
				t.Fatal("expected an input error")
			}
		})
	}
}
