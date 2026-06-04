package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConfigBDD(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Suite")
}

var _ = Describe("Config", func() {
	BeforeEach(func() {
		cfg = nil
		sources = nil
		cfgFile = ""
	})

	It("defaults to cloud-compatible region, domain and text output", func() {
		c := Get()
		Expect(GetBackend()).To(Equal("cloud"))
		Expect(c.Region).To(Equal("ap-guangzhou"))
		Expect(c.Domain).To(Equal("tencentags.com"))
		Expect(c.Output).To(Equal("text"))
	})

	It("sets output and cloud credentials", func() {
		SetOutput("json")
		Expect(GetOutput()).To(Equal("json"))
		SetSecretID("sid")
		SetSecretKey("skey")
		SetToken("tok")
		Expect(GetSecretID()).To(Equal("sid"))
		Expect(GetSecretKey()).To(Equal("skey"))
		Expect(GetToken()).To(Equal("tok"))
	})

	It("resolves STS token from environment before config file", func() {
		tmpDir := GinkgoT().TempDir()
		cfgPath := filepath.Join(tmpDir, "config.toml")
		Expect(os.WriteFile(cfgPath, []byte("[auth]\ntoken = \"config-token\"\n"), 0o600)).To(Succeed())
		SetConfigFile(cfgPath)
		Expect(os.Setenv("TENCENTCLOUD_TOKEN", "env-token")).To(Succeed())
		defer func() {
			Expect(os.Unsetenv("TENCENTCLOUD_TOKEN")).To(Succeed())
			SetConfigFile("")
		}()

		Expect(Init()).To(Succeed())

		Expect(GetToken()).To(Equal("env-token"))
		Expect(GetSource("token")).To(Equal("TENCENTCLOUD_TOKEN"))
	})

	It("validates output, region and domain", func() {
		SetOutput("xml")
		Expect(ValidateBasics()).To(HaveOccurred())
		SetOutput("text")
		SetRegion("bad region")
		Expect(ValidateBasics()).To(HaveOccurred())
		SetRegion("ap-guangzhou")
		SetDomain("https://bad")
		Expect(ValidateBasics()).To(HaveOccurred())
	})

	It("requires Tencent Cloud credentials with exit 4", func() {
		SetSecretID("")
		SetSecretKey("")
		err := Validate()
		Expect(err).To(HaveOccurred())
		var cliErr *output.CLIError
		Expect(errors.As(err, &cliErr)).To(BeTrue())
		Expect(cliErr.ExitCode).To(Equal(output.ExitAuthOrPermission))
	})

	It("accepts full Tencent Cloud credentials", func() {
		SetSecretID("id")
		SetSecretKey("key")
		Expect(Validate()).To(Succeed())
	})

	It("computes endpoints", func() {
		c := Get()
		Expect(c.ControlPlaneEndpoint()).To(Equal("ags.tencentcloudapi.com"))
		c.CloudEndpoint = "custom.endpoint.com"
		Expect(c.ControlPlaneEndpoint()).To(Equal("custom.endpoint.com"))
		Expect(c.DataPlaneRegionDomain()).To(Equal("ap-guangzhou.tencentags.com"))
	})
})
