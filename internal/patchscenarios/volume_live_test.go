package patchscenarios

import (
	"fmt"
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

// Keep this acceptance contract independent of the registry and selected plan.
// Changing the canonical obligations requires an explicit change to this list.
func canonicalVolumeMountAssertions() []string {
	return []string{
		"mount.cleanup",
		"mount.data-path",
		"mount.fork",
		"mount.invalid-reference",
		"mount.readonly",
		"mount.reclaim.delete",
		"mount.reclaim.retain",
		"mount.template-ref",
		"mount.template.isolation",
		"mount.template.reuse",
		"mount.volume-ref",
		"mount.volume-ref.by-id",
	}
}

func checkCanonicalVolumeMountAssertions(assertions []string) error {
	want := canonicalVolumeMountAssertions()
	if !slices.Equal(slices.Sorted(slices.Values(assertions)), slices.Sorted(slices.Values(want))) {
		return fmt.Errorf("canonical mount assertions = %v, want %v", assertions, want)
	}
	return nil
}

func TestCanonicalVolumeMountAssertionSet(t *testing.T) {
	assertions := canonicalVolumeMountAssertions()
	reversed := slices.Clone(assertions)
	slices.Reverse(reversed)
	renamed := slices.Clone(assertions)
	renamed[0] = "mount.unexpected"
	duplicated := slices.Clone(assertions)
	duplicated[1] = duplicated[0]
	for _, tc := range []struct {
		name       string
		assertions []string
		valid      bool
	}{
		{"registered", Registry["volume.mount-lifecycle"].Assertions, true},
		{"reordered", reversed, true},
		{"empty", nil, false},
		{"renamed", renamed, false},
		{"duplicated", duplicated, false},
		{"extra", append(slices.Clone(assertions), "mount.unexpected"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkCanonicalVolumeMountAssertions(tc.assertions); (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
}

func TestVolumeMountAcceptanceRequiresEveryAssertion(t *testing.T) {
	const id = "volume.mount-lifecycle"
	assertions := canonicalVolumeMountAssertions()
	if err := checkCanonicalVolumeMountAssertions(Registry[id].Assertions); err != nil {
		t.Fatal(err)
	}
	plan := volumeMountAcceptancePlan()
	if len(plan.Bindings) != 1 || plan.Bindings[0].Scenario != id {
		t.Fatalf("canonical mount plan = %+v", plan)
	}
	if err := checkCanonicalVolumeMountAssertions(plan.Bindings[0].Assertions); err != nil {
		t.Fatal(err)
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
			results, err := patchtest.Run(t.Context(), plan, registry, os.Args[0], []string{"GORACE=atexit_sleep_ms=0"})
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

func TestVolumeMountAcceptanceRejectsShrinkingRegistryAndExecution(t *testing.T) {
	const id = "volume.mount-lifecycle"
	assertions := canonicalVolumeMountAssertions()
	for i, removed := range assertions {
		t.Run(removed, func(t *testing.T) {
			reduced := slices.Delete(slices.Clone(assertions), i, i+1)
			registry := patchtest.Registry{id: {Assertions: reduced, Run: func(s *patchtest.Session) error {
				s.NoResources() // This synthetic scenario never creates cloud resources.
				if _, err := s.CLI(s.Context, "-test.run=^$"); err != nil {
					return err
				}
				for _, assertion := range reduced {
					if err := s.Assert(assertion, true); err != nil {
						return err
					}
				}
				return nil
			}}}
			plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{{Scenario: id, Assertions: reduced}}}
			results, err := patchtest.Run(t.Context(), plan, registry, os.Args[0], []string{"GORACE=atexit_sleep_ms=0"})
			if err != nil || len(results) != 1 || results[0].Status != "pass" || len(results[0].Assertions) != len(reduced) || results[0].Calls != 1 || results[0].Cleanup != "pass" {
				t.Fatalf("self-consistent reduced scenario: results=%+v, error=%v", results, err)
			}
			// The runner can accept a consistently reduced scenario. The independent
			// canonical contract must still reject its registration and selection.
			if checkCanonicalVolumeMountAssertions(registry[id].Assertions) == nil || checkCanonicalVolumeMountAssertions(plan.Bindings[0].Assertions) == nil {
				t.Fatal("accepted a canonical assertion removed from registration, selection and execution")
			}
		})
	}
}

// The preview candidate provides Volume management for fixture setup and
// cleanup. This explicit opt-in exercises the canonical mounts against the real
// service; it is separate from the strict overlay-only report.
func TestVolumeMountLive(t *testing.T) {
	plan := volumeMountAcceptancePlan()
	if len(plan.Bindings) != 1 || plan.Bindings[0].Scenario != "volume.mount-lifecycle" {
		t.Fatalf("canonical mount plan = %+v", plan)
	}
	if err := checkCanonicalVolumeMountAssertions(plan.Bindings[0].Assertions); err != nil {
		t.Fatal(err)
	}
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
	results, err := patchtest.Run(t.Context(), plan, registry, binary, env)
	t.Logf("sanitized mount scenario results: %+v", results)
	if err != nil {
		t.Fatal(err)
	}
}
