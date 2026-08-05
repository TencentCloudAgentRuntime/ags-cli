package identity_test

import (
	"context"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("agr identity commands", func() {
	var cli *testutil.CLI

	BeforeEach(func() {
		cli = testutil.NewCLI()
		DeferCleanup(cli.Cleanup)
	})

	Describe("identity --help", func() {
		It("shows identity subcommands", func() {
			result := cli.Run(context.Background(), "identity", "--help")
			result.ExpectSuccess()
			Expect(result.Stdout).To(ContainSubstring("create"))
			Expect(result.Stdout).To(ContainSubstring("list"))
			Expect(result.Stdout).To(ContainSubstring("get"))
			Expect(result.Stdout).To(ContainSubstring("update"))
			Expect(result.Stdout).To(ContainSubstring("delete"))
			Expect(result.Stdout).To(ContainSubstring("token"))
		})
	})

	Describe("identity create --help", func() {
		It("shows create flags", func() {
			result := cli.Run(context.Background(), "identity", "create", "--help")
			result.ExpectSuccess()
			Expect(result.Stdout).To(ContainSubstring("--name"))
			Expect(result.Stdout).To(ContainSubstring("--allowed-oauth2-return-urls"))
			Expect(result.Stdout).To(ContainSubstring("--tags"))
		})
	})

	Describe("identity token create --help", func() {
		It("shows token create flags", func() {
			result := cli.Run(context.Background(), "identity", "token", "create", "--help")
			result.ExpectSuccess()
			Expect(result.Stdout).To(ContainSubstring("--identity-id"))
			Expect(result.Stdout).To(ContainSubstring("--user-id"))
		})
	})

	Describe("identity update --help", func() {
		It("shows update flags", func() {
			result := cli.Run(context.Background(), "identity", "update", "--help")
			result.ExpectSuccess()
			Expect(result.Stdout).To(ContainSubstring("--name"))
			Expect(result.Stdout).To(ContainSubstring("--allowed-oauth2-return-urls"))
			Expect(result.Stdout).To(ContainSubstring("--tags"))
			Expect(result.Stdout).To(ContainSubstring("<workload-identity-id>"))
		})
	})
})
