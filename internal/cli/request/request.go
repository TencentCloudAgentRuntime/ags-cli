// Package request contains shared helpers for the CLI --request input mode.
package request

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/cobra"
)

// ReadFlag reads a --request-style value from inline JSON, @file, or stdin.
func ReadFlag(value string) ([]byte, error) {
	return ReadFlagFrom(value, os.Stdin)
}

// ReadFlagFrom reads a --request-style value from inline JSON, @file, or the provided stdin reader.
func ReadFlagFrom(value string, stdin io.Reader) ([]byte, error) {
	switch {
	case value == "-":
		if stdin == nil {
			return nil, output.NewUsageError("INVALID_REQUEST_INPUT",
				"stdin is not available for --request",
				"Provide valid JSON via stdin, an inline string, or @file.")
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, output.NewUsageError("INVALID_REQUEST_INPUT",
				fmt.Sprintf("failed to read request from stdin: %v", err),
				"Provide valid JSON via stdin, an inline string, or @file.")
		}
		return data, nil
	case strings.HasPrefix(value, "@"):
		data, err := os.ReadFile(value[1:])
		if err != nil {
			return nil, output.NewUsageError("INVALID_REQUEST_INPUT",
				fmt.Sprintf("failed to read request file %s: %v", value[1:], err),
				"Check that the request file exists and is readable.")
		}
		return data, nil
	default:
		return []byte(value), nil
	}
}

// ParseFlag reads a --request value and decodes it as a top-level JSON object.
func ParseFlag(value string) (map[string]any, error) {
	data, err := ReadFlag(value)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := DecodeJSON(data, &raw); err != nil {
		return nil, output.NewUsageError("INVALID_REQUEST_JSON",
			fmt.Sprintf("invalid JSON in --request: %v", err),
			"Provide valid JSON as a string, @file, or - for stdin.")
	}
	result, ok := raw.(map[string]any)
	if !ok {
		return nil, output.NewUsageError("INVALID_REQUEST_JSON",
			"--request must be a JSON object",
			"The top-level value must be a JSON object, not an array or scalar.")
	}
	return result, nil
}

// ParseJSONFlagValue reads a JSON-valued flag from inline JSON, @file, or stdin.
func ParseJSONFlagValue(flagName, value string, target any) error {
	data, err := ReadFlag(value)
	if err != nil {
		return err
	}
	if err := DecodeJSON(data, target); err != nil {
		return output.NewUsageError("INVALID_JSON_FLAG",
			fmt.Sprintf("invalid JSON for --%s: %v", flagName, err),
			fmt.Sprintf("Provide a valid JSON value for --%s, @file, or - for stdin.", flagName))
	}
	return nil
}

// MergePositional reads a --request payload and overlays one positional field.
func MergePositional(rawRequest, fieldName, positional string) ([]byte, error) {
	raw, err := ReadFlag(rawRequest)
	if err != nil {
		return nil, err
	}
	var probe map[string]any
	if err := DecodeJSON(raw, &probe); err != nil {
		return nil, output.NewUsageError("INVALID_REQUEST_JSON",
			fmt.Sprintf("invalid JSON in --request: %v", err),
			"Provide a valid JSON object as --request.")
	}
	if probe == nil {
		return nil, output.NewUsageError("INVALID_REQUEST_JSON",
			"--request must be a JSON object",
			"The top-level value must be a JSON object, not null/array/scalar.")
	}
	if existing, ok := probe[fieldName]; ok {
		if s, _ := existing.(string); s != "" && s != positional {
			return nil, output.NewUsageError("REQUEST_ARG_CONFLICT",
				fmt.Sprintf("%s in --request does not match positional argument", fieldName),
				fmt.Sprintf("Use the same %s in the request JSON and positional argument, or omit it from --request.", fieldName))
		}
	}
	probe[fieldName] = positional
	return json.Marshal(probe)
}

// ValidatePayload performs CLI-layer syntax validation for a request payload.
func ValidatePayload(commandID string, raw []byte) error {
	if len(raw) == 0 {
		return output.NewUsageError("INVALID_REQUEST_JSON", "--request payload is empty", "Provide a JSON object as --request.")
	}
	var probe any
	if err := DecodeJSON(raw, &probe); err != nil {
		return output.NewUsageError("INVALID_REQUEST_JSON",
			fmt.Sprintf("invalid JSON in --request: %v", err),
			fmt.Sprintf("Provide a valid JSON object as --request. Run 'agr schema %s -o json' for the field reference.", commandID))
	}
	if _, ok := probe.(map[string]any); !ok {
		return output.NewUsageError("INVALID_REQUEST_JSON",
			"--request must be a JSON object",
			"The top-level value must be a JSON object, not an array or scalar.")
	}
	return nil
}

// ParseError wraps a typed SDK FromJsonString error into a stable usage error.
func ParseError(commandName string, err error) error {
	if err == nil {
		return nil
	}
	return output.NewUsageError("INVALID_REQUEST_JSON", err.Error(), fmt.Sprintf("Run 'agr schema %s -o json' for the documented request fields.", commandName))
}

// Conflict returns a description when --request is combined with generated flags.
func Conflict(cmd *cobra.Command, commandID string) string {
	cat, err := apimeta.Get()
	if err != nil {
		return ""
	}
	flags := apimeta.BuildFlags(cat.Spec, cat.Mapping).Flags
	conflicting := map[string]bool{}
	for _, f := range flags {
		if f.Command != commandID {
			continue
		}
		if f.Positional {
			continue
		}
		if cmd.Flags().Changed(f.Flag) {
			conflicting["--"+f.Flag] = true
		}
		for _, alias := range f.Aliases {
			if cmd.Flags().Changed(alias) {
				conflicting["--"+alias] = true
			}
		}
	}
	if len(conflicting) == 0 {
		return ""
	}
	keys := make([]string, 0, len(conflicting))
	for k := range conflicting {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// ConflictDetail formats Conflict for callers that do not need an error type.
func ConflictDetail(cmd *cobra.Command, commandID string) string {
	c := Conflict(cmd, commandID)
	if c == "" {
		return ""
	}
	return fmt.Sprintf("--request cannot be combined with %s", c)
}

// SchemaForCommand looks up the mapped API action for a resource command.
func SchemaForCommand(commandID string) (string, *apimeta.ActionMapping, bool) {
	cat, err := apimeta.Get()
	if err != nil {
		return "", nil, false
	}
	for name, a := range cat.Mapping.Actions {
		if a.Status == apimeta.StatusMapped && a.Command == commandID {
			return name, a, true
		}
	}
	return "", nil, false
}

// SupportsGeneratedSkeleton reports whether a mapped command has request fields.
func SupportsGeneratedSkeleton(commandID string) (bool, bool) {
	_, action, ok := SchemaForCommand(commandID)
	if !ok {
		return false, false
	}
	cat, err := apimeta.Get()
	if err != nil {
		return false, true
	}
	obj := cat.Spec.Object(action.Request)
	if obj == nil {
		return false, true
	}
	for _, m := range obj.Members {
		if !m.Disabled {
			return true, true
		}
	}
	return false, true
}

// GeneratedSkeleton returns the apimeta-derived skeleton for commandID.
func GeneratedSkeleton(commandID string) (map[string]any, bool) {
	cat, err := apimeta.Get()
	if err != nil {
		return nil, false
	}
	if _, _, ok := SchemaForCommand(commandID); !ok {
		return nil, false
	}
	rep := apimeta.BuildSkeletons(cat.Spec, cat.Mapping)
	tmpl, ok := rep.Skeletons[commandID]
	return tmpl, ok
}

// DecodeJSON retains integer precision while requiring exactly one JSON value.
func DecodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}
