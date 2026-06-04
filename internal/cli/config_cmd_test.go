package cli

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = Describe("config command", func() {
	AfterEach(func() {
		config.SetConfigFile("")
		tokenFlag = ""
	})

	It("persists token from key=value syntax and masks it in command output", func() {
		cfgPath := filepath.Join(GinkgoT().TempDir(), "config.toml")
		config.SetConfigFile(cfgPath)

		result, err := configSetFn(nil, []string{"token=token-1234567890"})
		Expect(err).NotTo(HaveOccurred())

		var out bytes.Buffer
		result.RenderText(&out)
		Expect(out.String()).To(ContainSubstring("toke...7890"))
		Expect(out.String()).NotTo(ContainSubstring("token-1234567890"))

		content, readErr := os.ReadFile(cfgPath)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("token-1234567890"))
	})

	It("persists token from --token syntax", func() {
		cfgPath := filepath.Join(GinkgoT().TempDir(), "config.toml")
		config.SetConfigFile(cfgPath)
		tokenFlag = "flag-token-1234"

		_, err := configSetFn(&cobra.Command{}, nil)
		Expect(err).NotTo(HaveOccurred())

		content, readErr := os.ReadFile(cfgPath)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("flag-token-1234"))
	})

	It("masks token in config show output", func() {
		config.SetToken("token-abcdef123456")
		defer config.SetToken("")

		result, err := configShowFn(nil, nil)
		Expect(err).NotTo(HaveOccurred())

		var out bytes.Buffer
		result.RenderText(&out)
		Expect(out.String()).To(ContainSubstring("toke...3456"))
		Expect(out.String()).NotTo(ContainSubstring("token-abcdef123456"))
	})
})
