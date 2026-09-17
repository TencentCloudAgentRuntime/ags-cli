// Package parsers contains the small parser registry used by generated API
// commands to translate normalized CLI inputs into TencentCloud request values.
package parsers

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	requestio "github.com/TencentCloudAgentRuntime/ags-cli/internal/cli/request"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// FieldContext identifies the command and API field currently being parsed.
// Stdin is provided for parsers that accept "-" or other stream-backed inputs.
type FieldContext struct {
	Command string
	Action  string
	Field   string
	Stdin   io.Reader
}

// FieldInputs contains the collected CLI inputs for one API field, keyed by the
// descriptor's input or flag name.
type FieldInputs struct {
	ByFlag map[string]FlagInput
}

// FlagInput is the normalized value captured from a single CLI flag or
// positional argument. Changed distinguishes "not provided" from a zero value.
type FlagInput struct {
	Changed bool
	Values  []string
}

// FieldParser converts one field's CLI inputs into an API request value. The
// boolean return value reports whether the field should be included at all.
type FieldParser interface {
	Parse(ctx FieldContext, inputs FieldInputs) (any, bool, error)
}

type parserFunc func(FieldContext, FieldInputs) (any, bool, error)

// Parse adapts a parser function to the FieldParser interface.
func (f parserFunc) Parse(ctx FieldContext, inputs FieldInputs) (any, bool, error) {
	return f(ctx, inputs)
}

// FieldParsers maps stable parser IDs from mapping.yaml to parser implementations.
var FieldParsers = map[string]FieldParser{
	"common.default_string":       parserFunc(defaultString),
	"common.default_bool":         parserFunc(defaultBool),
	"common.default_int":          parserFunc(defaultInt),
	"common.default_string_array": parserFunc(defaultStringArray),
	"common.default_json":         parserFunc(defaultJSON),
}

// Exists reports whether a parser ID is registered.
func Exists(name string) bool {
	_, ok := FieldParsers[name]
	return ok
}

func defaultString(_ FieldContext, inputs FieldInputs) (any, bool, error) {
	for _, in := range inputs.ByFlag {
		if in.Changed && len(in.Values) > 0 {
			return in.Values[len(in.Values)-1], true, nil
		}
	}
	return nil, false, nil
}

func defaultBool(_ FieldContext, inputs FieldInputs) (any, bool, error) {
	for _, in := range inputs.ByFlag {
		if in.Changed {
			if len(in.Values) == 0 {
				return true, true, nil
			}
			return in.Values[len(in.Values)-1] == "true", true, nil
		}
	}
	return nil, false, nil
}

func defaultInt(ctx FieldContext, inputs FieldInputs) (any, bool, error) {
	for flag, in := range inputs.ByFlag {
		if in.Changed && len(in.Values) > 0 {
			value, err := strconv.Atoi(in.Values[len(in.Values)-1])
			if err != nil {
				return nil, false, fmt.Errorf("%s.%s --%s: %w", ctx.Command, ctx.Field, flag, err)
			}
			return value, true, nil
		}
	}
	return nil, false, nil
}

func defaultStringArray(_ FieldContext, inputs FieldInputs) (any, bool, error) {
	for _, in := range inputs.ByFlag {
		if in.Changed {
			return append([]string(nil), in.Values...), true, nil
		}
	}
	return nil, false, nil
}

func defaultJSON(ctx FieldContext, inputs FieldInputs) (any, bool, error) {
	for flag, in := range inputs.ByFlag {
		if !in.Changed || len(in.Values) == 0 {
			continue
		}
		var out any
		if err := parseJSONValue(ctx, flag, in.Values[len(in.Values)-1], &out); err != nil {
			return nil, false, err
		}
		return out, true, nil
	}
	return nil, false, nil
}

func parseJSONValue(ctx FieldContext, flag, value string, target any) error {
	data, err := readJSONFlagValue(ctx, value)
	if err != nil {
		return err
	}
	if err := requestio.DecodeJSON(data, target); err != nil {
		return output.NewUsageError(
			"INVALID_JSON_FLAG",
			fmt.Sprintf("invalid JSON for --%s: %v", flag, err),
			fmt.Sprintf("Provide a valid JSON value for --%s, @file, or - for stdin.", flag),
		)
	}
	return nil
}

func readJSONFlagValue(ctx FieldContext, value string) ([]byte, error) {
	switch {
	case value == "-":
		if ctx.Stdin == nil {
			return nil, output.NewUsageError("INVALID_REQUEST_INPUT", "stdin is not available for JSON flag value", "Provide inline JSON or @file.")
		}
		data, err := io.ReadAll(ctx.Stdin)
		if err != nil {
			return nil, output.NewUsageError("INVALID_REQUEST_INPUT", fmt.Sprintf("failed to read JSON flag from stdin: %v", err), "Provide valid JSON via stdin, inline JSON, or @file.")
		}
		return data, nil
	case strings.HasPrefix(value, "@"):
		data, err := os.ReadFile(strings.TrimPrefix(value, "@"))
		if err != nil {
			return nil, output.NewUsageError("INVALID_REQUEST_INPUT", fmt.Sprintf("failed to read JSON flag file %s: %v", strings.TrimPrefix(value, "@"), err), "Check that the file exists and is readable.")
		}
		return data, nil
	default:
		return []byte(value), nil
	}
}
