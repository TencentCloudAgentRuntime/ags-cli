//go:build preview

package apicontract_test

import (
	"os"
	"slices"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchcoverage"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchscenarios"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The plan is derived from the committed coverage manifest, so these specs run
// exactly the assertions the repository promises for the patched contract.
func sandboxScenarioBindings(scenario string) []patchcoverage.Binding {
	plan, err := patchcoverage.Load("../..", patchscenarios.Registry.Coverage(), true)
	Expect(err).NotTo(HaveOccurred())
	var bindings []patchcoverage.Binding
	for _, binding := range plan.Bindings {
		if binding.Scenario == scenario {
			bindings = append(bindings, binding)
		}
	}
	Expect(bindings).NotTo(BeEmpty())
	return bindings
}

// sandboxLiveEnv mirrors the candidate environment of testutil.CLI while keeping
// AGR_* overrides of the caller out of the child process.
func sandboxLiveEnv(cli *testutil.CLI) []string {
	env := make([]string, 0, len(os.Environ())+10)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if key == "HOME" || key == "USERPROFILE" || strings.HasPrefix(key, "AGR_") {
			continue
		}
		env = append(env, item)
	}
	env = append(env,
		"HOME="+cli.Home,
		"USERPROFILE="+cli.Home,
		"TENCENTCLOUD_SECRET_ID="+cli.Config.SecretID,
		"TENCENTCLOUD_SECRET_KEY="+cli.Config.SecretKey,
		"AGR_REGION="+cli.Config.Region,
		"AGR_NON_INTERACTIVE=1",
	)
	if token := os.Getenv("TENCENTCLOUD_TOKEN"); token != "" {
		env = append(env, "TENCENTCLOUD_TOKEN="+token)
	}
	if cli.Config.Domain != "" {
		env = append(env, "AGR_DOMAIN="+cli.Config.Domain)
	}
	if cli.Config.CloudEndpoint != "" {
		env = append(env, "AGR_CLOUD_ENDPOINT="+cli.Config.CloudEndpoint)
	}
	return env
}

var _ = Describe("Sandbox keepalive and log format", func() {
	for _, scenario := range []string{"sandbox.keepalive", "sandbox.log-format"} {
		It("runs "+scenario+" against the live backend", func(ctx SpecContext) {
			bindings := sandboxScenarioBindings(scenario)
			cli := testutil.NewCLI()
			DeferCleanup(cli.Cleanup)

			results, err := patchtest.Run(ctx,
				patchcoverage.Plan{Bindings: bindings},
				patchscenarios.Registry,
				cli.BinaryPath,
				sandboxLiveEnv(cli),
			)
			Expect(err).NotTo(HaveOccurred(), "results: %+v", results)
			Expect(results).To(HaveLen(1))

			result := results[0]
			Expect(result.Status).To(Equal("pass"), "results: %+v", results)
			Expect(result.Cleanup).To(Equal("pass"), "results: %+v", results)
			Expect(result.Calls).To(BeNumerically(">", 0))

			expected := []string{}
			for _, binding := range bindings {
				for _, assertion := range binding.Assertions {
					if !slices.Contains(expected, assertion) {
						expected = append(expected, assertion)
					}
				}
			}
			slices.Sort(expected)
			Expect(result.Assertions).To(Equal(expected), "every required assertion must execute")
		})
	}
})
