// Package apimeta parses and validates the human-maintained mapping
// between TencentCloud AGS API actions (api.json) and AGR CLI resource
// commands. It is the anti-corruption layer that prevents stale mappings,
// missing fields, and alias / shorthand conflicts from silently shipping.
package apimeta

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Status values for an action's mapping state.
const (
	StatusMapped       = "mapped"
	StatusRawOnly      = "raw_only"
	StatusDeferredOnly = "deferred_with_reason"
)

// Mapping is the parsed mapping.yaml document.
type Mapping struct {
	APIVersion string                    `yaml:"api_version"`
	Actions    map[string]*ActionMapping `yaml:"actions"`
}

// ActionMapping is a single action's mapping entry.
type ActionMapping struct {
	Name     string                   `yaml:"-"`
	Command  string                   `yaml:"command"`
	Request  string                   `yaml:"request"`
	Response string                   `yaml:"response"`
	Status   string                   `yaml:"status"`
	Reason   string                   `yaml:"reason"`
	Fields   map[string]*FieldMapping `yaml:"fields"`
}

// FieldMapping describes how one request member maps to a CLI flag.
//
// All fields are optional. By default the generator derives the long
// flag from the spec member name via kebab-case; a mapping entry only
// needs to appear when the field requires a non-default behavior
// (alias, shorthand, positional, custom flag name, or explicit
// exclusion from the cobra flag set).
type FieldMapping struct {
	Flag       string         `yaml:"flag"`
	Shorthand  string         `yaml:"shorthand"`
	Aliases    []string       `yaml:"aliases"`
	Parser     string         `yaml:"parser"`
	Inputs     []InputMapping `yaml:"inputs"`
	Positional bool           `yaml:"positional"`
	// Excluded marks a field as not exposed via cobra flags. The field
	// remains accepted via `--request` JSON; only the dedicated flag
	// is suppressed. Useful for internal or unstable fields.
	Excluded bool `yaml:"excluded"`
}

// InputMapping describes one CLI input feeding an API request field.
type InputMapping struct {
	Flag      string   `yaml:"flag"`
	Type      string   `yaml:"type"`
	Form      string   `yaml:"form"`
	Default   string   `yaml:"default"`
	Shorthand string   `yaml:"shorthand"`
	Aliases   []string `yaml:"aliases"`
}

// LoadMapping reads a mapping.yaml file from disk.
func LoadMapping(path string) (*Mapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mapping: %w", err)
	}
	return ParseMapping(data)
}

// ParseMapping parses raw mapping.yaml bytes.
func ParseMapping(data []byte) (*Mapping, error) {
	m := &Mapping{}
	if err := yaml.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parse mapping: %w", err)
	}
	for name, a := range m.Actions {
		a.Name = name
	}
	return m, nil
}

// SortedActionNames returns action names in deterministic order.
func (m *Mapping) SortedActionNames() []string {
	names := make([]string, 0, len(m.Actions))
	for n := range m.Actions {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// MappedActionNames returns only the mapped (resource command) actions.
func (m *Mapping) MappedActionNames() []string {
	var names []string
	for _, n := range m.SortedActionNames() {
		if m.Actions[n].Status == StatusMapped {
			names = append(names, n)
		}
	}
	return names
}

// Action returns the mapping for the given action name.
func (m *Mapping) Action(name string) (*ActionMapping, bool) {
	a, ok := m.Actions[name]
	return a, ok
}

// kebabCaseField mirrors KebabCase locally so mapping validation can compare
// generated flag names without depending on generator-only packages.
func kebabCaseField(name string) string {
	if name == "" {
		return ""
	}
	runes := []rune(name)
	var buf strings.Builder
	for i, r := range runes {
		isUpper := r >= 'A' && r <= 'Z'
		if isUpper && i > 0 {
			prev := runes[i-1]
			prevUpper := prev >= 'A' && prev <= 'Z'
			var next rune
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			nextLower := next >= 'a' && next <= 'z'
			if !prevUpper || nextLower {
				buf.WriteByte('-')
			}
		}
		if isUpper {
			buf.WriteRune(r - 'A' + 'a')
		} else {
			buf.WriteRune(r)
		}
	}
	out := buf.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	return strings.Trim(out, "-")
}

// SeverityError is the default severity: a hard CI failure.
const SeverityError = "error"

// SeverityWarning is the soft severity: surfaced to maintainers but
// does not fail CI. Used for advisory invariants (e.g.
// OVERRIDE_FLAG_EQUALS_DEFAULT) that catch redundancy without
// blocking the pipeline.
const SeverityWarning = "warning"

// Issue describes a mapping validation problem.
type Issue struct {
	Action   string
	Field    string
	Code     string
	Detail   string
	Severity string // SeverityError (default) or SeverityWarning
}

// IsError reports whether the issue should fail CI. Issues with no
// severity recorded default to "error" for backwards compatibility
// with the legacy callers that pre-date the Severity field.
func (i Issue) IsError() bool {
	return i.Severity == "" || i.Severity == SeverityError
}

// String formats the issue for generator and CI diagnostics.
func (i Issue) String() string {
	prefix := i.Code
	if i.Severity == SeverityWarning {
		prefix = "WARN " + prefix
	}
	if i.Field != "" {
		return fmt.Sprintf("[%s] %s: field=%s: %s", prefix, i.Action, i.Field, i.Detail)
	}
	if i.Action != "" {
		return fmt.Sprintf("[%s] %s: %s", prefix, i.Action, i.Detail)
	}
	return fmt.Sprintf("[%s] %s", prefix, i.Detail)
}

// Validate cross-checks the mapping against the api spec and reports any
// invariant violations. The CI mapping invariant check fails when this
// returns a non-empty list.
func (m *Mapping) Validate(spec *Spec) []Issue {
	var issues []Issue

	// Every effective API action must appear in the mapping.
	for _, name := range spec.SortedActionNames() {
		if _, ok := m.Actions[name]; !ok {
			issues = append(issues, Issue{Action: name, Code: "MISSING_MAPPING", Detail: "action exists in api.json but not in mapping.yaml"})
		}
	}

	// Every mapping action must reference an effective API action and meet status invariants.
	for _, name := range m.SortedActionNames() {
		a := m.Actions[name]
		specAction, ok := spec.Actions[name]
		if !ok {
			issues = append(issues, Issue{Action: name, Code: "STALE_MAPPING", Detail: "action present in mapping.yaml but not in api.json"})
			continue
		}

		switch a.Status {
		case StatusMapped:
			if a.Command == "" {
				issues = append(issues, Issue{Action: name, Code: "MAPPED_MISSING_COMMAND", Detail: "mapped action requires a command"})
			}
			if a.Request != specAction.Input {
				issues = append(issues, Issue{Action: name, Code: "REQUEST_MISMATCH", Detail: fmt.Sprintf("mapping request=%s does not match api.json input=%s", a.Request, specAction.Input)})
			}
			if a.Response != specAction.Output {
				issues = append(issues, Issue{Action: name, Code: "RESPONSE_MISMATCH", Detail: fmt.Sprintf("mapping response=%s does not match api.json output=%s", a.Response, specAction.Output)})
			}
			if a.Request != "" && spec.Object(a.Request) == nil {
				issues = append(issues, Issue{Action: name, Code: "MISSING_REQUEST_OBJECT", Detail: fmt.Sprintf("request object %s not found", a.Request)})
			}
			if a.Response != "" && spec.Object(a.Response) == nil {
				issues = append(issues, Issue{Action: name, Code: "MISSING_RESPONSE_OBJECT", Detail: fmt.Sprintf("response object %s not found", a.Response)})
			}

			reqObj := spec.Object(a.Request)
			if reqObj != nil {
				// every spec member must be present in fields (unless disabled),
				// every mapping field must correspond to a spec member.
				specMembers := map[string]Member{}
				for _, m := range reqObj.Members {
					if m.Disabled {
						continue
					}
					specMembers[m.Name] = m
				}
				// Mapping is by-need: a member missing from a.Fields is
				// NOT an error. The generator falls back to the
				// canonical kebab-case flag derived from the member
				// name. Only mapping entries that reference a field
				// the spec does not declare are stale (drift).
				for fieldName := range a.Fields {
					if _, ok := specMembers[fieldName]; !ok {
						issues = append(issues, Issue{Action: name, Field: fieldName, Code: "STALE_FIELD", Detail: "mapping field does not exist in request object"})
					}
				}

				// alias / shorthand conflicts inside the same command,
				// considering both hand-written flag names AND the
				// generator's canonical kebab-case flag for each member
				// (whether or not the member has an explicit mapping).
				flagSeen := map[string]string{} // flagOrAlias -> sourceField
				shortSeen := map[string]string{}
				addFlag := func(flagName, fieldName, kind string) {
					if flagName == "" {
						return
					}
					if other, ok := flagSeen[flagName]; ok && other != fieldName {
						issues = append(issues, Issue{Action: name, Field: fieldName, Code: "FLAG_CONFLICT", Detail: fmt.Sprintf("%s --%s conflicts with field %s", kind, flagName, other)})
						return
					}
					flagSeen[flagName] = fieldName
				}
				// Reserve every spec member's canonical kebab-case flag
				// first; an explicit override or alias that collides
				// with another member's canonical flag will then trip
				// FLAG_CONFLICT.
				for memberName := range specMembers {
					fm := a.Fields[memberName]
					if fm != nil && fm.Excluded {
						continue
					}
					addFlag(kebabCaseField(memberName), memberName, "generated flag")
				}
				for fieldName, fm := range a.Fields {
					if fm.Excluded {
						if fm.Flag != "" || fm.Shorthand != "" || len(fm.Aliases) > 0 || fm.Positional {
							issues = append(issues, Issue{Action: name, Field: fieldName, Code: "EXCLUDED_WITH_FLAG", Detail: "excluded fields cannot also declare flag/shorthand/aliases/positional"})
						}
						continue
					}
					for _, in := range fm.Inputs {
						if in.Flag == "" {
							issues = append(issues, Issue{Action: name, Field: fieldName, Code: "INPUT_MISSING_FLAG", Detail: "input requires flag"})
						}
						if in.Type != "" && !supportedInputType(in.Type) {
							issues = append(issues, Issue{Action: name, Field: fieldName, Code: "INVALID_INPUT_TYPE", Detail: fmt.Sprintf("unsupported input type %q", in.Type)})
						}
						addFlag(in.Flag, fieldName, "input")
						for _, al := range in.Aliases {
							addFlag(al, fieldName, "input alias")
						}
						shorthand := in.Shorthand
						if shorthand != "" {
							if other, ok := shortSeen[shorthand]; ok && other != fieldName {
								issues = append(issues, Issue{Action: name, Field: fieldName, Code: "SHORTHAND_CONFLICT", Detail: fmt.Sprintf("-%s conflicts with field %s", shorthand, other)})
							} else {
								shortSeen[shorthand] = fieldName
							}
						}
					}
					if fm.Flag != "" && fm.Flag != kebabCaseField(fieldName) {
						addFlag(fm.Flag, fieldName, "flag")
					}
					// Soft warning: a mapping entry that explicitly
					// declares the same flag the generator would derive
					// adds nothing but visual noise. Surface it so
					// maintainers can clean it up; do NOT fail CI.
					if fm.Flag != "" && fm.Flag == kebabCaseField(fieldName) {
						issues = append(issues, Issue{
							Action: name, Field: fieldName,
							Code:     "OVERRIDE_FLAG_EQUALS_DEFAULT",
							Detail:   fmt.Sprintf("explicit flag %q is identical to the generator default; remove the override", fm.Flag),
							Severity: SeverityWarning,
						})
					}
					for _, al := range fm.Aliases {
						addFlag(al, fieldName, "alias")
					}
					if fm.Shorthand != "" {
						if other, ok := shortSeen[fm.Shorthand]; ok && other != fieldName {
							issues = append(issues, Issue{Action: name, Field: fieldName, Code: "SHORTHAND_CONFLICT", Detail: fmt.Sprintf("-%s conflicts with field %s", fm.Shorthand, other)})
						} else {
							shortSeen[fm.Shorthand] = fieldName
						}
					}
				}
			}

		case StatusRawOnly, StatusDeferredOnly:
			if strings.TrimSpace(a.Reason) == "" {
				issues = append(issues, Issue{Action: name, Code: "MISSING_REASON", Detail: fmt.Sprintf("status=%s requires a reason", a.Status)})
			}
			if a.Command != "" {
				issues = append(issues, Issue{Action: name, Code: "UNEXPECTED_COMMAND", Detail: fmt.Sprintf("status=%s must not have a command", a.Status)})
			}
			// raw_only / deferred entries that document a request/response
			// reference must still match api.json so a renamed object does
			// not silently break `agr api call` callers or future mapping.
			if a.Request != "" {
				if a.Request != specAction.Input {
					issues = append(issues, Issue{Action: name, Code: "REQUEST_MISMATCH", Detail: fmt.Sprintf("mapping request=%s does not match api.json input=%s", a.Request, specAction.Input)})
				}
				if spec.Object(a.Request) == nil {
					issues = append(issues, Issue{Action: name, Code: "MISSING_REQUEST_OBJECT", Detail: fmt.Sprintf("request object %s not found", a.Request)})
				}
			}
			if a.Response != "" {
				if a.Response != specAction.Output {
					issues = append(issues, Issue{Action: name, Code: "RESPONSE_MISMATCH", Detail: fmt.Sprintf("mapping response=%s does not match api.json output=%s", a.Response, specAction.Output)})
				}
				if spec.Object(a.Response) == nil {
					issues = append(issues, Issue{Action: name, Code: "MISSING_RESPONSE_OBJECT", Detail: fmt.Sprintf("response object %s not found", a.Response)})
				}
			}

		default:
			issues = append(issues, Issue{Action: name, Code: "INVALID_STATUS", Detail: fmt.Sprintf("unknown status: %q (allowed: mapped, raw_only, deferred_with_reason)", a.Status)})
		}
	}

	return issues
}

func supportedInputType(t string) bool {
	switch t {
	case "string", "bool", "int", "int64", "integer", "json", "string_array", "enum":
		return true
	}
	return false
}
