// Package apivalue provides SDK-independent views of control-plane JSON.
package apivalue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
)

// Object retains the complete wire object, including fields unknown to the SDK.
type Object map[string]any

// Project is only for existing presentation adapters. The original Object must
// remain the authoritative result; never return the projection as wire data.
func Project(value, target any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

// Extend retains existing scalar normalization and preserves new or structured
// wire fields, including nested members missing from a presentation SDK model.
func Extend(normalized map[string]any, value any) map[string]any {
	raw, err := Decode(value)
	if err != nil {
		return normalized
	}
	for name, value := range raw {
		_, exists := normalized[name]
		switch value.(type) {
		case map[string]any, []any:
			normalized[name] = value
		default:
			if !exists {
				normalized[name] = value
			}
		}
	}
	return normalized
}

func Decode(value any) (Object, error) {
	if v, ok := value.(Object); ok {
		return maps.Clone(v), nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var obj Object
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&obj); err != nil {
		return nil, fmt.Errorf("invalid API object: %w", err)
	}
	if obj == nil {
		return nil, fmt.Errorf("missing API object")
	}
	return obj, nil
}

func (o Object) String(name string) string { value, _ := o[name].(string); return value }
func (o Object) Object(name string) Object {
	value, err := Decode(o[name])
	if err != nil {
		return nil
	}
	return value
}

// Objects is a best-effort accessor for presentation and optional metadata.
func (o Object) Objects(name string) []Object {
	result, _ := o.ReadObjects(name)
	return result
}

// ReadObjects validates a resource list before it is used as a successful result.
// Missing/null lists retain the SDK's empty-list behavior; malformed members do not.
func (o Object) ReadObjects(name string) ([]Object, error) {
	raw, err := json.Marshal(o[name])
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", name, err)
	}
	var result []Object
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid %s array: %w", name, err)
	}
	for i, item := range result {
		if item == nil {
			return nil, fmt.Errorf("invalid %s[%d]: null resource", name, i)
		}
	}
	return result, nil
}

func (o Object) Int64(name string) int64 {
	switch value := o[name].(type) {
	case json.Number:
		n, _ := value.Int64()
		return n
	case float64:
		return int64(value)
	case int64:
		return value
	case uint64:
		return int64(value)
	case int:
		return int64(value)
	}
	return 0
}
func (o Object) Display(name string) string {
	value := o[name]
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	if b, ok := value.(bool); ok {
		return strconv.FormatBool(b)
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

// ExtendResponse preserves fields outside an explicitly normalized wire surface.
func ExtendResponse(normalized map[string]any, response Object, consumed ...string) map[string]any {
	response = maps.Clone(response)
	for _, name := range consumed {
		delete(response, name)
	}
	for name, value := range response {
		if _, exists := normalized[name]; !exists {
			normalized[name] = value
		}
	}
	return normalized
}
