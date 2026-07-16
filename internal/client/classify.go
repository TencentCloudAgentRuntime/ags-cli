package client

import (
	"errors"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

// ClassifyError is the unified error classification entry point. It tries
// cloud SDK error classification first (most specific), then falls back to
// the generic Go error classifier. All command-layer error returns should
// go through this function for consistent user-facing output.
//
// Classification priority:
//  1. Already a *output.CLIError → return as-is
//  2. TencentCloud SDK error → ClassifyCloudError (kind/code/hint from API response)
//  3. Generic Go error → output.ClassifyError (timeout/network/canceled/internal)
func ClassifyError(err error) *output.CLIError {
	if err == nil {
		return nil
	}

	// If already classified, return directly.
	if cliErr, ok := err.(*output.CLIError); ok {
		return attachFix(cliErr)
	}

	// Check if it's a TencentCloud SDK error using errors.As (safe for
	// uncomparable error types that would panic with == comparison).
	var sdkErr *sdkerrors.TencentCloudSDKError
	if errors.As(err, &sdkErr) {
		classified := ClassifyCloudError(err)
		if cliErr, ok := classified.(*output.CLIError); ok {
			return attachFix(cliErr)
		}
	}

	// Fall through to generic Go error classification.
	return attachFix(output.ClassifyError(err))
}

// attachFix applies Fix to a CLIError wrapper.
func attachFix(cliErr *output.CLIError) *output.CLIError {
	if cliErr == nil || cliErr.Failure == nil {
		return cliErr
	}
	AttachFix(cliErr.Failure)
	return cliErr
}

// AttachFix populates the Failure.Fix field with an actionable command based
// on the error kind. Exported so that non-error result paths (e.g.
// CmdResult{Failure: ...}) can also apply Fix consistently.
func AttachFix(failure *output.Failure) *output.Failure {
	if failure == nil || failure.Fix != "" {
		return failure
	}
	switch failure.Kind {
	case output.KindNotFound:
		failure.Fix = "agr instance list / agr tool list"
	case output.KindAuthOrPermission:
		failure.Fix = "agr init / agr doctor"
	case output.KindNetwork:
		failure.Fix = "agr doctor"
	case output.KindTimeout:
		failure.Fix = "retry with longer --timeout or check agr doctor"
	case output.KindRateLimit:
		failure.Fix = "wait and retry"
	}
	return failure
}
