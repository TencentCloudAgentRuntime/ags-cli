//go:build preview

package harnesscli

import (
	"encoding/json"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
)

func spec(t *testing.T, raw string) apivalue.Object {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func paths(changes []Change) map[string]string {
	out := map[string]string{}
	for _, change := range changes {
		out[change.Path] = change.Op
	}
	return out
}

func TestDiffSpecs(t *testing.T) {
	from := spec(t, `{
		"HarnessEngine":"pi","HarnessEngineVersion":"1.3.0",
		"ModelProviders":[{"ModelProviderId":"mp-a","Revision":2,"Models":["x"]},{"ModelProviderId":"mp-b","Revision":1,"Models":["y"]}],
		"DefaultModel":{"ModelProviderId":"mp-a","Revision":2,"Model":"x"},
		"SystemPrompt":"old","Skills":[],"MCPServers":[{"Name":"docs","Source":{"Remote":{"URL":"https://a"}}}]}`)
	to := spec(t, `{
		"HarnessEngine":"pi","HarnessEngineVersion":"1.3.0",
		"ModelProviders":[{"ModelProviderId":"mp-b","Revision":1,"Models":["y"]},{"ModelProviderId":"mp-a","Revision":3,"Models":["x"]}],
		"DefaultModel":{"ModelProviderId":"mp-a","Revision":3,"Model":"x"},
		"SystemPrompt":"new",
		"Skills":[{"Source":{"Registry":{"RegistryId":"r","RecordId":"s","VersionId":"v"}}}],
		"MCPServers":[{"Name":"docs","Source":{"Remote":{"URL":"https://b"}}}]}`)

	got := paths(DiffSpecs(from, to, false))
	want := map[string]string{
		"ModelProviders[mp-a].Revision":      "replace",
		"DefaultModel.Revision":              "replace",
		"SystemPrompt":                       "replace",
		"Skills":                             "add",
		"MCPServers[docs].Source.Remote.URL": "replace",
	}
	if len(got) != len(want) {
		t.Fatalf("changes=%v, want %v", got, want)
	}
	for path, op := range want {
		if got[path] != op {
			t.Errorf("%s: op=%q want %q (all=%v)", path, got[path], op, got)
		}
	}

	// The draft has no pins: comparing with it must not report pin changes,
	// and reordering keyed lists is never a change.
	pinned := paths(DiffSpecs(from, to, true))
	if _, ok := pinned["ModelProviders[mp-a].Revision"]; ok {
		t.Fatalf("pins compared against the draft: %v", pinned)
	}
	if _, ok := pinned["DefaultModel.Revision"]; ok {
		t.Fatalf("default pin compared against the draft: %v", pinned)
	}
	if changes := DiffSpecs(from, from, false); len(changes) != 0 || changes == nil {
		t.Fatalf("identical specs: %v", changes)
	}
}

func TestDiffSpecsRemovalAndAbsentLists(t *testing.T) {
	from := spec(t, `{"MCPServers":[{"Name":"a"},{"Name":"b"}],"Plugins":[]}`)
	to := spec(t, `{"MCPServers":[{"Name":"b"}]}`)
	got := paths(DiffSpecs(from, to, false))
	if len(got) != 1 || got["MCPServers[a]"] != "remove" {
		t.Fatalf("changes=%v", got)
	}
}

func TestFormatChange(t *testing.T) {
	for _, tc := range []struct {
		change Change
		want   string
	}{
		{Change{Path: "SystemPrompt", Op: "replace", Old: "a", New: "b"}, `~ SystemPrompt  "a" -> "b"`},
		{Change{Path: "Skills", Op: "add", New: []any{}}, "+ Skills  []"},
		{Change{Path: "MCPServers[a]", Op: "remove", Old: map[string]any{"Name": "a"}}, `- MCPServers[a]  {"Name":"a"}`},
	} {
		if got := FormatChange(tc.change); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}
