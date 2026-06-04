package cli

import (
	"bytes"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("error rendering", func() {
	It("extracts request IDs from failure details for text output", func() {
		failure := &output.Failure{
			Code:    "InternalError.Backend",
			Kind:    output.KindGenericError,
			Message: "backend failed",
			Details: map[string]any{"RequestId": "req-123"},
		}

		Expect(failureRequestID(failure)).To(Equal("req-123"))
	})

	It("ignores missing or non-string request IDs", func() {
		Expect(failureRequestID(nil)).To(BeEmpty())
		Expect(failureRequestID(&output.Failure{})).To(BeEmpty())
		Expect(failureRequestID(&output.Failure{Details: map[string]any{"RequestId": 123}})).To(BeEmpty())
	})

	Describe("writeFailureText", func() {
		It("renders Code, Message, and RequestId on dedicated labeled lines", func() {
			var buf bytes.Buffer
			writeFailureText(&buf, &output.Failure{
				Code:    "InternalError.Backend",
				Kind:    output.KindGenericError,
				Message: "backend failed",
				Hint:    "Run 'agr doctor'.",
				Details: map[string]any{"RequestId": "req-123"},
			})

			Expect(buf.String()).To(Equal(
				"Error: backend failed\n" +
					"Code: InternalError.Backend\n" +
					"RequestId: req-123\n" +
					"Hint: Run 'agr doctor'.\n",
			))
		})

		It("omits RequestId when the failure does not carry one", func() {
			var buf bytes.Buffer
			writeFailureText(&buf, &output.Failure{
				Code:    "INVALID_USAGE",
				Message: "bad flag",
				Hint:    "Run 'agr --help'.",
			})

			Expect(buf.String()).To(Equal(
				"Error: bad flag\n" +
					"Code: INVALID_USAGE\n" +
					"Hint: Run 'agr --help'.\n",
			))
		})

		It("omits the Code line when no error code is present", func() {
			var buf bytes.Buffer
			writeFailureText(&buf, &output.Failure{
				Message: "something broke",
			})

			Expect(buf.String()).To(Equal("Error: something broke\n"))
		})

		It("includes Retryable when the failure is marked retryable", func() {
			var buf bytes.Buffer
			writeFailureText(&buf, &output.Failure{
				Code:      "RequestLimitExceeded",
				Message:   "throttled",
				Retryable: true,
				Details:   map[string]any{"RequestId": "req-xyz"},
			})

			Expect(buf.String()).To(Equal(
				"Error: throttled\n" +
					"Code: RequestLimitExceeded\n" +
					"RequestId: req-xyz\n" +
					"Retryable: yes\n",
			))
		})

		It("is a no-op when the failure is nil", func() {
			var buf bytes.Buffer
			writeFailureText(&buf, nil)
			Expect(buf.String()).To(BeEmpty())
		})
	})
})
