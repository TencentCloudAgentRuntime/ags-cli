//go:build preview

package harnesscli

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
)

// Change is one field-level difference between two harness specs.
type Change struct {
	Path string `json:"Path"`
	Op   string `json:"Op"`
	Old  any    `json:"Old,omitempty"`
	New  any    `json:"New,omitempty"`
}

// Named list members are matched by identity rather than index, so reordering
// is not reported as a change and an inserted element reads as one addition.
var listKeys = map[string]func(map[string]any) string{
	"ModelProviders": func(m map[string]any) string { return fmt.Sprint(m["ModelProviderId"]) },
	"MCPServers":     func(m map[string]any) string { return fmt.Sprint(m["Name"]) },
	"Skills":         registryKey,
	"Plugins":        registryKey,
}

func registryKey(m map[string]any) string {
	source, _ := m["Source"].(map[string]any)
	ref, _ := source["Registry"].(map[string]any)
	return fmt.Sprintf("%v/%v", ref["RegistryId"], ref["RecordId"])
}

// DiffSpecs compares two harness specs. When ignorePins is true the provider
// revision pins are dropped first, because the draft never carries them.
func DiffSpecs(from, to apivalue.Object, ignorePins bool) []Change {
	left, right := normalize(from, ignorePins), normalize(to, ignorePins)
	var changes []Change
	diffValue("", left, right, &changes)
	if changes == nil {
		return []Change{}
	}
	return changes
}

func normalize(spec apivalue.Object, ignorePins bool) map[string]any {
	var out map[string]any
	raw, _ := json.Marshal(spec)
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{}
	}
	if ignorePins {
		if providers, ok := out["ModelProviders"].([]any); ok {
			for _, provider := range providers {
				if m, ok := provider.(map[string]any); ok {
					delete(m, "Revision")
				}
			}
		}
		if model, ok := out["DefaultModel"].(map[string]any); ok {
			delete(model, "Revision")
		}
	}
	// An absent list and an empty list mean the same configuration.
	for key, value := range out {
		if list, ok := value.([]any); ok && len(list) == 0 {
			delete(out, key)
		}
	}
	return out
}

func diffValue(path string, left, right any, changes *[]Change) {
	if reflect.DeepEqual(left, right) {
		return
	}
	switch {
	case left == nil:
		*changes = append(*changes, Change{Path: path, Op: "add", New: right})
		return
	case right == nil:
		*changes = append(*changes, Change{Path: path, Op: "remove", Old: left})
		return
	}
	leftMap, leftIsMap := left.(map[string]any)
	rightMap, rightIsMap := right.(map[string]any)
	if leftIsMap && rightIsMap {
		keys := slices.Sorted(maps.Keys(leftMap))
		for key := range rightMap {
			if _, ok := leftMap[key]; !ok {
				keys = append(keys, key)
			}
		}
		slices.Sort(keys)
		for _, key := range slices.Compact(keys) {
			diffValue(join(path, key), leftMap[key], rightMap[key], changes)
		}
		return
	}
	leftList, leftIsList := left.([]any)
	rightList, rightIsList := right.([]any)
	if keyOf, keyed := listKeys[path]; keyed && leftIsList && rightIsList {
		diffKeyedList(path, leftList, rightList, keyOf, changes)
		return
	}
	*changes = append(*changes, Change{Path: path, Op: "replace", Old: left, New: right})
}

func diffKeyedList(path string, left, right []any, keyOf func(map[string]any) string, changes *[]Change) {
	index := func(items []any) ([]string, map[string]any) {
		var order []string
		byKey := map[string]any{}
		for _, item := range items {
			m, _ := item.(map[string]any)
			key := keyOf(m)
			order = append(order, key)
			byKey[key] = item
		}
		return order, byKey
	}
	leftOrder, leftByKey := index(left)
	rightOrder, rightByKey := index(right)
	for _, key := range leftOrder {
		diffValue(fmt.Sprintf("%s[%s]", path, key), leftByKey[key], rightByKey[key], changes)
	}
	for _, key := range rightOrder {
		if _, ok := leftByKey[key]; !ok {
			diffValue(fmt.Sprintf("%s[%s]", path, key), nil, rightByKey[key], changes)
		}
	}
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// FormatChange renders one change as a single readable line.
func FormatChange(change Change) string {
	switch change.Op {
	case "add":
		return fmt.Sprintf("+ %s  %s", change.Path, compact(change.New))
	case "remove":
		return fmt.Sprintf("- %s  %s", change.Path, compact(change.Old))
	default:
		return fmt.Sprintf("~ %s  %s -> %s", change.Path, compact(change.Old), compact(change.New))
	}
}

func compact(value any) string {
	if text, ok := value.(string); ok {
		line, _, multi := strings.Cut(text, "\n")
		if len(line) > 60 {
			line = line[:60] + "..."
		} else if multi {
			line += " ..."
		}
		return fmt.Sprintf("%q", line)
	}
	raw, _ := json.Marshal(value)
	if len(raw) > 120 {
		return string(raw[:117]) + "..."
	}
	return string(raw)
}
