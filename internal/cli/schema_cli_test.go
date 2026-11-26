package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
)

// The schema contract is only observable through the assembled CLI, so these
// tests re-execute the test binary as `agr`. That keeps the registry attach,
// flag parsing, envelope rendering and exit-code path identical to the shipped
// binary while staying inside `go test`.

const (
	schemaHelperEnv = "AGR_SCHEMA_CLI_HELPER"
	updateGoldenEnv = "UPDATE_GOLDEN"
)

// TestSchemaCLIHelper is the child process for runSchemaCLI.
func TestSchemaCLIHelper(t *testing.T) {
	if os.Getenv(schemaHelperEnv) != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing CLI arguments after --")
	}
	os.Args = append([]string{"agr"}, os.Args[separator+1:]...)
	cli.SetIOStreams(&iostreams.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
	registry, err := commands.Registry()
	if err != nil {
		t.Fatalf("build command registry: %v", err)
	}
	if err := cli.AttachCommandRegistry(registry); err != nil {
		t.Fatalf("attach command registry: %v", err)
	}
	cli.Execute()
	os.Exit(0)
}

// cliResult is one CLI invocation with stdout and stderr kept apart, because a
// machine-readable failure must never write to stderr.
type cliResult struct {
	stdout   []byte
	stderr   []byte
	exitCode int
	home     string
}

func runSchemaCLI(t *testing.T, args ...string) cliResult {
	t.Helper()
	home := t.TempDir()
	cmd := exec.CommandContext(t.Context(), os.Args[0], append([]string{"-test.run=^TestSchemaCLIHelper$", "--"}, args...)...)
	cmd.Env = append(isolatedEnv(),
		schemaHelperEnv+"=1",
		"HOME="+home,
		"USERPROFILE="+home,
		// Parallel helpers inherit one GOCOVERDIR from `go test -coverprofile`
		// and race to rename the same meta-data file.
		"GOCOVERDIR="+t.TempDir(),
		"GORACE=atexit_sleep_ms=0",
	)
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	result := cliResult{home: home}
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		result.exitCode = exitErr.ExitCode()
	default:
		t.Fatalf("run agr %s: %v (stderr=%s)", strings.Join(args, " "), err, stderr.String())
	}
	result.stdout, result.stderr = stdout.Bytes(), stderr.Bytes()
	return result
}

// isolatedEnv drops every AGR_* and TENCENTCLOUD_* variable so credentials,
// debug switches and config overrides from the parent shell cannot reach the
// helper process. A leaked credential would let these assertions call the real
// cloud API instead of stopping at the missing-credentials boundary.
func isolatedEnv() []string {
	parent := os.Environ()
	env := make([]string, 0, len(parent))
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "AGR_") || strings.HasPrefix(upper, "TENCENTCLOUD_") {
			continue
		}
		env = append(env, entry)
	}
	return env
}

// schemaArg and schemaFlag mirror the published positional and flag entries.
type schemaArg struct {
	Name      string `json:"Name"`
	Required  bool   `json:"Required"`
	Variadic  bool   `json:"Variadic"`
	AfterDash bool   `json:"AfterDash"`
}

type schemaFlag struct {
	Name string `json:"Name"`
}

// commandSchema is the subset of `agr schema -o json` these tests assert on.
type commandSchema struct {
	Name           string       `json:"Name"`
	Kind           string       `json:"Kind"`
	SupportsDryRun bool         `json:"SupportsDryRun"`
	Args           []schemaArg  `json:"Args"`
	Flags          []schemaFlag `json:"Flags"`
	Failures       []string     `json:"Failures"`
}

func (s commandSchema) hasFlag(name string) bool {
	return slices.ContainsFunc(s.Flags, func(flag schemaFlag) bool {
		return flag.Name == name
	})
}

func (s commandSchema) declares(code string) bool {
	return slices.Contains(s.Failures, code)
}

type failureEnvelope struct {
	SchemaVersion string `json:"SchemaVersion"`
	Command       string `json:"Command"`
	Status        string `json:"Status"`
	Failure       *struct {
		Code      string `json:"Code"`
		Kind      string `json:"Kind"`
		Message   string `json:"Message"`
		Hint      string `json:"Hint"`
		Retryable bool   `json:"Retryable"`
	} `json:"Failure"`
}

// decodeFailure parses a `-o json` failure envelope and asserts that it is the
// only output the command produced.
func decodeFailure(t *testing.T, result cliResult) failureEnvelope {
	t.Helper()
	if len(result.stderr) != 0 {
		t.Fatalf("stderr must stay empty in JSON mode, got %q", result.stderr)
	}
	var envelope failureEnvelope
	if err := json.Unmarshal(result.stdout, &envelope); err != nil {
		t.Fatalf("decode failure envelope: %v\n%s", err, result.stdout)
	}
	if envelope.SchemaVersion != "agr.v1" || envelope.Status != "failed" || envelope.Failure == nil {
		t.Fatalf("unexpected envelope: %s", result.stdout)
	}
	if envelope.Failure.Message == "" || envelope.Failure.Hint == "" {
		t.Fatalf("failure lacks a message or hint: %s", result.stdout)
	}
	return envelope
}

// schemaCatalog loads the published schema once per call.
func schemaCatalog(t *testing.T) map[string]commandSchema {
	t.Helper()
	result := runSchemaCLI(t, "schema", "-o", "json")
	if result.exitCode != 0 {
		t.Fatalf("agr schema failed: exit=%d stderr=%s", result.exitCode, result.stderr)
	}
	var envelope struct {
		Data struct {
			Commands []commandSchema `json:"Commands"`
		} `json:"Data"`
	}
	if err := json.Unmarshal(result.stdout, &envelope); err != nil {
		t.Fatalf("decode schema catalog: %v", err)
	}
	byName := make(map[string]commandSchema, len(envelope.Data.Commands))
	for _, command := range envelope.Data.Commands {
		byName[command.Name] = command
	}
	return byName
}

func commandSchemaFor(t *testing.T, catalog map[string]commandSchema, name string) commandSchema {
	t.Helper()
	schema, ok := catalog[name]
	if !ok {
		t.Fatalf("schema for %q is missing", name)
	}
	return schema
}

// decodeEnvelope unmarshals a successful `-o json` envelope from stdout.
func decodeEnvelope(t *testing.T, result cliResult, target any) error {
	t.Helper()
	if len(result.stderr) != 0 {
		t.Fatalf("stderr must stay empty in JSON mode, got %q", result.stderr)
	}
	if err := json.Unmarshal(result.stdout, target); err != nil {
		return fmt.Errorf("decode envelope: %w\n%s", err, result.stdout)
	}
	return nil
}

// jsonArgs appends -o json before the first -- so it never turns into a remote
// command argument.
func jsonArgs(args ...string) []string {
	if separator := slices.Index(args, "--"); separator >= 0 {
		withOutput := append([]string{}, args[:separator]...)
		withOutput = append(withOutput, "-o", "json")
		return append(withOutput, args[separator:]...)
	}
	return append(append([]string{}, args...), "-o", "json")
}

// decodeEnvelopeFailure returns the failure code of any envelope status, which
// covers partially successful commands such as instance.delete.
func decodeEnvelopeFailure(t *testing.T, result cliResult) string {
	t.Helper()
	var envelope failureEnvelope
	if err := decodeEnvelope(t, result, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Failure == nil {
		t.Fatalf("envelope reports no failure: %s", result.stdout)
	}
	return envelope.Failure.Code
}

func normalizeSchemaGolden(t *testing.T, raw []byte) []byte {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode schema envelope: %v", err)
	}
	// DurationMs measures wall-clock time and would make the snapshot flaky.
	if meta, ok := envelope["Meta"].(map[string]any); ok {
		meta["DurationMs"] = float64(0)
	}
	normalized, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		t.Fatalf("encode normalized schema: %v", err)
	}
	return append(normalized, '\n')
}

func firstLineDifference(want, got []byte) string {
	wantLines := strings.Split(string(want), "\n")
	gotLines := strings.Split(string(got), "\n")
	for i := 0; i < min(len(wantLines), len(gotLines)); i++ {
		if wantLines[i] != gotLines[i] {
			return fmt.Sprintf("line %d:\n  want: %s\n  got:  %s", i+1, wantLines[i], gotLines[i])
		}
	}
	if len(wantLines) != len(gotLines) {
		return fmt.Sprintf("snapshot has %d lines, want %d", len(gotLines), len(wantLines))
	}
	return "no line differs"
}
