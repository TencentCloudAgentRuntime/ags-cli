package apimeta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
)

// ValidateRequest checks the final wire payload against this channel's API
// contract. Presence/business constraints stay with the existing command and
// service validators; null remains compatible with SDK pointer fields.
func (s *Spec) ValidateRequest(action string, payload map[string]any) error {
	a, ok := s.Actions[action]
	if !ok {
		return fmt.Errorf("action %s is absent from the current contract", action)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	return s.validateValue("object", a.Input, value, action)
}

func (s *Spec) validateValue(kind, member string, value any, path string) error {
	if value == nil {
		return nil
	}
	bad := func() error { return fmt.Errorf("%s: expected %s (%s)", path, kind, member) }
	switch kind {
	case "string_map":
		object, ok := value.(map[string]any)
		if !ok {
			return bad()
		}
		for name, v := range object {
			if _, ok := v.(string); !ok {
				return fmt.Errorf("%s.%s: expected string", path, name)
			}
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return bad()
		}
		schema := s.Object(member)
		if schema == nil {
			return fmt.Errorf("%s: unknown object %s", path, member)
		}
		fields := map[string]Member{}
		for _, field := range schema.Members {
			if !field.Disabled {
				fields[field.Name] = field
			}
		}
		for name, v := range object {
			field, ok := fields[name]
			if !ok {
				return fmt.Errorf("%s.%s: unknown field in current channel", path, name)
			}
			if err := s.validateValue(field.Type, field.Member, v, path+"."+name); err != nil {
				return err
			}
		}
	case "list", "array":
		values, ok := value.([]any)
		if !ok {
			return bad()
		}
		childKind := member
		if s.Object(member) != nil {
			childKind = "object"
		}
		for i, v := range values {
			if err := s.validateValue(childKind, member, v, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string", "binary":
		if _, ok := value.(string); !ok {
			return bad()
		}
	case "bool":
		if _, ok := value.(bool); !ok {
			return bad()
		}
	case "int", "int64", "uint", "uint64", "integer":
		number, ok := value.(json.Number)
		if !ok {
			return bad()
		}
		r, ok := new(big.Rat).SetString(string(number))
		if !ok || !r.IsInt() {
			return bad()
		}
		if (kind == "uint" || kind == "uint64") && r.Sign() < 0 {
			return bad()
		}
	case "float", "double":
		if _, ok := value.(json.Number); !ok {
			return bad()
		}
	default:
		return fmt.Errorf("%s: unsupported contract type %s", path, kind)
	}
	return nil
}
