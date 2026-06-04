package apikey_test

import (
	"context"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAPIKeyCommands(t *testing.T) { testutil.RunSpecs(t, "AGR apikey command live smoke") }

var _ = BeforeSuite(testutil.SetupSuite)
var _ = AfterSuite(testutil.CleanupSuite)

var _ = Describe("apikey commands", Ordered, func() {
	var cli *testutil.CLI
	var keyID string

	BeforeAll(func() {
		cli = testutil.NewCLI()
		cli.InitConfig()
	})

	AfterAll(func() {
		if keyID != "" && !testutil.State().Config.KeepResources {
			_ = cli.Run(context.Background(), "--output", "json", "apikey", "delete", keyID)
		}
		cli.Cleanup()
	})

	It("executes agr apikey list", func() {
		result := cli.Run(context.Background(), "--output", "json", "apikey", "list")
		result.ExpectSuccess()
		Expect(result.Envelope().Command).To(Equal("apikey.list"))
	})

	It("executes agr apikey create", func() {
		result := cli.Run(context.Background(), "--output", "json", "apikey", "create", "--name", "agr-live-apikey")
		if result.ExitCode != 0 {
			env := result.Envelope()
			if env.Failure != nil && env.Failure.Code == "LimitExceeded.APIKeyQuota" {
				Skip("API key quota exhausted for this live-test account")
			}
		}
		result.ExpectSuccess()
		env := result.Envelope()
		Expect(env.Command).To(Equal("apikey.create"))
		keyID = testutil.StringField(env.Data, "KeyId")
	})

	It("executes agr apikey delete", func() {
		if keyID == "" {
			Skip("API key was not created")
		}
		result := cli.Run(context.Background(), "--output", "json", "apikey", "delete", keyID)
		result.ExpectSuccess()
		Expect(result.Envelope().Command).To(Equal("apikey.delete"))
		keyID = ""
	})
})
