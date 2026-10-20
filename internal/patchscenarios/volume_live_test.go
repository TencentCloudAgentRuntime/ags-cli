package patchscenarios

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchcoverage"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

// Canonical mount references no longer have overlay coverage bindings. Keep the
// complete mount scenario selectable independently of the remaining patch plan.
func volumeMountAcceptancePlan() patchcoverage.Plan {
	const id = "volume.mount-lifecycle"
	return patchcoverage.Plan{Bindings: []patchcoverage.Binding{{Scenario: id, Assertions: Registry[id].Assertions}}}
}

func TestVolumeMountAcceptancePlan(t *testing.T) {
	plan := volumeMountAcceptancePlan()
	if len(plan.Bindings) != 1 || plan.Bindings[0].Scenario != "volume.mount-lifecycle" {
		t.Fatalf("canonical mount scenario not selected: %+v", plan)
	}
	want := Registry["volume.mount-lifecycle"].Assertions
	if len(want) == 0 || !slices.Equal(plan.Bindings[0].Assertions, want) {
		t.Fatalf("canonical mount assertions incomplete: %+v", plan)
	}
}

// The preview candidate provides Volume management for fixture setup and
// cleanup. This explicit opt-in exercises the canonical mounts against the real
// service; it is separate from the strict overlay-only report.
func TestVolumeMountLive(t *testing.T) {
	binary := os.Getenv("AGR_VOLUME_MOUNT_E2E_BINARY")
	if binary == "" {
		t.Skip("requires explicit AGR_VOLUME_MOUNT_E2E_BINARY and authorized cloud credentials")
	}
	if !filepath.IsAbs(binary) || os.Getenv("AGR_REGION") == "" {
		t.Fatal("absolute preview candidate binary and AGR_REGION are required")
	}
	if _, err := volumeMountStorageFromEnv(); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	env := append(os.Environ(), "HOME="+home, "USERPROFILE="+home)
	scenario := Registry["volume.mount-lifecycle"]
	run := scenario.Run
	scenario.Run = func(s *patchtest.Session) error {
		err := run(s)
		if err != nil {
			t.Logf("mount scenario diagnostic: %v", err)
		}
		return err
	}
	registry := patchtest.Registry{"volume.mount-lifecycle": scenario}
	results, err := patchtest.Run(t.Context(), volumeMountAcceptancePlan(), registry, binary, env)
	t.Logf("sanitized mount scenario results: %+v", results)
	if err != nil {
		t.Fatal(err)
	}
}
