package client

import (
	"errors"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

// ClassifyCloudError wraps a TencentCloud SDK error into a CLIError with
// the correct kind/exit code based on the SDK error code. This eliminates
// string matching in the generic ClassifyError.
func ClassifyCloudError(err error) error {
	if err == nil {
		return nil
	}
	var sdkErr *sdkerrors.TencentCloudSDKError
	if !errors.As(err, &sdkErr) {
		return err
	}

	code := sdkErr.GetCode()
	msg := sdkErr.GetMessage()
	requestID := sdkErr.GetRequestId()

	switch {
	case code == "AuthFailure" || strings.HasPrefix(code, "AuthFailure."):
		return newCloudCLIError(output.KindAuthOrPermission, code, msg, authFailureHint(code), false, requestID)
	case code == "ResourceNotFound.SandboxTool":
		return newCloudCLIError(output.KindNotFound, code, msg, "Run 'agr tool list' to find available tools.", false, requestID)
	case code == "ResourceNotFound.SandboxInstance":
		return newCloudCLIError(output.KindNotFound, code, msg, "Run 'agr instance list' to find active instances.", false, requestID)
	case code == "ResourceNotFound" || strings.HasPrefix(code, "ResourceNotFound."):
		return newCloudCLIError(output.KindNotFound, code, msg, "Verify the resource ID is correct and the resource has not been deleted.", false, requestID)
	case code == "ResourceInsufficient" || strings.HasPrefix(code, "LimitExceeded."):
		return newCloudCLIError(output.KindRateLimit, code, msg, "Safe to retry after a brief wait.", true, requestID)
	case strings.Contains(code, "DuplicatedClientToken") || strings.Contains(code, "ClientTokenConflict"):
		return newCloudCLIError(output.KindConflict, "CLIENT_TOKEN_CONFLICT", msg,
			"This token may have been used already. Use a different --client-token or omit it for a new instance.", false, requestID)
	case code == "UnauthorizedOperation" || strings.HasPrefix(code, "UnauthorizedOperation."):
		return newCloudCLIError(output.KindAuthOrPermission, code, msg, "Check your permissions.", false, requestID)
	case strings.HasPrefix(code, "InvalidParameter") || strings.HasPrefix(code, "MissingParameter") || strings.HasPrefix(code, "InvalidParameterValue"):
		return newCloudCLIError(output.KindUsage, code, msg, "Check the command flags or request payload and try again.", false, requestID)
	case code == "RequestLimitExceeded":
		return newCloudCLIError(output.KindRateLimit, code, msg, "Safe to retry after a brief wait.", true, requestID)
	default:
		return newCloudCLIError(output.KindGenericError, code, msg, "Run 'agr doctor' to diagnose configuration and connectivity.", false, requestID)
	}
}

func authFailureHint(code string) string {
	if config.GetToken() == "" {
		return "Check your credentials (TENCENTCLOUD_SECRET_ID/TENCENTCLOUD_SECRET_KEY)."
	}
	if code == "AuthFailure.TokenInvalidExpired" ||
		code == "AuthFailure.TokenFailure" ||
		code == "AuthFailure.InvalidToken" ||
		strings.Contains(strings.ToLower(code), "token") {
		return "Session token may have expired. Run your STS refresh script and retry."
	}
	return "Check your STS credentials (TENCENTCLOUD_SECRET_ID/TENCENTCLOUD_SECRET_KEY/TENCENTCLOUD_TOKEN), refresh the session token if needed, and retry."
}

func newCloudCLIError(kind, code, message, hint string, retryable bool, requestID string) *output.CLIError {
	return output.NewCLIError(withCloudRequestID(&output.Failure{
		Code:      code,
		Kind:      kind,
		Message:   message,
		Hint:      hint,
		Retryable: retryable,
	}, requestID))
}

func withCloudRequestID(f *output.Failure, requestID string) *output.Failure {
	if requestID == "" {
		return f
	}
	if f.Details == nil {
		f.Details = map[string]any{}
	}
	f.Details["RequestId"] = requestID
	return f
}
