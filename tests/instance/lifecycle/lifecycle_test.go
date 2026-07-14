package lifecycle_test

import (
	"context"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestInstanceLifecycleCommands(t *testing.T) {
	testutil.RunSpecs(t, "AGR instance lifecycle command live smoke")
}

var _ = BeforeSuite(testutil.SetupSuite)
var _ = AfterSuite(testutil.CleanupSuite)

var _ = Describe("instance lifecycle commands", Ordered, func() {
	var cli *testutil.CLI
	var tracker *testutil.ResourceTracker
	var instanceID string

	BeforeAll(func() {
		cli = testutil.NewCLI()
		cli.InitConfig()
		tracker = testutil.NewResourceTracker(cli)
	})

	AfterAll(func() {
		tracker.Cleanup()
		cli.Cleanup()
	})

	It("executes agr instance list", func() {
		result := cli.Run(context.Background(), "--output", "json", "instance", "list", "--limit", "1")
		result.ExpectSuccess()
		Expect(result.Envelope().Command).To(Equal("instance.list"))
	})

	It("executes agr instance create", func() {
		toolID := testutil.State().EnsureToolID(context.Background())
		result := cli.Run(context.Background(), "--output", "json", "instance", "create", "--tool-id", toolID, "--timeout", "300s")
		result.ExpectSuccess()
		env := result.Envelope()
		Expect(env.Command).To(Equal("instance.create"))
		instanceID = testutil.StringField(env.Data, "InstanceId")
		tracker.AddInstance(instanceID)
		testutil.WaitForInstanceStatus(context.Background(), instanceID, "RUNNING")
	})

	It("executes agr instance get", func() {
		result := cli.Run(context.Background(), "--output", "json", "instance", "get", instanceID)
		result.ExpectSuccess()
		Expect(result.Envelope().Command).To(Equal("instance.get"))
	})

	It("executes agr instance update", func() {
		result := cli.Run(context.Background(), "--output", "json", "instance", "update", instanceID, "--timeout", "6m")
		result.ExpectSuccess()
		Expect(result.Envelope().Command).To(Equal("instance.update"))
	})

	It("executes agr instance pause", func() {
		result := cli.Run(context.Background(), "--output", "json", "instance", "pause", instanceID)
		result.ExpectSuccess()
		Expect(result.Envelope().Command).To(Equal("instance.pause"))
		testutil.WaitForInstanceStatus(context.Background(), instanceID, "PAUSED")
	})

	It("executes agr instance resume", func() {
		result := cli.Run(context.Background(), "--output", "json", "instance", "resume", instanceID)
		result.ExpectSuccess()
		Expect(result.Envelope().Command).To(Equal("instance.resume"))
		testutil.WaitForInstanceStatus(context.Background(), instanceID, "RUNNING")
	})

	It("executes agr instance delete", func() {
		result := cli.Run(context.Background(), "--output", "json", "instance", "delete", instanceID, "--ignore-not-found")
		result.ExpectSuccess()
		Expect(result.Envelope().Command).To(Equal("instance.delete"))
		tracker.ForgetInstance(instanceID)
		instanceID = ""
	})
})
