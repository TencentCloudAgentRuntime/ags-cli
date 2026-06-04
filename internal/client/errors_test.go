package client

import (
	"errors"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

var _ = Describe("ClassifyCloudError", func() {
	It("preserves TencentCloud service error request IDs in failure details", func() {
		err := sdkerrors.NewTencentCloudSDKError("InternalError.Backend", "backend failed", "req-123")

		classified := ClassifyCloudError(err)

		var cliErr *output.CLIError
		Expect(errors.As(classified, &cliErr)).To(BeTrue())
		Expect(cliErr.Failure.Code).To(Equal("InternalError.Backend"))
		Expect(cliErr.Failure.Message).To(Equal("backend failed"))
		Expect(cliErr.Failure.Details).To(HaveKeyWithValue("RequestId", "req-123"))
	})

	It("omits request ID details when the service did not return one", func() {
		err := sdkerrors.NewTencentCloudSDKError("InternalError.Backend", "backend failed", "")

		classified := ClassifyCloudError(err)

		var cliErr *output.CLIError
		Expect(errors.As(classified, &cliErr)).To(BeTrue())
		Expect(cliErr.Failure.Details).To(BeNil())
	})

	It("adds an STS refresh hint for token auth failures", func() {
		config.SetToken("session-token")
		defer config.SetToken("")
		err := sdkerrors.NewTencentCloudSDKError("AuthFailure.TokenInvalidExpired", "token expired", "req-123")

		classified := ClassifyCloudError(err)

		var cliErr *output.CLIError
		Expect(errors.As(classified, &cliErr)).To(BeTrue())
		Expect(cliErr.Failure.Hint).To(ContainSubstring("STS refresh"))
		Expect(cliErr.Failure.Hint).To(ContainSubstring("retry"))
	})
})
