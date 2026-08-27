package lifecycle

import (
	"context"
	"fmt"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Deployment CLI lifecycle", func() {
	var cli *testutil.CLI
	var tracker *ResourceTracker

	BeforeEach(func() {
		cli = newCLI()
		cli.Config.Region = testutil.ResolveRegion("ap-shanghai")
		cli.InitConfig()
		tracker = NewResourceTracker(cli)
	})

	It("creates, gets, filters, updates, and deletes a Deployment", func() {
		toolName := uniqueName("ags-cli-e2e-deployment-tool")
		createTool := cli.Run(context.Background(), "--output", "json", "tool", "create",
			"--wait",
			"--tool-name", toolName,
			"--tool-type", "mobile",
			"--description", "AGR CLI Deployment lifecycle E2E tool",
			"--default-timeout", "10m",
			"--network-configuration", `{"NetworkMode":"PUBLIC"}`,
			"--persistent",
			"--tags", `[{"Key":"ags-cli-e2e","Value":"deployment-tool"}]`,
		)
		createToolEnv := createTool.Envelope()
		toolID := createdResourceID(createToolEnv, "ToolId")
		tracker.AddTool(toolID)
		createTool.ExpectSuccess()
		Expect(createToolEnv.Command).To(Equal("tool.create"))
		Expect(createToolEnv.Status).To(Equal("succeeded"))
		Expect(toolID).NotTo(BeEmpty())
		Expect(stringField(createToolEnv.Data, "ToolName")).To(Equal(toolName))
		Expect(stringField(createToolEnv.Data, "Status")).To(Equal("ACTIVE"))
		Expect(createToolEnv.Data["Persistent"]).To(BeTrue())

		deploymentName := uniqueName("ags-cli-e2e-deployment")

		create := cli.Run(context.Background(), "--output", "json", "deployment", "create",
			"--deployment-name", deploymentName,
			"--tool-id", toolID,
			"--scaling-configuration", `{"MinInstanceCount":0,"MaxInstanceCount":1,"MaxInstanceRequestConcurrency":20}`,
			"--lifecycle-configuration", `{"IdleTimeoutSeconds":60,"IdleAction":"STOP"}`,
			"--tags", `[{"Key":"ags-cli-e2e","Value":"deployment"}]`,
		)
		createEnv := create.Envelope()
		deploymentID := createdDeploymentID(createEnv)
		tracker.AddDeployment(deploymentID)
		create.ExpectSuccess()
		created := objectField(createEnv.Data, "Deployment")
		Expect(createEnv.Command).To(Equal("deployment.create"))
		Expect(createEnv.Status).To(Equal("succeeded"))
		Expect(deploymentID).NotTo(BeEmpty())
		Expect(stringField(created, "DeploymentName")).To(Equal(deploymentName))
		Expect(stringField(created, "ToolId")).To(Equal(toolID))

		get := cli.Run(context.Background(), "--output", "json", "deployment", "get", deploymentID)
		get.ExpectSuccess()
		got := objectField(get.Envelope().Data, "Deployment")
		Expect(stringField(got, "DeploymentId")).To(Equal(deploymentID))
		Expect(stringField(got, "DeploymentName")).To(Equal(deploymentName))

		filters := fmt.Sprintf(`[{"Name":"deployment-id","Values":[%q]},{"Name":"tool-id","Values":[%q]}]`, deploymentID, toolID)
		list := cli.Run(context.Background(), "--output", "json", "deployment", "list", "--filters", filters, "--limit", "10")
		list.ExpectSuccess()
		items := arrayField(list.Envelope().Data, "DeploymentSet")
		Expect(items).To(HaveLen(1), list.Diagnostics())
		listed, ok := items[0].(map[string]any)
		Expect(ok).To(BeTrue(), list.Diagnostics())
		Expect(stringField(listed, "DeploymentId")).To(Equal(deploymentID))

		update := cli.Run(context.Background(), "--output", "json", "deployment", "update", deploymentID,
			"--scaling-configuration", `{"MinInstanceCount":0,"MaxInstanceCount":2,"MaxInstanceRequestConcurrency":25}`,
			"--lifecycle-configuration", `{"IdleTimeoutSeconds":120,"IdleAction":"PAUSE"}`,
			"--tags", `[{"Key":"ags-cli-e2e","Value":"updated"}]`,
		)
		update.ExpectSuccess()
		updated := objectField(update.Envelope().Data, "Deployment")
		Expect(numberField(objectField(updated, "ScalingConfiguration"), "MaxInstanceCount")).To(Equal(2))
		Expect(numberField(objectField(updated, "LifecycleConfiguration"), "IdleTimeoutSeconds")).To(Equal(120))
		Expect(stringField(objectField(updated, "LifecycleConfiguration"), "IdleAction")).To(Equal("PAUSE"))

		deleteResult := cli.Run(context.Background(), "--output", "json", "deployment", "delete", deploymentID, "--wait")
		deleteResult.ExpectSuccess()
		Expect(deleteResult.Envelope().Command).To(Equal("deployment.delete"))
		tracker.ForgetDeployment(deploymentID)

		missing := cli.Run(context.Background(), "--output", "json", "deployment", "get", deploymentID)
		Expect(missing.ExitCode).NotTo(Equal(0), missing.Diagnostics())
		Expect(missing.Envelope().Failure).NotTo(BeNil())
		Expect(missing.Envelope().Failure.Kind).To(Equal("not_found"))

		deleteTool := cli.Run(context.Background(), "--output", "json", "tool", "delete", toolID, "--wait", "--yes")
		deleteTool.ExpectSuccess()
		tracker.ForgetTool(toolID)
	})
})

func createdDeploymentID(env testutil.Envelope) string {
	if deployment, ok := env.Data["Deployment"].(map[string]any); ok {
		if id, ok := deployment["DeploymentId"].(string); ok {
			return id
		}
	}
	return createdResourceID(env, "DeploymentId")
}

func objectField(data map[string]any, key string) map[string]any {
	value, ok := data[key].(map[string]any)
	ExpectWithOffset(1, ok).To(BeTrue(), "%s is %T, want object", key, data[key])
	return value
}

func arrayField(data map[string]any, key string) []any {
	value, ok := data[key].([]any)
	ExpectWithOffset(1, ok).To(BeTrue(), "%s is %T, want array", key, data[key])
	return value
}
