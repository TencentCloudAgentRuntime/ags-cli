// Package integ_test provides credential-free integration tests that exercise
// the real agr binary in a subprocess. They verify CLI behavior (help text,
// JSON envelope, exit codes, schema output, error classification) without
// requiring any cloud credentials or network access.
//
// These tests are designed to run in any CI environment in<30 seconds and
// enable external contributors to validate their changes locally.
//
// Reference: https://github.com/TencentCloudAgentRuntime/ags-cli/issues/74
package integ_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// --- Test binary setup ---

var (
	// binaryOnce + binaryPath/buildErr are written under binaryOnce.Do and read
	// afterwards across tests. Safe for sequential tests; NOT safe if any test
	// opts into t.Parallel() — the reads would race the once-init write.
	binaryOnce sync.Once
	binaryPath string
	buildErr   error
)

// testBinary returns the path to the compiled agr binary. It is built once
// per test run and shared across all tests.
func testBinary(t *testing.T) string {
	t.Helper()
	binaryOnce.Do(func() {
		repoRoot := findRepoRoot(t)
		dir, err := os.MkdirTemp("", "agr-integ-*")
		if err != nil {
			buildErr = err
			return
		}
		bin := filepath.Join(dir, "agr")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		// #nosec G204 -- test-only command with controlled args.
		cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "./cmd/agr")
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = &buildError{err: err, output: string(out)}
			return
		}
		binaryPath = bin
	})
	if buildErr != nil {
		t.Fatalf("failed to build agr binary: %v", buildErr)
	}
	return binaryPath
}

type buildError struct {
	err    error
	output string
}

func (e *buildError) Error() string {
	return e.err.Error() + "\n" + e.output
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	mod := strings.TrimSpace(string(out))
	return filepath.Dir(mod)
}

// --- Execution helpers ---

type result struct {
	stdout   string
	stderr   string
	exitCode int
}

// credentialFreeEnv returns a minimal environment for running agr without any
// host credentials leaking in. We whitelist only PATH and HOME/USERPROFILE;
// every TENCENTCLOUD_* and AGR_* var is dropped so tests cannot be influenced
// by a developer's local shell config (e.g. an exported STS token).
//
// There is intentionally no AGR_CONFIG_FILE-style override: the config file
// path is derived from HOME (or the --config flag), so isolating HOME is the
// correct lever.
func credentialFreeEnv(home string) []string {
	env := []string{
		"HOME=" + home,
		"USERPROFILE=" + home,
		"PATH=" + os.Getenv("PATH"),
	}
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "TENCENTCLOUD_") || strings.HasPrefix(e, "AGR_") {
			continue
		}
		// HOME/USERPROFILE/PATH already set explicitly above; skip duplicates.
		if strings.HasPrefix(e, "HOME=") ||
			strings.HasPrefix(e, "USERPROFILE=") ||
			strings.HasPrefix(e, "PATH=") {
			continue
		}
		env = append(env, e)
	}
	return env
}

// execAgr runs the compiled binary with the given env and args, returning
// captured stdout/stderr and exit code. A non-ExitError failure is fatal.
func execAgr(t *testing.T, env []string, args ...string) result {
	t.Helper()
	bin := testBinary(t)
	// #nosec G204 -- test-only command with controlled args.
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run agr: %v", err)
		}
	}
	return result{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		exitCode: exitCode,
	}
}

// run executes agr with the given args in a credential-free environment.
func run(t *testing.T, args ...string) result {
	t.Helper()
	return execAgr(t, credentialFreeEnv(t.TempDir()), args...)
}

// --- JSON Envelope ---

// envelope represents the agr.v1 JSON output format.
type envelope struct {
	SchemaVersion string         `json:"SchemaVersion"`
	Command       string         `json:"Command"`
	Status        string         `json:"Status"`
	Data          map[string]any `json:"Data"`
	Failure       *struct {
		Code    string `json:"Code"`
		Kind    string `json:"Kind"`
		Message string `json:"Message"`
		Fix     string `json:"Fix"`
	} `json:"Failure"`
}

func parseEnvelope(t *testing.T, output string) envelope {
	t.Helper()
	jsonStart := strings.Index(output, "{")
	if jsonStart < 0 {
		t.Fatalf("output does not contain JSON:\n%s", output)
	}
	var env envelope
	if err := json.Unmarshal([]byte(output[jsonStart:]), &env); err != nil {
		t.Fatalf("failed to parse JSON envelope: %v\n%s", err, output)
	}
	return env
}

// =============================================================================
// Tests
// =============================================================================

// --- Help text ---

func TestHelp_RootHelp(t *testing.T) {
	r := run(t, "--help")
	if r.exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", r.exitCode, r.stderr)
	}
	for _, want := range []string{"instance", "tool", "apikey", "config"} {
		if !strings.Contains(r.stdout, want) {
			t.Fatalf("root help missing %q\n%s", want, r.stdout)
		}
	}
}

func TestHelp_SubcommandHelp(t *testing.T) {
	cases := []struct {
		args     []string
		contains []string
	}{
		{args: []string{"instance", "--help"}, contains: []string{"create", "list", "delete"}},
		{args: []string{"tool", "--help"}, contains: []string{"create", "list", "update", "delete"}},
		{args: []string{"instance", "create", "--help"}, contains: []string{"--tool-name", "--timeout"}},
		{args: []string{"apikey", "--help"}, contains: []string{"create", "list", "delete"}},
		{args: []string{"config", "--help"}, contains: []string{"path", "show", "set"}},
	}
	for _, tc := range cases {
		name := strings.Join(tc.args, "_")
		t.Run(name, func(t *testing.T) {
			r := run(t, tc.args...)
			if r.exitCode != 0 {
				t.Fatalf("exit code = %d, want 0\nstderr: %s", r.exitCode, r.stderr)
			}
			for _, want := range tc.contains {
				if !strings.Contains(r.stdout, want) {
					t.Fatalf("help for %v missing %q\n%s", tc.args, want, r.stdout)
				}
			}
		})
	}
}

// --- Schema output ---

func TestSchema_JSONOutputForCommand(t *testing.T) {
	cases := []string{
		"instance.create",
		"instance.list",
		"tool.create",
		"tool.list",
		"apikey.create",
		"apikey.delete",
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			r := run(t, "schema", cmd, "-o", "json")
			if r.exitCode != 0 {
				t.Fatalf("exit code = %d\nstderr: %s\nstdout: %s", r.exitCode, r.stderr, r.stdout)
			}
			env := parseEnvelope(t, r.stdout)
			if env.SchemaVersion != "agr.v1" {
				t.Fatalf("Schema = %q, want %q", env.SchemaVersion, "agr.v1")
			}
			if env.Command != "schema" {
				t.Fatalf("Command = %q, want %q", env.Command, "schema")
			}
			if env.Status != "succeeded" {
				t.Fatalf("Status = %q, want %q", env.Status, "succeeded")
			}
			// Data should have a Name field matching the queried command.
			if name, ok := env.Data["Name"].(string); !ok || name != cmd {
				t.Fatalf("Data.Name = %v, want %q", env.Data["Name"], cmd)
			}
		})
	}
}

func TestSchema_ListAllCommands(t *testing.T) {
	r := run(t, "schema", "-o", "json")
	if r.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", r.exitCode, r.stderr)
	}
	env := parseEnvelope(t, r.stdout)
	commands, ok := env.Data["Commands"]
	if !ok {
		t.Fatal("Data.Commands not present in schema list output")
	}
	cmdList, ok := commands.([]any)
	if !ok || len(cmdList) == 0 {
		t.Fatalf("Commands should be a non-empty array, got: %T", commands)
	}
	// Verify at least some well-known commands are listed.
	output := r.stdout
	for _, want := range []string{"instance.create", "tool.list", "apikey.create"} {
		if !strings.Contains(output, want) {
			t.Fatalf("schema list missing command %q", want)
		}
	}
}

// --- Version ---

func TestVersion_TextOutput(t *testing.T) {
	r := run(t, "version")
	if r.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "agr") {
		t.Fatalf("version output missing 'agr':\n%s", r.stdout)
	}
}

func TestVersion_JSONEnvelope(t *testing.T) {
	r := run(t, "version", "-o", "json")
	if r.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", r.exitCode, r.stderr)
	}
	env := parseEnvelope(t, r.stdout)
	if env.SchemaVersion != "agr.v1" {
		t.Fatalf("Schema = %q, want %q", env.SchemaVersion, "agr.v1")
	}
	if env.Command != "version" {
		t.Fatalf("Command = %q, want %q", env.Command, "version")
	}
	if env.Status != "succeeded" {
		t.Fatalf("Status = %q, want %q", env.Status, "succeeded")
	}
	if _, ok := env.Data["Version"]; !ok {
		t.Fatal("Data.Version not present")
	}
}

// --- Explain ---

func TestExplain_KnownErrorCode(t *testing.T) {
	cases := []struct {
		code     string
		contains string
	}{
		{code: "INVALID_JSON_FLAG", contains: "INVALID_JSON_FLAG"},
		{code: "4", contains: "auth"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			r := run(t, "explain", tc.code)
			if r.exitCode != 0 {
				t.Fatalf("exit code = %d\nstderr: %s", r.exitCode, r.stderr)
			}
			if !strings.Contains(r.stdout, tc.contains) {
				t.Fatalf("explain %s output missing %q\n%s", tc.code, tc.contains, r.stdout)
			}
		})
	}
}

func TestExplain_UnknownErrorCode(t *testing.T) {
	r := run(t, "explain", "TOTALLY_BOGUS_CODE_12345")
	// Should still succeed (exit0) and show "Unknown error code" or similar.
	if r.exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", r.exitCode, r.stderr)
	}
	combined := r.stdout + r.stderr
	if !strings.Contains(combined, "nknown") && !strings.Contains(combined, "not found") && !strings.Contains(combined, "TOTALLY_BOGUS_CODE_12345") {
		t.Fatalf("explain unknown code: unexpected output\n%s", combined)
	}
}

// --- Doctor (no credentials) ---

func TestDoctor_RunsWithoutCredentials(t *testing.T) {
	r := run(t, "doctor")
	// Doctor checks will fail (no credentials), but the command itself should
	// execute and produce diagnostic output. The exit code is not asserted
	// here — it may vary with doctor's implementation, and a strict assertion
	// would make this test brittle. We only care that the credential
	// diagnostics surface in the output.
	combined := r.stdout + r.stderr
	for _, want := range []string{"SecretId", "SecretKey"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("doctor output missing %q\n%s", want, combined)
		}
	}
}

// --- Invalid input / exit codes ---

func TestExitCode_UnknownCommand(t *testing.T) {
	r := run(t, "nonexistent-command-xyz")
	if r.exitCode == 0 {
		t.Fatal("expected non-zero exit code for unknown command")
	}
	if !strings.Contains(r.stderr, "unknown command") {
		t.Fatalf("expected 'unknown command' in stderr\n%s", r.stderr)
	}
}

func TestExitCode_MissingRequiredArgs(t *testing.T) {
	// instance delete without arguments should fail (usage error, exit 2).
	// Assert non-zero rather than ==2 so the test survives changes to the
	// command's arg validation; the exact usage-exit mapping is covered by
	// internal/output unit tests.
	r := run(t, "instance", "delete")
	if r.exitCode == 0 {
		t.Fatalf("expected non-zero exit code for missing required args\nstdout: %s\nstderr: %s", r.stdout, r.stderr)
	}
}

func TestExitCode_InvalidFlag(t *testing.T) {
	r := run(t, "instance", "list", "--nonexistent-flag-xyz")
	if r.exitCode == 0 {
		t.Fatal("expected non-zero exit code for invalid flag")
	}
}

// --- JSON Envelope schema compliance ---

func TestEnvelope_FailureStructure(t *testing.T) {
	// Trigger an error that produces a JSON failure envelope.
	r := run(t, "--output", "json", "instance", "get", "ins-does-not-exist-000")
	// Should fail (no credentials or network).
	if r.exitCode == 0 {
		t.Fatal("expected non-zero exit code")
	}
	env := parseEnvelope(t, r.stdout)
	if env.SchemaVersion != "agr.v1" {
		t.Fatalf("SchemaVersion = %q, want %q", env.SchemaVersion, "agr.v1")
	}
	if env.Status != "failed" {
		t.Fatalf("Status = %q, want %q", env.Status, "failed")
	}
	if env.Failure == nil {
		t.Fatal("Failure should be present for error responses")
	}
	if env.Failure.Code == "" {
		t.Fatal("Failure.Code should not be empty")
	}
	if env.Failure.Kind == "" {
		t.Fatal("Failure.Kind should not be empty")
	}
	if env.Failure.Message == "" {
		t.Fatal("Failure.Message should not be empty")
	}
	// Data should be nil or empty for failure responses (not mixed with success data).
	if len(env.Data) > 0 {
		t.Fatalf("Data should be nil/empty for failure, got: %v", env.Data)
	}
}

func TestEnvelope_SuccessStructure(t *testing.T) {
	r := run(t, "--output", "json", "version")
	if r.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", r.exitCode, r.stderr)
	}
	env := parseEnvelope(t, r.stdout)
	if env.SchemaVersion != "agr.v1" {
		t.Fatalf("SchemaVersion = %q, want %q", env.SchemaVersion, "agr.v1")
	}
	if env.Status != "succeeded" {
		t.Fatalf("Status = %q, want %q", env.Status, "succeeded")
	}
	if env.Failure != nil {
		t.Fatalf("Failure should be nil for success, got: %+v", env.Failure)
	}
	if env.Data == nil {
		t.Fatal("Data should be present for success responses")
	}
	// Command field should be set.
	if env.Command == "" {
		t.Fatal("Command field should not be empty in envelope")
	}
}

func TestEnvelope_FailureHasFix(t *testing.T) {
	// Failure envelope for auth errors should include a Fix hint.
	r := run(t, "--output", "json", "instance", "list")
	if r.exitCode == 0 {
		// If it somehow succeeds (unlikely without creds), skip.
		t.Skip("command succeeded without credentials")
	}
	env := parseEnvelope(t, r.stdout)
	if env.Failure == nil {
		t.Fatalf("expected Failure envelope, got none\nstdout: %s", r.stdout)
	}
	if env.Failure.Kind != "auth" {
		t.Fatalf("Kind = %q, want %q (credential-free instance list should fail as auth)", env.Failure.Kind, "auth")
	}
	if env.Failure.Fix == "" {
		t.Fatalf("auth error should have a non-empty Fix hint, got empty Fix for code=%s", env.Failure.Code)
	}
}

// --- Edge cases / flag interactions ---

func TestEdgeCase_OutputFlagVariants(t *testing.T) {
	// -o json, --output json, --output=json should all work.
	variants := [][]string{
		{"-o", "json", "version"},
		{"--output", "json", "version"},
		{"--output=json", "version"},
	}
	for _, args := range variants {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			r := run(t, args...)
			if r.exitCode != 0 {
				t.Fatalf("exit %d\nstderr: %s", r.exitCode, r.stderr)
			}
			env := parseEnvelope(t, r.stdout)
			if env.SchemaVersion != "agr.v1" {
				t.Fatalf("SchemaVersion = %q", env.SchemaVersion)
			}
		})
	}
}

func TestEdgeCase_DoubleHelpFlag(t *testing.T) {
	// --help --help should not crash.
	r := run(t, "--help", "--help")
	if r.exitCode != 0 {
		t.Fatalf("exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
}

func TestEdgeCase_HelpAfterCommand(t *testing.T) {
	// `agr instance --help` and `agr help instance` should both work.
	r1 := run(t, "instance", "--help")
	r2 := run(t, "help", "instance")
	if r1.exitCode != 0 {
		t.Fatalf("instance --help exit %d", r1.exitCode)
	}
	if r2.exitCode != 0 {
		t.Fatalf("help instance exit %d", r2.exitCode)
	}
	// Both should contain "create".
	if !strings.Contains(r1.stdout, "create") || !strings.Contains(r2.stdout, "create") {
		t.Fatal("both help forms should list subcommands")
	}
}

func TestEdgeCase_EmptyArgs(t *testing.T) {
	// Running agr with no args should show help and exit 0.
	r := run(t)
	if r.exitCode != 0 {
		t.Fatalf("exit %d for bare 'agr'\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "Usage") {
		t.Fatalf("bare 'agr' should show usage:\n%s", r.stdout)
	}
}

// --- Config commands (don't need credentials) ---

func TestConfig_Path(t *testing.T) {
	r := run(t, "config", "path")
	if r.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", r.exitCode, r.stderr)
	}
	// Output may have a hint line; first line should be the path.
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	firstLine := strings.TrimSpace(lines[0])
	if firstLine == "" {
		t.Fatal("config path output is empty")
	}
	if !strings.HasSuffix(firstLine, ".toml") {
		t.Fatalf("config path should end with .toml, got: %s", firstLine)
	}
}

func TestConfig_PathJSON(t *testing.T) {
	r := run(t, "config", "path", "-o", "json")
	if r.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", r.exitCode, r.stderr)
	}
	env := parseEnvelope(t, r.stdout)
	if env.Status != "succeeded" {
		t.Fatalf("Status = %q, want %q", env.Status, "succeeded")
	}
	if _, ok := env.Data["Path"]; !ok {
		t.Fatal("Data.Path should be present in config path JSON output")
	}
}

func TestConfig_SetAndShow(t *testing.T) {
	// Use a dedicated config file in a temp dir.
	home := t.TempDir()
	configFile := filepath.Join(home, ".agr", "config.toml")
	runEnv := credentialFreeEnv(home)

	runWithConfig := func(args ...string) result {
		t.Helper()
		fullArgs := append([]string{"--config", configFile}, args...)
		return execAgr(t, runEnv, fullArgs...)
	}

	// Set a config value.
	r := runWithConfig("config", "set", "region", "ap-shanghai")
	if r.exitCode != 0 {
		t.Fatalf("config set failed: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}

	// Show config to verify the value persists.
	r = runWithConfig("config", "show")
	if r.exitCode != 0 {
		t.Fatalf("config show failed: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "ap-shanghai") {
		t.Fatalf("config show missing 'ap-shanghai':\n%s", r.stdout)
	}

	// Set another value.
	r = runWithConfig("config", "set", "output", "json")
	if r.exitCode != 0 {
		t.Fatalf("config set output failed: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}

	// Show config via JSON output.
	r = runWithConfig("config", "show", "-o", "json")
	if r.exitCode != 0 {
		t.Fatalf("config show -o json failed: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	env := parseEnvelope(t, r.stdout)
	if env.Status != "succeeded" {
		t.Fatalf("Status = %q, want succeeded", env.Status)
	}
}

func TestConfig_SetInvalidKey(t *testing.T) {
	home := t.TempDir()
	configFile := filepath.Join(home, ".agr", "config.toml")
	r := execAgr(t, credentialFreeEnv(home), "--config", configFile, "config", "set", "totally_invalid_key_xyz", "value")
	// Should fail with non-zero exit code and mention "unknown" key.
	if r.exitCode == 0 {
		t.Fatal("expected non-zero exit code for invalid config key")
	}
	combined := r.stdout + r.stderr
	if !strings.Contains(strings.ToLower(combined), "unknown") {
		t.Fatalf("error output should mention 'unknown':\n%s", combined)
	}
}

// --- Completion ---

func TestCompletion_BashOutput(t *testing.T) {
	r := run(t, "completion", "bash")
	if r.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "bash") && !strings.Contains(r.stdout, "complete") {
		t.Fatalf("completion bash output unexpected:\n%s", r.stdout[:min(len(r.stdout), 200)])
	}
}

func TestCompletion_ZshOutput(t *testing.T) {
	r := run(t, "completion", "zsh")
	if r.exitCode != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "zsh") && !strings.Contains(r.stdout, "compdef") {
		t.Fatalf("completion zsh output unexpected:\n%s", r.stdout[:min(len(r.stdout), 200)])
	}
}

// --- Schema metadata correctness (issue #94) ---

func TestSchema_PreCacheImageRegistryTypeIncludesCustom(t *testing.T) {
	// The ImageRegistryType enum in pre-cache-image-task commands should
	// include "custom" in addition to "enterprise" and "personal".
	cmds := []string{
		"pre-cache-image-task.create",
		"pre-cache-image-task.get",
	}
	for _, cmd := range cmds {
		t.Run(cmd, func(t *testing.T) {
			r := run(t, "schema", cmd, "-o", "json")
			if r.exitCode != 0 {
				t.Fatalf("exit code = %d\nstderr: %s\nstdout: %s", r.exitCode, r.stderr, r.stdout)
			}
			env := parseEnvelope(t, r.stdout)
			reqSchema, ok := env.Data["RequestSchema"].(map[string]any)
			if !ok {
				t.Fatalf("Data.RequestSchema not an object: %v", env.Data["RequestSchema"])
			}
			props, ok := reqSchema["Properties"].(map[string]any)
			if !ok {
				t.Fatalf("RequestSchema.Properties not an object: %v", reqSchema["Properties"])
			}
			irt, ok := props["ImageRegistryType"].(map[string]any)
			if !ok {
				t.Fatalf("Properties.ImageRegistryType not an object: %v", props["ImageRegistryType"])
			}
			rawValues, ok := irt["Values"].([]any)
			if !ok {
				t.Fatalf("ImageRegistryType.Values not an array: %v", irt["Values"])
			}
			values := make([]string, len(rawValues))
			for i, v := range rawValues {
				values[i], _ = v.(string)
			}
			for _, want := range []string{"enterprise", "personal", "custom"} {
				found := false
				for _, v := range values {
					if v == want {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("schema %s: ImageRegistryType.Values = %v, missing %q", cmd, values, want)
				}
			}
		})
	}
}
