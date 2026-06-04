package cli

import (
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("doctorFailure", func() {
	It("returns usage failure for error checks", func() {
		err := doctorFailure([]DoctorCheck{{
			Name:    "ConfigFile",
			Status:  "error",
			Failure: output.NewUsageError("DOCTOR_CHECKS_FAILED", "bad config", "fix config").Failure,
		}})
		Expect(err).NotTo(BeNil())
		Expect(err.ExitCode).To(Equal(output.ExitUsage))
		Expect(err.Failure.Code).To(Equal("DOCTOR_CHECKS_FAILED"))
	})

	It("returns network failure for connectivity checks", func() {
		err := doctorFailure([]DoctorCheck{{
			Name:    "Connectivity",
			Status:  "error",
			Failure: (&output.Failure{Code: "NETWORK_ERROR", Kind: output.KindNetwork, Message: "dns failed", Hint: "Check network connectivity and DNS settings, then retry.", Retryable: true}),
		}})
		Expect(err).NotTo(BeNil())
		Expect(err.ExitCode).To(Equal(output.ExitGenericError))
		Expect(err.Failure.Kind).To(Equal(output.KindNetwork))
		Expect(err.Failure.Retryable).To(BeTrue())
		Expect(err.Failure.Details).To(HaveKeyWithValue("Check", "Connectivity"))
		Expect(err.Failure.Message).To(Equal("doctor connectivity check failed"))
		Expect(err.Failure.Message).NotTo(ContainSubstring("configuration"))
		Expect(err.Failure.Hint).To(ContainSubstring("network connectivity and DNS"))
	})

	It("classifies common probe DNS failures as network failures", func() {
		err := classifyDoctorError(assertErr("lookup tencentags.com: no such host"))
		Expect(err.Failure.Kind).To(Equal(output.KindNetwork))
		Expect(err.ExitCode).To(Equal(output.ExitGenericError))

		err = classifyDoctorError(output.NewCLIError(&output.Failure{
			Code:    "ClientError.NetworkError",
			Kind:    output.KindGenericError,
			Message: `Fail to get response because Post "https://ags.tencentcloudapi.com/": dial tcp: lookup ags.tencentcloudapi.com: no such host`,
			Hint:    "debug",
		}))
		Expect(err.Failure.Kind).To(Equal(output.KindNetwork))
		Expect(err.Failure.Retryable).To(BeTrue())
	})

	It("returns nil when there are no error checks", func() {
		err := doctorFailure([]DoctorCheck{{Name: "SecretId", Status: "warning"}, {Name: "TokenCache", Status: "ok"}})
		Expect(err).To(BeNil())
	})

	It("keeps STS token doctor checks informational", func() {
		err := doctorFailure([]DoctorCheck{{Name: "Token", Status: "ok", Message: "Session token is configured (using STS credentials)"}})
		Expect(err).To(BeNil())
	})

	It("returns auth failure when credentials are missing", func() {
		err := doctorFailure([]DoctorCheck{{
			Name:    "SecretId",
			Status:  "error",
			Failure: output.NewAuthError("MISSING_CLOUD_CREDENTIALS", "SecretId is not configured", "init").Failure,
		}})
		Expect(err).NotTo(BeNil())
		Expect(err.ExitCode).To(Equal(output.ExitAuthOrPermission))
		Expect(err.Failure.Kind).To(Equal(output.KindAuthOrPermission))
		Expect(err.Failure.Code).To(Equal("DOCTOR_CHECKS_FAILED"))
	})
})

type assertErr string

func (e assertErr) Error() string { return string(e) }
