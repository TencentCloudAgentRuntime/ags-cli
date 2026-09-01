package tool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestToolCommands(t *testing.T) { testutil.RunSpecs(t, "AGR tool command live smoke") }

var _ = BeforeSuite(testutil.SetupSuite)
var _ = AfterSuite(testutil.CleanupSuite)

var _ = Describe("tool commands", Ordered, func() {
	var cli *testutil.CLI
	var toolID string
	var forkedToolID string
	var nameSuffix string

	BeforeAll(func() {
		cli = testutil.NewCLI()
		cli.InitConfig()
		nameSuffix = fmt.Sprintf("%d", time.Now().UnixNano())
	})

	AfterAll(func() {
		if forkedToolID != "" && !testutil.State().Config.KeepResources {
			_ = cli.Run(context.Background(), "--output", "json", "tool", "delete", forkedToolID)
		}
		if toolID != "" && !testutil.State().Config.KeepResources {
			_ = cli.Run(context.Background(), "--output", "json", "tool", "delete", toolID)
		}
		cli.Cleanup()
	})

	It("executes agr tool list", func() {
		result := cli.Run(context.Background(), "--output", "json", "tool", "list", "--limit", "1")
		result.ExpectSuccess()
		Expect(result.Envelope().Command).To(Equal("tool.list"))
	})

	It("executes agr tool create", func() {
		name := "agr-live-tool-command-" + nameSuffix
		result := cli.Run(context.Background(), "--output", "json", "tool", "create",
			"--tool-name", name,
			"--tool-type", "code-interpreter",
			"--description", "AGR live command test",
			"--default-timeout", "5m",
			"--network-configuration", `{"NetworkMode":"PUBLIC"}`,
		)
		result.ExpectSuccess()
		env := result.Envelope()
		Expect(env.Command).To(Equal("tool.create"))
		toolID = testutil.StringField(env.Data, "ToolId")
		Expect(toolID).NotTo(BeEmpty())
		testutil.State().WaitForToolStatus(context.Background(), toolID, "ACTIVE")
	})

	It("executes agr tool get", func() {
		result := cli.Run(context.Background(), "--output", "json", "tool", "get", toolID)
		result.ExpectSuccess()
		Expect(result.Envelope().Command).To(Equal("tool.get"))
	})

	It("executes agr tool fork", func() {
		name := "agr-live-tool-command-fork-" + nameSuffix
		result := cli.Run(context.Background(), "--output", "json", "tool", "fork", toolID,
			"--tool-name", name,
			"--description", "AGR live command fork test",
			"--persistent=false",
		)
		result.ExpectSuccess()
		env := result.Envelope()
		Expect(env.Command).To(Equal("tool.fork"))
		forkedToolID = testutil.StringField(env.Data, "ToolId")
		Expect(forkedToolID).NotTo(BeEmpty())
	})

	It("executes agr tool update", func() {
		result := cli.Run(context.Background(), "--output", "json", "tool", "update", toolID, "--description", "AGR live command test updated")
		result.ExpectSuccess()
		Expect(result.Envelope().Command).To(Equal("tool.update"))
	})

	It("executes agr tool delete", func() {
		result := cli.Run(context.Background(), "--output", "json", "tool", "delete", toolID)
		result.ExpectSuccess()
		Expect(result.Envelope().Command).To(Equal("tool.delete"))
		toolID = ""
	})
})

var _ = Describe("WAA computer configuration", Ordered, func() {
	var cli *testutil.CLI
	var toolID string
	var forkedToolID string
	var imageID string
	var updatedImageID string
	var nameSuffix string

	BeforeAll(func() {
		imageID = strings.TrimSpace(os.Getenv("AGR_TEST_WAA_IMAGE_ID"))
		updatedImageID = strings.TrimSpace(os.Getenv("AGR_TEST_WAA_UPDATED_IMAGE_ID"))
		if imageID == "" || updatedImageID == "" {
			Skip("AGR_TEST_WAA_IMAGE_ID and AGR_TEST_WAA_UPDATED_IMAGE_ID are required for WAA live coverage")
		}
		if imageID == updatedImageID {
			Skip("AGR_TEST_WAA_IMAGE_ID and AGR_TEST_WAA_UPDATED_IMAGE_ID must differ")
		}
		cli = testutil.NewCLI()
		cli.InitConfig()
		nameSuffix = fmt.Sprintf("%d", time.Now().UnixNano())
	})

	AfterAll(func() {
		if cli == nil {
			return
		}
		if forkedToolID != "" && !testutil.State().Config.KeepResources {
			_ = cli.Run(context.Background(), "--output", "json", "tool", "delete", forkedToolID)
		}
		if toolID != "" && !testutil.State().Config.KeepResources {
			_ = cli.Run(context.Background(), "--output", "json", "tool", "delete", toolID)
		}
		cli.Cleanup()
	})

	It("creates and reads a WAA tool", func() {
		result := cli.Run(context.Background(), "--output", "json", "tool", "create",
			"--tool-name", "agr-live-waa-"+nameSuffix,
			"--tool-type", "waa",
			"--network-configuration", `{"NetworkMode":"PUBLIC"}`,
			"--computer-configuration", computerConfigurationJSON(imageID),
			"--wait",
		)
		result.ExpectSuccess()
		toolID = testutil.StringField(result.Envelope().Data, "ToolId")
		Expect(toolID).NotTo(BeEmpty())

		getResult := cli.Run(context.Background(), "--output", "json", "tool", "get", toolID)
		getResult.ExpectSuccess()
		expectWAAImageID(getResult.Envelope().Data, imageID)
	})

	It("updates and reads the WAA image", func() {
		result := cli.Run(context.Background(), "--output", "json", "tool", "update", toolID,
			"--computer-configuration", computerConfigurationJSON(updatedImageID),
			"--wait",
		)
		result.ExpectSuccess()
		expectWAAImageID(result.Envelope().Data, updatedImageID)

		getResult := cli.Run(context.Background(), "--output", "json", "tool", "get", toolID)
		getResult.ExpectSuccess()
		expectWAAImageID(getResult.Envelope().Data, updatedImageID)
	})

	It("forks the WAA configuration", func() {
		result := cli.Run(context.Background(), "--output", "json", "tool", "fork", toolID,
			"--tool-name", "agr-live-waa-fork-"+nameSuffix,
			"--wait",
		)
		result.ExpectSuccess()
		forkedToolID = testutil.StringField(result.Envelope().Data, "ToolId")
		Expect(forkedToolID).NotTo(BeEmpty())
		expectWAAImageID(result.Envelope().Data, updatedImageID)

		getResult := cli.Run(context.Background(), "--output", "json", "tool", "get", forkedToolID)
		getResult.ExpectSuccess()
		expectWAAImageID(getResult.Envelope().Data, updatedImageID)
	})
})

func computerConfigurationJSON(imageID string) string {
	payload, err := json.Marshal(map[string]any{
		"WAAConfiguration": map[string]string{"ImageId": imageID},
	})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return string(payload)
}

func expectWAAImageID(data map[string]any, expected string) {
	computer, ok := data["ComputerConfiguration"].(map[string]any)
	ExpectWithOffset(1, ok).To(BeTrue(), "ComputerConfiguration should be an object: %#v", data["ComputerConfiguration"])
	waa, ok := computer["WAAConfiguration"].(map[string]any)
	ExpectWithOffset(1, ok).To(BeTrue(), "WAAConfiguration should be an object: %#v", computer["WAAConfiguration"])
	ExpectWithOffset(1, testutil.StringField(waa, "ImageId")).To(Equal(expected))
}
