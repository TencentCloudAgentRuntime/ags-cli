package tool_test

import (
	"context"
	"fmt"
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
