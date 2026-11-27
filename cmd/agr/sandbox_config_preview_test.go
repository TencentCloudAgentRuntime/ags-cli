//go:build preview

package main

import (
	"encoding/json"
	"errors"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// Same command set as the stable-channel guard, kept local because the two
// channel files are compiled separately.
var sandboxConfigCommands = []string{
	"tool.create",
	"tool.update",
	"instance.create",
	"instance.update",
	"tool.fork",
}

var sandboxConfigPreviewProperties = []struct {
	property string
	flag     string
	schema   string
	flagType string
}{
	{property: "KeepaliveConfiguration", flag: "keepalive-configuration", schema: "object", flagType: "json"},
	{property: "AutoPause", flag: "auto-pause", schema: "bool", flagType: "bool"},
	{property: "AutoResume", flag: "auto-resume", schema: "bool", flagType: "bool"},
}

func TestSandboxConfigPreviewSchemaMatchesHelp(t *testing.T) {
	root := contractRoot()
	for _, commandID := range sandboxConfigCommands {
		schema := schemaForCommand(t, commandID)
		command, ok := findCobraCommand(root, commandID)
		if !ok {
			t.Fatalf("command %s not found", commandID)
		}
		help := commandHelp(t, command)
		for _, want := range sandboxConfigPreviewProperties {
			property, ok := schema.RequestSchema[want.property]
			if !ok {
				t.Fatalf("%s schema is missing %s", commandID, want.property)
			}
			if property.Type != want.schema {
				t.Errorf("%s %s type = %q, want %q", commandID, want.property, property.Type, want.schema)
			}
			if property.CliFlag == nil || *property.CliFlag != want.flag {
				t.Errorf("%s %s cli flag = %v, want %q", commandID, want.property, property.CliFlag, want.flag)
			}
			flag, ok := schema.Flags[want.flag]
			if !ok {
				t.Fatalf("%s schema has no --%s flag", commandID, want.flag)
			}
			if flag.Type != want.flagType {
				t.Errorf("%s --%s type = %q, want %q", commandID, want.flag, flag.Type, want.flagType)
			}
			if flag.Description == "" || !strings.Contains(help, "--"+want.flag) {
				t.Errorf("%s help does not document --%s:\n%s", commandID, want.flag, help)
			}
		}
		if !strings.Contains(help, "no default") {
			t.Errorf("%s help must state that the CLI applies no keepalive default:\n%s", commandID, help)
		}
	}
}

// The override flag is optional but must accept an explicit false, and the
// inherited sentinel must not be presented as a client default.
func TestSandboxConfigPreviewAutoFlagsDocumentExplicitFalse(t *testing.T) {
	for _, commandID := range sandboxConfigCommands {
		schema := schemaForCommand(t, commandID)
		for _, name := range []string{"auto-pause", "auto-resume"} {
			flag := schema.Flags[name]
			if !strings.Contains(flag.Description, "false") {
				t.Errorf("%s --%s must document the explicit false: %q", commandID, name, flag.Description)
			}
		}
	}
}

func TestSandboxConfigPreviewLogHelpCoversContentFormat(t *testing.T) {
	schema := schemaForCommand(t, "tool.create")
	flag, ok := schema.Flags["log-configuration"]
	if !ok {
		t.Fatal("tool.create is missing --log-configuration")
	}
	if !strings.Contains(flag.Format, "ContentFormat") {
		t.Errorf("--log-configuration format does not mention ContentFormat: %q", flag.Format)
	}
	if _, ok := schemaForCommand(t, "tool.update").Flags["log-configuration"]; ok {
		t.Error("tool.update must not accept --log-configuration")
	}
}

// Rejections that belong to the CLI contract must happen before any request is
// attempted, so they stay provable without credentials.
func TestSandboxConfigPreviewRejectsBadInputLocally(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		marker string
		code   string
	}{
		{
			name:   "malformed keepalive json",
			args:   []string{"tool", "create", "-n", "sandbox-config", "-t", "code-interpreter", "--keepalive-configuration", `{"KeepaliveMode":`},
			marker: "INVALID_JSON",
			code:   "INVALID_JSON_FLAG",
		},
		{
			name:   "unknown top-level member",
			args:   []string{"instance", "update", "ins-xxxx", "--request", `{"KeepaliveMode":"REQUEST"}`},
			marker: "INVALID_REQUEST_JSON",
			code:   "INVALID_REQUEST_JSON",
		},
		{
			name:   "missing selector",
			args:   []string{"instance", "update", "--auto-pause"},
			marker: "missing required argument",
			code:   "MISSING_REQUIRED_ARG",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runAGRExecute(t, append(tc.args, "-o", "json")...)
			if err == nil {
				t.Fatalf("command unexpectedly succeeded:\n%s", out)
			}
			if !strings.Contains(out, tc.marker) {
				t.Fatalf("output does not report %s:\n%s", tc.marker, out)
			}
			// The message is only prose; the envelope code, kind and exit code are
			// the contract a caller actually branches on.
			var envelope struct {
				Failure *struct {
					Code string `json:"Code"`
					Kind string `json:"Kind"`
				} `json:"Failure"`
			}
			if err := json.Unmarshal([]byte(out), &envelope); err != nil {
				t.Fatalf("output is not a JSON envelope: %v\n%s", err, out)
			}
			if envelope.Failure == nil || envelope.Failure.Code != tc.code || envelope.Failure.Kind != "usage" {
				t.Fatalf("failure = %+v, want %s/usage\n%s", envelope.Failure, tc.code, out)
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
				t.Fatalf("exit code = %v, want 2", err)
			}
		})
	}
}

// Every JSON flag example in the rendered help must be runnable as written.
// A Go raw string that keeps escape backslashes still mentions the flag name, so
// a "help documents the flag" assertion passes while the documented command
// fails with INVALID_JSON_FLAG. Splitting the example into the arguments a shell
// would produce and parsing each JSON value catches that.
func TestSandboxConfigPreviewHelpExamplesParseAsShellArgs(t *testing.T) {
	root := contractRoot()
	for _, commandID := range sandboxConfigCommands {
		schema := schemaForCommand(t, commandID)
		command, ok := findCobraCommand(root, commandID)
		if !ok {
			t.Fatalf("command %s not found", commandID)
		}
		help := commandHelp(t, command)
		for _, flagName := range slices.Sorted(maps.Keys(schema.Flags)) {
			flag := schema.Flags[flagName]
			if flag.Type != "json" {
				continue
			}
			for _, example := range flag.Examples {
				if !strings.Contains(help, example) {
					t.Fatalf("%s: --%s example is not rendered in the real help: %q", commandID, flagName, example)
				}
				args := sandboxShellArgs(t, example)
				if len(args) < 3 || args[0] != "agr" || args[1] != strings.SplitN(commandID, ".", 2)[0] {
					t.Fatalf("%s: --%s example does not start with its command: %v", commandID, flagName, args)
				}
				value, found := sandboxFlagValue(args, "--"+flagName)
				if !found {
					t.Fatalf("%s: --%s example does not pass the flag: %v", commandID, flagName, args)
				}
				// @file and stdin inputs are resolved by the CLI, not in the example.
				if strings.HasPrefix(value, "@") || value == "-" {
					continue
				}
				var parsed any
				if err := json.Unmarshal([]byte(value), &parsed); err != nil {
					t.Fatalf("%s: --%s example value %q is not JSON: %v", commandID, flagName, value, err)
				}
			}
		}
	}
}

// sandboxShellArgs splits one help example into the argv a POSIX shell produces.
// It covers the quoting the examples use: single quotes, double quotes and
// backslash escapes outside single quotes.
func sandboxShellArgs(t *testing.T, example string) []string {
	t.Helper()
	var (
		args    []string
		current strings.Builder
		quote   rune
		inWord  bool
	)
	flush := func() {
		if inWord {
			args = append(args, current.String())
			current.Reset()
			inWord = false
		}
	}
	runes := []rune(example)
	for i := 0; i < len(runes); i++ {
		switch r := runes[i]; {
		case r == '\\' && quote != '\'':
			i++
			if i == len(runes) {
				t.Fatalf("example %q ends with a dangling escape", example)
			}
			current.WriteRune(runes[i])
			inWord = true
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
			inWord = true
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t':
			flush()
		default:
			current.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		t.Fatalf("example %q has an unbalanced quote", example)
	}
	flush()
	return args
}

// sandboxFlagValue returns the value the arguments pass to flag, accepting both
// "--flag value" and "--flag=value".
func sandboxFlagValue(args []string, flag string) (string, bool) {
	for i, arg := range args {
		if arg == flag {
			if i+1 >= len(args) {
				return "", false
			}
			return args[i+1], true
		}
		if value, ok := strings.CutPrefix(arg, flag+"="); ok {
			return value, true
		}
	}
	return "", false
}
