package lifecycle

import (
	"context"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Tool CLI lifecycle", func() {
	var cli *testutil.CLI
	var tracker *ResourceTracker

	BeforeEach(func() {
		cli = newCLI()
		cli.InitConfig()
		tracker = NewResourceTracker(cli)
	})

	It("lists, creates, gets, filters, and deletes tools using JSON output", func() {
		list := cli.Run(context.Background(), "--output", "json", "tool", "list", "--limit", "1")
		list.ExpectSuccess()
		listEnv := list.Envelope()
		Expect(listEnv.Command).To(Equal("tool.list"))
		Expect(listEnv.Status).To(Equal("succeeded"))
		Expect(listEnv.Data).To(HaveKey("Pagination"))

		toolName := uniqueName("ags-cli-e2e-tool")
		create := cli.Run(context.Background(), "--output", "json", "tool", "create",
			"--wait",
			"-n", toolName,
			"-t", "code-interpreter",
			"-d", "AGR CLI E2E tool",
			"--default-timeout", "10m",
			"--network-configuration", `{"NetworkMode":"PUBLIC"}`,
			"--tags", `[{"Key":"ags-cli-e2e","Value":"true"}]`,
		)
		createEnv := create.Envelope()
		toolID := createdResourceID(createEnv, "ToolId")
		tracker.AddTool(toolID)
		create.ExpectSuccess()
		Expect(createEnv.Command).To(Equal("tool.create"))
		Expect(createEnv.Status).To(Equal("succeeded"))
		Expect(toolID).NotTo(BeEmpty())
		Expect(stringField(createEnv.Data, "Status")).To(Equal("ACTIVE"))

		Expect(stringField(createEnv.Data, "ToolName")).To(Equal(toolName))
		Expect(stringField(createEnv.Data, "ToolType")).To(Equal("code-interpreter"))

		jq := cli.Run(context.Background(), "--output", "json", "--jq", ".Data.ToolId", "tool", "get", toolID)
		jq.ExpectSuccess()
		Expect(strings.TrimSpace(jq.Stdout)).To(Equal(toolID), jq.Diagnostics())
		Expect(jq.Stderr).To(BeEmpty(), jq.Diagnostics())

		filtered := cli.Run(context.Background(), "--output", "json", "tool", "list", "--tool-ids", toolID, "--limit", "10")
		filtered.ExpectSuccess()
		filteredItems := itemsField(filtered.Envelope().Data)
		Expect(filteredItems).To(HaveLen(1), filtered.Diagnostics())

		missing := cli.Run(context.Background(), "--output", "json", "tool", "get", "sdt-ags-cli-e2e-missing")
		Expect(missing.ExitCode).NotTo(Equal(0), missing.Diagnostics())
		Expect(missing.Stderr).To(BeEmpty(), missing.Diagnostics())
		missingEnv := missing.Envelope()
		Expect(missingEnv.Status).To(Equal("failed"))
		Expect(missingEnv.Failure).NotTo(BeNil())
		Expect(missingEnv.Failure.Kind).To(Equal("not_found"))

		deleted := cli.Run(context.Background(), "--output", "json", "tool", "delete", toolID, "--wait", "--yes")
		deleted.ExpectSuccess()
		deletedEnv := deleted.Envelope()
		Expect(numberField(deletedEnv.Data, "Deleted")).To(Equal(1))
		tracker.ForgetTool(toolID)
	})
})
