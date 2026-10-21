package patchscenarios

import (
	"os"
	"path/filepath"
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

func TestVolumeMountAcceptanceRequiresEveryAssertion(t *testing.T) {
	const id = "volume.mount-lifecycle"
	assertions := Registry[id].Assertions
	if len(assertions) == 0 {
		t.Fatal("mount scenario has no assertions")
	}
	for _, omitted := range append([]string{""}, assertions...) {
		name, wantStatus, wantCount := "complete", "pass", len(assertions)
		if omitted != "" {
			name, wantStatus, wantCount = "missing "+omitted, "fail", len(assertions)-1
		}
		t.Run(name, func(t *testing.T) {
			registry := patchtest.Registry{id: {Assertions: assertions, Run: func(s *patchtest.Session) error {
				s.NoResources()
				// A local test binary supplies the runner's required subprocess call.
				// No cloud API or real mount scenario is invoked by this unit test.
				if _, err := s.CLI(s.Context, "-test.run=^$"); err != nil {
					return err
				}
				for _, assertion := range assertions {
					if assertion != omitted {
						if err := s.Assert(assertion, true); err != nil {
							return err
						}
					}
				}
				return nil
			}}}
			results, err := patchtest.Run(t.Context(), volumeMountAcceptancePlan(), registry, os.Args[0], []string{"GORACE=atexit_sleep_ms=0"})
			if (err != nil) != (omitted != "") || len(results) != 1 {
				t.Fatalf("omitted %q: results=%+v, error=%v", omitted, results, err)
			}
			result := results[0]
			if result.Scenario != id || result.Status != wantStatus || len(result.Assertions) != wantCount || result.Calls != 1 || result.Cleanup != "pass" {
				t.Fatalf("omitted %q: result=%+v", omitted, result)
			}
		})
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
