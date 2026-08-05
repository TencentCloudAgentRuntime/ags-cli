package credential_test

import (
	"context"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("agr credential commands", func() {
	var cli *testutil.CLI

	BeforeEach(func() {
		cli = testutil.NewCLI()
		DeferCleanup(cli.Cleanup)
	})

	Describe("credential --help", func() {
		It("shows credential subcommands", func() {
			result := cli.Run(context.Background(), "credential", "--help")
			result.ExpectSuccess()
			Expect(result.Stdout).To(ContainSubstring("provider"))
			Expect(result.Stdout).To(ContainSubstring("secret"))
			Expect(result.Stdout).To(ContainSubstring("oauth2"))
		})
	})

	Describe("credential provider --help", func() {
		It("shows provider subcommands", func() {
			result := cli.Run(context.Background(), "credential", "provider", "--help")
			result.ExpectSuccess()
			Expect(result.Stdout).To(ContainSubstring("create"))
			Expect(result.Stdout).To(ContainSubstring("list"))
			Expect(result.Stdout).To(ContainSubstring("get"))
			Expect(result.Stdout).To(ContainSubstring("update"))
			Expect(result.Stdout).To(ContainSubstring("delete"))
		})
	})

	Describe("credential provider create --help", func() {
		It("shows correct flags", func() {
			result := cli.Run(context.Background(), "credential", "provider", "create", "--help")
			result.ExpectSuccess()
			Expect(result.Stdout).To(ContainSubstring("--name"))
			Expect(result.Stdout).To(ContainSubstring("--type"))
			Expect(result.Stdout).To(ContainSubstring("--provider-config"))
			Expect(result.Stdout).To(ContainSubstring("AKSK"))
			Expect(result.Stdout).To(ContainSubstring("SecretMultiUser"))
			Expect(result.Stdout).To(ContainSubstring("OAuth2"))
		})
	})

	Describe("credential secret --help", func() {
		It("shows secret subcommands", func() {
			result := cli.Run(context.Background(), "credential", "secret", "--help")
			result.ExpectSuccess()
			Expect(result.Stdout).To(ContainSubstring("set"))
			Expect(result.Stdout).To(ContainSubstring("list"))
			Expect(result.Stdout).To(ContainSubstring("get"))
			Expect(result.Stdout).To(ContainSubstring("delete"))
		})
	})

	Describe("credential secret set --help", func() {
		It("shows correct flags", func() {
			result := cli.Run(context.Background(), "credential", "secret", "set", "--help")
			result.ExpectSuccess()
			Expect(result.Stdout).To(ContainSubstring("--credential-provider-id"))
			Expect(result.Stdout).To(ContainSubstring("--user-id"))
			Expect(result.Stdout).To(ContainSubstring("--secret"))
			Expect(result.Stdout).To(ContainSubstring("--from-stdin"))
			Expect(result.Stdout).To(ContainSubstring("--overwrite-allowed"))
			Expect(result.Stdout).To(ContainSubstring("--scope"))
			Expect(result.Stdout).To(ContainSubstring("--metadata"))
		})
	})

	Describe("credential oauth2 --help", func() {
		It("shows oauth2 subcommands", func() {
			result := cli.Run(context.Background(), "credential", "oauth2", "--help")
			result.ExpectSuccess()
			Expect(result.Stdout).To(ContainSubstring("acquire"))
			Expect(result.Stdout).To(ContainSubstring("complete"))
		})
	})

	Describe("credential oauth2 acquire --help", func() {
		It("shows correct flags", func() {
			result := cli.Run(context.Background(), "credential", "oauth2", "acquire", "--help")
			result.ExpectSuccess()
			Expect(result.Stdout).To(ContainSubstring("--token"))
			Expect(result.Stdout).To(ContainSubstring("--credential-provider-id"))
			Expect(result.Stdout).To(ContainSubstring("--flow"))
			Expect(result.Stdout).To(ContainSubstring("--scopes"))
			Expect(result.Stdout).To(ContainSubstring("--session-uri"))
			Expect(result.Stdout).To(ContainSubstring("AUTHORIZATION_CODE"))
		})
	})
})
