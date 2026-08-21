package apimeta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
)

const apiPatchFilename = "api.patch.json"

// PatchStatus describes how a checked-in API patch relates to an upstream
// api.json document.
type PatchStatus string

const (
	PatchStatusActive   PatchStatus = "ACTIVE"
	PatchStatusPartial  PatchStatus = "PARTIAL"
	PatchStatusObsolete PatchStatus = "OBSOLETE"
	PatchStatusConflict PatchStatus = "CONFLICT"
)

// PatchOperationReport describes the rebase result for one mutating JSON
// Patch operation. Guard-only test operations are omitted unless they fail.
type PatchOperationReport struct {
	Index  int
	Op     string
	Path   string
	Status PatchStatus
	Detail string
}

// PatchReport summarizes how an RFC 6902 patch relates to an upstream API
// document.
type PatchReport struct {
	Status     PatchStatus
	Operations []PatchOperationReport
}

// LoadEffectiveSpec loads api.json, applies api.patch.json to the raw JSON,
// and only then parses the effective API metadata.
func LoadEffectiveSpec(apiDir string) (*Spec, error) {
	data, err := LoadEffectiveJSON(apiDir)
	if err != nil {
		return nil, err
	}
	return ParseSpec(data)
}

// LoadEffectiveJSON returns the raw effective API document for generation,
// validation, and maintainer review.
func LoadEffectiveJSON(apiDir string) ([]byte, error) {
	base, err := os.ReadFile(filepath.Join(apiDir, "api.json"))
	if err != nil {
		return nil, fmt.Errorf("read upstream API spec: %w", err)
	}
	patchData, err := os.ReadFile(filepath.Join(apiDir, apiPatchFilename))
	if err != nil {
		return nil, fmt.Errorf("read API patch: %w", err)
	}
	return ApplyAPIPatch(base, patchData)
}

// ApplyAPIPatch applies a validated RFC 6902 patch without allowing add to
// overwrite an existing value. It validates references after all operations
// have been applied.
func ApplyAPIPatch(base, patchData []byte) ([]byte, error) {
	patch, err := decodeAndValidatePatch(patchData)
	if err != nil {
		return nil, err
	}

	working := append([]byte(nil), base...)
	for i, operation := range patch {
		kind := operation.Kind()
		path, _ := operation.Path()
		if kind == "add" {
			state, detail, err := classifyAdd(working, path, operationValue(operation))
			if err != nil {
				return nil, fmt.Errorf("patch operation %d add %s: %w", i, path, err)
			}
			switch state {
			case PatchStatusObsolete:
				return nil, fmt.Errorf("patch operation %d add %s is obsolete: %s", i, path, detail)
			case PatchStatusConflict:
				return nil, fmt.Errorf("patch operation %d add %s conflicts with upstream: %s", i, path, detail)
			}
		}

		working, err = applyOne(working, operation)
		if err != nil {
			return nil, fmt.Errorf("apply patch operation %d %s %s: %w", i, kind, path, err)
		}
	}
	if err := validateEffectiveJSON(working); err != nil {
		return nil, err
	}
	return working, nil
}

// EvaluateAPIPatch classifies each mutating operation against an upstream
// document without allowing a conflicting operation to overwrite it.
func EvaluateAPIPatch(upstream, patchData []byte) (PatchReport, error) {
	patch, err := decodeAndValidatePatch(patchData)
	if err != nil {
		return PatchReport{}, err
	}

	working := append([]byte(nil), upstream...)
	results := make([]PatchOperationReport, 0, len(patch))
	for i, operation := range patch {
		kind := operation.Kind()
		path, _ := operation.Path()

		if kind == "test" && isGuardForNext(patch, i) {
			continue
		}

		var result PatchOperationReport
		switch kind {
		case "test":
			result, err = evaluateStandaloneTest(working, i, path, operation)
		case "add":
			result, err = evaluateAdd(&working, i, path, operation)
		case "replace":
			result, err = evaluateReplace(&working, patch, i, path, operation)
		case "remove":
			result, err = evaluateRemove(&working, patch, i, path, operation)
		}
		if err != nil {
			return PatchReport{}, err
		}
		if result.Status != "" {
			results = append(results, result)
		}
	}

	status := summarizePatchStatus(results)
	if status != PatchStatusConflict {
		if err := validateEffectiveJSON(working); err != nil {
			return PatchReport{}, fmt.Errorf("validate rebased effective API: %w", err)
		}
	}
	return PatchReport{Status: status, Operations: results}, nil
}

func decodeAndValidatePatch(data []byte) (jsonpatch.Patch, error) {
	patch, err := jsonpatch.DecodePatch(data)
	if err != nil {
		return nil, fmt.Errorf("decode RFC 6902 API patch: %w", err)
	}
	for i, operation := range patch {
		kind := operation.Kind()
		if kind != "test" && kind != "add" && kind != "replace" && kind != "remove" {
			return nil, fmt.Errorf("patch operation %d uses unsupported op %q", i, kind)
		}
		path, err := operation.Path()
		if err != nil {
			return nil, fmt.Errorf("patch operation %d path: %w", i, err)
		}
		if _, err := pointerTokens(path); err != nil {
			return nil, fmt.Errorf("patch operation %d path: %w", i, err)
		}
		if kind != "remove" && operationValue(operation) == nil {
			return nil, fmt.Errorf("patch operation %d %s %s requires value", i, kind, path)
		}
		if kind == "replace" || kind == "remove" {
			if i == 0 || patch[i-1].Kind() != "test" {
				return nil, fmt.Errorf("patch operation %d %s %s must be preceded by test", i, kind, path)
			}
			guardPath, _ := patch[i-1].Path()
			if guardPath != path {
				return nil, fmt.Errorf("patch operation %d %s %s has test guard for %s", i, kind, path, guardPath)
			}
		}
		if kind == "add" {
			tokens, _ := pointerTokens(path)
			for _, token := range tokens {
				if isArrayIndex(token) {
					return nil, fmt.Errorf("patch operation %d add %s uses a numeric array index; append with /- instead", i, path)
				}
			}
			if len(tokens) > 0 && tokens[len(tokens)-1] == "-" {
				if len(tokens) != 4 || tokens[0] != "objects" || tokens[2] != "members" {
					return nil, fmt.Errorf("patch operation %d add %s appends to an unsupported array", i, path)
				}
			}
		}
	}
	return patch, nil
}

func evaluateStandaloneTest(doc []byte, index int, path string, operation jsonpatch.Operation) (PatchOperationReport, error) {
	current, exists, err := pointerValue(doc, path)
	if err != nil {
		return PatchOperationReport{}, err
	}
	want := operationValue(operation)
	if !exists || !jsonValuesEqual(current, want) {
		return PatchOperationReport{Index: index, Op: "test", Path: path, Status: PatchStatusConflict, Detail: "test precondition does not match upstream"}, nil
	}
	return PatchOperationReport{}, nil
}

func evaluateAdd(doc *[]byte, index int, path string, operation jsonpatch.Operation) (PatchOperationReport, error) {
	state, detail, err := classifyAdd(*doc, path, operationValue(operation))
	if err != nil {
		return PatchOperationReport{}, err
	}
	if state == PatchStatusActive {
		*doc, err = applyOne(*doc, operation)
		if err != nil {
			return PatchOperationReport{}, fmt.Errorf("apply active add %s: %w", path, err)
		}
	}
	return PatchOperationReport{Index: index, Op: "add", Path: path, Status: state, Detail: detail}, nil
}

func evaluateReplace(doc *[]byte, patch jsonpatch.Patch, index int, path string, operation jsonpatch.Operation) (PatchOperationReport, error) {
	current, exists, err := pointerValue(*doc, path)
	if err != nil {
		return PatchOperationReport{}, err
	}
	desired := operationValue(operation)
	expected := operationValue(patch[index-1])
	result := PatchOperationReport{Index: index, Op: "replace", Path: path}
	switch {
	case exists && jsonValuesEqual(current, desired):
		result.Status = PatchStatusObsolete
		result.Detail = "upstream already contains the replacement value"
	case exists && jsonValuesEqual(current, expected):
		result.Status = PatchStatusActive
		result.Detail = "upstream still contains the guarded value"
		*doc, err = applyOne(*doc, operation)
		if err != nil {
			return PatchOperationReport{}, fmt.Errorf("apply active replace %s: %w", path, err)
		}
	default:
		result.Status = PatchStatusConflict
		result.Detail = "upstream matches neither the guarded nor replacement value"
	}
	return result, nil
}

func evaluateRemove(doc *[]byte, patch jsonpatch.Patch, index int, path string, operation jsonpatch.Operation) (PatchOperationReport, error) {
	current, exists, err := pointerValue(*doc, path)
	if err != nil {
		return PatchOperationReport{}, err
	}
	expected := operationValue(patch[index-1])
	result := PatchOperationReport{Index: index, Op: "remove", Path: path}
	switch {
	case !exists:
		result.Status = PatchStatusObsolete
		result.Detail = "upstream no longer contains the removed value"
	case jsonValuesEqual(current, expected):
		result.Status = PatchStatusActive
		result.Detail = "upstream still contains the guarded value"
		*doc, err = applyOne(*doc, operation)
		if err != nil {
			return PatchOperationReport{}, fmt.Errorf("apply active remove %s: %w", path, err)
		}
	default:
		result.Status = PatchStatusConflict
		result.Detail = "upstream value differs from the guarded value"
	}
	return result, nil
}

func classifyAdd(doc []byte, path string, value []byte) (PatchStatus, string, error) {
	tokens, err := pointerTokens(path)
	if err != nil {
		return "", "", err
	}
	if len(tokens) >= 2 && tokens[len(tokens)-2] == "members" && tokens[len(tokens)-1] == "-" {
		return classifyMemberAppend(doc, tokens[:len(tokens)-1], value)
	}
	current, exists, err := pointerValue(doc, path)
	if err != nil {
		return "", "", err
	}
	if !exists {
		return PatchStatusActive, "upstream does not contain the target path", nil
	}
	if jsonValuesEqual(current, value) {
		return PatchStatusObsolete, "upstream already contains the added value", nil
	}
	return PatchStatusConflict, "upstream already contains a different value", nil
}

func classifyMemberAppend(doc []byte, parentTokens []string, value []byte) (PatchStatus, string, error) {
	parent, exists, err := pointerValueTokens(doc, parentTokens)
	if err != nil {
		return "", "", err
	}
	if !exists {
		return PatchStatusActive, "upstream does not contain the target members array", nil
	}
	var members []json.RawMessage
	if err := json.Unmarshal(parent, &members); err != nil {
		return "", "", fmt.Errorf("target members path is not an array: %w", err)
	}
	var added struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(value, &added); err != nil || added.Name == "" {
		return "", "", fmt.Errorf("member append requires an object with non-empty name")
	}
	for _, member := range members {
		var existing struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(member, &existing); err != nil {
			return "", "", fmt.Errorf("decode existing member: %w", err)
		}
		if existing.Name != added.Name {
			continue
		}
		if jsonValuesEqual(member, value) {
			return PatchStatusObsolete, fmt.Sprintf("member %s already exists with the same value", added.Name), nil
		}
		return PatchStatusConflict, fmt.Sprintf("member %s already exists with a different value", added.Name), nil
	}
	return PatchStatusActive, fmt.Sprintf("member %s is absent upstream", added.Name), nil
}

func validateEffectiveJSON(data []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse effective API JSON: %w", err)
	}
	if root["actions"] == nil || root["objects"] == nil {
		return fmt.Errorf("effective API must contain actions and objects")
	}
	spec, err := ParseSpec(data)
	if err != nil {
		return err
	}
	for _, name := range spec.SortedActionNames() {
		action := spec.Actions[name]
		if action.Input == "" || spec.Object(action.Input) == nil {
			return fmt.Errorf("effective API action %s references missing input object %q", name, action.Input)
		}
		if action.Output == "" || spec.Object(action.Output) == nil {
			return fmt.Errorf("effective API action %s references missing output object %q", name, action.Output)
		}
	}
	for _, objectName := range spec.SortedObjectNames() {
		object := spec.Objects[objectName]
		if object == nil {
			return fmt.Errorf("effective API object %s must not be null", objectName)
		}
		seen := map[string]bool{}
		for _, member := range object.Members {
			if member.Name == "" {
				return fmt.Errorf("effective API object %s contains a member with empty name", objectName)
			}
			if seen[member.Name] {
				return fmt.Errorf("effective API object %s contains duplicate member %s", objectName, member.Name)
			}
			seen[member.Name] = true
			if referencesObject(member) && spec.Object(member.Member) == nil {
				return fmt.Errorf("effective API member %s.%s references missing object %q", objectName, member.Name, member.Member)
			}
		}
	}
	return nil
}

func referencesObject(member Member) bool {
	if strings.EqualFold(member.Type, "object") {
		return true
	}
	return strings.EqualFold(member.Type, "list") && member.Member != "" && !IsScalar(strings.ToLower(member.Member))
}

func applyOne(doc []byte, operation jsonpatch.Operation) ([]byte, error) {
	options := jsonpatch.NewApplyOptions()
	options.SupportNegativeIndices = false
	options.AllowMissingPathOnRemove = false
	options.EnsurePathExistsOnAdd = false
	options.EscapeHTML = false
	return jsonpatch.Patch{operation}.ApplyWithOptions(doc, options)
}

func operationValue(operation jsonpatch.Operation) []byte {
	value := operation["value"]
	if value == nil {
		return nil
	}
	return []byte(*value)
}

func isGuardForNext(patch jsonpatch.Patch, index int) bool {
	if index+1 >= len(patch) || patch[index].Kind() != "test" {
		return false
	}
	nextKind := patch[index+1].Kind()
	if nextKind != "replace" && nextKind != "remove" {
		return false
	}
	path, _ := patch[index].Path()
	nextPath, _ := patch[index+1].Path()
	return path == nextPath
}

func summarizePatchStatus(results []PatchOperationReport) PatchStatus {
	active := 0
	obsolete := 0
	for _, result := range results {
		switch result.Status {
		case PatchStatusConflict:
			return PatchStatusConflict
		case PatchStatusActive:
			active++
		case PatchStatusObsolete:
			obsolete++
		}
	}
	if active > 0 && obsolete > 0 {
		return PatchStatusPartial
	}
	if obsolete > 0 {
		return PatchStatusObsolete
	}
	return PatchStatusActive
}

func pointerValue(data []byte, path string) ([]byte, bool, error) {
	tokens, err := pointerTokens(path)
	if err != nil {
		return nil, false, err
	}
	return pointerValueTokens(data, tokens)
}

func pointerValueTokens(data []byte, tokens []string) ([]byte, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var current any
	if err := decoder.Decode(&current); err != nil {
		return nil, false, fmt.Errorf("decode JSON document: %w", err)
	}
	for _, token := range tokens {
		switch node := current.(type) {
		case map[string]any:
			value, ok := node[token]
			if !ok {
				return nil, false, nil
			}
			current = value
		case []any:
			if token == "-" {
				return nil, false, nil
			}
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(node) {
				return nil, false, nil
			}
			current = node[index]
		default:
			return nil, false, nil
		}
	}
	value, err := json.Marshal(current)
	if err != nil {
		return nil, false, fmt.Errorf("encode JSON pointer value: %w", err)
	}
	return value, true, nil
}

func pointerTokens(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("JSON Pointer %q must start with /", path)
	}
	raw := strings.Split(path[1:], "/")
	tokens := make([]string, len(raw))
	for i, token := range raw {
		var b strings.Builder
		for j := 0; j < len(token); j++ {
			if token[j] != '~' {
				b.WriteByte(token[j])
				continue
			}
			if j+1 >= len(token) || (token[j+1] != '0' && token[j+1] != '1') {
				return nil, fmt.Errorf("JSON Pointer %q has invalid escape", path)
			}
			j++
			if token[j] == '0' {
				b.WriteByte('~')
			} else {
				b.WriteByte('/')
			}
		}
		tokens[i] = b.String()
	}
	return tokens, nil
}

func isArrayIndex(token string) bool {
	if token == "" || token == "-" {
		return false
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func jsonValuesEqual(a, b []byte) bool {
	return a != nil && b != nil && jsonpatch.Equal(a, b)
}
