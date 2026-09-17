package apimeta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
)

// Compare each intermediate state and failure against the original whole-document
// library application, so later operations also see identical mutations.
func TestPatchDocumentMatchesWholeDocument(t *testing.T) {
	cases := []struct{ name, base, patch string }{
		{"dependent objects", `{"objects":{}}`, `[{"op":"add","path":"/objects/A","value":{"members":[]}},{"op":"add","path":"/objects/A/members/-","value":{"name":"Field"}},{"op":"test","path":"/objects/A/members/0/name","value":"Field"},{"op":"replace","path":"/objects/A/members/0/name","value":"Other"},{"op":"remove","path":"/objects/A/members/0"}]`},
		{"escaped and empty keys", `{"a/b":{"~":null,"":1}}`, `[{"op":"test","path":"/a~1b/~0","value":null},{"op":"replace","path":"/a~1b/","value":9007199254740993},{"op":"test","path":"/a~1b/","value":9007199254740993},{"op":"remove","path":"/a~1b/~0"},{"op":"add","path":"/a~1b/~0","value":"<tag>"}]`},
		{"array in array", `{"a":[[1,2],3]}`, `[{"op":"add","path":"/a/0/1","value":4},{"op":"remove","path":"/a/0/0"},{"op":"replace","path":"/a/0/0","value":null},{"op":"test","path":"/a/0/1","value":2}]`},
		{"root array", `[1,2]`, `[{"op":"add","path":"/-","value":3},{"op":"remove","path":"/0"},{"op":"replace","path":"/0","value":4}]`},
		{"root replacement", `{"a":1}`, `[{"op":"test","path":"","value":{"a":1}},{"op":"replace","path":"","value":{"b":[1]}},{"op":"add","path":"/b/-","value":2}]`},
		{"missing test null", `{}`, `[{"op":"test","path":"/missing","value":null}]`},
		{"missing replace", `{}`, `[{"op":"replace","path":"/missing","value":1}]`},
		{"missing remove", `{}`, `[{"op":"remove","path":"/missing"}]`},
		{"missing parent", `{}`, `[{"op":"add","path":"/missing/child","value":1}]`},
		{"null parent", `{"a":null}`, `[{"op":"add","path":"/a/b","value":1}]`},
		{"scalar parent", `{"a":1}`, `[{"op":"replace","path":"/a/b","value":1}]`},
		{"negative index", `{"a":[1]}`, `[{"op":"remove","path":"/a/-1"}]`},
		{"escaped array index", `{"array":[{}]}`, `[{"op":"add","path":"/array/0~1child","value":null}]`},
		{"out of bounds", `{"a":[1]}`, `[{"op":"add","path":"/a/2","value":2}]`},
		{"failed guard", `{"a":1}`, `[{"op":"test","path":"/a","value":2}]`},
		{"number equality", `{"a":1.0}`, `[{"op":"test","path":"/a","value":1}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := decodePatchDocument([]byte(tc.base))
			if err != nil {
				t.Fatal(err)
			}
			patch, err := jsonpatch.DecodePatch([]byte(tc.patch))
			if err != nil {
				t.Fatal(err)
			}
			whole := []byte(tc.base)
			for i, op := range patch {
				want, wantErr := applyOne(whole, op)
				gotErr := doc.apply(op)
				if (wantErr == nil) != (gotErr == nil) {
					t.Fatalf("operation %d: whole=%v local=%v", i, wantErr, gotErr)
				}
				if wantErr != nil {
					return
				}
				got, err := doc.bytes()
				if err != nil {
					t.Fatal(err)
				}
				if !sameJSON(t, got, want) {
					t.Fatalf("operation %d: got %s want %s", i, got, want)
				}
				whole = want
			}
		})
	}
}

func TestPatchDocumentRejectsTrailingJSON(t *testing.T) {
	for _, raw := range []string{`{} {}`, `{} garbage`, `{"x":`} {
		if _, err := decodePatchDocument([]byte(raw)); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestPatchApplicationDoesNotShareState(t *testing.T) {
	base := []byte(`{"actions":{},"objects":{}}`)
	original := bytes.Clone(base)
	patch := []byte(`[{"op":"add","path":"/objects/Added","value":{"type":"object","members":[]}}]`)
	for range 4 {
		t.Run("independent", func(t *testing.T) {
			t.Parallel()
			if _, err := ApplyAPIPatch(base, patch); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(base, original) {
				t.Fatal("mutated caller input")
			}
		})
	}
}

func FuzzPatchDocumentArrayIndices(f *testing.F) {
	for _, key := range []string{"0", "1", "-", "-1", "01", "+1", "2", "", "~0", "~1"} {
		f.Add(key)
	}
	f.Fuzz(func(t *testing.T, key string) {
		path, _ := json.Marshal("/array/" + key)
		patch, err := jsonpatch.DecodePatch([]byte(fmt.Sprintf(`[{"op":"add","path":%s,"value":null}]`, path)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pointerTokens("/array/" + key); err != nil {
			return
		}
		base := []byte(`{"array":[1]}`)
		doc, err := decodePatchDocument(base)
		if err != nil {
			t.Fatal(err)
		}
		want, wantErr := applyOne(base, patch[0])
		gotErr := doc.apply(patch[0])
		if (wantErr == nil) != (gotErr == nil) {
			t.Fatalf("key %q: whole=%v local=%v", key, wantErr, gotErr)
		}
		if gotErr == nil {
			got, err := doc.bytes()
			if err != nil {
				t.Fatal(err)
			}
			if !sameJSON(t, want, got) {
				t.Fatalf("key %q: got %s want %s", key, got, want)
			}
		}
	})
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	decode := func(raw []byte) any {
		t.Helper()
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var value any
		if err := dec.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	return reflect.DeepEqual(decode(a), decode(b))
}

func TestApplyAPIPatchChecksSequentialState(t *testing.T) {
	base := []byte(`{"actions":{},"objects":{}}`)
	prefix := `{"op":"add","path":"/objects/A","value":{"type":"object","members":[]}},
 {"op":"add","path":"/objects/A/members/-","value":{"name":"Value","type":"string","member":"string"}}`
	for _, suffix := range []string{
		`,{"op":"add","path":"/objects/A/members/-","value":{"name":"Value","type":"int","member":"int64"}}`,
		`,{"op":"test","path":"/objects/A/members/0/name","value":"Wrong"},{"op":"replace","path":"/objects/A/members/0/name","value":"Other"}`,
	} {
		if _, err := ApplyAPIPatch(base, []byte("["+prefix+suffix+"]")); err == nil {
			t.Fatal("accepted conflict against earlier operation")
		}
	}
	patch := []byte("[" + prefix + `,{"op":"test","path":"/objects/A/members/0/name","value":"Value"},{"op":"replace","path":"/objects/A/members/0/name","value":"Other"}]`)
	raw, err := ApplyAPIPatch(base, patch)
	if err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateAPIPatch(base, patch)
	if err != nil || report.Status != PatchStatusActive {
		t.Fatalf("report=%v err=%v", report, err)
	}
	spec, err := ParseSpec(raw)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Objects["A"].Members[0].Name != "Other" {
		t.Fatal("later replacement was not applied")
	}
}
