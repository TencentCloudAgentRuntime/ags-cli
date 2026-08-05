package lifecycle

import (
	"context"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Instance CLI lifecycle", func() {
	var cli *testutil.CLI
	var tracker *ResourceTracker

	BeforeEach(func() {
		cli = newCLI()
		cli.InitConfig()
		tracker = NewResourceTracker(cli)
	})

	It("creates, inspects, lists, exercises data plane, and deletes an instance", func() {
		toolID := testutil.State().EnsureToolID(context.Background())
		args := []string{"--output", "json", "instance", "create", "--wait", "--timeout", "5m", "--tool-id", toolID}

		create := cli.Run(context.Background(), args...)
		createEnv := create.Envelope()
		instanceID := createdResourceID(createEnv, "InstanceId")
		tracker.AddInstance(instanceID)
		create.ExpectSuccess()
		Expect(createEnv.Command).To(Equal("instance.create"))
		Expect(createEnv.Status).To(Equal("succeeded"))
		Expect(instanceID).NotTo(BeEmpty())
		Expect(stringField(createEnv.Data, "Status")).To(Equal("RUNNING"))

		pause := cli.Run(context.Background(), "--output", "json", "instance", "pause", instanceID, "--wait")
		pause.ExpectSuccess()
		pauseEnv := pause.Envelope()
		Expect(pauseEnv.Command).To(Equal("instance.pause"))
		Expect(stringField(pauseEnv.Data, "Status")).To(Equal("PAUSED"))

		resume := cli.Run(context.Background(), "--output", "json", "instance", "resume", instanceID, "--wait")
		resume.ExpectSuccess()
		resumeEnv := resume.Envelope()
		Expect(resumeEnv.Command).To(Equal("instance.resume"))
		Expect(stringField(resumeEnv.Data, "Status")).To(Equal("RUNNING"))

		list := cli.Run(context.Background(), "--output", "json", "instance", "list", "--limit", "20")
		list.ExpectSuccess()
		Expect(list.Envelope().Data).To(HaveKey("Items"))

		code := cli.Run(context.Background(), "--output", "json", "instance", "code", "run", instanceID,
			"--code", "print('ags-cli-e2e-code')")
		code.ExpectSuccess()
		codeEnv := code.Envelope()
		Expect(codeEnv.Command).To(Equal("instance.code.run"))
		Expect(codeEnv.Status).To(Equal("succeeded"))
		Expect(stringField(codeEnv.Data, "Stdout")).To(ContainSubstring("ags-cli-e2e-code"))

		execText := cli.Run(context.Background(), "instance", "exec", instanceID,
			"--", "sh", "-lc", "printf ags-cli-e2e-stdout; printf ags-cli-e2e-stderr >&2")
		execText.ExpectSuccess()
		Expect(execText.Stdout).To(ContainSubstring("ags-cli-e2e-stdout"), execText.Diagnostics())
		Expect(execText.Stderr).To(ContainSubstring("ags-cli-e2e-stderr"), execText.Diagnostics())

		remoteFailure := cli.Run(context.Background(), "instance", "exec", instanceID, "--", "sh", "-lc", "exit 7")
		remoteFailure.ExpectExit(7)
		Expect(strings.TrimSpace(remoteFailure.Stdout)).To(BeEmpty(), remoteFailure.Diagnostics())

		deleted := cli.Run(context.Background(), "--output", "json", "instance", "delete", instanceID, "--wait")
		deleted.ExpectSuccess()
		deletedEnv := deleted.Envelope()
		Expect(numberField(deletedEnv.Data, "Deleted")).To(Equal(1))
		tracker.ForgetInstance(instanceID)
	})

	It("returns a structured failure and non-zero exit code for a missing instance", func() {
		missing := cli.Run(context.Background(), "--output", "json", "instance", "get", "sdi-ags-cli-e2e-missing")
		Expect(missing.ExitCode).NotTo(Equal(0), missing.Diagnostics())
		Expect(missing.Stderr).To(BeEmpty(), missing.Diagnostics())
		env := missing.Envelope()
		Expect(env.Status).To(Equal("failed"))
		Expect(env.Failure).NotTo(BeNil())
		Expect(env.Failure.Kind).To(Equal("not_found"))
	})
})
