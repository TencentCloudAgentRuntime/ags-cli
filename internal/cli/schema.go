package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	requestio "github.com/TencentCloudAgentRuntime/ags-cli/internal/cli/request"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/spf13/cobra"
)

func init() {
	schemaCmd.RunE = Wrap("schema", schemaFn)
	rootCmd.AddCommand(schemaCmd)
}

// CommandSchema describes a command's interface for machine consumption.
type CommandSchema struct {
	Name            string         `json:"Name"`
	Kind            string         `json:"Kind,omitempty"`
	ResolvedFrom    string         `json:"ResolvedFrom,omitempty"`
	Summary         string         `json:"Summary"`
	Aliases         []string       `json:"Aliases,omitempty"`
	Subcommands     []string       `json:"Subcommands,omitempty"`
	Mutation        bool           `json:"Mutation"`
	CreatesResource bool           `json:"CreatesResource"`
	Idempotency     string         `json:"Idempotency"`
	SupportsDryRun  bool           `json:"SupportsDryRun"`
	Interactive     bool           `json:"Interactive"`
	RequiresAuth    bool           `json:"RequiresAuth"`
	SupportsJson    bool           `json:"SupportsJson"`
	SupportsNdjson  bool           `json:"SupportsNdjson"`
	SupportsJq      bool           `json:"SupportsJq"`
	SupportsRequest bool           `json:"SupportsRequest"`
	RequestSchema   *RequestSchema `json:"RequestSchema"`
	Args            []ArgSchema    `json:"Args,omitempty"`
	Flags           []FlagSchema   `json:"Flags,omitempty"`
	Examples        []string       `json:"Examples,omitempty"`
	Output          string         `json:"Output,omitempty"`
	Failures        []string       `json:"Failures,omitempty"`
}

// RequestSchema describes the --request JSON schema.
type RequestSchema struct {
	Type                 string                    `json:"Type"`
	AdditionalProperties bool                      `json:"AdditionalProperties"`
	Required             []string                  `json:"Required,omitempty"`
	AnyOf                []RequirementSchema       `json:"AnyOf,omitempty"`
	Properties           map[string]PropertySchema `json:"Properties,omitempty"`
}

// RequirementSchema models a JSON-schema anyOf requirement entry for generated
// request schemas.
type RequirementSchema struct {
	Required []string `json:"Required,omitempty"`
}

// PropertySchema describes a property in a request schema.
type PropertySchema struct {
	Type    string   `json:"Type"`
	Minimum *int     `json:"Minimum,omitempty"`
	Values  []string `json:"Values,omitempty"`
	CliFlag *string  `json:"CliFlag"`
	Aliases []string `json:"Aliases,omitempty"`
}

// ArgSchema describes a positional argument.
type ArgSchema struct {
	Name      string `json:"Name"`
	Type      string `json:"Type"`
	Required  bool   `json:"Required"`
	Variadic  bool   `json:"Variadic,omitempty"`
	AfterDash bool   `json:"AfterDash,omitempty"`
}

// FlagSchema describes a flag.
type FlagSchema struct {
	Name             string   `json:"Name"`
	Shorthand        string   `json:"Shorthand,omitempty"`
	Type             string   `json:"Type"`
	Description      string   `json:"Description,omitempty"`
	Format           string   `json:"Format,omitempty"`
	Default          string   `json:"Default,omitempty"`
	Examples         []string `json:"Examples,omitempty"`
	Values           []string `json:"Values,omitempty"`
	IncompatibleWith []string `json:"IncompatibleWith,omitempty"`
	AllowsOutput     []string `json:"AllowsOutput,omitempty"`
}

var schemaCmd = &cobra.Command{
	Use:   "schema [command-name]",
	Short: "Show command schema for machine consumption",
	Long: `Show the schema of commands for machine consumption.

Without arguments, shows all command schemas.
With a command name (dot-separated), shows that command's schema.`,
	Example: exampleBlocks(
		"agr schema -o json",
		"agr schema instance.code.run -o json",
		"agr schema instance.create -o json",
	),
	Args: cobra.MaximumNArgs(1),
}

func schemaFn(cmd *cobra.Command, args []string) (*CmdResult, error) {
	catalog := buildSchemaCatalog(cmd.Root())

	if len(args) == 1 {
		if schema, ok := catalog.Lookup(cmd.Root(), args[0]); ok {
			return OK(schema, func(w io.Writer) { renderSchemaText(w, schema) }), nil
		}
		return nil, fmt.Errorf("unknown command: %s", args[0])
	}

	data := map[string]any{"Commands": catalog.Ordered, "ExitCodes": exitCodeTable()}
	return OK(data, func(w io.Writer) {
		fmt.Fprintf(w, "%-35s %s\n", "COMMAND", "SUMMARY")
		for _, s := range catalog.Ordered {
			fmt.Fprintf(w, "%-35s %s\n", s.Name, s.Summary)
		}
	}), nil
}

func renderSchemaText(w io.Writer, s CommandSchema) {
	fmt.Fprintf(w, "Command:         %s\n", s.Name)
	fmt.Fprintf(w, "Summary:         %s\n", s.Summary)
	if len(s.Aliases) > 0 {
		fmt.Fprintf(w, "Aliases:         %s\n", strings.Join(s.Aliases, ", "))
	}
	if s.Kind != "" {
		fmt.Fprintf(w, "Kind:            %s\n", s.Kind)
	}
	if s.ResolvedFrom != "" {
		fmt.Fprintf(w, "ResolvedFrom:    %s\n", s.ResolvedFrom)
	}
	fmt.Fprintf(w, "Mutation:        %v\n", s.Mutation)
	fmt.Fprintf(w, "CreatesResource: %v\n", s.CreatesResource)
	fmt.Fprintf(w, "Idempotency:     %s\n", s.Idempotency)
	fmt.Fprintf(w, "Interactive:     %v\n", s.Interactive)
	fmt.Fprintf(w, "RequiresAuth:    %v\n", s.RequiresAuth)
	fmt.Fprintf(w, "SupportsJson:    %v\n", s.SupportsJson)
	fmt.Fprintf(w, "SupportsNdjson:  %v\n", s.SupportsNdjson)
	fmt.Fprintf(w, "SupportsJq:      %v\n", s.SupportsJq)
	fmt.Fprintf(w, "SupportsRequest: %v\n", s.SupportsRequest)
	if len(s.Subcommands) > 0 {
		fmt.Fprintln(w, "\nSubcommands:")
		for _, subcommand := range s.Subcommands {
			fmt.Fprintf(w, "  %s\n", subcommand)
		}
	}
	if len(s.Args) > 0 {
		fmt.Fprintln(w, "\nArgs:")
		for _, a := range s.Args {
			req := ""
			if a.Required {
				req = " (required)"
			}
			fmt.Fprintf(w, "  %-20s %s%s\n", a.Name, a.Type, req)
		}
	}
	if len(s.Flags) > 0 {
		fmt.Fprintln(w, "\nFlags:")
		for _, f := range s.Flags {
			sh := ""
			if f.Shorthand != "" {
				sh = fmt.Sprintf(" (-%s)", f.Shorthand)
			}
			fmt.Fprintf(w, "  --%s%s  %s\n", f.Name, sh, f.Type)
			if f.Description != "" {
				fmt.Fprintf(w, "      %s\n", f.Description)
			}
			if f.Format != "" {
				fmt.Fprintf(w, "      Format: %s\n", f.Format)
			}
			if f.Default != "" {
				fmt.Fprintf(w, "      Default: %s\n", f.Default)
			}
			for _, ex := range f.Examples {
				fmt.Fprintf(w, "      Example: %s\n", ex)
			}
		}
	}
}

type schemaCatalog struct {
	Ordered []CommandSchema
	byName  map[string]CommandSchema
}

// Lookup resolves a command schema by canonical command ID or visible command
// path, recording ResolvedFrom when the input was an alias.
func (c schemaCatalog) Lookup(root *cobra.Command, name string) (CommandSchema, bool) {
	if schema, ok := c.byName[name]; ok {
		return schema, true
	}
	cmd, ok := findPublicCommandByID(root, name)
	if !ok {
		return CommandSchema{}, false
	}
	schema, ok := c.byName[canonicalCommandID(cmd)]
	if ok && name != "" && name != schema.Name {
		schema.ResolvedFrom = name
	}
	return schema, ok
}

func deriveCommandSchema(cmd *cobra.Command, commandID string) CommandSchema {
	schema := CommandSchema{
		Name:            commandID,
		Kind:            "command",
		Summary:         cmd.Short,
		Mutation:        false,
		CreatesResource: false,
		Idempotency:     "none",
		SupportsDryRun:  false,
		Interactive:     false,
		RequiresAuth:    false,
		SupportsJson:    true,
		SupportsNdjson:  false,
		SupportsJq:      true,
		SupportsRequest: false,
		Args:            []ArgSchema{},
		Flags:           []FlagSchema{},
	}
	if cmd.HasAvailableSubCommands() {
		schema.Kind = "group"
		schema.Output = "CommandGroup"
	} else if !cmd.Runnable() {
		schema.Kind = "command"
		schema.Output = "Help"
	}
	return schema
}

func findPublicCommandByID(root *cobra.Command, commandID string) (*cobra.Command, bool) {
	current := root
	for _, part := range strings.Split(commandID, ".") {
		found := false
		for _, child := range current.Commands() {
			if !isPublicCommand(child) {
				continue
			}
			if child.Name() == part || containsString(child.Aliases, part) {
				current = child
				found = true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	return current, true
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func cliFlag(name string) *string { return &name }

func getAllSchemas() []CommandSchema {
	schemas := registeredSchemaSeeds()
	schemas = mergeSchemaOverrides(schemas, buildHandwrittenSchemas())
	enrichSchemasFromGenerator(schemas)
	for i := range schemas {
		schemas[i] = finalizeSchema(schemas[i])
	}
	return schemas
}

func buildSchemaCatalog(root *cobra.Command) schemaCatalog {
	schemas := getAllSchemas()
	seededByName := map[string]CommandSchema{}
	for _, schema := range schemas {
		seededByName[schema.Name] = schema
	}

	catalog := schemaCatalog{byName: map[string]CommandSchema{}}
	seen := map[string]bool{}
	walkPublicCommands(root, func(cmd *cobra.Command) {
		commandID := canonicalCommandID(cmd)
		if commandID == "" || seen[commandID] {
			return
		}
		seen[commandID] = true

		schema, ok := seededByName[commandID]
		if !ok {
			schema = deriveCommandSchema(cmd, commandID)
		}
		schema.Name = commandID
		if cmd.Short != "" {
			schema.Summary = cmd.Short
		}
		schema.Aliases = append([]string(nil), cmd.Aliases...)
		schema.Subcommands = publicSubcommandIDs(cmd)
		if len(schema.Subcommands) > 0 {
			schema.Kind = "group"
			schema.Output = "CommandGroup"
		} else if schema.Kind == "" {
			schema.Kind = "command"
		}
		schema = finalizeSchema(schema)
		catalog.Ordered = append(catalog.Ordered, schema)
		catalog.byName[schema.Name] = schema
	})

	for _, schema := range schemas {
		if seen[schema.Name] {
			continue
		}
		schema = finalizeSchema(schema)
		catalog.Ordered = append(catalog.Ordered, schema)
		catalog.byName[schema.Name] = schema
	}

	return catalog
}

func finalizeSchema(schema CommandSchema) CommandSchema {
	if schema.Kind == "" {
		if len(schema.Subcommands) > 0 || schema.Output == "CommandGroup" {
			schema.Kind = "group"
		} else {
			schema.Kind = "command"
		}
	}
	if schema.RequiresAuth {
		schema.Failures = appendUniqueStrings(schema.Failures, "MISSING_CLOUD_CREDENTIALS", "AUTH_FAILED")
	}
	if schema.SupportsRequest {
		schema.Failures = appendUniqueStrings(schema.Failures, "REQUEST_FLAG_CONFLICT", "REQUEST_ARG_CONFLICT", "INVALID_REQUEST_JSON")
	}
	if hasFlagType(schema.Flags, "json") {
		schema.Failures = appendUniqueStrings(schema.Failures, "INVALID_JSON_FLAG")
	}
	if schema.SupportsJq {
		schema.Failures = appendUniqueStrings(schema.Failures, "INVALID_JQ_EXPRESSION")
	}
	return schema
}

func walkPublicCommands(root *cobra.Command, visit func(*cobra.Command)) {
	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, child := range parent.Commands() {
			if !isPublicCommand(child) {
				continue
			}
			visit(child)
			walk(child)
		}
	}
	walk(root)
}

func publicSubcommandIDs(cmd *cobra.Command) []string {
	var ids []string
	for _, child := range cmd.Commands() {
		if !isPublicCommand(child) {
			continue
		}
		ids = append(ids, canonicalCommandID(child))
	}
	return ids
}

func isPublicCommand(cmd *cobra.Command) bool {
	return cmd != nil && cmd.IsAvailableCommand() && !cmd.Hidden
}

var (
	registrySchemaSeedOrder []string
	registrySchemaSeedsByID = map[string]CommandSchema{}
)

func registerRegistrySchemaDescriptors(descriptors []command.Descriptor) {
	registrySchemaSeedOrder = registrySchemaSeedOrder[:0]
	registrySchemaSeedsByID = map[string]CommandSchema{}
	for _, desc := range descriptors {
		if desc.Spec.Hidden {
			continue
		}
		schema := schemaFromDescriptor(desc)
		registrySchemaSeedOrder = append(registrySchemaSeedOrder, schema.Name)
		registrySchemaSeedsByID[schema.Name] = schema
	}
}

func registeredSchemaSeeds() []CommandSchema {
	out := make([]CommandSchema, 0, len(registrySchemaSeedOrder))
	for _, id := range registrySchemaSeedOrder {
		out = append(out, cloneCommandSchema(registrySchemaSeedsByID[id]))
	}
	return out
}

func schemaFromDescriptor(desc command.Descriptor) CommandSchema {
	spec := desc.Spec
	mutation, createsResource, requiresAuth := schemaEffects(spec.Output.Effects)
	return CommandSchema{
		Name:            spec.ID,
		Kind:            "command",
		Summary:         spec.Short,
		Aliases:         append([]string(nil), spec.Aliases...),
		Mutation:        mutation,
		CreatesResource: createsResource,
		Idempotency:     "none",
		SupportsDryRun:  false,
		Interactive:     !spec.SupportsJSON && !spec.SupportsNDJSON,
		RequiresAuth:    requiresAuth,
		SupportsJson:    spec.SupportsJSON,
		SupportsNdjson:  spec.SupportsNDJSON,
		SupportsJq:      spec.SupportsJSON || spec.SupportsNDJSON,
		SupportsRequest: false,
		Args:            commandArgsToSchema(spec.Args),
		Flags:           commandFlagsToSchema(spec.Flags),
		Examples:        append([]string(nil), spec.Examples...),
		Output:          spec.Output.DataType,
	}
}

func schemaEffects(effects []string) (mutation, createsResource, requiresAuth bool) {
	if len(effects) > 0 {
		requiresAuth = true
	}
	for _, effect := range effects {
		kind, _, _ := strings.Cut(effect, ":")
		switch kind {
		case "create":
			mutation = true
			createsResource = true
		case "delete":
			mutation = true
		}
	}
	return mutation, createsResource, requiresAuth
}

func commandArgsToSchema(args []command.ArgSpec) []ArgSchema {
	out := make([]ArgSchema, 0, len(args))
	for _, arg := range args {
		out = append(out, ArgSchema{
			Name:     commandArgSchemaName(arg.Name),
			Type:     "string",
			Required: arg.Required,
			Variadic: arg.Repeatable,
		})
	}
	return out
}

func commandArgSchemaName(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == ' ' || r == '.'
	})
	if len(parts) == 0 {
		return ""
	}
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "")
}

func commandFlagsToSchema(flags []command.FlagSpec) []FlagSchema {
	out := make([]FlagSchema, 0, len(flags))
	for _, flag := range flags {
		if flag.Hidden {
			continue
		}
		schema := FlagSchema{
			Name:        flag.Name,
			Shorthand:   flag.Shorthand,
			Type:        schemaFlagType(flag.Type),
			Description: flag.Usage,
			Format:      flag.Format,
			Examples:    append([]string(nil), flag.Examples...),
			Values:      append([]string(nil), flag.Values...),
		}
		if flag.Default != nil {
			schema.Default = fmt.Sprint(flag.Default)
		}
		out = append(out, schema)
	}
	return out
}

func schemaFlagType(flagType command.FlagType) string {
	switch flagType {
	case command.FlagInt:
		return "integer"
	case command.FlagStringArray:
		return "string_array"
	default:
		return string(flagType)
	}
}

func mergeSchemaOverrides(base, overrides []CommandSchema) []CommandSchema {
	order := make([]string, 0, len(base)+len(overrides))
	merged := map[string]CommandSchema{}
	for _, schema := range base {
		order = append(order, schema.Name)
		merged[schema.Name] = cloneCommandSchema(schema)
	}
	for _, override := range overrides {
		current, ok := merged[override.Name]
		if ok {
			merged[override.Name] = mergeCommandSchema(current, override)
			continue
		}
		order = append(order, override.Name)
		merged[override.Name] = cloneCommandSchema(override)
	}
	out := make([]CommandSchema, 0, len(order))
	for _, name := range order {
		out = append(out, merged[name])
	}
	return out
}

func mergeCommandSchema(base, override CommandSchema) CommandSchema {
	merged := cloneCommandSchema(base)
	if override.Kind != "" {
		merged.Kind = override.Kind
	}
	if override.Summary != "" {
		merged.Summary = override.Summary
	}
	if len(override.Aliases) > 0 {
		merged.Aliases = appendUniqueStrings(merged.Aliases, override.Aliases...)
	}
	if len(override.Subcommands) > 0 {
		merged.Subcommands = append([]string(nil), override.Subcommands...)
	}
	merged.Mutation = merged.Mutation || override.Mutation
	merged.CreatesResource = merged.CreatesResource || override.CreatesResource
	if override.Idempotency != "" {
		merged.Idempotency = override.Idempotency
	}
	merged.SupportsDryRun = merged.SupportsDryRun || override.SupportsDryRun
	merged.Interactive = merged.Interactive || override.Interactive
	merged.RequiresAuth = merged.RequiresAuth || override.RequiresAuth
	merged.SupportsJson = merged.SupportsJson || override.SupportsJson
	merged.SupportsNdjson = merged.SupportsNdjson || override.SupportsNdjson
	merged.SupportsJq = merged.SupportsJq || override.SupportsJq
	merged.SupportsRequest = merged.SupportsRequest || override.SupportsRequest
	if override.RequestSchema != nil {
		merged.RequestSchema = cloneRequestSchema(override.RequestSchema)
	}
	if len(override.Args) > 0 {
		merged.Args = cloneArgSchemas(override.Args)
	}
	if len(override.Flags) > 0 {
		merged.Flags = mergeFlagSchemas(merged.Flags, override.Flags)
	}
	if len(override.Examples) > 0 {
		merged.Examples = append([]string(nil), override.Examples...)
	}
	if override.Output != "" {
		merged.Output = override.Output
	}
	merged.Failures = appendUniqueStrings(merged.Failures, override.Failures...)
	return merged
}

func mergeFlagSchemas(base, overrides []FlagSchema) []FlagSchema {
	merged := append([]FlagSchema(nil), base...)
	index := map[string]int{}
	for i, flag := range merged {
		index[flag.Name] = i
	}
	for _, override := range overrides {
		if idx, ok := index[override.Name]; ok {
			merged[idx] = mergeFlagSchema(merged[idx], override)
			continue
		}
		index[override.Name] = len(merged)
		merged = append(merged, override)
	}
	return merged
}

func mergeFlagSchema(base, override FlagSchema) FlagSchema {
	merged := base
	if override.Shorthand != "" {
		merged.Shorthand = override.Shorthand
	}
	if override.Type != "" {
		merged.Type = override.Type
	}
	if override.Description != "" {
		merged.Description = override.Description
	}
	if override.Format != "" {
		merged.Format = override.Format
	}
	if override.Default != "" {
		merged.Default = override.Default
	}
	if len(override.Examples) > 0 {
		merged.Examples = append([]string(nil), override.Examples...)
	}
	if len(override.Values) > 0 {
		merged.Values = append([]string(nil), override.Values...)
	}
	if len(override.IncompatibleWith) > 0 {
		merged.IncompatibleWith = append([]string(nil), override.IncompatibleWith...)
	}
	if len(override.AllowsOutput) > 0 {
		merged.AllowsOutput = append([]string(nil), override.AllowsOutput...)
	}
	return merged
}

func cloneCommandSchema(in CommandSchema) CommandSchema {
	out := in
	out.Aliases = append([]string(nil), in.Aliases...)
	out.Subcommands = append([]string(nil), in.Subcommands...)
	out.Args = cloneArgSchemas(in.Args)
	out.Flags = append([]FlagSchema(nil), in.Flags...)
	out.Examples = append([]string(nil), in.Examples...)
	out.Failures = append([]string(nil), in.Failures...)
	out.RequestSchema = cloneRequestSchema(in.RequestSchema)
	return out
}

func cloneArgSchemas(in []ArgSchema) []ArgSchema {
	out := make([]ArgSchema, len(in))
	copy(out, in)
	return out
}

func cloneRequestSchema(in *RequestSchema) *RequestSchema {
	if in == nil {
		return nil
	}
	out := *in
	out.Required = append([]string(nil), in.Required...)
	out.AnyOf = append([]RequirementSchema(nil), in.AnyOf...)
	if in.Properties != nil {
		out.Properties = map[string]PropertySchema{}
		for key, prop := range in.Properties {
			copied := prop
			copied.Values = append([]string(nil), prop.Values...)
			copied.Aliases = append([]string(nil), prop.Aliases...)
			out.Properties[key] = copied
		}
	}
	return &out
}

// enrichSchemasFromGenerator overlays generator-derived flag and
// request-schema metadata onto the hand-written `agr schema` table so
// the schema output for a resource command reflects mapping.yaml +
// api.json instead of going stale. Hand-written aliases / shorthand
// stay authoritative; canonical long flag, api documentation, and the
// request schema's Required list are pulled in from the apimeta.
func enrichSchemasFromGenerator(schemas []CommandSchema) {
	cat, err := apimeta.Get()
	if err != nil {
		return
	}
	flags := apimeta.BuildFlags(cat.Spec, cat.Mapping)
	flagsByCommand := map[string][]apimeta.FieldFlag{}
	for _, f := range flags.Flags {
		flagsByCommand[f.Command] = append(flagsByCommand[f.Command], f)
	}
	requiredByCommand := map[string][]string{}
	for _, action := range cat.Mapping.MappedActionNames() {
		a := cat.Mapping.Actions[action]
		obj := cat.Spec.Object(a.Request)
		if obj == nil {
			continue
		}
		var req []string
		for _, m := range obj.Members {
			if m.Disabled {
				continue
			}
			if m.Required {
				req = append(req, m.Name)
			}
		}
		requiredByCommand[a.Command] = req
	}
	for i := range schemas {
		schema := &schemas[i]
		generated, ok := flagsByCommand[schema.Name]
		if ok {
			existing := map[string]int{}
			for j, fl := range schema.Flags {
				existing[fl.Name] = j
			}
			for _, gf := range generated {
				// Excluded fields per NextPlan §8: still accepted via
				// --request, but not registered as a cobra flag and not
				// surfaced in `agr schema`.
				if gf.Excluded || gf.Positional {
					continue
				}
				idx, found := existing[gf.Flag]
				fl := FlagSchema{
					Name:        gf.Flag,
					Shorthand:   gf.Shorthand,
					Type:        gf.Type,
					Description: gf.Description,
					Format:      gf.Format,
					Examples:    append([]string(nil), gf.Examples...),
					Values:      append([]string(nil), gf.Values...),
				}
				if found {
					prev := schema.Flags[idx]
					if fl.Description == "" {
						fl.Description = prev.Description
					}
					if fl.Shorthand == "" {
						fl.Shorthand = prev.Shorthand
					}
					fl.Default = prev.Default
					if len(fl.Examples) == 0 {
						fl.Examples = prev.Examples
					}
					if len(fl.Values) == 0 {
						fl.Values = prev.Values
					}
					fl.IncompatibleWith = prev.IncompatibleWith
					fl.AllowsOutput = prev.AllowsOutput
					if fl.Format == "" {
						fl.Format = prev.Format
					}
					schema.Flags[idx] = fl
					continue
				}
				schema.Flags = append(schema.Flags, fl)
			}
		}
		// Mark every mapped command with a non-empty request object as
		// `SupportsRequest=true` and synthesise a RequestSchema from
		// the apimeta. Hand-written entries still win when present.
		for _, action := range cat.Mapping.MappedActionNames() {
			a := cat.Mapping.Actions[action]
			if a.Command != schema.Name {
				continue
			}
			obj := cat.Spec.Object(a.Request)
			if obj == nil {
				continue
			}
			hasField := false
			for _, m := range obj.Members {
				if !m.Disabled {
					hasField = true
					break
				}
			}
			if !hasField {
				continue
			}
			schema.SupportsRequest = true
			if schema.RequestSchema == nil {
				schema.RequestSchema = &RequestSchema{Type: "object", AdditionalProperties: false}
			}
			if schema.RequestSchema.Properties == nil {
				schema.RequestSchema.Properties = map[string]PropertySchema{}
			}
			for _, m := range obj.Members {
				if m.Disabled {
					continue
				}
				prop, exists := schema.RequestSchema.Properties[m.Name]
				if !exists {
					prop = PropertySchema{Type: m.Type}
				}
				if prop.Type == "" {
					prop.Type = m.Type
				}
				flagName := propertyCLIFlag(m.Name, a.Fields[m.Name])
				if flagName != "" {
					prop.CliFlag = cliFlag(flagName)
				}
				if fm, ok := a.Fields[m.Name]; ok {
					prop.Aliases = append([]string(nil), fm.Aliases...)
					if fm.Flag != "" && fm.Flag != flagName && !containsString(prop.Aliases, fm.Flag) {
						prop.Aliases = append(prop.Aliases, fm.Flag)
					}
				}
				schema.RequestSchema.Properties[m.Name] = prop
			}
		}
		if req, ok := requiredByCommand[schema.Name]; ok && schema.RequestSchema != nil {
			schema.RequestSchema.Required = req
		}
		if schema.SupportsRequest {
			ensureSchemaFlag(schema, FlagSchema{Name: "request", Type: "string"})
			if supports, ok := requestio.SupportsGeneratedSkeleton(schema.Name); !ok || supports {
				ensureSchemaFlag(schema, FlagSchema{Name: "generate-skeleton", Type: "bool"})
			}
		}
	}
}

func ensureSchemaFlag(schema *CommandSchema, flag FlagSchema) {
	for i := range schema.Flags {
		if schema.Flags[i].Name == flag.Name {
			existing := schema.Flags[i]
			if existing.Type == "" {
				existing.Type = flag.Type
			}
			if existing.Description == "" {
				existing.Description = flag.Description
			}
			if existing.Shorthand == "" {
				existing.Shorthand = flag.Shorthand
			}
			if existing.Format == "" {
				existing.Format = flag.Format
			}
			if existing.Default == "" {
				existing.Default = flag.Default
			}
			if len(existing.Examples) == 0 {
				existing.Examples = flag.Examples
			}
			if len(existing.Values) == 0 {
				existing.Values = flag.Values
			}
			if len(existing.IncompatibleWith) == 0 {
				existing.IncompatibleWith = flag.IncompatibleWith
			}
			if len(existing.AllowsOutput) == 0 {
				existing.AllowsOutput = flag.AllowsOutput
			}
			schema.Flags[i] = existing
			return
		}
	}
	schema.Flags = append(schema.Flags, flag)
}

func propertyCLIFlag(memberName string, fm *apimeta.FieldMapping) string {
	if fm != nil {
		if fm.Positional || fm.Excluded {
			return ""
		}
		if fm.Flag != "" {
			return fm.Flag
		}
	}
	return apimeta.KebabCase(memberName)
}

func hasFlagType(flags []FlagSchema, typ string) bool {
	for _, flag := range flags {
		if flag.Type == typ {
			return true
		}
	}
	return false
}

func buildHandwrittenSchemas() []CommandSchema {
	schemas := []CommandSchema{
		{
			Name: "instance.create", Summary: "Create a new sandbox instance",
			Mutation: true, CreatesResource: true,
			Idempotency: "client_token_cloud_only", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: true,
			RequestSchema: &RequestSchema{
				Type: "object", AdditionalProperties: false,
				AnyOf: []RequirementSchema{
					{Required: []string{"ToolName"}},
					{Required: []string{"ToolId"}},
				},
				Properties: map[string]PropertySchema{
					"ToolName":            {Type: "string", CliFlag: cliFlag("tool-name")},
					"ToolId":              {Type: "string", CliFlag: cliFlag("tool-id")},
					"Timeout":             {Type: "string", CliFlag: cliFlag("timeout")},
					"AuthMode":            {Type: "enum", Values: []string{"DEFAULT", "TOKEN", "NONE", "PUBLIC"}, CliFlag: cliFlag("auth-mode")},
					"MountOptions":        {Type: "array", CliFlag: cliFlag("mount-options")},
					"CustomConfiguration": {Type: "object", CliFlag: cliFlag("custom-configuration")},
					"Metadata":            {Type: "array", CliFlag: cliFlag("metadata")},
					"ClientToken":         {Type: "string", CliFlag: cliFlag("client-token")},
				},
			},
			Args: []ArgSchema{},
			Flags: []FlagSchema{
				{Name: "tool-name", Shorthand: "t", Type: "string"},
				{Name: "tool-id", Type: "string"},
				{Name: "timeout", Type: "string"},
				{Name: "mount-options", Type: "json"},
				{Name: "custom-configuration", Type: "json"},
				{Name: "metadata", Type: "json"},
				{Name: "auth-mode", Type: "enum", Values: []string{"DEFAULT", "TOKEN", "NONE", "PUBLIC"}},
				{Name: "client-token", Type: "string"},
				{Name: "request", Type: "string"},
				{Name: "generate-skeleton", Type: "bool"},
			},
			Output: "Instance", Failures: []string{"INVALID_TOOL", "CLIENT_TOKEN_CONFLICT", "CONFLICTING_FLAGS", "MISSING_REQUIRED_FLAG"},
		},
		{
			Name: "instance.list", Summary: "List sandbox instances",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{},
			Flags: []FlagSchema{
				{Name: "tool-id", Type: "string"},
				{Name: "filters", Type: "json"},
				{Name: "offset", Type: "integer"},
				{Name: "limit", Type: "integer"},
			},
			Output: "InstanceList", Failures: []string{"AUTH_FAILED", "INVALID_PAGINATION"},
		},
		{
			Name: "instance.get", Summary: "Get instance details",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "InstanceId", Type: "string", Required: true}},
			Output:          "Instance", Failures: []string{"INSTANCE_NOT_FOUND", "MISSING_REQUIRED_ARG"},
		},
		{
			Name: "instance.update", Summary: "Update a sandbox instance",
			Mutation: true, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: true,
			RequestSchema: &RequestSchema{Type: "object", AdditionalProperties: false, Properties: map[string]PropertySchema{
				"InstanceId": {Type: "string", CliFlag: nil},
				"Timeout":    {Type: "string", CliFlag: cliFlag("timeout")},
				"Metadata":   {Type: "array", CliFlag: cliFlag("metadata")},
			}},
			Args:     []ArgSchema{{Name: "InstanceId", Type: "string", Required: true}},
			Flags:    []FlagSchema{{Name: "timeout", Type: "string"}, {Name: "metadata", Type: "json"}, {Name: "request", Type: "string"}, {Name: "generate-skeleton", Type: "bool"}},
			Failures: []string{"MISSING_REQUIRED_ARG"},
		},
		{
			Name: "instance.pause", Summary: "Pause a sandbox instance",
			Mutation: true, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "InstanceId", Type: "string", Required: true, Variadic: true}},
			Failures:        []string{"MISSING_REQUIRED_ARG"},
		},
		{
			Name: "instance.resume", Summary: "Resume a sandbox instance",
			Mutation: true, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "InstanceId", Type: "string", Required: true}},
			Failures:        []string{"MISSING_REQUIRED_ARG"},
		},
		{
			Name: "instance.delete", Summary: "Delete sandbox instances",
			Mutation: true, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "InstanceId", Type: "string", Required: true}},
			Flags:           []FlagSchema{{Name: "ignore-not-found", Type: "bool"}},
			Output:          "DeleteResult", Failures: []string{"INSTANCE_NOT_FOUND", "MISSING_REQUIRED_ARG", "REQUEST_FLAG_CONFLICT", "PARTIAL_DELETE_FAILED"},
		},
		{
			Name: "instance.code.run", Summary: "Execute code in an existing or temporary sandbox instance",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: true, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "InstanceId", Type: "string", Required: false}},
			Flags: []FlagSchema{
				{Name: "code", Shorthand: "c", Type: "string"},
				{Name: "file", Shorthand: "f", Type: "string_array"},
				{Name: "language", Shorthand: "l", Type: "enum", Values: []string{"python", "javascript", "typescript", "r", "java", "bash"}},
				{Name: "stream", Shorthand: "s", Type: "bool", IncompatibleWith: []string{"output=json"}, AllowsOutput: []string{"text", "ndjson"}},
				{Name: "create-temp-instance", Type: "bool"},
				{Name: "cleanup", Type: "enum", Values: []string{"always", "success", "never"}, Default: "always"},
				{Name: "tool-name", Shorthand: "t", Type: "string"},
				{Name: "tool-id", Type: "string"},
			},
			Output: "RunResult", Failures: []string{"MISSING_INSTANCE", "REMOTE_CODE_FAILED", "CONFLICTING_INPUTS", "MISSING_CODE", "UNSUPPORTED_LANGUAGE", "CONFLICTING_FLAGS", "INVALID_CLEANUP", "MISSING_REQUIRED_FLAG"},
		},
		{
			Name: "instance.exec", Summary: "Execute command in an existing or temporary sandbox instance",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: true, SupportsJq: true,
			SupportsRequest: false,
			Args: []ArgSchema{
				{Name: "InstanceId", Type: "string", Required: false},
				{Name: "Command", Type: "string", Required: true, Variadic: true, AfterDash: true},
			},
			Flags: []FlagSchema{
				{Name: "stream", Shorthand: "s", Type: "bool", IncompatibleWith: []string{"output=json"}, AllowsOutput: []string{"text", "ndjson"}},
				{Name: "cwd", Type: "string"},
				{Name: "env", Type: "string_array"},
				{Name: "user", Type: "string"},
				{Name: "create-temp-instance", Type: "bool"},
				{Name: "cleanup", Type: "enum", Values: []string{"always", "success", "never"}, Default: "always"},
				{Name: "tool-name", Shorthand: "t", Type: "string"},
				{Name: "tool-id", Type: "string"},
			},
			Output: "ExecResult", Failures: []string{"MISSING_INSTANCE", "REMOTE_COMMAND_FAILED", "INVALID_ENV", "CONFLICTING_FLAGS", "INVALID_CLEANUP", "MISSING_REQUIRED_FLAG"},
		},
		{
			Name: "instance.file.upload", Summary: "Upload file to sandbox instance",
			Mutation: true, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args: []ArgSchema{
				{Name: "InstanceId", Type: "string", Required: true},
				{Name: "LocalPath", Type: "string", Required: true},
				{Name: "RemotePath", Type: "string", Required: true},
			},
			Flags:  []FlagSchema{{Name: "user", Type: "string"}},
			Output: "FileUploadResult", Failures: []string{"MISSING_INSTANCE", "INVALID_LOCAL_PATH"},
		},
		{
			Name: "instance.file.download", Summary: "Download file from sandbox instance",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args: []ArgSchema{
				{Name: "InstanceId", Type: "string", Required: true},
				{Name: "RemotePath", Type: "string", Required: true},
				{Name: "LocalPath", Type: "string", Required: true},
			},
			Flags:  []FlagSchema{{Name: "user", Type: "string"}},
			Output: "FileDownloadResult", Failures: []string{"MISSING_INSTANCE", "INVALID_LOCAL_PATH", "STDOUT_CONFLICT"},
		},
		{
			Name: "instance.login", Summary: "Login to instance via terminal",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: true,
			RequiresAuth: true, SupportsJson: false, SupportsNdjson: false, SupportsJq: false,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "InstanceId", Type: "string", Required: true}},
		},
		{
			Name: "instance.browser.vnc", Summary: "Show VNC URL for browser sandbox",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "InstanceId", Type: "string", Required: true}},
			Flags:           []FlagSchema{{Name: "port", Type: "integer"}},
			Output:          "BrowserUrls",
			Failures:        []string{"INVALID_PORT"},
		},
		{
			Name: "instance.proxy", Summary: "Forward a sandbox port to localhost",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: true,
			RequiresAuth: true, SupportsJson: false, SupportsNdjson: false, SupportsJq: false,
			SupportsRequest: false,
			Args: []ArgSchema{
				{Name: "InstanceId", Type: "string", Required: true},
				{Name: "PortSpec", Type: "string", Required: true},
			},
			Flags: []FlagSchema{
				{Name: "address", Type: "string"},
				{Name: "verbose", Type: "bool"},
			},
			Failures: []string{"INVALID_ADDRESS", "INVALID_PORT"},
		},
		{
			Name: "instance.mobile.connect", Summary: "Connect to mobile sandbox",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "InstanceId", Type: "string", Required: true}},
			Failures:        []string{"ADB_NOT_FOUND"},
		},
		{
			Name: "instance.mobile.disconnect", Summary: "Disconnect from mobile sandbox",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: false, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "InstanceId", Type: "string", Required: false}},
			Flags:           []FlagSchema{{Name: "all", Type: "bool"}},
			Failures:        []string{"NO_ACTIVE_TUNNEL"},
		},
		{
			Name: "instance.mobile.list", Summary: "List active mobile sandbox connections",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: false, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
		},
		{
			Name: "instance.mobile.adb", Summary: "Execute adb command on mobile sandbox",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: false, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args: []ArgSchema{
				{Name: "InstanceId", Type: "string", Required: true},
				{Name: "AdbArgs", Type: "string", Required: true, Variadic: true, AfterDash: true},
			},
			Failures: []string{"NO_ACTIVE_TUNNEL", "REMOTE_COMMAND_FAILED", "MISSING_SEPARATOR"},
		},
		{
			Name: "tool.create", Summary: "Create a new sandbox tool",
			Mutation: true, CreatesResource: true,
			Idempotency: "client_token", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: true,
			RequestSchema: &RequestSchema{
				Type: "object", AdditionalProperties: false,
				Required: []string{"ToolName", "ToolType"},
				Properties: map[string]PropertySchema{
					"ToolName":             {Type: "string", CliFlag: cliFlag("tool-name")},
					"ToolType":             {Type: "string", CliFlag: cliFlag("tool-type")},
					"Description":          {Type: "string", CliFlag: cliFlag("description")},
					"NetworkConfiguration": {Type: "object", CliFlag: cliFlag("network-configuration")},
					"Tags":                 {Type: "array", CliFlag: cliFlag("tags")},
					"StorageMounts":        {Type: "array", CliFlag: cliFlag("storage-mounts")},
					"CustomConfiguration":  {Type: "object", CliFlag: cliFlag("custom-configuration")},
					"LogConfiguration":     {Type: "object", CliFlag: cliFlag("log-configuration")},
					"Persistent":           {Type: "bool", CliFlag: cliFlag("persistent")},
					"ClientToken":          {Type: "string", CliFlag: cliFlag("client-token")},
					"DefaultTimeout":       {Type: "string", CliFlag: cliFlag("default-timeout")},
					"RoleArn":              {Type: "string", CliFlag: cliFlag("role-arn")},
				},
			},
			Flags: []FlagSchema{
				{Name: "tool-name", Shorthand: "n", Type: "string"},
				{Name: "tool-type", Shorthand: "t", Type: "string"},
				{Name: "description", Shorthand: "d", Type: "string"},
				{Name: "default-timeout", Type: "string"},
				{Name: "network-configuration", Type: "json"},
				{Name: "tags", Type: "json"},
				{Name: "role-arn", Type: "string"},
				{Name: "storage-mounts", Type: "json"},
				{Name: "custom-configuration", Type: "json"},
				{Name: "log-configuration", Type: "json"},
				{Name: "persistent", Type: "bool"},
				{Name: "client-token", Type: "string"},
				{Name: "request", Type: "string"},
				{Name: "generate-skeleton", Type: "bool"},
			},
			Output: "Tool", Failures: []string{"CLIENT_TOKEN_CONFLICT", "MISSING_REQUIRED_FLAG"},
		},
		{
			Name: "tool.fork", Summary: "Fork a sandbox tool",
			Mutation: true, CreatesResource: true,
			Idempotency: "client_token", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			RequestSchema: &RequestSchema{
				Type: "object", AdditionalProperties: false,
				Required: []string{"ToolName"},
				Properties: map[string]PropertySchema{
					"ToolName":             {Type: "string", CliFlag: cliFlag("tool-name")},
					"ToolType":             {Type: "string", CliFlag: cliFlag("tool-type")},
					"Description":          {Type: "string", CliFlag: cliFlag("description")},
					"NetworkConfiguration": {Type: "object", CliFlag: cliFlag("network-configuration")},
					"Tags":                 {Type: "array", CliFlag: cliFlag("tags")},
					"StorageMounts":        {Type: "array", CliFlag: cliFlag("storage-mounts")},
					"CustomConfiguration":  {Type: "object", CliFlag: cliFlag("custom-configuration")},
					"LogConfiguration":     {Type: "object", CliFlag: cliFlag("log-configuration")},
					"Persistent":           {Type: "bool", CliFlag: cliFlag("persistent")},
					"ClientToken":          {Type: "string", CliFlag: cliFlag("client-token")},
					"DefaultTimeout":       {Type: "string", CliFlag: cliFlag("default-timeout")},
					"RoleArn":              {Type: "string", CliFlag: cliFlag("role-arn")},
				},
			},
			Args: []ArgSchema{{Name: "SourceToolId", Type: "string", Required: true}},
			Flags: []FlagSchema{
				{Name: "tool-name", Shorthand: "n", Type: "string"},
				{Name: "tool-type", Shorthand: "t", Type: "string"},
				{Name: "description", Shorthand: "d", Type: "string"},
				{Name: "default-timeout", Type: "string"},
				{Name: "network-configuration", Type: "json"},
				{Name: "tags", Type: "json"},
				{Name: "role-arn", Type: "string"},
				{Name: "storage-mounts", Type: "json"},
				{Name: "custom-configuration", Type: "json"},
				{Name: "log-configuration", Type: "json"},
				{Name: "persistent", Type: "bool"},
				{Name: "client-token", Type: "string"},
			},
			Output:   "Tool",
			Failures: []string{"CLIENT_TOKEN_CONFLICT", "MISSING_REQUIRED_ARG", "MISSING_REQUIRED_FLAG", "TOOL_NOT_FOUND"},
		},
		{
			Name: "tool.list", Summary: "List sandbox tools",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Flags: []FlagSchema{
				{Name: "tool-ids", Type: "string_array"},
				{Name: "filters", Type: "json"},
				{Name: "offset", Type: "integer"},
				{Name: "limit", Type: "integer"},
			},
			Output:   "ToolList",
			Failures: []string{"INVALID_PAGINATION"},
		},
		{
			Name: "tool.get", Summary: "Get tool details",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "ToolId", Type: "string", Required: true}},
			Failures:        []string{"MISSING_REQUIRED_ARG", "TOOL_NOT_FOUND"},
		},
		{
			Name: "tool.update", Summary: "Update a sandbox tool",
			Mutation: true, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: true,
			RequestSchema: &RequestSchema{
				Type: "object", AdditionalProperties: false,
				Properties: map[string]PropertySchema{
					"ToolId":               {Type: "string", CliFlag: nil},
					"Description":          {Type: "string", CliFlag: cliFlag("description")},
					"NetworkConfiguration": {Type: "object", CliFlag: cliFlag("network-configuration")},
					"Tags":                 {Type: "array", CliFlag: cliFlag("tags")},
					"CustomConfiguration":  {Type: "object", CliFlag: cliFlag("custom-configuration")},
				},
			},
			Args: []ArgSchema{{Name: "ToolId", Type: "string", Required: true}},
			Flags: []FlagSchema{
				{Name: "description", Shorthand: "d", Type: "string"},
				{Name: "network-configuration", Type: "json"},
				{Name: "tags", Type: "json"},
				{Name: "custom-configuration", Type: "json"},
				{Name: "request", Type: "string"},
				{Name: "generate-skeleton", Type: "bool"},
			},
			Failures: []string{"MISSING_REQUIRED_FLAG"},
		},
		{
			Name: "tool.delete", Summary: "Delete sandbox tools",
			Mutation: true, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "ToolId", Type: "string", Required: true, Variadic: true}},
			Failures:        []string{"MISSING_REQUIRED_ARG", "REQUEST_FLAG_CONFLICT"},
		},
		{
			Name: "apikey.create", Summary: "Create a new API key",
			Mutation: true, CreatesResource: true,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Flags:           []FlagSchema{{Name: "name", Shorthand: "n", Type: "string"}},
			Failures:        []string{"MISSING_REQUIRED_FLAG"},
		},
		{
			Name: "apikey.list", Summary: "List API keys",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
		},
		{
			Name: "apikey.delete", Summary: "Delete an API key",
			Mutation: true, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "KeyId", Type: "string", Required: true}},
			Failures:        []string{"MISSING_REQUIRED_ARG"},
		},
		{
			Name: "pre-cache-image-task.create", Summary: "Create an image pre-cache task",
			Mutation: true, CreatesResource: true,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: true,
			RequestSchema: &RequestSchema{Type: "object", AdditionalProperties: false, Required: []string{"Image"}, Properties: map[string]PropertySchema{
				"Image":             {Type: "string", CliFlag: nil},
				"ImageRegistryType": {Type: "enum", Values: []string{"enterprise", "personal"}, CliFlag: nil},
			}},
			Flags: []FlagSchema{{Name: "request", Type: "string"}, {Name: "generate-skeleton", Type: "bool"}},
		},
		{
			Name: "pre-cache-image-task.get", Summary: "Describe an image pre-cache task",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: true,
			RequestSchema: &RequestSchema{Type: "object", AdditionalProperties: false, Properties: map[string]PropertySchema{
				"Image":             {Type: "string", CliFlag: nil},
				"ImageDigest":       {Type: "string", CliFlag: nil},
				"ImageRegistryType": {Type: "enum", Values: []string{"enterprise", "personal"}, CliFlag: nil},
			}},
			Args:  []ArgSchema{{Name: "ImageDigest", Type: "string", Required: true}},
			Flags: []FlagSchema{{Name: "request", Type: "string"}, {Name: "generate-skeleton", Type: "bool"}},
		},
		{
			Name: "api.call", Summary: "Send a raw API request",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: true, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args: []ArgSchema{
				{Name: "Action", Type: "string", Required: true},
			},
			Flags: []FlagSchema{
				{Name: "request", Type: "string", Description: "Raw API request body as JSON, @file, or - for stdin (required)"},
			},
			Output:   "RawAPIResponse",
			Failures: []string{"MISSING_ACTION", "MISSING_REQUIRED_FLAG", "INVALID_REQUEST_JSON"},
		},
		{
			Name: "config.path", Summary: "Print the configuration file path",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: false, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Output:          "ConfigPath",
			Failures:        []string{"INVALID_JQ_EXPRESSION"},
		},
		{
			Name: "config.show", Summary: "Show current configuration values and sources",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: false, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Output:          "ConfigStatus",
			Failures:        []string{"INVALID_JQ_EXPRESSION"},
		},
		{
			Name: "config.set", Summary: "Set a configuration value",
			Mutation: true, CreatesResource: false,
			Idempotency: "local_config", SupportsDryRun: false, Interactive: false,
			RequiresAuth: false, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args: []ArgSchema{
				{Name: "Key", Type: "string", Required: true},
				{Name: "Value", Type: "string", Required: true},
			},
			Output:   "ConfigWriteResult",
			Failures: []string{"INVALID_CONFIG_KEY", "INVALID_CONFIG", "CONFIG_WRITE_FAILED", "INVALID_JQ_EXPRESSION"},
		},
		{
			Name: "init", Summary: "Initialize local CLI configuration",
			Mutation: false, CreatesResource: false,
			Idempotency: "local_config", SupportsDryRun: false, Interactive: false,
			RequiresAuth: false, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Flags:           []FlagSchema{{Name: "secret-id", Type: "string"}, {Name: "secret-key", Type: "string"}, {Name: "overwrite", Type: "bool"}},
		},
		{
			Name: "explain", Summary: "Explain error codes and exit codes",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: false, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "Code", Type: "string", Required: true}},
		},
		{
			Name: "status", Summary: "Show current CLI configuration status",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: false, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
		},
		{
			Name: "help", Summary: "Help about any command",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: false, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "Command", Type: "string", Required: false, Variadic: true}},
		},
		{
			Name: "schema", Summary: "Show command schema for machine consumption",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: false, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "CommandName", Type: "string", Required: false}},
		},
		{
			Name: "doctor", Summary: "Diagnose CLI configuration issues",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: false, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
		},
		{
			Name: "version", Summary: "Print version information",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: false, SupportsJson: true, SupportsNdjson: false, SupportsJq: true,
			SupportsRequest: false,
		},
		{
			Name: "completion", Summary: "Generate shell completion script",
			Mutation: false, CreatesResource: false,
			Idempotency: "none", SupportsDryRun: false, Interactive: false,
			RequiresAuth: false, SupportsJson: false, SupportsNdjson: false, SupportsJq: false,
			SupportsRequest: false,
			Args:            []ArgSchema{{Name: "Shell", Type: "string", Required: true}},
		},
	}
	return schemas
}

func appendUniqueStrings(items []string, values ...string) []string {
	for _, value := range values {
		if !containsString(items, value) {
			items = append(items, value)
		}
	}
	return items
}
