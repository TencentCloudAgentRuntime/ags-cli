//go:build preview

package harnesscli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	requestio "github.com/TencentCloudAgentRuntime/ags-cli/internal/cli/request"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// SingleStdin rejects more than one flag reading "-" from stdin; the second
// reader would silently see an empty stream.
func SingleStdin(req command.Request, names ...string) error {
	var readers []string
	for _, name := range append([]string{"request"}, names...) {
		if Changed(req, name) && strings.TrimSpace(String(req, name)) == "-" {
			readers = append(readers, "--"+name)
		}
	}
	if len(readers) > 1 {
		return output.NewUsageError("STDIN_CONFLICT",
			fmt.Sprintf("only one flag can read stdin, got %s", strings.Join(readers, ", ")),
			"Pass the other values inline or as @file.")
	}
	return nil
}

// JSONFlag decodes a JSON flag through the shared inline/@file/stdin reader.
func JSONFlag(req command.Request, name string, target any) error {
	raw, err := requestio.ReadFlagFrom(String(req, name), req.Stdin)
	if err != nil {
		return err
	}
	if err := requestio.DecodeJSON(raw, target); err != nil {
		return output.NewUsageError("INVALID_JSON_FLAG",
			fmt.Sprintf("invalid JSON for --%s: %v", name, err),
			fmt.Sprintf("Provide a valid JSON value for --%s, @file, or - for stdin.", name)).
			WithCause(output.WithContext(err, map[string]any{"Stage": "request_parse", "Field": name}))
	}
	return nil
}

// TextFlag returns the flag value, reading @file when the value starts with @.
func TextFlag(req command.Request, name string) (string, error) {
	value := String(req, name)
	if !strings.HasPrefix(value, "@") {
		return value, nil
	}
	data, err := os.ReadFile(value[1:])
	if err != nil {
		return "", output.NewUsageError("INVALID_FILE_INPUT",
			fmt.Sprintf("failed to read --%s file %s: %v", name, value[1:], err),
			"Check that the file exists and is readable, or use --request for a literal value starting with @.")
	}
	return string(data), nil
}

var modelInputs = []string{"TEXT", "IMAGE"}

// RegisteredModels converts repeated --model <id>[:TEXT,IMAGE] values. Without
// a suffix the model is registered as text-only, and the explicit Input is sent
// so the stored registration never depends on a server default.
func RegisteredModels(values []string) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		id, suffix, hasSuffix := cutLast(strings.TrimSpace(value), ":")
		inputs := []string{"TEXT"}
		if hasSuffix {
			if parsed, ok := parseInputs(suffix); ok {
				inputs = parsed
			} else {
				// The suffix is not an input list, so the colon belongs to the model ID.
				id = strings.TrimSpace(value)
			}
		}
		if id == "" {
			return nil, output.NewUsageError("INVALID_MODEL", fmt.Sprintf("--model %q has an empty model ID", value),
				"Example: --model hy3 or --model qwen-vl:TEXT,IMAGE")
		}
		if seen[id] {
			return nil, output.NewUsageError("DUPLICATE_MODEL", fmt.Sprintf("model %q is repeated", id), "Register each model ID once.")
		}
		seen[id] = true
		out = append(out, map[string]any{"ID": id, "Input": inputs})
	}
	return out, nil
}

func cutLast(value, sep string) (string, string, bool) {
	index := strings.LastIndex(value, sep)
	if index < 0 {
		return value, "", false
	}
	return value[:index], value[index+len(sep):], true
}

func parseInputs(suffix string) ([]string, bool) {
	var inputs []string
	for _, part := range strings.Split(suffix, ",") {
		input := strings.ToUpper(strings.TrimSpace(part))
		if !slices.Contains(modelInputs, input) || slices.Contains(inputs, input) {
			return nil, false
		}
		inputs = append(inputs, input)
	}
	return inputs, len(inputs) > 0
}
