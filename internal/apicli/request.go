package apicli

import (
	"fmt"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli/parsers"
	requestio "github.com/TencentCloudAgentRuntime/ags-cli/internal/cli/request"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// ParserDispatcher resolves and invokes field parsers by parser ID. Tests use
// this seam to assert request assembly without relying on global parser state.
type ParserDispatcher interface {
	Parse(parserID string, ctx parsers.FieldContext, inputs parsers.FieldInputs) (any, bool, error)
}

// DefaultParserDispatcher uses the built-in parser registry.
type DefaultParserDispatcher struct{}

// Parse dispatches parserID to the built-in parser registry.
func (DefaultParserDispatcher) Parse(parserID string, ctx parsers.FieldContext, inputs parsers.FieldInputs) (any, bool, error) {
	parser, ok := parsers.FieldParsers[parserID]
	if !ok {
		return nil, false, fmt.Errorf("parser %q is not registered", parserID)
	}
	return parser.Parse(ctx, inputs)
}

// RequestSource reads a complete request payload from a command invocation.
// Returning ok=false means the builder should assemble the request from flags
// and positional arguments instead.
type RequestSource interface {
	Read(command.Request) (map[string]any, bool, error)
}

// JSONRequestSource implements --request JSON, @file, and stdin input.
type JSONRequestSource struct{}

// Read returns the decoded --request object when raw request mode is active.
func (JSONRequestSource) Read(req command.Request) (map[string]any, bool, error) {
	flag, ok := req.Flags["request"]
	if !ok || !flag.Changed || strings.TrimSpace(flag.String) == "" {
		return nil, false, nil
	}
	raw, err := requestio.ReadFlagFrom(flag.String, req.Stdin)
	if err != nil {
		return nil, false, err
	}
	var decoded any
	if err := requestio.DecodeJSON(raw, &decoded); err != nil {
		return nil, false, output.NewUsageError(
			"INVALID_REQUEST_JSON",
			fmt.Sprintf("invalid JSON in --request: %v", err),
			"Provide a valid JSON object as --request.",
		)
	}
	out, ok := decoded.(map[string]any)
	if !ok {
		return nil, false, output.NewUsageError(
			"INVALID_REQUEST_JSON",
			"--request must be a JSON object",
			"The top-level value must be a JSON object, not an array or scalar.",
		)
	}
	return out, true, nil
}

// RequestBuilder builds a TencentCloud request map from CLI flags and args.
// It supports two mutually exclusive modes: raw --request JSON with positional
// resource IDs merged in, or field-by-field assembly through FieldSpec parsers.
type RequestBuilder struct {
	desc       APIDescriptor
	parsers    ParserDispatcher
	rawRequest RequestSource
}

// NewRequestBuilder creates a builder for one generated API descriptor.
func NewRequestBuilder(desc APIDescriptor) *RequestBuilder {
	return &RequestBuilder{
		desc:       desc,
		parsers:    DefaultParserDispatcher{},
		rawRequest: JSONRequestSource{},
	}
}

// WithParserDispatcher returns a copy using dispatcher for field parsing.
func (b *RequestBuilder) WithParserDispatcher(dispatcher ParserDispatcher) *RequestBuilder {
	next := *b
	next.parsers = dispatcher
	return &next
}

// WithRequestSource returns a copy using source for raw request mode.
func (b *RequestBuilder) WithRequestSource(source RequestSource) *RequestBuilder {
	next := *b
	next.rawRequest = source
	return &next
}

// Build converts a command request into a Cloud API request map and validates
// required fields after raw-request merging or parser output collection.
func (b *RequestBuilder) Build(req command.Request) (map[string]any, error) {
	if b.rawRequest != nil {
		raw, ok, err := b.rawRequest.Read(req)
		if err != nil {
			return nil, err
		}
		if ok {
			// Raw request mode still honors positional required args so commands
			// like "tool update <id> --request {...}" keep their CLI ergonomics.
			if conflict := requestConflict(req); conflict != "" {
				return nil, output.NewUsageError("REQUEST_FLAG_CONFLICT",
					fmt.Sprintf("--request cannot be combined with %s", conflict),
					"Use --request for the complete request body, or use individual flags.")
			}
			if raw == nil {
				raw = map[string]any{}
			}
			if err := mergePositionals(raw, b.desc.Fields, req); err != nil {
				return nil, err
			}
			if err := validateRequiredFields(raw, b.desc.Fields); err != nil {
				return nil, err
			}
			return raw, nil
		}
	}

	out := map[string]any{}
	for _, field := range b.desc.Fields {
		inputs := collectInputs(field, req)
		parserID := field.Parser
		if parserID == "" {
			parserID = defaultParser(field)
		}
		value, ok, err := b.parsers.Parse(parserID, parsers.FieldContext{
			Command: b.desc.Spec.ID,
			Action:  b.desc.API.Action,
			Field:   field.Name,
			Stdin:   req.Stdin,
		}, inputs)
		if err != nil {
			return nil, err
		}
		if ok {
			out[field.Name] = value
		}
	}
	if err := validateRequiredFields(out, b.desc.Fields); err != nil {
		return nil, err
	}
	return out, nil
}

func collectInputs(field FieldSpec, req command.Request) parsers.FieldInputs {
	out := parsers.FieldInputs{ByFlag: map[string]parsers.FlagInput{}}
	for _, input := range field.Inputs {
		if input.Positional {
			if value, ok := req.ArgValues[input.Name]; ok {
				out.ByFlag[input.Name] = parsers.FlagInput{Changed: true, Values: []string{value}}
			}
			continue
		}
		flagName := input.Flag
		value, ok := req.Flags[flagName]
		if !ok {
			continue
		}
		// SendDefault is used by generated descriptors to preserve API-visible
		// defaults even when Cobra does not mark the flag as explicitly changed.
		changed := value.Changed || input.SendDefault
		values := value.Strings
		if len(values) == 0 && value.String != "" {
			values = []string{value.String}
		}
		if value.Type == command.FlagBool {
			values = []string{fmt.Sprintf("%t", value.Bool)}
		}
		if value.Type == command.FlagInt {
			values = []string{fmt.Sprintf("%d", value.Int)}
		}
		out.ByFlag[flagName] = parsers.FlagInput{Changed: changed, Values: values}
	}
	return out
}

func requestConflict(req command.Request) string {
	var changed []string
	for name, value := range req.Flags {
		if name == "request" || !value.Changed {
			continue
		}
		changed = append(changed, "--"+name)
	}
	if len(changed) == 0 {
		return ""
	}
	return strings.Join(changed, ", ")
}

func mergePositionals(raw map[string]any, fields []FieldSpec, req command.Request) error {
	for _, field := range fields {
		for _, input := range field.Inputs {
			if !input.Positional {
				continue
			}
			value, ok := req.ArgValues[input.Name]
			if !ok || strings.TrimSpace(value) == "" {
				continue
			}
			if existing, exists := raw[field.Name]; exists {
				if existingString, _ := existing.(string); existingString != "" && existingString != value {
					// Positional args are treated as an explicit confirmation of the
					// target resource, so mismatches should fail instead of silently
					// preferring either source.
					return output.NewUsageError("REQUEST_ARG_CONFLICT",
						fmt.Sprintf("%s in --request does not match positional %s", field.Name, input.Name),
						fmt.Sprintf("Use the same %s in the request JSON and positional argument, or omit it from --request.", field.Name))
				}
			}
			raw[field.Name] = value
		}
	}
	return nil
}

func validateRequiredFields(req map[string]any, fields []FieldSpec) error {
	for _, field := range fields {
		if !field.Required {
			continue
		}
		value, ok := req[field.Name]
		if ok && requiredValuePresent(value) {
			continue
		}
		return missingRequiredFieldError(field)
	}
	return nil
}

func requiredValuePresent(value any) bool {
	if value == nil {
		return false
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s) != ""
	}
	return true
}

func missingRequiredFieldError(field FieldSpec) error {
	arg := requiredPositionalName(field)
	if arg != "" {
		return output.NewUsageError(
			"MISSING_REQUIRED_ARG",
			fmt.Sprintf("missing required argument %s", arg),
			fmt.Sprintf("Provide %s or include %s in --request.", arg, field.Name),
		)
	}
	flag := requiredFlagName(field)
	if flag != "" {
		return output.NewUsageError(
			"MISSING_REQUIRED_FLAG",
			fmt.Sprintf("%s is required", flag),
			fmt.Sprintf("Provide %s or include %s in --request.", flag, field.Name),
		)
	}
	return output.NewUsageError(
		"MISSING_REQUIRED_FLAG",
		fmt.Sprintf("%s is required", field.Name),
		fmt.Sprintf("Provide %s in --request.", field.Name),
	)
}

func requiredPositionalName(field FieldSpec) string {
	for _, input := range field.Inputs {
		if input.Positional {
			return "<" + input.Name + ">"
		}
	}
	return ""
}

func requiredFlagName(field FieldSpec) string {
	for _, input := range field.Inputs {
		if input.Flag != "" {
			return "--" + input.Flag
		}
	}
	return ""
}

func defaultParser(field FieldSpec) string {
	for _, input := range field.Inputs {
		switch input.Type {
		case command.FlagBool:
			return "common.default_bool"
		case command.FlagStringArray:
			return "common.default_string_array"
		}
	}
	return "common.default_string"
}
