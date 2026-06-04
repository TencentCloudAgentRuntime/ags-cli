package debug_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestInstanceDebugCommand(t *testing.T) {
	testutil.RunSpecs(t, "AGR instance debug command live smoke")
}

var _ = BeforeSuite(testutil.SetupSuite)
var _ = AfterSuite(testutil.CleanupSuite)

var _ = Describe("instance debug", Ordered, func() {
	var cli *testutil.CLI
	var sourceToolID string
	var debugToolID string

	BeforeAll(func() {
		cli = testutil.NewCLI()
		cli.InitConfig()
		roleArn := os.Getenv("AGR_TEST_ROLE_ARN")
		if roleArn == "" {
			Skip("AGR_TEST_ROLE_ARN is required for image-mount debug tool live smoke")
		}
		name := fmt.Sprintf("agr-live-debug-source-%d", time.Now().UnixNano())
		result := cli.Run(context.Background(), "--output", "json", "tool", "create",
			"--tool-name", name,
			"--tool-type", "custom",
			"--description", "AGR live debug source",
			"--default-timeout", "5m",
			"--role-arn", roleArn,
			"--network-configuration", `{"NetworkMode":"PUBLIC"}`,
			"--custom-configuration", `{"Image":"ccr.ccs.tencentyun.com/ags-image/envd:v0.5.14","ImageRegistryType":"system","Command":["/bin/sh"],"Args":["-c","sleep 3600"],"Resources":{"CPU":"1","Memory":"2Gi"},"Probe":{"ReadyTimeoutMs":30000,"ProbeTimeoutMs":5000,"ProbePeriodMs":10000,"SuccessThreshold":1,"FailureThreshold":3}}`,
		)
		result.ExpectSuccess()
		sourceToolID = testutil.StringField(result.Envelope().Data, "ToolId")
	})

	AfterAll(func() {
		if debugToolID != "" && !testutil.State().Config.KeepResources {
			_ = cli.Run(context.Background(), "--output", "json", "tool", "delete", debugToolID)
		}
		if sourceToolID != "" && !testutil.State().Config.KeepResources {
			_ = cli.Run(context.Background(), "--output", "json", "tool", "delete", sourceToolID)
		}
		cli.Cleanup()
	})

	It("creates a debug tool from an existing tool", func() {
		name := fmt.Sprintf("agr-live-debug-%d", time.Now().UnixNano())
		result := cli.Run(context.Background(), "--output", "json", "instance", "debug", sourceToolID, "--debug-tool-name", name)
		result.ExpectSuccess()

		env := result.Envelope()
		Expect(env.Command).To(Equal("instance.debug"))
		Expect(testutil.StringField(env.Data, "SourceToolId")).To(Equal(sourceToolID))
		Expect(testutil.StringField(env.Data, "ToolName")).To(Equal(name))
		debugToolID = testutil.StringField(env.Data, "ToolId")
		Expect(debugToolID).NotTo(BeEmpty())

		getResult := cli.Run(context.Background(), "--output", "json", "tool", "get", debugToolID)
		getResult.ExpectSuccess()
		tool := getResult.Envelope().Data
		custom, ok := tool["CustomConfiguration"].(map[string]any)
		Expect(ok).To(BeTrue(), "CustomConfiguration should be an object: %#v", tool["CustomConfiguration"])
		Expect(stringArray(custom["Command"])).To(Equal([]string{"/envd"}))
		Expect(stringArray(custom["Args"])).To(BeEmpty())
		Expect(hasEnvdMount(tool["StorageMounts"])).To(BeTrue())
	})
})

func hasEnvdMount(value any) bool {
	mounts, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range mounts {
		mount, ok := item.(map[string]any)
		if !ok {
			continue
		}
		storageSource, ok := mount["StorageSource"].(map[string]any)
		if !ok {
			continue
		}
		image, ok := storageSource["Image"].(map[string]any)
		if !ok {
			continue
		}
		if mount["Name"] == "envd" && mount["MountPath"] == "/envd" &&
			image["Reference"] == "ccr.ccs.tencentyun.com/ags-image/envd:v0.5.14" &&
			image["SubPath"] == "/usr/bin/envd" {
			return true
		}
	}
	return false
}

func stringArray(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
