package patchcoverage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const baseAPI = `{"actions":{"Get":{"input":"Req","output":"Resp","status":"online"}},"objects":{"Req":{"members":[]},"Resp":{"members":[{"name":"Id","type":"string","member":"string","required":true}]}}}`

func TestDiffAndCoverageNewPeerFails(t *testing.T) {
	effective := strings.Replace(baseAPI, `"members":[]`, `"members":[{"name":"Name","type":"string","member":"string","required":false}]`, 1)
	entries, err := Diff([]byte(baseAPI), []byte(effective))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("want presence/name/type/member/required, got %+v", entries)
	}
	registry := Registry{"get": {"readback"}}
	var bindings []Binding
	for _, e := range entries {
		bindings = append(bindings, Binding{Entry: e.ID, Scenario: "get", Assertions: []string{"readback"}})
	}
	if err := Validate(entries, bindings, registry); err != nil {
		t.Fatal(err)
	}
	peer := strings.Replace(effective, `"required":false}`, `"required":false},{"name":"Other","type":"string","member":"string"}`, 1)
	more, err := Diff([]byte(baseAPI), []byte(peer))
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(more, bindings, registry); err == nil || !strings.Contains(err.Error(), "missing coverage") {
		t.Fatalf("new peer must fail: %v", err)
	}
	for _, mutate := range []func([]Binding) []Binding{
		func(b []Binding) []Binding { return b[1:] },
		func(b []Binding) []Binding { b[0].Scenario = "missing"; return b },
		func(b []Binding) []Binding { b[0].Assertions = []string{"missing"}; return b },
		func(b []Binding) []Binding { return append(b, b[0]) },
		func(b []Binding) []Binding { b[0].Entry = "stale"; return b },
	} {
		if Validate(entries, mutate(append([]Binding(nil), bindings...)), registry) == nil {
			t.Fatal("invalid mapping passed")
		}
	}
}

func TestDiffRawConstraintsAndRemovals(t *testing.T) {
	for _, field := range []string{"required", "type", "enum", "minimum", "maximum", "minLength", "maxLength", "pattern", "minItems", "maxItems", "disabled"} {
		t.Run(field, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal([]byte(baseAPI), &doc); err != nil {
				t.Fatal(err)
			}
			member := doc["objects"].(map[string]any)["Resp"].(map[string]any)["members"].([]any)[0].(map[string]any)
			member[field] = "changed"
			data, _ := json.Marshal(doc)
			delta, err := Diff([]byte(baseAPI), data)
			if err != nil || len(delta) != 1 || delta[0].Documentation {
				t.Fatalf("lost raw %s: %+v %v", field, delta, err)
			}
			reverse, err := Diff(data, []byte(baseAPI))
			if err != nil || len(reverse) != 1 {
				t.Fatalf("lost removal %s", field)
			}
		})
	}
	unknown := strings.Replace(baseAPI, `"status":"online"`, `"status":"online","surprise":true`, 1)
	if _, err := Diff([]byte(baseAPI), []byte(unknown)); err == nil {
		t.Fatal("unknown metadata passed")
	}
	removed := strings.Replace(baseAPI, `"Get":{"input":"Req","output":"Resp","status":"online"}`, ``, 1)
	entries, err := Diff([]byte(baseAPI), []byte(removed))
	if err != nil || len(entries) != 4 {
		t.Fatalf("action removal: %+v %v", entries, err)
	}
}

func TestMemberOrderAndDocumentation(t *testing.T) {
	a := `{"objects":{"R":{"members":[{"name":"A"},{"name":"B"}]}}}`
	b := `{"objects":{"R":{"members":[{"name":"B"},{"name":"A"}]}}}`
	delta, err := Diff([]byte(a), []byte(b))
	if err != nil || len(delta) != 0 {
		t.Fatalf("order is not a contract change: %v %v", delta, err)
	}
	b = strings.Replace(a, `"name":"A"`, `"name":"A","document":"new constraint in prose"`, 1)
	delta, err = Diff([]byte(a), []byte(b))
	if err != nil || len(delta) != 1 || !delta[0].Documentation {
		t.Fatal(delta, err)
	}
	if Validate(delta, []Binding{{Entry: delta[0].ID}}, nil) == nil {
		t.Fatal("unreviewed prose passed")
	}
	if err := Validate(delta, []Binding{{Entry: delta[0].ID, Review: "reviewer must determine whether this needs live assertions"}}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAllVersionsAndDigest(t *testing.T) {
	root := t.TempDir()
	write := func(version, file, data string) {
		t.Helper()
		dir := filepath.Join(root, "api", "ags", version)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []string{"v1", "v2"} {
		write(v, "api.json", baseAPI)
		write(v, "api.patch.json", "[]")
		write(v, "e2e-coverage.yaml", "version: 1\ncoverage: []\n")
	}
	plan, err := Load(root, nil, true)
	if err != nil || len(plan.Entries) != 0 {
		t.Fatal(plan, err)
	}
	write("v2", "api.patch.json", "[ ]")
	other, err := Load(root, nil, true)
	if err != nil || other.PatchDigest == plan.PatchDigest {
		t.Fatal("patch digest did not change", err)
	}
	write("v2", "api.patch.json", `[{"op":"add","path":"/objects/Req/members/-","value":{"name":"X","type":"string","member":"string"}}]`)
	if _, err := Load(root, nil, true); err == nil {
		t.Fatal("second version escaped coverage")
	}
	if _, err := Load(root, nil, false); err != nil {
		t.Fatal(err)
	}
	write("v2", "api.patch.json", `[{"op":"copy","from":"/objects/Req","path":"/objects/Other"}]`)
	if _, err := Load(root, nil, false); err == nil {
		t.Fatal("unsupported op silently accepted")
	}
	if DigestFiles(map[string][]byte{"a": []byte("bc")}) == DigestFiles(map[string][]byte{"ab": []byte("c")}) {
		t.Fatal("ambiguous digest")
	}
}

func TestManifestStrictness(t *testing.T) {
	for _, manifest := range []string{
		"version: 1\ncoverage: []\nunknown: true\n",
		"version: 1\nversion: 1\ncoverage: []\n",
		"version: 1\ncoverage: []\n---\nversion: 1\n",
		"version: 2\ncoverage: []\n",
	} {
		root := t.TempDir()
		dir := filepath.Join(root, "api", "ags", "v1")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string]string{"api.json": baseAPI, "api.patch.json": "[]", "e2e-coverage.yaml": manifest} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := Load(root, nil, true); err == nil {
			t.Fatalf("invalid manifest passed: %s", manifest)
		}
	}
}

func TestEmptyMembersChanges(t *testing.T) {
	for _, path := range []string{"", "/actions/Get", "/objects/Req/members/0"} {
		t.Run(path, func(t *testing.T) {
			base := `{"actions":{"Get":{}},"objects":{"Req":{"members":[{"name":"A"}]}}}`
			var doc map[string]any
			if err := json.Unmarshal([]byte(base), &doc); err != nil {
				t.Fatal(err)
			}
			target := doc
			switch path {
			case "/actions/Get":
				target = doc["actions"].(map[string]any)["Get"].(map[string]any)
			case "/objects/Req/members/0":
				target = doc["objects"].(map[string]any)["Req"].(map[string]any)["members"].([]any)[0].(map[string]any)
			}
			target["members"] = []any{}
			effective, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			for _, pair := range [][2][]byte{{[]byte(base), effective}, {effective, []byte(base)}} {
				if _, err := Diff(pair[0], pair[1]); err == nil {
					t.Fatal("unknown empty members change accepted")
				}
			}
		})
	}
	base := []byte(`{"objects":{"Req":{}}}`)
	effective := []byte(`{"objects":{"Req":{"members":[]}}}`)
	for _, pair := range [][2][]byte{{base, effective}, {effective, base}} {
		delta, err := Diff(pair[0], pair[1])
		if err != nil || len(delta) != 1 || delta[0].ID != "/objects/Req/members/@exists" {
			t.Fatalf("lost members presence: %+v %v", delta, err)
		}
		if Validate(delta, nil, nil) == nil {
			t.Fatal("members presence escaped coverage")
		}
	}
}
