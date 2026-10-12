package patchscenarios

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchcoverage"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

// Promotion removes Session from overlay-driven runs. Keep an explicit opt-in
// entry point for real lifecycle/EventCount acceptance with cleanup reporting.
func TestSessionLive(t *testing.T) {
	binary := os.Getenv("AGR_SESSION_E2E_BINARY")
	if binary == "" {
		t.Skip("requires explicit AGR_SESSION_E2E_BINARY and authorized cloud credentials")
	}
	if !filepath.IsAbs(binary) || os.Getenv("AGR_REGION") == "" {
		t.Fatal("absolute candidate binary and AGR_REGION are required")
	}
	for _, id := range []string{"session.lifecycle", "session.event-count"} {
		t.Run(id, func(t *testing.T) {
			plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{{Scenario: id, Assertions: Registry[id].Assertions}}}
			home := t.TempDir()
			env := append(os.Environ(), "HOME="+home, "USERPROFILE="+home)
			results, err := patchtest.Run(t.Context(), plan, Registry, binary, env)
			t.Logf("sanitized scenario results: %+v", results)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
