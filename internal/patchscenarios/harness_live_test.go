package patchscenarios

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchcoverage"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

// TestHarnessLive runs only the harness lifecycle against a real backend; the
// strict patch runner selects every overlay scenario at once.
func TestHarnessLive(t *testing.T) {
	binary := os.Getenv("AGR_HARNESS_E2E_BINARY")
	if binary == "" {
		t.Skip("requires explicit AGR_HARNESS_E2E_BINARY and authorized cloud credentials")
	}
	if !filepath.IsAbs(binary) || os.Getenv("AGR_REGION") == "" {
		t.Fatal("absolute candidate binary and AGR_REGION are required")
	}
	const id = "harness.lifecycle"
	plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{{Scenario: id, Assertions: Registry[id].Assertions}}}
	home := t.TempDir()
	// Reuse an existing config file (credentials, region, domain) without
	// putting secrets on command lines or in the process environment.
	if source := os.Getenv("AGR_HARNESS_E2E_CONFIG"); source != "" {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(home, ".agr"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".agr", "config.toml"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	env := append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "AGR_NO_UPDATE_CHECK=1")
	results, err := patchtest.Run(t.Context(), plan, Registry, binary, env)
	if harnessLastFailure != "" {
		t.Logf("first failing step: %s", harnessLastFailure)
	}
	t.Logf("sanitized scenario results: %+v", results)
	if err != nil {
		t.Fatal(err)
	}
}
