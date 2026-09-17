package apimeta

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Channel identifies a complete metadata projection, not a runtime feature flag.
type Channel string

const (
	Stable  Channel = "stable"
	Preview Channel = "preview"
)

// Contract keeps all metadata consumers on the same channel.
type Contract struct {
	Channel Channel
	Spec    *Spec
	Mapping *Mapping
	Help    *Help
}

// LoadContract validates one complete projection. Both channels require the
// overlay files to exist, so a missing checkout file cannot silently hide work.
func LoadContract(dir string, channel Channel) (*Contract, error) {
	if channel != Stable && channel != Preview {
		return nil, fmt.Errorf("invalid contract channel %q", channel)
	}
	parts := make(map[string][]byte, 3)
	for _, name := range []string{"api", "mapping", "help"} {
		ext := ".json"
		if name == "mapping" {
			ext = ".yaml"
		}
		base, err := os.ReadFile(filepath.Join(dir, name+ext))
		if err != nil {
			return nil, err
		}
		patch, err := os.ReadFile(filepath.Join(dir, name+".patch.json"))
		if err != nil {
			return nil, err
		}
		if name == "mapping" {
			var value map[string]any
			if err := yaml.Unmarshal(base, &value); err != nil {
				return nil, fmt.Errorf("decode mapping: %w", err)
			}
			base, err = json.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("mapping must use JSON-compatible string keys: %w", err)
			}
		}
		if channel == Preview {
			if name == "api" {
				base, err = ApplyAPIPatch(base, patch)
			} else {
				base, err = applyMetadataPatch(base, patch)
			}
			if err != nil {
				return nil, fmt.Errorf("%s %s patch: %w", channel, name, err)
			}
		}
		parts[name] = base
	}
	if err := validateEffectiveJSON(parts["api"]); err != nil {
		return nil, err
	}
	spec, err := ParseSpec(parts["api"])
	if err != nil {
		return nil, err
	}
	mapping, err := ParseMapping(parts["mapping"])
	if err != nil {
		return nil, err
	}
	help, err := ParseHelp(parts["help"])
	if err != nil {
		return nil, err
	}
	for _, issue := range mapping.Validate(spec) {
		if issue.IsError() {
			return nil, fmt.Errorf("%s contract: %s", channel, issue.String())
		}
	}
	if err := validateHelp(spec, mapping, help); err != nil {
		return nil, fmt.Errorf("%s help: %w", channel, err)
	}
	return &Contract{Channel: channel, Spec: spec, Mapping: mapping, Help: help}, nil
}

func applyMetadataPatch(base, data []byte) ([]byte, error) {
	patch, err := decodePatch(data, false)
	if err != nil {
		return nil, err
	}
	doc, err := decodePatchDocument(base)
	if err != nil {
		return nil, err
	}
	for i, op := range patch {
		path, _ := op.Path()
		if op.Kind() == "add" {
			tokens, _ := pointerTokens(path)
			if len(tokens) == 0 {
				return nil, fmt.Errorf("add cannot replace the document")
			}
			parent, exists := doc.lookup(tokens[:len(tokens)-1])
			object, isObject := parent.(map[string]any)
			if !exists || !isObject || object == nil {
				return nil, fmt.Errorf("add %s requires an object parent; replace arrays with a test guard", path)
			}
			if _, exists := object[tokens[len(tokens)-1]]; exists {
				return nil, fmt.Errorf("add %s would overwrite an existing key", path)
			}
		}
		err = doc.apply(op)
		if err != nil {
			return nil, fmt.Errorf("operation %d: %w", i, err)
		}
	}
	return doc.bytes()
}

func validateHelp(spec *Spec, mapping *Mapping, help *Help) error {
	if help.APIVersion != mapping.APIVersion {
		return fmt.Errorf("api_version does not match mapping")
	}
	commands := map[string]*ActionMapping{}
	for _, name := range mapping.MappedActionNames() {
		a := mapping.Actions[name]
		commands[a.Command] = a
	}
	for id, h := range help.Commands {
		a := commands[id]
		if a == nil {
			return fmt.Errorf("unknown command %s", id)
		}
		obj := spec.Object(a.Request)
		fields := map[string]Member{}
		if obj != nil {
			for _, member := range obj.Members {
				if !member.Disabled {
					fields[member.Name] = member
				}
			}
		}
		for name, field := range h.Fields {
			if _, ok := fields[name]; !ok {
				return fmt.Errorf("%s: unknown field %s", id, name)
			}
			inputs := map[string]bool{KebabCase(name): true}
			if fm := a.Fields[name]; fm != nil {
				if fm.Flag != "" {
					inputs = map[string]bool{fm.Flag: true}
				}
				if len(fm.Inputs) > 0 {
					inputs = map[string]bool{}
					for _, in := range fm.Inputs {
						inputs[in.Flag] = true
					}
				}
				if fm.Excluded {
					inputs = map[string]bool{}
				}
			}
			for input := range field.Inputs {
				if !inputs[input] {
					return fmt.Errorf("%s.%s: unknown input %s", id, name, input)
				}
			}
		}
	}
	return nil
}

// ValidateCommandRetention rejects overlays that remove or rename stable API
// command paths. Handwritten module paths are checked by the registry tests.
func ValidateCommandRetention(stable, preview *Contract) error {
	paths := map[string]bool{}
	for _, name := range preview.Mapping.MappedActionNames() {
		paths[preview.Mapping.Actions[name].Command] = true
	}
	for _, name := range stable.Mapping.MappedActionNames() {
		path := stable.Mapping.Actions[name].Command
		if !paths[path] {
			return fmt.Errorf("preview removes stable command %s", strings.ReplaceAll(path, ".", " "))
		}
	}
	return nil
}
