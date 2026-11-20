package patchscenarios

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchcoverage"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

// Run the Endpoint scenario alone in a test account. This targeted run does
// not produce the full committed-head patch report.
func TestEndpointLive(t *testing.T) {
	binary := os.Getenv("AGR_ENDPOINT_E2E_BINARY")
	if binary == "" {
		t.Skip("requires an explicit candidate binary and authorized cloud credentials")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("candidate binary must be an absolute path")
	}
	for _, key := range []string{
		"AGR_CLOUD_ENDPOINT", "AGR_REGION", "AGR_ENDPOINT_FIXTURE_RECORD_ID",
		"TENCENTCLOUD_SECRET_ID", "TENCENTCLOUD_SECRET_KEY",
	} {
		if os.Getenv(key) == "" {
			t.Fatalf("%s is required", key)
		}
	}
	const id = "endpoint.lifecycle"
	fullPlan, err := patchcoverage.Load("../..", Registry.Coverage(), true)
	if err != nil {
		t.Fatal(err)
	}
	var targetPlan patchcoverage.Plan
	for _, binding := range fullPlan.Bindings {
		if binding.Scenario == id {
			targetPlan.Bindings = append(targetPlan.Bindings, binding)
		}
	}
	if len(targetPlan.Bindings) == 0 {
		t.Fatal("Endpoint coverage bindings are missing")
	}
	home := t.TempDir()
	env := append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "AGR_NON_INTERACTIVE=1")
	scenario := Registry[id]
	run := scenario.Run
	scenario.Run = func(s *patchtest.Session) error {
		err := run(s)
		if err != nil {
			t.Logf("Endpoint scenario diagnostic: %v", err)
		}
		return err
	}
	results, err := patchtest.Run(t.Context(), targetPlan, patchtest.Registry{id: scenario}, binary, env)
	t.Logf("Endpoint lifecycle results: %+v", results)
	if err != nil {
		t.Fatal(err)
	}
}
