package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/token"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/cobra"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func init() {
	doctorCmd.RunE = Wrap("doctor", doctorFn)
	rootCmd.AddCommand(doctorCmd)
}

// DoctorCheck is one diagnostic row emitted by "agr doctor".
type DoctorCheck struct {
	Name    string          `json:"Name"`
	Status  string          `json:"Status"`
	Message string          `json:"Message"`
	Hint    string          `json:"Hint,omitempty"`
	Failure *output.Failure `json:"-"`
}

var doctorCmd = &cobra.Command{
	Use:     "doctor",
	Short:   "Diagnose CLI configuration issues",
	Example: exampleBlocks("agr doctor", "agr doctor -o json"),
}

func doctorFn(cmd *cobra.Command, args []string) (*CmdResult, error) {
	cfg := config.Get()
	var checks []DoctorCheck
	if configInitErr != nil {
		checks = append(checks, DoctorCheck{
			Name:    "ConfigFile",
			Status:  "error",
			Message: configInitErr.Error(),
			Hint:    "Run: fix the config file path or TOML syntax",
			Failure: output.NewUsageError("DOCTOR_CHECKS_FAILED", "doctor found one or more configuration errors", "Run: fix the reported issue and rerun agr doctor").Failure,
		})
		return doctorResult(checks)
	}
	if configBasicsErr != nil {
		checks = append(checks, doctorConfigCheck(configBasicsErr))
		return doctorResult(checks)
	}
	if configCommandErr != nil {
		checks = append(checks, doctorConfigCheck(configCommandErr))
	}

	if configCommandErr == nil {
		checks = append(checks, DoctorCheck{Name: "Output", Status: "ok", Message: fmt.Sprintf("output format is %s", configuredOutput)})
	}

	hasSecretID := cfg.Auth.SecretID != "" || os.Getenv("TENCENTCLOUD_SECRET_ID") != ""
	hasSecretKey := cfg.Auth.SecretKey != "" || os.Getenv("TENCENTCLOUD_SECRET_KEY") != ""
	hasToken := cfg.Auth.Token != "" || os.Getenv("TENCENTCLOUD_TOKEN") != ""
	if hasSecretID {
		checks = append(checks, DoctorCheck{Name: "SecretId", Status: "ok", Message: "SecretId is configured"})
	} else {
		checks = append(checks, DoctorCheck{
			Name:    "SecretId",
			Status:  "error",
			Message: "SecretId is not configured",
			Hint:    "Run: agr init --secret-id <id> --secret-key <key>",
			Failure: output.NewAuthError("MISSING_CLOUD_CREDENTIALS", "SecretId is not configured", "Run: agr init --secret-id <id> --secret-key <key>").Failure,
		})
	}
	if hasSecretKey {
		checks = append(checks, DoctorCheck{Name: "SecretKey", Status: "ok", Message: "SecretKey is configured"})
	} else {
		checks = append(checks, DoctorCheck{
			Name:    "SecretKey",
			Status:  "error",
			Message: "SecretKey is not configured",
			Hint:    "Run: agr init --secret-id <id> --secret-key <key>",
			Failure: output.NewAuthError("MISSING_CLOUD_CREDENTIALS", "SecretKey is not configured", "Run: agr init --secret-id <id> --secret-key <key>").Failure,
		})
	}
	if hasToken {
		checks = append(checks, DoctorCheck{Name: "Token", Status: "ok", Message: "Session token is configured (using STS credentials)"})
	}

	if _, err := token.NewCache(); err != nil {
		checks = append(checks, DoctorCheck{Name: "TokenCache", Status: "warning", Message: fmt.Sprintf("Token cache initialization failed: %v", err), Hint: "Run: mkdir -p ~/.agr && chmod 700 ~/.agr"})
	} else {
		checks = append(checks, DoctorCheck{Name: "TokenCache", Status: "ok", Message: "Token cache is accessible"})
	}
	if permWarn := config.CheckConfigFilePermissions(); permWarn != "" {
		checks = append(checks, DoctorCheck{Name: "ConfigFilePermission", Status: "warning", Message: permWarn, Hint: "Run: chmod 600 ~/.agr/config.toml"})
	} else {
		checks = append(checks, DoctorCheck{Name: "ConfigFilePermission", Status: "ok", Message: "config file permissions are secure"})
	}
	if hasSecretID && hasSecretKey {
		cloud, err := newCloudClient()
		if err != nil {
			checks = append(checks, doctorErrorCheck("Connectivity", fmt.Sprintf("failed to initialize cloud client: %v", err), "Run: agr status", err))
		} else if _, err := cloudDescribeSandboxInstanceList(cmd.Context(), cloud, &ags.DescribeSandboxInstanceListRequest{Limit: int64Ptr(1)}); err != nil {
			checks = append(checks, doctorErrorCheck("Connectivity", fmt.Sprintf("API probe failed: %v", err), "Run: agr doctor --debug", err))
		} else {
			checks = append(checks, DoctorCheck{Name: "Connectivity", Status: "ok", Message: "API reachable, credentials valid"})
		}
	} else {
		checks = append(checks, DoctorCheck{Name: "Connectivity", Status: "warning", Message: "skipped because credentials are missing", Hint: "Run: agr init --secret-id <id> --secret-key <key>"})
	}
	return doctorResult(checks)
}

func doctorResult(checks []DoctorCheck) (*CmdResult, error) {
	data := map[string]any{"Checks": checks}
	if cliErr := doctorFailure(checks); cliErr != nil {
		return &CmdResult{Data: data, Failure: cliErr.Failure, ExitCode: cliErr.ExitCode, RenderText: renderDoctorChecks(checks)}, nil
	}
	return OK(data, renderDoctorChecks(checks)), nil
}

func doctorConfigCheck(err error) DoctorCheck {
	issue := describeConfigIssue(err)
	return DoctorCheck{
		Name:    issue.Name,
		Status:  "error",
		Message: err.Error(),
		Hint:    "Run: " + issue.Hint,
		Failure: output.NewUsageError("DOCTOR_CHECKS_FAILED", "doctor found one or more configuration errors", "Run: fix the reported issue and rerun agr doctor").Failure,
	}
}

func doctorErrorCheck(name, message, hint string, err error) DoctorCheck {
	cliErr := classifyDoctorError(err)
	if cliErr.Failure.Hint != "" {
		hint = cliErr.Failure.Hint
	}
	return DoctorCheck{Name: name, Status: "error", Message: message, Hint: hint, Failure: cliErr.Failure}
}

func classifyDoctorError(err error) *output.CLIError {
	cliErr := output.ClassifyError(err)
	msg := strings.ToLower(err.Error())
	if cliErr.Failure != nil {
		msg += " " + strings.ToLower(cliErr.Failure.Message)
	}
	if isNetworkProbeMessage(msg) {
		return output.NewCLIError(&output.Failure{
			Code:      "NETWORK_ERROR",
			Kind:      output.KindNetwork,
			Message:   err.Error(),
			Hint:      "Check network connectivity and DNS settings, then retry.",
			Retryable: true,
		})
	}
	return cliErr
}

func isNetworkProbeMessage(msg string) bool {
	return strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "fail to get response") ||
		strings.Contains(msg, "dial tcp")
}

func renderDoctorChecks(checks []DoctorCheck) func(io.Writer) {
	return func(w io.Writer) {
		hasErrors := false
		for _, c := range checks {
			mark := "ok"
			if c.Status == "warning" {
				mark = "warning"
			}
			if c.Status == "error" {
				mark = "error"
				hasErrors = true
			}
			fmt.Fprintf(w, "%s %s: %s\n", mark, c.Name, c.Message)
			if c.Hint != "" {
				fmt.Fprintf(w, "  Hint: %s\n", c.Hint)
			}
		}
		if hasErrors {
			fmt.Fprintln(ios.ErrOut, "\nSome checks failed. Please fix the issues above.")
		}
	}
}

func doctorFailure(checks []DoctorCheck) *output.CLIError {
	for _, check := range checks {
		if check.Status == "error" {
			kind := output.KindGenericError
			retryable := false
			message := "doctor found one or more configuration errors"
			hint := "Run: fix the reported issue and rerun agr doctor"
			if check.Failure != nil {
				kind = check.Failure.Kind
				retryable = check.Failure.Retryable
				if kind == output.KindNetwork {
					message = "doctor connectivity check failed"
				} else if check.Name != "" {
					message = fmt.Sprintf("doctor %s check failed", strings.ToLower(check.Name))
				}
				if check.Failure.Hint != "" {
					hint = check.Failure.Hint
				} else if check.Hint != "" {
					hint = check.Hint
				}
			}
			return output.NewCLIError(&output.Failure{
				Code:      "DOCTOR_CHECKS_FAILED",
				Kind:      kind,
				Message:   message,
				Hint:      hint,
				Retryable: retryable,
				Details:   map[string]any{"Check": check.Name},
			})
		}
	}
	return nil
}
