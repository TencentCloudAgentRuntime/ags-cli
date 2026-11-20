package patchscenarios

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchcoverage"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

// The full harness lifecycle also deletes the Sandbox Tool that DeleteHarness
// keeps; that typed-SDK call cannot be served by a self-signed loopback
// fixture, so like volume.mount-lifecycle the complete scenario is evidenced
// only by a live run. Command behavior is covered by cmd/agr fake-backend tests.

func TestHarnessAssertionsAreUniqueAndRegistered(t *testing.T) {
	scenario, ok := Registry["harness.lifecycle"]
	if !ok || scenario.Run == nil {
		t.Fatal("harness.lifecycle is not registered")
	}
	sorted := slices.Sorted(slices.Values(scenario.Assertions))
	if len(slices.Compact(slices.Clone(sorted))) != len(sorted) {
		t.Fatalf("duplicate assertion IDs: %v", sorted)
	}
}

func TestHarnessLifecycleRequiresFixture(t *testing.T) {
	root := filepath.Clean("../..")
	binary := filepath.Join(t.TempDir(), "agr-preview")
	build := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-tags=preview", "-o", binary, "./cmd/agr")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{{Scenario: "harness.lifecycle", Assertions: harnessAssertions}}}
	for _, missing := range append(slices.Clone(harnessFixtureVars), harnessContractFixtureEnv) {
		t.Run("missing="+missing, func(t *testing.T) {
			t.Setenv("AGR_HARNESS_ROLE_ARN", "qcs::cam::uin/100000000001:roleName/AGSFixtureRole")
			t.Setenv("AGR_HARNESS_SUBNET_ID", "subnet-fixture")
			t.Setenv("AGR_HARNESS_SECURITY_GROUP_ID", "sg-fixture")
			t.Setenv("AGR_HARNESS_TOKENHUB_API_KEY_ID", "key-fixture")
			t.Setenv("AGR_HARNESS_MODEL", "hy3")
			t.Setenv(harnessContractFixtureEnv, harnessContractTestFixture)
			t.Setenv(missing, "")
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
			defer server.Close()
			if _, err := patchtest.Run(t.Context(), plan, Registry, binary, volumeEnv(t, server.URL)); err == nil {
				t.Fatalf("missing %s produced a passing run", missing)
			}
			if calls.Load() != 0 {
				t.Fatalf("scenario contacted the service without %s", missing)
			}
		})
	}
}

const harnessContractTestFixture = `{
  "SkillReference":"MARKETPLACE/rec-skill/rv-skill",
  "PluginReference":"MARKETPLACE/rec-plugin/rv-plugin",
  "MCPReference":"MARKETPLACE/rec-mcp/rv-mcp",
  "SessionSpaceID":"space-fixture",
  "MCPURL":"https://mcp.example.invalid/mcp",
  "MCPCredential":"cp-fixture/mcp-user/read",
  "OpenAIBaseURL":"https://llm.example.invalid/v1",
  "OpenAICredential":"cp-fixture/model-user/inference",
  "OpenAIModel":"fixture-model"
}`

func TestHarnessContractReadbackRejectsMissingFields(t *testing.T) {
	t.Setenv(harnessContractFixtureEnv, harnessContractTestFixture)
	fixture, err := loadHarnessContractFixture()
	if err != nil {
		t.Fatal(err)
	}
	const response = `{
  "Skills":[{"Source":{"Registry":{"RegistryId":"MARKETPLACE","RecordId":"rec-skill","VersionId":"rv-skill"}}}],
  "Plugins":[{"Source":{"Registry":{"RegistryId":"MARKETPLACE","RecordId":"rec-plugin","VersionId":"rv-plugin"}}}],
  "SessionConfiguration":{"SessionSpaceId":"space-fixture"},
  "MCPServers":[
    {"Name":"registry","Source":{"Registry":{"RegistryId":"MARKETPLACE","RecordId":"rec-mcp","VersionId":"rv-mcp"}}},
    {"Name":"remote","Source":{"Remote":{"URL":"https://mcp.example.invalid/mcp"}},"Credential":{"CredentialProviderId":"cp-fixture","UserId":"mcp-user","Scope":"read"},"Headers":[{"Name":"Authorization","Value":"Bearer ${credential}"}]}
  ]
}`
	for _, missing := range []struct{ check, field string }{
		{"skills", "Skills"}, {"plugins", "Plugins"}, {"session", "SessionConfiguration"},
		{"mcp-registry", "registry"}, {"mcp-remote", "Source"}, {"mcp-credential", "Credential"}, {"mcp-headers", "Headers"},
	} {
		t.Run(missing.check, func(t *testing.T) {
			var spec map[string]any
			if err := json.Unmarshal([]byte(response), &spec); err != nil {
				t.Fatal(err)
			}
			for _, result := range fixture.checkAttachments(spec) {
				if !result.ok {
					t.Fatalf("complete readback failed %s", result.field)
				}
			}
			servers := spec["MCPServers"].([]any)
			switch missing.field {
			case "registry":
				spec["MCPServers"] = servers[1:]
			case "Source", "Credential", "Headers":
				delete(harnessObject(servers[1]), missing.field)
			default:
				delete(spec, missing.field)
			}
			for _, result := range fixture.checkAttachments(spec) {
				if want := result.field != missing.check; result.ok != want {
					t.Errorf("after dropping %s, %s check = %t", missing.field, result.field, result.ok)
				}
			}
		})
	}
}

func TestHarnessCleanupRetriesBoundToolAfterHarnessDisappears(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a Unix mock CLI executable")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "agr")
	const fakeCLI = `#!/bin/sh
set -eu
missing() { printf '%s\n' '{"Status":"failed","Failure":{"Code":"NOT_FOUND","Kind":"not_found"}}'; exit 1; }
case "$1 $2 $3" in
  'harness get hns-fixture')
    test ! -f "$OWNED_FIXTURE_DIR/harness-deleted" || missing
    printf '%s\n' '{"Status":"succeeded","Data":{"Harness":{"ResourceInfo":{"ToolId":"sdt-fixture","DeploymentId":"dep-fixture"}}}}'
    ;;
  'harness delete hns-fixture')
    touch "$OWNED_FIXTURE_DIR/harness-deleted"
    printf '%s\n' '{"Status":"succeeded"}'
    ;;
  'deployment get dep-fixture') missing ;;
  'tool get sdt-fixture')
    test ! -f "$OWNED_FIXTURE_DIR/tool-deleted" || missing
    printf '%s\n' '{"Status":"succeeded"}'
    ;;
  'tool delete sdt-fixture')
    if test ! -f "$OWNED_FIXTURE_DIR/tool-delete-attempted"; then
      touch "$OWNED_FIXTURE_DIR/tool-delete-attempted"
      printf '%s\n' '{"Status":"failed","Failure":{"Code":"TEMPORARY","Kind":"network"}}'
      exit 1
    fi
    touch "$OWNED_FIXTURE_DIR/tool-deleted"
    printf '%s\n' '{"Status":"succeeded"}'
    ;;
  *) exit 9 ;;
esac
`
	if err := os.WriteFile(binary, []byte(fakeCLI), 0o700); err != nil {
		t.Fatal(err)
	}
	const scenario = "harness.cleanup"
	registry := patchtest.Registry{scenario: {Run: func(s *patchtest.Session) error {
		owned := harnessCleanup{harnessID: "hns-fixture"}
		s.Cleanup(func(ctx context.Context) error { return owned.remove(s, ctx) })
		return owned.remove(s, s.Context)
	}}}
	plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{{Scenario: scenario}}}
	results, err := patchtest.Run(t.Context(), plan, registry, binary, append(os.Environ(), "OWNED_FIXTURE_DIR="+dir))
	if err == nil || len(results) != 1 || results[0].Cleanup != "pass" {
		t.Fatalf("expected first delete failure followed by successful cleanup retry, results=%+v, err=%v", results, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tool-deleted")); err != nil {
		t.Fatalf("cleanup skipped the Tool after the Harness disappeared: %v", err)
	}
}
