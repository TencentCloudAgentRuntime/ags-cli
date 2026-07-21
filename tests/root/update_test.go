package root_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("agr update command", func() {
	var cli *testutil.CLI

	BeforeEach(func() {
		cli = testutil.NewCLI()
		DeferCleanup(cli.Cleanup)
	})

	// Tests that call the real VERSION endpoint are guarded by AGR_LIVE_TEST.
	// They are skipped in CI unless the env var is set, avoiding flakes from
	// network unavailability.

	It("checks for updates in text mode", func() {
		if os.Getenv("AGR_LIVE_TEST") == "" {
			Skip("requires network (set AGR_LIVE_TEST=1)")
		}
		result := cli.Run(context.Background(), "update")
		result.ExpectSuccess()
		Expect(result.Stdout).To(ContainSubstring("Current:"))
		Expect(result.Stdout).To(ContainSubstring("Latest:"))
	})

	It("checks for updates with --check flag", func() {
		if os.Getenv("AGR_LIVE_TEST") == "" {
			Skip("requires network (set AGR_LIVE_TEST=1)")
		}
		result := cli.Run(context.Background(), "update", "--check")
		result.ExpectSuccess()
		Expect(result.Stdout).To(ContainSubstring("Current:"))
	})

	It("returns JSON envelope with update data", func() {
		if os.Getenv("AGR_LIVE_TEST") == "" {
			Skip("requires network (set AGR_LIVE_TEST=1)")
		}
		result := cli.Run(context.Background(), "--output", "json", "update")
		result.ExpectSuccess()
		env := result.Envelope()
		Expect(env.Command).To(Equal("update"))
		Expect(env.Status).To(Equal("succeeded"))
		Expect(env.Data).To(HaveKey("current"))
		Expect(env.Data).To(HaveKey("latest"))
		Expect(env.Data).To(HaveKey("update_available"))
	})

	// The following tests do NOT depend on network — they verify suppression
	// behavior which does not call the remote endpoint.

	It("does not show background update notice for 'agr update' itself", func() {
		if os.Getenv("AGR_LIVE_TEST") == "" {
			Skip("requires network (set AGR_LIVE_TEST=1)")
		}
		result := cli.Run(context.Background(), "update")
		result.ExpectSuccess()
		Expect(result.Stderr).NotTo(ContainSubstring("Update available"))
	})

	It("does not show update notice in non-interactive mode", func() {
		result := cli.Run(context.Background(), "--non-interactive", "version")
		result.ExpectSuccess()
		Expect(result.Stderr).NotTo(ContainSubstring("Update available"))
	})

	It("does not show update notice in JSON mode", func() {
		result := cli.Run(context.Background(), "--output", "json", "version")
		result.ExpectSuccess()
		Expect(result.Stderr).NotTo(ContainSubstring("Update available"))
	})

	// Offline test using a local httptest server via _TEST_VERSION_URL.
	// This verifies the full update flow without depending on real network.
	// Uses cli.ExtraEnv instead of os.Setenv to avoid race conditions in
	// parallel test execution.
	It("checks for updates using mock server (offline)", func() {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "v99.0.0\n")
		}))
		defer ts.Close()

		cli.ExtraEnv = append(cli.ExtraEnv, "_TEST_VERSION_URL="+ts.URL)

		result := cli.Run(context.Background(), "update")
		result.ExpectSuccess()
		Expect(result.Stdout).To(ContainSubstring("Current:"))
		Expect(result.Stdout).To(ContainSubstring("Latest:"))
		Expect(result.Stdout).To(ContainSubstring("v99.0.0"))
		Expect(result.Stdout).To(ContainSubstring("Update available"))
	})

	It("returns JSON envelope with mock server (offline)", func() {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "v99.0.0\n")
		}))
		defer ts.Close()

		cli.ExtraEnv = append(cli.ExtraEnv, "_TEST_VERSION_URL="+ts.URL)

		result := cli.Run(context.Background(), "--output", "json", "update")
		result.ExpectSuccess()
		env := result.Envelope()
		Expect(env.Command).To(Equal("update"))
		Expect(env.Status).To(Equal("succeeded"))
		Expect(env.Data["latest"]).To(Equal("v99.0.0"))
		Expect(env.Data["update_available"]).To(BeTrue())
	})
})
