// Package apimeta turns the parsed Spec + Mapping into generated Go metadata
// models and on-demand maintainer reports.
package apimeta

import (
	"sort"
	"strings"
)

// CatalogFile is the serialized runtime metadata embedded by
// generated_model.go. It carries two facts: Actions (per-action mapping
// state and CLI projection) and Objects (request / response / nested
// object structure). Derived views are computed from this model.
type CatalogFile struct {
	APIVersion string                   `json:"ApiVersion"`
	Service    string                   `json:"Service,omitempty"`
	Actions    []CatalogActionEntry     `json:"Actions"`
	Objects    map[string]CatalogObject `json:"Objects"`
}

// CatalogActionEntry summarises an action's mapping state and CLI
// projection. raw_only / deferred entries carry only Status + Reason.
//
// CLI is the action -> command projection: the resource command, plus
// per-field overrides (flag / shorthand / aliases / positional /
// excluded) that the generator records for each spec member touched
// by the mapping. Members not listed here are derived purely from the
// spec at runtime via apispec.KebabCase.
type CatalogActionEntry struct {
	Action   string                `json:"Action"`
	Status   string                `json:"Status"`
	Command  string                `json:"Command,omitempty"`
	Request  string                `json:"Request,omitempty"`
	Response string                `json:"Response,omitempty"`
	Reason   string                `json:"Reason,omitempty"`
	CLI      *CatalogCLIProjection `json:"CLI,omitempty"`
}

// CatalogCLIProjection captures the command/field metadata for a
// mapped action.
type CatalogCLIProjection struct {
	Command string                       `json:"Command"`
	Fields  map[string]CatalogFieldEntry `json:"Fields,omitempty"`
}

// CatalogFieldEntry is the catalog-level view of a mapping override
// for one request member. Empty / zero values indicate "no override".
type CatalogFieldEntry struct {
	Flag       string              `json:"Flag,omitempty"`
	Shorthand  string              `json:"Shorthand,omitempty"`
	Aliases    []string            `json:"Aliases,omitempty"`
	Parser     string              `json:"Parser,omitempty"`
	Inputs     []CatalogInputEntry `json:"Inputs,omitempty"`
	Positional bool                `json:"Positional,omitempty"`
	Excluded   bool                `json:"Excluded,omitempty"`
}

// CatalogInputEntry is the catalog-level view of one CLI input override.
type CatalogInputEntry struct {
	Flag      string   `json:"Flag,omitempty"`
	Type      string   `json:"Type,omitempty"`
	Form      string   `json:"Form,omitempty"`
	Default   string   `json:"Default,omitempty"`
	Shorthand string   `json:"Shorthand,omitempty"`
	Aliases   []string `json:"Aliases,omitempty"`
}

// CatalogObject is the catalog-level view of a request/response/nested
// object. Members preserve the spec ordering so derived schema views
// stay deterministic.
type CatalogObject struct {
	Document string          `json:"Document,omitempty"`
	Members  []CatalogMember `json:"Members,omitempty"`
}

// CatalogMember mirrors apispec.Member but with stable JSON keys.
type CatalogMember struct {
	Name     string `json:"Name"`
	Type     string `json:"Type,omitempty"`
	Member   string `json:"Member,omitempty"`
	Required bool   `json:"Required,omitempty"`
	Document string `json:"Document,omitempty"`
}

// BuildCatalog assembles the unified runtime catalog. Per NextPlan
// §9.2 only `actions` and `objects` are persisted; all other views
// are derived at runtime.
func BuildCatalog(spec *Spec, mapping *Mapping) *CatalogFile {
	cat := &CatalogFile{
		APIVersion: mapping.APIVersion,
		Objects:    map[string]CatalogObject{},
	}

	for _, n := range mapping.SortedActionNames() {
		a := mapping.Actions[n]
		entry := CatalogActionEntry{
			Action:   n,
			Status:   a.Status,
			Command:  a.Command,
			Request:  a.Request,
			Response: a.Response,
			Reason:   a.Reason,
		}
		if a.Status == StatusMapped {
			entry.CLI = &CatalogCLIProjection{Command: a.Command}
			if len(a.Fields) > 0 {
				fields := map[string]CatalogFieldEntry{}
				for name, fm := range a.Fields {
					if fm == nil {
						continue
					}
					fields[name] = CatalogFieldEntry{
						Flag:       fm.Flag,
						Shorthand:  fm.Shorthand,
						Aliases:    append([]string(nil), fm.Aliases...),
						Parser:     fm.Parser,
						Inputs:     catalogInputs(fm.Inputs),
						Positional: fm.Positional,
						Excluded:   fm.Excluded,
					}
				}
				if len(fields) > 0 {
					entry.CLI.Fields = fields
				}
			}
		}
		cat.Actions = append(cat.Actions, entry)
	}

	// Persist every spec object (request, response, nested). Runtime
	// views walk this map to derive schemas / flags / skeletons.
	for _, name := range spec.SortedObjectNames() {
		obj := spec.Object(name)
		if obj == nil {
			continue
		}
		members := make([]CatalogMember, 0, len(obj.Members))
		for _, m := range obj.Members {
			if m.Disabled {
				continue
			}
			members = append(members, CatalogMember{
				Name:     m.Name,
				Type:     m.Type,
				Member:   m.Member,
				Required: m.Required,
				Document: stripTags(m.Document),
			})
		}
		cat.Objects[name] = CatalogObject{
			Document: stripTags(obj.Document),
			Members:  members,
		}
	}

	return cat
}

func catalogInputs(inputs []InputMapping) []CatalogInputEntry {
	if len(inputs) == 0 {
		return nil
	}
	out := make([]CatalogInputEntry, 0, len(inputs))
	for _, in := range inputs {
		out = append(out, CatalogInputEntry{
			Flag:      in.Flag,
			Type:      in.Type,
			Form:      in.Form,
			Default:   in.Default,
			Shorthand: in.Shorthand,
			Aliases:   append([]string(nil), in.Aliases...),
		})
	}
	return out
}

// CoverageReport is the structured coverage info the CI gate consumes.
type CoverageReport struct {
	APIVersion       string                  `json:"ApiVersion"`
	TotalActions     int                     `json:"TotalActions"`
	MappedActions    int                     `json:"MappedActions"`
	RawOnlyActions   int                     `json:"RawOnlyActions"`
	DeferredActions  int                     `json:"DeferredActions"`
	Actions          []CoverageActionEntry   `json:"Actions"`
	UnmappedActions  []string                `json:"UnmappedActions"`
	StaleMappings    []string                `json:"StaleMappings"`
	GeneratedFlagsBy map[string]int          `json:"GeneratedFlagsBy"`
	UnknownFields    []CoverageFieldDeficit  `json:"UnknownFields"`
	MissingFields    []CoverageFieldDeficit  `json:"MissingFields"`
	ConflictHints    []CoverageConflictEntry `json:"ConflictHints"`
}

// CoverageActionEntry is one row of the coverage report.
type CoverageActionEntry struct {
	Action   string `json:"Action"`
	Status   string `json:"Status"`
	Command  string `json:"Command,omitempty"`
	Request  string `json:"Request,omitempty"`
	Response string `json:"Response,omitempty"`
	Reason   string `json:"Reason,omitempty"`
}

// CoverageFieldDeficit describes a member that is in api.json but not in
// mapping (or vice versa).
type CoverageFieldDeficit struct {
	Action string `json:"Action"`
	Field  string `json:"Field"`
}

// CoverageConflictEntry describes an alias / shorthand conflict.
type CoverageConflictEntry struct {
	Action string `json:"Action"`
	Field  string `json:"Field"`
	Detail string `json:"Detail"`
}

// BuildCoverage assembles the coverage report (exported so the api
// command can render it without writing to disk).
func BuildCoverage(spec *Spec, mapping *Mapping) *CoverageReport {
	return buildCoverage(spec, mapping)
}

func buildCoverage(spec *Spec, mapping *Mapping) *CoverageReport {
	rep := &CoverageReport{
		APIVersion:       mapping.APIVersion,
		TotalActions:     len(spec.Actions),
		GeneratedFlagsBy: map[string]int{},
	}
	for _, n := range spec.SortedActionNames() {
		entry := CoverageActionEntry{Action: n}
		if a, ok := mapping.Action(n); ok {
			entry.Status = a.Status
			entry.Command = a.Command
			entry.Request = a.Request
			entry.Response = a.Response
			entry.Reason = a.Reason
			switch a.Status {
			case StatusMapped:
				rep.MappedActions++
				rep.GeneratedFlagsBy[a.Command] = len(a.Fields)
			case StatusRawOnly:
				rep.RawOnlyActions++
			case StatusDeferredOnly:
				rep.DeferredActions++
			}
		} else {
			entry.Status = "unknown"
			rep.UnmappedActions = append(rep.UnmappedActions, n)
		}
		rep.Actions = append(rep.Actions, entry)
	}
	for _, n := range mapping.SortedActionNames() {
		if _, ok := spec.Actions[n]; !ok {
			rep.StaleMappings = append(rep.StaleMappings, n)
		}
	}
	for _, issue := range mapping.Validate(spec) {
		switch issue.Code {
		case "MISSING_FIELD":
			rep.MissingFields = append(rep.MissingFields, CoverageFieldDeficit{Action: issue.Action, Field: issue.Field})
		case "STALE_FIELD":
			rep.UnknownFields = append(rep.UnknownFields, CoverageFieldDeficit{Action: issue.Action, Field: issue.Field})
		case "FLAG_CONFLICT", "SHORTHAND_CONFLICT":
			rep.ConflictHints = append(rep.ConflictHints, CoverageConflictEntry{Action: issue.Action, Field: issue.Field, Detail: issue.Detail})
		}
	}
	return rep
}

// FieldFlag represents a generated long flag for one request member.
//
// Source records whether the canonical long flag came from the spec
// kebab-case default (`spec-default`) or a mapping override
// (`mapping-override`). Excluded == true means the field intentionally
// has no cobra flag; it is still accepted via `--request` JSON.
type FieldFlag struct {
	Action      string   `json:"Action"`
	Command     string   `json:"Command"`
	Field       string   `json:"Field"`
	Flag        string   `json:"Flag"`
	Shorthand   string   `json:"Shorthand,omitempty"`
	Aliases     []string `json:"Aliases,omitempty"`
	Type        string   `json:"Type"`
	Form        string   `json:"Form,omitempty"`
	Default     string   `json:"Default,omitempty"`
	Format      string   `json:"Format,omitempty"`
	Examples    []string `json:"Examples,omitempty"`
	Values      []string `json:"Values,omitempty"`
	NestedType  string   `json:"NestedType,omitempty"`
	Required    bool     `json:"Required"`
	Description string   `json:"Description"`
	Positional  bool     `json:"Positional,omitempty"`
	Source      string   `json:"Source"`
	Excluded    bool     `json:"Excluded,omitempty"`
}

// FlagsReport is the aggregate report of generated long flags.
type FlagsReport struct {
	APIVersion string      `json:"ApiVersion"`
	Flags      []FieldFlag `json:"Flags"`
}

// BuildFlags exposes flag generation for runtime consumption.
func BuildFlags(spec *Spec, mapping *Mapping) *FlagsReport {
	return buildRequestFlags(spec, mapping)
}

func buildRequestFlags(spec *Spec, mapping *Mapping) *FlagsReport {
	rep := &FlagsReport{APIVersion: mapping.APIVersion}
	for _, n := range mapping.MappedActionNames() {
		a := mapping.Actions[n]
		obj := spec.Object(a.Request)
		if obj == nil {
			continue
		}
		// Iterate the spec member list (not the mapping fields) so
		// every API member is represented, even when mapping does not
		// list it. mapping.fields acts as a sparse override.
		for _, m := range obj.Members {
			if m.Disabled {
				continue
			}
			fm := a.Fields[m.Name]
			inputs := fieldInputs(m, fm)
			for _, in := range inputs {
				if in.Flag == "" {
					continue
				}
				source := "spec-default"
				if fm != nil {
					source = "mapping-override"
				}
				desc := FieldDescription(a.Command, m.Name, stripTags(m.Document))
				inputHelp := InputHelpFor(a.Command, m.Name, in.Flag, desc)
				usage := inputHelp.Usage
				if usage == "" {
					usage = desc
				}
				rep.Flags = append(rep.Flags, FieldFlag{
					Action:      n,
					Command:     a.Command,
					Field:       m.Name,
					Flag:        in.Flag,
					Shorthand:   in.Shorthand,
					Aliases:     append([]string(nil), in.Aliases...),
					Type:        schemaInputType(m, in),
					Form:        in.Form,
					Default:     in.Default,
					Format:      inputHelp.Format,
					Examples:    append([]string(nil), inputHelp.Examples...),
					Values:      append([]string(nil), inputHelp.Values...),
					NestedType:  m.Member,
					Required:    m.Required,
					Description: usage,
					Positional:  fm != nil && fm.Positional && in.Flag == KebabCase(m.Name),
					Source:      source,
					Excluded:    fm != nil && fm.Excluded,
				})
			}
		}
	}
	sort.Slice(rep.Flags, func(i, j int) bool {
		if rep.Flags[i].Command != rep.Flags[j].Command {
			return rep.Flags[i].Command < rep.Flags[j].Command
		}
		return rep.Flags[i].Field < rep.Flags[j].Field
	})
	return rep
}

func fieldInputs(m Member, fm *FieldMapping) []InputMapping {
	canonical := KebabCase(m.Name)
	if fm != nil && len(fm.Inputs) > 0 {
		out := make([]InputMapping, 0, len(fm.Inputs))
		for _, in := range fm.Inputs {
			if in.Flag == "" {
				in.Flag = canonical
			}
			if in.Type == "" {
				in.Type = ScalarFlagType(m)
			}
			out = append(out, in)
		}
		return out
	}
	in := InputMapping{Flag: canonical, Type: ScalarFlagType(m)}
	if fm != nil {
		if fm.Flag != "" && fm.Flag != canonical {
			in.Flag = fm.Flag
			in.Aliases = append(in.Aliases, canonical)
		}
		in.Shorthand = fm.Shorthand
		seen := map[string]bool{in.Flag: true}
		for _, al := range in.Aliases {
			seen[al] = true
		}
		for _, al := range fm.Aliases {
			if al == "" || seen[al] {
				continue
			}
			in.Aliases = append(in.Aliases, al)
			seen[al] = true
		}
	}
	return []InputMapping{in}
}

func schemaInputType(m Member, in InputMapping) string {
	switch in.Type {
	case "string_array":
		return "string_array"
	case "enum":
		return "enum"
	case "json":
		return "json"
	case "bool":
		return "bool"
	case "int", "integer":
		return "integer"
	case "int64":
		return "integer"
	case "string":
		return "string"
	}
	return ScalarFlagType(m)
}

// SkeletonReport stores per-command request skeletons.
type SkeletonReport struct {
	APIVersion string                    `json:"ApiVersion"`
	Skeletons  map[string]map[string]any `json:"Skeletons"` // command -> example payload
}

// BuildSkeletons exposes skeleton generation.
func BuildSkeletons(spec *Spec, mapping *Mapping) *SkeletonReport {
	return buildRequestSkeletons(spec, mapping)
}

func buildRequestSkeletons(spec *Spec, mapping *Mapping) *SkeletonReport {
	rep := &SkeletonReport{APIVersion: mapping.APIVersion, Skeletons: map[string]map[string]any{}}
	for _, n := range mapping.MappedActionNames() {
		a := mapping.Actions[n]
		obj := spec.Object(a.Request)
		if obj == nil {
			continue
		}
		rep.Skeletons[a.Command] = skeletonForObject(spec, obj, 0)
	}
	return rep
}

func skeletonForObject(spec *Spec, obj *Object, depth int) map[string]any {
	out := map[string]any{}
	if depth > 4 {
		return out
	}
	for _, m := range obj.Members {
		if m.Disabled {
			continue
		}
		out[m.Name] = skeletonValue(spec, m, depth)
	}
	return out
}

func skeletonValue(spec *Spec, m Member, depth int) any {
	switch strings.ToLower(m.Type) {
	case "string":
		return ""
	case "bool":
		return false
	case "int", "int64", "uint", "uint64":
		return 0
	case "float", "double":
		return 0.0
	case "list":
		if IsScalar(m.Member) {
			return []any{}
		}
		nested := spec.Object(m.Member)
		if nested == nil {
			return []any{}
		}
		return []any{skeletonForObject(spec, nested, depth+1)}
	case "object":
		nested := spec.Object(m.Member)
		if nested == nil {
			return map[string]any{}
		}
		return skeletonForObject(spec, nested, depth+1)
	}
	return nil
}

// CommandSchemaReport is the machine-readable command metadata.
type CommandSchemaReport struct {
	APIVersion string                 `json:"ApiVersion"`
	Commands   []CommandSchemaEntry   `json:"Commands"`
	Actions    []CommandSchemaActions `json:"Actions"`
}

// CommandSchemaEntry describes one command (mapped action).
type CommandSchemaEntry struct {
	Command  string      `json:"Command"`
	Action   string      `json:"Action"`
	Request  string      `json:"Request"`
	Response string      `json:"Response"`
	Flags    []FieldFlag `json:"Flags"`
}

// CommandSchemaActions surfaces every api.json action and its mapping
// status (so the schema command can show raw_only and deferred entries).
type CommandSchemaActions struct {
	Action  string `json:"Action"`
	Status  string `json:"Status"`
	Command string `json:"Command,omitempty"`
	Reason  string `json:"Reason,omitempty"`
}

// BuildCommandSchemas exposes command schema generation.
func BuildCommandSchemas(spec *Spec, mapping *Mapping) *CommandSchemaReport {
	return buildCommandSchemas(spec, mapping)
}

func buildCommandSchemas(spec *Spec, mapping *Mapping) *CommandSchemaReport {
	flags := buildRequestFlags(spec, mapping).Flags
	flagsByCmd := map[string][]FieldFlag{}
	for _, f := range flags {
		flagsByCmd[f.Command] = append(flagsByCmd[f.Command], f)
	}

	rep := &CommandSchemaReport{APIVersion: mapping.APIVersion}
	for _, n := range mapping.MappedActionNames() {
		a := mapping.Actions[n]
		rep.Commands = append(rep.Commands, CommandSchemaEntry{
			Command:  a.Command,
			Action:   n,
			Request:  a.Request,
			Response: a.Response,
			Flags:    flagsByCmd[a.Command],
		})
	}
	for _, n := range spec.SortedActionNames() {
		a, ok := mapping.Action(n)
		if !ok {
			rep.Actions = append(rep.Actions, CommandSchemaActions{Action: n, Status: "unknown"})
			continue
		}
		rep.Actions = append(rep.Actions, CommandSchemaActions{
			Action:  n,
			Status:  a.Status,
			Command: a.Command,
			Reason:  a.Reason,
		})
	}
	return rep
}

func stripTags(s string) string {
	out := s
	for {
		i := strings.IndexByte(out, '<')
		if i < 0 {
			break
		}
		j := strings.IndexByte(out[i:], '>')
		if j < 0 {
			break
		}
		out = out[:i] + out[i+j+1:]
	}
	out = strings.ReplaceAll(out, "\u00a0", " ")
	out = strings.TrimSpace(out)
	return out
}
