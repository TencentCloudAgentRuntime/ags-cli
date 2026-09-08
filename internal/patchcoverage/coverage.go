// Package patchcoverage derives coverage obligations from the complete effective
// API, not from a hand-maintained list of patch operations.
package patchcoverage

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"go.yaml.in/yaml/v3"
)

type Entry struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Before        any    `json:"before"`
	After         any    `json:"after"`
	Documentation bool   `json:"documentation"`
}

type Binding struct {
	Entry      string   `yaml:"entry" json:"entry"`
	Scenario   string   `yaml:"scenario" json:"scenario"`
	Assertions []string `yaml:"assertions" json:"assertions"`
	Review     string   `yaml:"review" json:"review,omitempty"`
}

type Manifest struct {
	Version  int       `yaml:"version"`
	Coverage []Binding `yaml:"coverage"`
}

type Plan struct {
	Entries     []Entry   `json:"entries"`
	Bindings    []Binding `json:"bindings"`
	PatchDigest string    `json:"patch_digest"`
	Digest      string    `json:"plan_digest"`
}

// Registry maps scenario IDs to the assertion IDs implemented by that scenario.
type Registry map[string][]string

// Diff compares raw metadata so attributes absent from the generator's typed
// projection cannot silently disappear. Member names replace array offsets.
func Diff(base, effective []byte) ([]Entry, error) {
	left, err := flatten(base)
	if err != nil {
		return nil, err
	}
	right, err := flatten(effective)
	if err != nil {
		return nil, err
	}
	keys := maps.Clone(left)
	maps.Copy(keys, right)
	var entries []Entry
	for _, path := range slices.Sorted(maps.Keys(keys)) {
		a, aok := left[path]
		b, bok := right[path]
		if aok == bok && reflect.DeepEqual(a, b) {
			continue
		}
		doc, known := classify(path)
		if !known {
			return nil, fmt.Errorf("unclassified API change %s; extend the classifier and its tests", path)
		}
		kind := "change"
		if !aok {
			kind = "add"
		} else if !bok {
			kind = "remove"
		}
		entries = append(entries, Entry{ID: path, Kind: kind, Before: a, After: b, Documentation: doc})
	}
	return entries, nil
}

func escape(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1") }

func flatten(data []byte) (map[string]any, error) {
	var root map[string]any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&root); err != nil {
		return nil, err
	}
	out := map[string]any{}
	var walk func(any, string) error
	walk = func(value any, path string) error {
		switch v := value.(type) {
		case map[string]any:
			// A presence marker catches empty Action/Object additions as well.
			out[path+"/@exists"] = true
			for _, key := range slices.Sorted(maps.Keys(v)) {
				if key == "@exists" {
					return fmt.Errorf("reserved metadata key at %s", path)
				}
				if err := walk(v[key], path+"/"+escape(key)); err != nil {
					return err
				}
			}
		case []any:
			parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
			if len(parts) == 3 && parts[0] == "objects" && parts[2] == "members" {
				out[path+"/@exists"] = true
				seen := map[string]bool{}
				for _, raw := range v {
					member, ok := raw.(map[string]any)
					if !ok {
						return fmt.Errorf("invalid member at %s", path)
					}
					name, ok := member["name"].(string)
					if !ok || name == "" || seen[name] {
						return fmt.Errorf("missing or duplicate member name at %s", path)
					}
					seen[name] = true
					if err := walk(member, path+"/"+escape(name)); err != nil {
						return err
					}
				}
			} else {
				out[path] = v
			}
		default:
			out[path] = v
		}
		return nil
	}
	if err := walk(root, ""); err != nil {
		return nil, err
	}
	return out, nil
}

func classify(path string) (documentation, known bool) {
	p := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(p) == 4 && p[0] == "objects" && p[2] == "members" && p[3] == "@exists" {
		return false, true
	}
	if len(p) == 3 && p[0] == "actions" {
		switch p[2] {
		case "document", "name":
			return true, true
		case "@exists", "input", "output", "status":
			return false, true
		}
	}
	if len(p) == 3 && p[0] == "objects" {
		switch p[2] {
		case "document":
			return true, true
		case "@exists", "type", "usage":
			return false, true
		}
	}
	if len(p) == 5 && p[0] == "objects" && p[2] == "members" {
		switch p[4] {
		case "document", "example":
			return true, true
		case "@exists", "name", "type", "member", "required", "disabled",
			"enum", "minimum", "maximum", "minLength", "maxLength", "pattern", "minItems", "maxItems":
			return false, true
		}
	}
	return false, false
}

// DigestFiles hashes sorted, length-prefixed path/content pairs. Whitespace
// changes intentionally invalidate evidence too.
func DigestFiles(files map[string][]byte) string {
	h := sha256.New()
	for _, path := range slices.Sorted(maps.Keys(files)) {
		for _, value := range [][]byte{[]byte(path), files[path]} {
			_ = binary.Write(h, binary.BigEndian, uint64(len(value)))
			_, _ = h.Write(value)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Load always scans all API versions and rejects orphan/missing manifests.
// validate=false is for printing obligations before a manifest is authored.
func Load(root string, registry Registry, validate bool) (Plan, error) {
	var plan Plan
	patches := map[string][]byte{}
	versions := map[string]bool{}
	err := filepath.WalkDir(filepath.Join(root, "api", "ags"), func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("API symlinks are not supported: %s", path)
		}
		if !e.IsDir() && (e.Name() == "api.json" || e.Name() == "api.patch.json" || e.Name() == "e2e-coverage.yaml") {
			versions[filepath.Dir(path)] = true
		}
		return nil
	})
	if err != nil {
		return plan, err
	}
	if len(versions) == 0 {
		return plan, fmt.Errorf("no API versions found")
	}
	for _, dir := range slices.Sorted(maps.Keys(versions)) {
		base, err := os.ReadFile(filepath.Join(dir, "api.json"))
		if err != nil {
			return plan, err
		}
		patch, err := os.ReadFile(filepath.Join(dir, "api.patch.json"))
		if err != nil {
			return plan, err
		}
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return plan, err
		}
		rel = filepath.ToSlash(rel)
		patches[rel+"/api.patch.json"] = patch
		effective, err := apimeta.ApplyAPIPatch(base, patch)
		if err != nil {
			return plan, err
		}
		entries, err := Diff(base, effective)
		if err != nil {
			return plan, fmt.Errorf("%s: %w", rel, err)
		}
		for i := range entries {
			entries[i].ID = rel + "#" + entries[i].ID
		}
		plan.Entries = append(plan.Entries, entries...)
		if !validate {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, "e2e-coverage.yaml"))
		if err != nil {
			return plan, err
		}
		var manifest Manifest
		d := yaml.NewDecoder(bytes.NewReader(data))
		d.KnownFields(true)
		if err := d.Decode(&manifest); err != nil {
			return plan, err
		}
		if err := d.Decode(new(any)); err != io.EOF {
			return plan, fmt.Errorf("%s: expected exactly one YAML document", rel)
		}
		if manifest.Version != 1 {
			return plan, fmt.Errorf("%s: unsupported coverage version", rel)
		}
		for i := range manifest.Coverage {
			manifest.Coverage[i].Entry = rel + "#" + manifest.Coverage[i].Entry
		}
		if err := Validate(entries, manifest.Coverage, registry); err != nil {
			return plan, err
		}
		plan.Bindings = append(plan.Bindings, manifest.Coverage...)
	}
	plan.PatchDigest = DigestFiles(patches)
	data, err := json.Marshal(plan)
	if err != nil {
		return plan, err
	}
	plan.Digest = DigestFiles(map[string][]byte{"plan-v1": data})
	return plan, nil
}

func Validate(entries []Entry, bindings []Binding, registry Registry) error {
	remaining := map[string]Entry{}
	for _, e := range entries {
		remaining[e.ID] = e
	}
	for _, b := range bindings {
		e, ok := remaining[b.Entry]
		if !ok {
			return fmt.Errorf("unknown, stale or duplicate coverage entry: %s", b.Entry)
		}
		delete(remaining, b.Entry)
		if e.Documentation && b.Scenario == "" {
			if strings.TrimSpace(b.Review) == "" || len(b.Assertions) != 0 {
				return fmt.Errorf("%s: documentation requires a review rationale", b.Entry)
			}
			continue
		}
		assertions, ok := registry[b.Scenario]
		if !ok || len(b.Assertions) == 0 || b.Review != "" {
			return fmt.Errorf("%s: registered scenario and assertions required", b.Entry)
		}
		seen := map[string]bool{}
		for _, id := range b.Assertions {
			if id == "" || seen[id] || !slices.Contains(assertions, id) {
				return fmt.Errorf("%s: unknown or duplicate assertion %q", b.Entry, id)
			}
			seen[id] = true
		}
	}
	if len(remaining) > 0 {
		return fmt.Errorf("missing coverage: %s", strings.Join(slices.Sorted(maps.Keys(remaining)), ", "))
	}
	return nil
}
