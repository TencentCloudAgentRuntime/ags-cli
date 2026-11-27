package cli_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

// These tests pin the five schema/runtime drift categories tracked in issue
// #158. Every runtime assertion goes through the real CLI so the schema and the
// observed failure code cannot silently disagree again.

func TestSchemaLeafCommandsDeclareUsageFailures(t *testing.T) {
	catalog := schemaCatalog(t)
	leaves := 0
	for name, schema := range catalog {
		// Groups only dispatch to their children and never validate positionals.
		if schema.Kind != "command" {
			continue
		}
		leaves++
		if !schema.declares("INVALID_USAGE") {
			t.Errorf("%s is a leaf command but does not declare INVALID_USAGE", name)
		}
		// A required positional after `--` is validated by the remote execution
		// command, which reports a separator-specific code instead.
		validatedRequired := slices.ContainsFunc(schema.Args, func(arg schemaArg) bool {
			return arg.Required && !arg.AfterDash
		})
		if validatedRequired && !schema.declares("MISSING_REQUIRED_ARG") {
			t.Errorf("%s has a required positional but does not declare MISSING_REQUIRED_ARG", name)
		}
	}
	if leaves == 0 {
		t.Fatal("schema catalog contains no leaf commands")
	}
}

func TestSchemaSupportsDryRunMatchesFlagPresence(t *testing.T) {
	catalog := schemaCatalog(t)
	for name, schema := range catalog {
		if schema.Kind != "command" {
			continue
		}
		if schema.hasFlag("dry-run") && !schema.SupportsDryRun {
			t.Errorf("%s exposes --dry-run but reports SupportsDryRun=false", name)
		}
	}

	expected := []string{"instance.delete", "tool.delete"}
	if apimeta.BuildChannel == apimeta.Preview {
		expected = append(expected,
			"harness.create", "harness.update", "harness.delete",
			"model-provider.create", "model-provider.update",
		)
	}
	for _, name := range expected {
		schema := commandSchemaFor(t, catalog, name)
		if !schema.SupportsDryRun {
			t.Errorf("%s reports SupportsDryRun=false although it exposes --dry-run", name)
		}
	}
}

func TestSchemaArgMetadataMatchesRuntime(t *testing.T) {
	catalog := schemaCatalog(t)
	for _, tc := range []struct {
		name     string
		arg      string
		required bool
		variadic bool
	}{
		// instance.pause takes exactly one instance id.
		{name: "instance.pause", arg: "InstanceId", required: true, variadic: false},
		// instance.delete and tool.delete accept several ids.
		{name: "instance.delete", arg: "InstanceId", required: true, variadic: true},
		{name: "tool.delete", arg: "ToolId", required: true, variadic: true},
		// optional_positional:true used to hide this required snapshot id.
		{name: "instance.snapshot.delete", arg: "SnapshotId", required: true, variadic: false},
	} {
		schema := commandSchemaFor(t, catalog, tc.name)
		if len(schema.Args) == 0 || schema.Args[0].Name != tc.arg {
			t.Errorf("%s Args = %+v, want first arg %q", tc.name, schema.Args, tc.arg)
			continue
		}
		if got := schema.Args[0]; got.Required != tc.required || got.Variadic != tc.variadic {
			t.Errorf("%s Args[0] = %+v, want Required=%t Variadic=%t", tc.name, got, tc.required, tc.variadic)
		}
	}

	// The variadic marker is not cosmetic: two ids must pass argument
	// validation, and --dry-run keeps the assertion offline so a leaked
	// credential can never delete a real instance.
	result := runSchemaCLI(t, jsonArgs("instance", "delete", "ins-a", "ins-b", "--dry-run")...)
	if result.exitCode != 0 {
		t.Fatalf("instance.delete --dry-run failed: exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
	}
	var deleteEnvelope struct {
		Data struct {
			DryRun      bool     `json:"DryRun"`
			WouldDelete []string `json:"WouldDelete"`
		} `json:"Data"`
	}
	if err := decodeEnvelope(t, result, &deleteEnvelope); err != nil {
		t.Fatal(err)
	}
	if !deleteEnvelope.Data.DryRun {
		t.Fatalf("instance.delete --dry-run did not report DryRun: %s", result.stdout)
	}
	if want := []string{"ins-a", "ins-b"}; !slices.Equal(deleteEnvelope.Data.WouldDelete, want) {
		t.Fatalf("instance.delete WouldDelete = %v, want %v", deleteEnvelope.Data.WouldDelete, want)
	}
	// instance.pause takes one id only.
	result = runSchemaCLI(t, jsonArgs("instance", "pause", "ins-a", "ins-b")...)
	if result.exitCode != 2 || decodeFailure(t, result).Failure.Code != "INVALID_USAGE" {
		t.Fatalf("instance.pause accepted two ids: exit=%d %s", result.exitCode, result.stdout)
	}
	// The request-only form still covers the positional for delete commands.
	// --request cannot be combined with --dry-run, so the isolated environment
	// proves it through the exact runtime code instead: a bulk delete with no
	// credentials fails per id and reports PARTIAL_DELETE_FAILED.
	result = runSchemaCLI(t, jsonArgs("instance", "delete", "--request", `{"InstanceId":"ins-a"}`)...)
	if code := decodeEnvelopeFailure(t, result); code != "PARTIAL_DELETE_FAILED" {
		t.Fatalf("instance.delete --request-only form = %s, want PARTIAL_DELETE_FAILED", code)
	}
	result = runSchemaCLI(t, jsonArgs("instance", "snapshot", "delete", "--request", `{"SnapshotId":"snapshot-a"}`)...)
	if code := decodeEnvelopeFailure(t, result); code != "MISSING_CLOUD_CREDENTIALS" {
		t.Fatalf("instance.snapshot.delete --request-only form = %s, want MISSING_CLOUD_CREDENTIALS", code)
	}
}

func TestMissingRequiredArgRuntimeAndSchema(t *testing.T) {
	catalog := schemaCatalog(t)
	cases := []struct {
		command     string
		args        []string
		previewOnly bool
	}{
		{command: "instance.get", args: []string{"instance", "get"}},
		{command: "instance.pause", args: []string{"instance", "pause"}},
		{command: "instance.delete", args: []string{"instance", "delete"}},
		{command: "instance.lookup", args: []string{"instance", "lookup"}},
		{command: "instance.snapshot.delete", args: []string{"instance", "snapshot", "delete"}},
		{command: "instance.file.upload", args: []string{"instance", "file", "upload"}},
		{command: "instance.mobile.adb", args: []string{"instance", "mobile", "adb"}},
		{command: "tool.get", args: []string{"tool", "get"}},
		{command: "tool.delete", args: []string{"tool", "delete"}},
		{command: "apikey.delete", args: []string{"apikey", "delete"}},
		{command: "api.call", args: []string{"api", "call"}},
		{command: "completion", args: []string{"completion"}},
		{command: "explain", args: []string{"explain"}},
		{command: "config.set", args: []string{"config", "set"}},
		{command: "config.set", args: []string{"config", "set", "region"}},
		{command: "credential.provider.get", args: []string{"credential", "provider", "get"}},
		{command: "deployment.get", args: []string{"deployment", "get"}},
		{command: "gpu.group.delete", args: []string{"gpu", "group", "delete"}},
		{command: "identity.get", args: []string{"identity", "get"}},
		{command: "harness.get", args: []string{"harness", "get"}, previewOnly: true},
		{command: "model-provider.get", args: []string{"model-provider", "get"}, previewOnly: true},
		{command: "harness.revision.get", args: []string{"harness", "revision", "get"}, previewOnly: true},
	}
	for _, tc := range cases {
		if tc.previewOnly && apimeta.BuildChannel != apimeta.Preview {
			continue
		}
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			if !commandSchemaFor(t, catalog, tc.command).declares("MISSING_REQUIRED_ARG") {
				t.Errorf("%s does not declare MISSING_REQUIRED_ARG", tc.command)
			}
			result := runSchemaCLI(t, jsonArgs(tc.args...)...)
			if result.exitCode != 2 {
				t.Fatalf("exit = %d, want 2; stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
			}
			failure := decodeFailure(t, result)
			if failure.Failure.Code != "MISSING_REQUIRED_ARG" || failure.Failure.Kind != "usage" || failure.Failure.Retryable {
				t.Fatalf("failure = %+v, want MISSING_REQUIRED_ARG/usage", failure.Failure)
			}
			if !strings.Contains(failure.Failure.Hint, "agr ") {
				t.Errorf("hint is not actionable: %q", failure.Failure.Hint)
			}
		})
	}
}

func TestInvalidUsageStaysForMalformedArguments(t *testing.T) {
	catalog := schemaCatalog(t)
	for _, tc := range []struct {
		command string
		args    []string
	}{
		{command: "instance.pause", args: []string{"instance", "pause", "ins-a", "ins-b"}},
		{command: "explain", args: []string{"explain", "A", "B"}},
		{command: "completion", args: []string{"completion", "bash", "zsh"}},
		{command: "config.set", args: []string{"config", "set", "a", "b", "c"}},
		// An empty key is a malformed pair, not a missing argument.
		{command: "config.set", args: []string{"config", "set", "=value"}},
		// --token is explicit but empty, which is malformed rather than absent.
		{command: "config.set", args: []string{"config", "set", "--token="}},
		{command: "config.set", args: []string{"--token=", "config", "set"}},
		{command: "config.set", args: []string{"--token=tok-123", "config", "set", "region", "ap-guangzhou"}},
		{command: "schema", args: []string{"schema", "a", "b"}},
		{command: "gpu.group.delete", args: []string{"gpu", "group", "delete", "gpu-a", "gpu-b"}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			if !commandSchemaFor(t, catalog, tc.command).declares("INVALID_USAGE") {
				t.Errorf("%s does not declare INVALID_USAGE", tc.command)
			}
			result := runSchemaCLI(t, jsonArgs(tc.args...)...)
			if result.exitCode != 2 {
				t.Fatalf("exit = %d, want 2; stdout=%s", result.exitCode, result.stdout)
			}
			if failure := decodeFailure(t, result); failure.Failure.Code != "INVALID_USAGE" || failure.Failure.Kind != "usage" {
				t.Fatalf("failure = %+v, want INVALID_USAGE/usage", failure.Failure)
			}
		})
	}

	// Scripts that unconditionally forward `--token="$TOKEN"` must still be able
	// to run `config set <key> <value>` when TOKEN is empty.
	result := runSchemaCLI(t, "--token=", "config", "set", "region", "ap-guangzhou", "-o", "json")
	if result.exitCode != 0 || len(result.stderr) != 0 {
		t.Fatalf("empty global --token= broke config set: exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
	}
	var setEnv struct {
		Status string `json:"Status"`
		Data   struct {
			Key   string `json:"Key"`
			Value string `json:"Value"`
			Path  string `json:"Path"`
		} `json:"Data"`
	}
	if err := decodeEnvelope(t, result, &setEnv); err != nil {
		t.Fatal(err)
	}
	if setEnv.Status != "succeeded" || setEnv.Data.Key != "region" || setEnv.Data.Value != "ap-guangzhou" {
		t.Fatalf("unexpected config set envelope: %+v", setEnv)
	}
}

func TestRequestModeStillSuppliesRequiredArguments(t *testing.T) {
	catalog := schemaCatalog(t)
	for _, tc := range []struct {
		name    string
		args    []string
		code    string
		command string
	}{
		{
			// Deliberate backwards-compatible exception: api.call validates the
			// action from --request before it validates the request body, so an
			// empty object reports MISSING_ACTION even though the command also
			// declares MISSING_REQUIRED_ARG. api.call declares both codes.
			name:    "api.call keeps MISSING_ACTION",
			args:    []string{"api", "call", "--request", "{}", "-o", "json"},
			code:    "MISSING_ACTION",
			command: "api.call",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !commandSchemaFor(t, catalog, tc.command).declares(tc.code) {
				t.Errorf("%s does not declare %s", tc.command, tc.code)
			}
			result := runSchemaCLI(t, tc.args...)
			if result.exitCode != 2 || decodeFailure(t, result).Failure.Code != tc.code {
				t.Fatalf("want %s at exit 2, got exit=%d %s", tc.code, result.exitCode, result.stdout)
			}
		})
	}

	// api.call declares both halves of its input contract.
	apiCall := commandSchemaFor(t, catalog, "api.call")
	for _, code := range []string{"MISSING_REQUIRED_ARG", "MISSING_ACTION"} {
		if !apiCall.declares(code) {
			t.Errorf("api.call does not declare %s", code)
		}
	}

	// A required flag still wins over the request-only shortcut.
	result := runSchemaCLI(t, "api", "call", "DescribeSandboxInstanceList", "-o", "json")
	if code := decodeFailure(t, result).Failure.Code; code != "MISSING_REQUIRED_FLAG" {
		t.Fatalf("api.call without --request: exit=%d %s", result.exitCode, result.stdout)
	}
}

func TestInstanceExecSeparatorContract(t *testing.T) {
	catalog := schemaCatalog(t)
	schema := commandSchemaFor(t, catalog, "instance.exec")
	if !schema.declares("MISSING_COMMAND_SEPARATOR") {
		t.Error("instance.exec does not declare MISSING_COMMAND_SEPARATOR")
	}
	// instance.exec validates the remote command itself after `--`, so the
	// schema must not promise MISSING_REQUIRED_ARG for its required Command arg.
	if schema.declares("MISSING_REQUIRED_ARG") {
		t.Errorf("instance.exec declares MISSING_REQUIRED_ARG but never returns it: %v", schema.Failures)
	}

	for _, args := range [][]string{
		{"instance", "exec"},
		{"instance", "exec", "--"},
		{"instance", "exec", "ins-a"},
		{"instance", "exec", "ins-a", "--"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			result := runSchemaCLI(t, jsonArgs(args...)...)
			if result.exitCode != 2 {
				t.Fatalf("exit = %d, want 2; stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
			}
			if failure := decodeFailure(t, result); failure.Failure.Code != "MISSING_COMMAND_SEPARATOR" || failure.Failure.Kind != "usage" {
				t.Fatalf("failure = %+v, want MISSING_COMMAND_SEPARATOR/usage", failure.Failure)
			}
		})
	}

	// With a separator and a command the CLI proceeds past argument validation
	// and only stops at the missing credentials of this test environment.
	result := runSchemaCLI(t, jsonArgs("instance", "exec", "--", "echo", "hi")...)
	if code := decodeFailure(t, result).Failure.Code; code == "MISSING_REQUIRED_ARG" || code == "MISSING_COMMAND_SEPARATOR" {
		t.Fatalf("instance exec -- echo hi hit %s, want a runtime (auth) failure", code)
	}
}

func TestExplainMissingCommandSeparator(t *testing.T) {
	result := runSchemaCLI(t, "explain", "MISSING_COMMAND_SEPARATOR", "-o", "json")
	if result.exitCode != 0 {
		t.Fatalf("explain failed: exit=%d stderr=%s", result.exitCode, result.stderr)
	}
	var envelope struct {
		Data struct {
			Code             string   `json:"Code"`
			Kind             string   `json:"Kind"`
			ExitCode         int      `json:"ExitCode"`
			Retryable        bool     `json:"Retryable"`
			Meaning          string   `json:"Meaning"`
			AffectedCommands []string `json:"AffectedCommands"`
			Fix              []string `json:"Fix"`
		} `json:"Data"`
	}
	if err := decodeEnvelope(t, result, &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope.Data
	if data.Code != "MISSING_COMMAND_SEPARATOR" || data.Kind != "usage" || data.ExitCode != 2 || data.Retryable {
		t.Fatalf("explain = %+v, want usage/exit 2/not retryable", data)
	}
	if strings.Contains(data.Meaning, "is not registered") {
		t.Fatalf("MISSING_COMMAND_SEPARATOR is still unregistered: %q", data.Meaning)
	}
	if !slices.Contains(data.AffectedCommands, "instance.exec") {
		t.Fatalf("AffectedCommands = %v, want instance.exec", data.AffectedCommands)
	}
	if len(data.Fix) == 0 {
		t.Fatal("explain returned no fix")
	}
}

func TestToolDeleteDeclaresPartialDeleteFailed(t *testing.T) {
	catalog := schemaCatalog(t)
	for _, command := range []string{"tool.delete", "instance.delete"} {
		if !commandSchemaFor(t, catalog, command).declares("PARTIAL_DELETE_FAILED") {
			t.Errorf("%s does not declare PARTIAL_DELETE_FAILED", command)
		}
	}
	result := runSchemaCLI(t, "explain", "PARTIAL_DELETE_FAILED", "-o", "json")
	if result.exitCode != 0 {
		t.Fatalf("explain PARTIAL_DELETE_FAILED failed: exit=%d stderr=%s", result.exitCode, result.stderr)
	}
	var envelope struct {
		Data struct {
			Kind             string   `json:"Kind"`
			ExitCode         int      `json:"ExitCode"`
			AffectedCommands []string `json:"AffectedCommands"`
		} `json:"Data"`
	}
	if err := decodeEnvelope(t, result, &envelope); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(envelope.Data), "is not registered") {
		t.Fatalf("PARTIAL_DELETE_FAILED is not explainable: %+v", envelope.Data)
	}
	// A partial bulk delete carries a business result, not a usage error: the
	// runtime reports kind=partial at exit 1.
	if envelope.Data.Kind != "partial" || envelope.Data.ExitCode != 1 {
		t.Fatalf("explain = %+v, want partial/exit 1", envelope.Data)
	}
	for _, command := range []string{"tool.delete", "instance.delete"} {
		if !slices.Contains(envelope.Data.AffectedCommands, command) {
			t.Errorf("AffectedCommands = %v, want %s", envelope.Data.AffectedCommands, command)
		}
	}
}

// Commands attached only through the command tree (rather than a registered
// schema seed) must still be reachable from the schema and from explain.
func TestUpdateAppearsInSchemaExplainAndRuntime(t *testing.T) {
	catalog := schemaCatalog(t)
	if !commandSchemaFor(t, catalog, "update").declares("INVALID_USAGE") {
		t.Fatal("update does not declare INVALID_USAGE")
	}

	result := runSchemaCLI(t, "explain", "INVALID_USAGE", "-o", "json")
	if result.exitCode != 0 {
		t.Fatalf("explain INVALID_USAGE failed: exit=%d stderr=%s", result.exitCode, result.stderr)
	}
	var envelope struct {
		Data struct {
			AffectedCommands []string `json:"AffectedCommands"`
		} `json:"Data"`
	}
	if err := decodeEnvelope(t, result, &envelope); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(envelope.Data.AffectedCommands, "update") {
		t.Fatalf("AffectedCommands = %v, want update", envelope.Data.AffectedCommands)
	}

	result = runSchemaCLI(t, jsonArgs("update", "--unknown-flag")...)
	if result.exitCode != 2 || decodeFailure(t, result).Failure.Code != "INVALID_USAGE" {
		t.Fatalf("update --unknown-flag: exit=%d %s", result.exitCode, result.stdout)
	}
}

// A developer shell with AGR_* config or credentials must never leak into the
// helper process: it would both hide auth failures and risk real API calls.
func TestSchemaHelperIgnoresParentAGRAndCloudEnv(t *testing.T) {
	t.Setenv("AGR_AUTH.SECRET_ID", "fake-id")
	t.Setenv("AGR_AUTH.SECRET_KEY", "fake-key")
	t.Setenv("AGR_DEBUG", "1")
	t.Setenv("TENCENTCLOUD_SECRET_ID", "fake-id")
	t.Setenv("TENCENTCLOUD_SECRET_KEY", "fake-key")

	result := runSchemaCLI(t, "schema", "-o", "json")
	if result.exitCode != 0 || len(result.stderr) != 0 {
		t.Fatalf("schema leaked parent env: exit=%d stderr=%q", result.exitCode, result.stderr)
	}
	result = runSchemaCLI(t, jsonArgs("instance", "get", "ins-a")...)
	if result.exitCode != 4 || decodeFailure(t, result).Failure.Code != "MISSING_CLOUD_CREDENTIALS" {
		t.Fatalf("leaked credentials: exit=%d %s", result.exitCode, result.stdout)
	}
}
