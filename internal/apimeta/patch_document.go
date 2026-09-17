package apimeta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
)

// patchDocument decodes the contract once per application. Operations still use
// json-patch, but only encode the affected value (or array), not the whole API.
// There is no cross-call cache: file changes and independent callers stay isolated.
type patchDocument struct{ root any }

func decodePatchDocument(data []byte) (*patchDocument, error) {
	if !json.Valid(data) {
		return nil, fmt.Errorf("invalid JSON document")
	}
	d := &patchDocument{}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&d.root); err != nil {
		return nil, fmt.Errorf("decode JSON document: %w", err)
	}
	return d, nil
}

func (d *patchDocument) bytes() ([]byte, error) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(d.root); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

func (d *patchDocument) lookup(tokens []string) (any, bool) {
	current := d.root
	for _, token := range tokens {
		switch node := current.(type) {
		case map[string]any:
			value, ok := node[token]
			if !ok {
				return nil, false
			}
			current = value
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(node) {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func (d *patchDocument) pointerValue(path string) ([]byte, bool, error) {
	tokens, err := pointerTokens(path)
	if err != nil {
		return nil, false, err
	}
	return d.pointerValueTokens(tokens)
}

func (d *patchDocument) pointerValueTokens(tokens []string) ([]byte, bool, error) {
	value, exists := d.lookup(tokens)
	if !exists {
		return nil, false, nil
	}
	data, err := json.Marshal(value)
	return data, true, err
}

func (d *patchDocument) apply(operation jsonpatch.Operation) error {
	path, _ := operation.Path()
	tokens, err := pointerTokens(path)
	if err != nil {
		return err
	}
	// Root operations touch the whole document. Empty path segments retain the
	// library's special empty-key behavior without reinterpreting it locally.
	if len(tokens) == 0 || slices.Contains(tokens, "") {
		raw, err := d.bytes()
		if err != nil {
			return err
		}
		result, err := applyOne(raw, operation)
		if err != nil {
			return err
		}
		next, err := decodePatchDocument(result)
		if err != nil {
			return err
		}
		d.root = next.root
		return nil
	}
	parentTokens, key := tokens[:len(tokens)-1], tokens[len(tokens)-1]
	parent, exists := d.lookup(parentTokens)
	if !exists {
		return fmt.Errorf("missing parent for %s", path)
	}
	local := map[string]any{}
	localPath := "/value"
	switch node := parent.(type) {
	case map[string]any:
		if value, ok := node[key]; ok {
			local["value"] = value
		}
	case []any:
		local["value"] = node
		// Keep the original escaped token; a slash inside an invalid array
		// index must not become an extra path segment in the local operation.
		localPath += path[strings.LastIndex(path, "/"):]
	default:
		return fmt.Errorf("parent of %s is not an object or array", path)
	}
	raw, err := json.Marshal(local)
	if err != nil {
		return err
	}
	// Only rewrite the path; the library continues to enforce operation/value,
	// test equality, array bounds, and missing-target semantics.
	op := maps.Clone(operation)
	encodedPath, err := json.Marshal(localPath)
	if err != nil {
		return err
	}
	rewritten := json.RawMessage(encodedPath)
	op["path"] = &rewritten
	result, err := applyOne(raw, op)
	if err != nil {
		return err
	}
	if operation.Kind() == "test" {
		return nil
	}
	next, err := decodePatchDocument(result)
	if err != nil {
		return err
	}
	values := next.root.(map[string]any)
	switch node := parent.(type) {
	case map[string]any:
		if value, ok := values["value"]; ok {
			node[key] = value
		} else {
			delete(node, key)
		}
	case []any:
		d.replace(parentTokens, values["value"])
	}
	return nil
}

// replace updates an already-resolved parent array after its length changes.
func (d *patchDocument) replace(tokens []string, value any) {
	if len(tokens) == 0 {
		d.root = value
		return
	}
	parent, _ := d.lookup(tokens[:len(tokens)-1])
	key := tokens[len(tokens)-1]
	switch node := parent.(type) {
	case map[string]any:
		node[key] = value
	case []any:
		index, _ := strconv.Atoi(key)
		node[index] = value
	}
}
