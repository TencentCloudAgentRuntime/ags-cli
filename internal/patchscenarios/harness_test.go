package patchscenarios

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
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
	for _, missing := range harnessFixtureVars {
		t.Run("missing="+missing, func(t *testing.T) {
			t.Setenv("AGR_HARNESS_ROLE_ARN", "qcs::cam::uin/100000000001:roleName/AGSFixtureRole")
			t.Setenv("AGR_HARNESS_SUBNET_ID", "subnet-fixture")
			t.Setenv("AGR_HARNESS_SECURITY_GROUP_ID", "sg-fixture")
			t.Setenv("AGR_HARNESS_TOKENHUB_API_KEY_ID", "key-fixture")
			t.Setenv("AGR_HARNESS_MODEL", "hy3")
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
