package patchscenarios

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchcoverage"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

// This explicit opt-in creates and deletes real Registry resources. Credentials
// and the target region must be supplied by the operator; the CLI home is isolated.
// A local run is not a strict, committed-head patch-e2e report.
func TestRegistryLive(t *testing.T) {
	binary := os.Getenv("AGR_REGISTRY_E2E_BINARY")
	if binary == "" {
		t.Skip("requires explicit AGR_REGISTRY_E2E_BINARY and authorized cloud credentials")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("candidate binary must be an absolute path")
	}
	if os.Getenv("AGR_REGION") == "" {
		t.Fatal("AGR_REGION is required")
	}
	id := os.Getenv("AGR_REGISTRY_E2E_SCENARIO")
	if id == "" {
		id = "registry.custom.lifecycle"
	}
	if _, ok := Registry[id]; !ok {
		t.Fatal("unknown Registry scenario")
	}
	plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{{Scenario: id, Assertions: Registry[id].Assertions}}}
	home := t.TempDir()
	env := append(os.Environ(), "HOME="+home, "USERPROFILE="+home)
	scenario := Registry[id]
	run := scenario.Run
	scenario.Run = func(s *patchtest.Session) error {
		err := run(s)
		if err != nil {
			t.Logf("scenario diagnostic: %v", err)
		}
		return err
	}
	results, err := patchtest.Run(t.Context(), plan, patchtest.Registry{id: scenario}, binary, env)
	t.Logf("sanitized scenario results: %+v", results)
	if err != nil {
		t.Fatal(err)
	}
}
