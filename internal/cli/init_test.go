package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("init", func() {
	It("does not write the removed internal config key", func() {
		tmpDir := GinkgoT().TempDir()
		cfgPath := filepath.Join(tmpDir, "config.toml")

		prevSecretID := secretID
		prevSecretKey := secretKey
		prevToken := tokenFlag
		prevOverwrite := initOverwrite
		defer func() {
			secretID = prevSecretID
			secretKey = prevSecretKey
			tokenFlag = prevToken
			initOverwrite = prevOverwrite
			config.SetConfigFile("")
		}()

		secretID = ""
		secretKey = ""
		tokenFlag = ""
		initOverwrite = false
		config.SetConfigFile(cfgPath)

		Expect(os.Setenv("TENCENTCLOUD_SECRET_ID", "sid")).To(Succeed())
		Expect(os.Setenv("TENCENTCLOUD_SECRET_KEY", "skey")).To(Succeed())
		defer func() {
			Expect(os.Unsetenv("TENCENTCLOUD_SECRET_ID")).To(Succeed())
			Expect(os.Unsetenv("TENCENTCLOUD_SECRET_KEY")).To(Succeed())
		}()

		_, err := initFn(nil, nil)
		Expect(err).NotTo(HaveOccurred())

		content, readErr := os.ReadFile(cfgPath)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(string(content)).NotTo(ContainSubstring("internal = false"))
		Expect(strings.TrimSpace(string(content))).To(ContainSubstring(`cloud_endpoint = "ags.tencentcloudapi.com"`))
	})

	It("writes optional STS session token from the global flag", func() {
		tmpDir := GinkgoT().TempDir()
		cfgPath := filepath.Join(tmpDir, "config.toml")

		prevSecretID := secretID
		prevSecretKey := secretKey
		prevToken := tokenFlag
		prevOverwrite := initOverwrite
		defer func() {
			secretID = prevSecretID
			secretKey = prevSecretKey
			tokenFlag = prevToken
			initOverwrite = prevOverwrite
			config.SetConfigFile("")
		}()

		secretID = "sid"
		secretKey = "skey"
		tokenFlag = "token-1234567890"
		initOverwrite = false
		config.SetConfigFile(cfgPath)

		_, err := initFn(nil, nil)
		Expect(err).NotTo(HaveOccurred())

		content, readErr := os.ReadFile(cfgPath)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(string(content)).To(ContainSubstring(`token = "token-1234567890"`))
	})
})
