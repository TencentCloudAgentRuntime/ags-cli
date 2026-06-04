// Package apicli converts generated API descriptors into executable command
// modules. It keeps the Cloud API mapping data separate from Cobra wiring so
// generated commands and hand-written workflows can share the same registry.
package apicli

import "github.com/TencentCloudAgentRuntime/ags-cli/internal/command"

// APIDescriptor describes one generated Cloud API backed CLI command.
type APIDescriptor struct {
	Spec   command.Spec
	Groups []command.GroupSpec
	API    APISpec
	// Fields describes request assembly. Each field maps one TencentCloud
	// request member to one or more CLI inputs and a parser.
	Fields []FieldSpec
	// DisableRequestFlag suppresses the generic --request JSON mode for
	// commands that do not accept an API request body.
	DisableRequestFlag bool
}

// APISpec records the TencentCloud API action and SDK request/response types.
type APISpec struct {
	Action string
	// RequestType and ResponseType are the SDK type names used for schema output
	// and diagnostics; execution still routes by Action.
	RequestType  string
	ResponseType string
}

// FieldSpec describes how one API request field is produced from CLI inputs.
type FieldSpec struct {
	Name        string
	Description string
	Required    bool
	// Parser is the stable parser ID registered in parsers.FieldParsers. Empty
	// values are resolved from input types by RequestBuilder.
	Parser string
	Inputs []InputSpec
}

// InputSpec describes one flag or positional argument feeding an API field.
type InputSpec struct {
	Name       string
	Flag       string
	Shorthand  string
	Aliases    []string
	Usage      string
	Format     string
	Examples   []string
	Values     []string
	Type       command.FlagType
	Default    any
	Positional bool
	// SendDefault asks the request builder to send the Cobra default even when
	// the user did not explicitly set the flag. This preserves API-visible
	// defaults that are part of the generated descriptor.
	SendDefault bool
}

// CommandSpec returns the command.Spec with generated flags materialized from
// field inputs, plus the generic --request flag unless disabled.
func (d APIDescriptor) CommandSpec() command.Spec {
	spec := d.Spec
	flags := append([]command.FlagSpec(nil), spec.Flags...)
	for _, field := range d.Fields {
		for _, input := range field.Inputs {
			if input.Positional || input.Flag == "" {
				continue
			}
			flags = append(flags, command.FlagSpec{
				Name:      input.Flag,
				Shorthand: input.Shorthand,
				Aliases:   input.Aliases,
				Usage:     input.Usage,
				Format:    input.Format,
				Examples:  append([]string(nil), input.Examples...),
				Values:    append([]string(nil), input.Values...),
				Type:      input.Type,
				Default:   input.Default,
				Generated: true,
			})
		}
	}
	if !d.DisableRequestFlag && !hasFlag(flags, "request") {
		flags = append(flags, command.FlagSpec{
			Name:      "request",
			Usage:     "Complete request body as JSON, @file, or - for stdin",
			Type:      command.FlagString,
			Generated: true,
		})
	}
	spec.Flags = flags
	return spec
}

func hasFlag(flags []command.FlagSpec, name string) bool {
	for _, flag := range flags {
		if flag.Name == name {
			return true
		}
		for _, alias := range flag.Aliases {
			if alias == name {
				return true
			}
		}
	}
	return false
}
