package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/cobra"
)

// ExplainData is the machine-readable explanation for one AGR error or exit
// code.
type ExplainData struct {
	Code             string   `json:"Code"`
	Kind             string   `json:"Kind"`
	ExitCode         int      `json:"ExitCode"`
	Retryable        bool     `json:"Retryable"`
	Meaning          string   `json:"Meaning"`
	AffectedCommands []string `json:"AffectedCommands"`
	Fix              []string `json:"Fix"`
	Related          []string `json:"Related"`
}

var explainCmd = &cobra.Command{
	Use:     "explain <CODE|exit-codes>",
	Short:   "Explain an AGR error code or exit codes",
	Example: exampleBlocks("agr explain MISSING_CLOUD_CREDENTIALS", "agr explain exit-codes"),
	Args:    cobra.ExactArgs(1),
}

func init() {
	explainCmd.RunE = Wrap("explain", explainFn)
	rootCmd.AddCommand(explainCmd)
}

func explainFn(cmd *cobra.Command, args []string) (*CmdResult, error) {
	code := strings.ToUpper(args[0])
	if strings.EqualFold(args[0], "exit-codes") {
		data := exitCodeTable()
		return OK(data, func(w io.Writer) {
			fmt.Fprintln(w, "0   success")
			fmt.Fprintln(w, "1   error")
			fmt.Fprintln(w, "2   usage")
			fmt.Fprintln(w, "4   auth")
			fmt.Fprintln(w, "255 remote_execution_failed")
		}), nil
	}
	lookupCode := normalizeExplainCode(code)
	data, _ := explainCodeData(lookupCode)
	data.Code = code
	return OK(data, func(w io.Writer) {
		fmt.Fprintf(w, "%s (kind: %s, exit: %d, retryable: %s)\n\n", data.Code, data.Kind, data.ExitCode, yesNo(data.Retryable))
		fmt.Fprintln(w, "Meaning:")
		fmt.Fprintf(w, "  %s\n\n", data.Meaning)
		fmt.Fprintln(w, "Affected commands:")
		fmt.Fprintf(w, "  %s\n\n", strings.Join(data.AffectedCommands, ", "))
		fmt.Fprintln(w, "Fix:")
		for _, f := range data.Fix {
			fmt.Fprintf(w, "  %s\n", f)
		}
	}), nil
}

func exitCodeTable() map[string]string {
	return map[string]string{"0": "success", "1": "error", "2": "usage", "4": "auth", "255": "remote_execution_failed"}
}

func normalizeExplainCode(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "0", "SUCCESS":
		return "SUCCESS"
	case "1", "ERROR":
		return "ERROR"
	case "2", "USAGE":
		return "USAGE"
	case "4", "AUTH":
		return "AUTH"
	case "255", "REMOTE_EXECUTION_FAILED":
		return "REMOTE_EXECUTION_FAILED"
	default:
		return strings.ToUpper(strings.TrimSpace(code))
	}
}

func explainCodeData(code string) (ExplainData, bool) {
	base := ExplainData{Code: code, Related: []string{"agr doctor"}}
	switch code {
	case "SUCCESS":
		base.Kind = output.KindSuccess
		base.ExitCode = output.ExitSuccess
		base.Meaning = "The command completed successfully."
		base.AffectedCommands = allCommandNames()
		base.Fix = []string{"No action needed."}
	case "ERROR":
		base.Kind = output.KindGenericError
		base.ExitCode = output.ExitGenericError
		base.Meaning = "The command failed for a non-usage, non-auth reason."
		base.AffectedCommands = allCommandNames()
		base.Fix = []string{"Inspect Failure.Code, Failure.Kind, and Failure.Hint in -o json output.", "Run: agr doctor"}
	case "USAGE":
		base.Kind = output.KindUsage
		base.ExitCode = output.ExitUsage
		base.Meaning = "The command line arguments, flags, or input are invalid."
		base.AffectedCommands = allCommandNames()
		base.Fix = []string{"Run: agr schema -o json", "Review the command help and retry with valid flags or arguments."}
	case "AUTH":
		base.Kind = output.KindAuthOrPermission
		base.ExitCode = output.ExitAuthOrPermission
		base.Meaning = "Tencent Cloud credentials are missing, invalid, or unauthorized."
		base.AffectedCommands = affectedCommands("AUTH_FAILED")
		base.Fix = []string{"agr init --secret-id <id> --secret-key <key>", "agr doctor"}
	case "REMOTE_EXECUTION_FAILED":
		base.Kind = output.KindRemoteExecFailed
		base.ExitCode = output.ExitRemoteExecFailed
		base.Meaning = "Remote code or command execution failed inside the sandbox."
		base.AffectedCommands = uniqueSorted(append(affectedCommands("REMOTE_CODE_FAILED"), affectedCommands("REMOTE_COMMAND_FAILED")...))
		base.Fix = []string{"Inspect Data.Error, Stdout, Stderr, and ExitCode in -o json output."}
	case "INSTANCE_NOT_FOUND", "NO_ACTIVE_TUNNEL":
		base.Kind = output.KindNotFound
		base.ExitCode = output.ExitGenericError
		base.Meaning = "The requested sandbox instance or local tunnel record does not exist."
		base.AffectedCommands = affectedCommands(code)
		base.Fix = []string{"agr instance list", "Use an active InstanceId, or recreate/connect the resource."}
	case "MISSING_CLOUD_CREDENTIALS", "AUTH_FAILED":
		base.Kind = output.KindAuthOrPermission
		base.ExitCode = output.ExitAuthOrPermission
		base.Meaning = "Tencent Cloud credentials are missing, invalid, or unauthorized."
		base.AffectedCommands = affectedCommands(code)
		base.Fix = []string{"agr init --secret-id <id> --secret-key <key>", "agr doctor"}
	case "INVALID_USAGE", "JQ_REQUIRES_JSON", "SKELETON_UNSUPPORTED", "CONFLICTING_FLAGS", "CONFLICTING_INPUTS",
		"MISSING_CODE", "INVALID_ENV", "INVALID_PORT", "INVALID_PAGINATION", "INVALID_LOCAL_PATH", "INVALID_ADDRESS",
		"INVALID_SHELL", "INVALID_CLEANUP", "MISSING_SEPARATOR", "STDOUT_CONFLICT", "ADB_NOT_FOUND", "UNSUPPORTED_LANGUAGE",
		"MISSING_ACTION", "NDJSON_REQUIRES_STREAM", "STREAM_JSON_CONFLICT", "TTY_REQUIRED", "UNIMPLEMENTED_COMMAND",
		"CONFIG_EXISTS", "CONFIG_INIT_FAILED", "DOCTOR_CHECKS_FAILED", "PARTIAL_DELETE_FAILED", "TOOL_NOT_FOUND",
		"INVALID_REQUEST_INPUT":
		base.Kind = output.KindUsage
		base.ExitCode = output.ExitUsage
		base.Meaning = meaningForCLIUsageCode(code)
		base.AffectedCommands = affectedCommands(code)
		base.Fix = fixForCLIUsageCode(code)
	case "UNKNOWN_REQUEST_FIELD", "REQUEST_FLAG_CONFLICT", "REQUEST_ARG_CONFLICT", "INVALID_JQ_EXPRESSION", "INVALID_JSON_FLAG", "INVALID_REQUEST_JSON", "MISSING_REQUIRED_FLAG", "MISSING_REQUIRED_ARG":
		base.Kind = output.KindUsage
		base.ExitCode = output.ExitUsage
		base.Meaning = "The command line arguments or input JSON are invalid."
		base.AffectedCommands = affectedCommands(code)
		base.Fix = []string{"agr schema -o json", "Review the command help and retry with valid flags or request fields."}
	case "INVALID_CONFIG", "INVALID_CONFIG_KEY", "CONFIG_WRITE_FAILED":
		base.Kind = output.KindUsage
		base.ExitCode = output.ExitUsage
		base.Meaning = "The local CLI configuration is invalid or could not be updated."
		base.AffectedCommands = affectedCommands(code)
		base.Fix = []string{"agr config show -o json", "Review the config key, value, and local file permissions before retrying."}
	case "MISSING_INSTANCE":
		base.Kind = output.KindUsage
		base.ExitCode = output.ExitUsage
		base.Meaning = "A required sandbox instance id was not provided."
		base.AffectedCommands = affectedCommands(code)
		base.Fix = []string{"Provide an instance id, or add --create-temp-instance where supported.", "agr schema instance.code.run -o json"}
	case "INVALID_TOOL":
		base.Kind = output.KindUsage
		base.ExitCode = output.ExitUsage
		base.Meaning = "The referenced tool selection is invalid or incomplete."
		base.AffectedCommands = affectedCommands(code)
		base.Fix = []string{"Provide a valid --tool-name or --tool-id.", "agr tool list -o json"}
	case "CLIENT_TOKEN_CONFLICT":
		base.Kind = output.KindConflict
		base.ExitCode = output.ExitGenericError
		base.Meaning = "The client token has already been used to create a resource."
		base.AffectedCommands = affectedCommands(code)
		base.Fix = []string{"Use a new --client-token for a new resource, or recover the original resource from your local record."}
	case "REMOTE_CODE_FAILED", "REMOTE_COMMAND_FAILED":
		base.Kind = output.KindRemoteExecFailed
		base.ExitCode = output.ExitRemoteExecFailed
		base.Meaning = "Remote code or command execution failed inside the sandbox."
		base.AffectedCommands = affectedCommands(code)
		base.Fix = []string{"Inspect Data.Error, Stdout, Stderr, and ExitCode in -o json output."}
	default:
		// Prefix-match common Tencent Cloud SDK error code families
		switch {
		case strings.HasPrefix(code, "AUTHFAILURE") || strings.HasPrefix(code, "UNAUTHORIZEDOPERATION"):
			base.Kind = output.KindAuthOrPermission
			base.ExitCode = output.ExitAuthOrPermission
			base.Meaning = "Authentication or authorization failed at the Tencent Cloud API level."
			base.AffectedCommands = affectedCommands("AUTH_FAILED")
			base.Fix = []string{"Check your credentials (TENCENTCLOUD_SECRET_ID/TENCENTCLOUD_SECRET_KEY).", "Verify your account has AGS permissions.", "agr doctor"}
		case strings.HasPrefix(code, "RESOURCENOTFOUND.SANDBOXTOOL"):
			base.Kind = output.KindNotFound
			base.ExitCode = output.ExitGenericError
			base.Meaning = "The requested sandbox tool was not found on the server."
			base.AffectedCommands = []string{"tool.get", "tool.delete", "tool.update", "instance.create"}
			base.Fix = []string{"agr tool list", "Verify the tool ID is correct and the tool has not been deleted."}
		case strings.HasPrefix(code, "RESOURCENOTFOUND.SANDBOXINSTANCE"):
			base.Kind = output.KindNotFound
			base.ExitCode = output.ExitGenericError
			base.Meaning = "The requested sandbox instance was not found on the server."
			base.AffectedCommands = affectedCommands("INSTANCE_NOT_FOUND")
			base.Fix = []string{"agr instance list", "Verify the instance ID is correct and the instance has not been deleted."}
		case strings.HasPrefix(code, "RESOURCENOTFOUND"):
			base.Kind = output.KindNotFound
			base.ExitCode = output.ExitGenericError
			base.Meaning = "The requested resource was not found on the server."
			base.AffectedCommands = []string{}
			base.Fix = []string{"Verify the resource ID is correct and the resource has not been deleted.", "Use the matching list command for that resource type."}
		case strings.HasPrefix(code, "LIMITEXCEEDED") || code == "REQUESTLIMITEXCEEDED" || strings.HasPrefix(code, "RESOURCEINSUFFICIENT"):
			base.Kind = output.KindRateLimit
			base.ExitCode = output.ExitGenericError
			base.Retryable = true
			base.Meaning = "A rate limit or resource quota has been exceeded."
			base.AffectedCommands = []string{"instance.create", "tool.create", "apikey.create"}
			base.Fix = []string{"Wait briefly and retry.", "Check your account quotas in the Tencent Cloud console."}
		case strings.HasPrefix(code, "INVALIDPARAMETER") || strings.HasPrefix(code, "MISSINGPARAMETER") || strings.HasPrefix(code, "INVALIDPARAMETERVALUE"):
			base.Kind = output.KindUsage
			base.ExitCode = output.ExitUsage
			base.Meaning = "One or more request parameters are invalid or missing."
			base.AffectedCommands = []string{}
			base.Fix = []string{"Check the command flags or --request payload.", "agr schema <command> -o json"}
		default:
			base.Kind = output.KindGenericError
			base.ExitCode = output.ExitGenericError
			base.Meaning = fmt.Sprintf("Unknown error code. '%s' is not registered in AGR CLI — it may be a server-side error from the Tencent Cloud AGS API.", code)
			base.AffectedCommands = []string{}
			base.Fix = []string{"agr doctor", "Check the Tencent Cloud AGS API documentation for this error code."}
		}
	}
	return base, true
}

func affectedCommands(code string) []string {
	var out []string
	for _, s := range getAllSchemas() {
		if (code == "AUTH_FAILED" || code == "MISSING_CLOUD_CREDENTIALS") && s.RequiresAuth {
			out = append(out, s.Name)
			continue
		}
		for _, f := range s.Failures {
			if f == code || (code == "INSTANCE_NOT_FOUND" && f == "MISSING_INSTANCE") || (code == "TOOL_NOT_FOUND" && f == "INVALID_TOOL") {
				out = append(out, s.Name)
			}
		}
	}
	return uniqueSorted(out)
}

func allCommandNames() []string {
	schemas := getAllSchemas()
	out := make([]string, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, s.Name)
	}
	return uniqueSorted(out)
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func meaningForCLIUsageCode(code string) string {
	switch code {
	case "CONFIG_EXISTS":
		return "The local AGR config file already exists and init was asked to create it again."
	case "CONFIG_INIT_FAILED":
		return "The CLI could not determine, create, read, or write the local configuration file."
	case "DOCTOR_CHECKS_FAILED":
		return "One or more doctor checks failed, so the CLI environment is not healthy."
	case "INVALID_SHELL":
		return "The requested shell is not supported for completion generation."
	case "JQ_REQUIRES_JSON":
		return "--jq was used without an explicit -o json output mode."
	case "SKELETON_UNSUPPORTED":
		return "--generate-skeleton was used on a command that does not accept request bodies."
	case "NDJSON_REQUIRES_STREAM":
		return "ndjson output was requested without a supported streaming command."
	case "STREAM_JSON_CONFLICT":
		return "--stream was used together with -o json, which is not supported."
	case "TTY_REQUIRED":
		return "The command requires an interactive TTY terminal."
	case "INVALID_ADDRESS":
		return "The requested local bind address is invalid."
	case "INVALID_PAGINATION":
		return "One or more pagination flags are invalid."
	case "INVALID_PORT":
		return "The requested port or port specification is invalid."
	case "INVALID_LOCAL_PATH":
		return "The referenced local file path is missing, unreadable, or not writable."
	case "STDOUT_CONFLICT":
		return "The chosen file download destination conflicts with stdout streaming."
	case "INVALID_ENV":
		return "One or more environment variables are not in KEY=VALUE format."
	case "MISSING_CODE":
		return "No code input was provided to a code execution command."
	case "UNSUPPORTED_LANGUAGE":
		return "The selected execution language is not supported."
	case "CONFLICTING_INPUTS":
		return "Multiple mutually exclusive code input sources were provided."
	case "ADB_NOT_FOUND":
		return "The adb binary was not found on the local machine."
	case "MISSING_SEPARATOR":
		return "The mobile adb command is missing the required -- separator before adb arguments."
	case "MISSING_ACTION":
		return "agr api call was invoked without the required API action name."
	case "CONFLICTING_FLAGS":
		return "The provided combination of flags is mutually incompatible."
	case "INVALID_CLEANUP":
		return "The cleanup policy for a temporary instance is invalid."
	case "UNIMPLEMENTED_COMMAND":
		return "A generated command exists in the command tree but does not have an implementation hook."
	case "PARTIAL_DELETE_FAILED":
		return "A bulk delete completed partially and one or more resources failed to delete."
	case "TOOL_NOT_FOUND":
		return "The referenced sandbox tool does not exist."
	case "INVALID_REQUEST_INPUT":
		return "The provided request input does not match the expected request shape."
	default:
		return "The command line arguments are invalid for the requested command."
	}
}

func fixForCLIUsageCode(code string) []string {
	switch code {
	case "CONFIG_EXISTS":
		return []string{"Rerun with --overwrite if replacing the local config is intended.", "agr config path"}
	case "CONFIG_INIT_FAILED":
		return []string{"Check the config file path, HOME, and local file permissions.", "agr config path"}
	case "DOCTOR_CHECKS_FAILED":
		return []string{"Review the failed doctor check details and fix them before retrying.", "agr doctor"}
	case "INVALID_SHELL":
		return []string{"Use one of: bash, zsh, fish, powershell."}
	case "JQ_REQUIRES_JSON":
		return []string{"Add -o json, for example: agr status -o json --jq '.Data'."}
	case "SKELETON_UNSUPPORTED":
		return []string{"Use --generate-skeleton only on request-based commands.", "agr schema -o json"}
	case "NDJSON_REQUIRES_STREAM":
		return []string{"Use -o json for a single envelope, or add --stream on a supported streaming command."}
	case "STREAM_JSON_CONFLICT":
		return []string{"Use -o ndjson for machine-readable stream events, or remove --stream to get a JSON envelope."}
	case "TTY_REQUIRED":
		return []string{"Run the command from an interactive terminal."}
	case "INVALID_ADDRESS":
		return []string{"Use localhost, 127.0.0.1, ::1, or another valid IP address."}
	case "INVALID_PAGINATION":
		return []string{"Use non-negative values for --offset and --limit."}
	case "INVALID_PORT":
		return []string{"Provide ports in the range 1-65535."}
	case "INVALID_LOCAL_PATH":
		return []string{"Verify the local file path exists and has the required read/write permissions."}
	case "STDOUT_CONFLICT":
		return []string{"Choose either stdout or a file destination, but not both at once."}
	case "INVALID_ENV":
		return []string{"Use --env KEY=VALUE."}
	case "MISSING_CODE":
		return []string{"Provide code with -c/--code, -f/--file, or stdin."}
	case "UNSUPPORTED_LANGUAGE":
		return []string{"Use one of: python, javascript, typescript, r, java, bash."}
	case "CONFLICTING_INPUTS":
		return []string{"Use only one of -c/--code, -f/--file, or piped stdin."}
	case "ADB_NOT_FOUND":
		return []string{"Install Android SDK Platform-Tools or set ADB_PATH to a valid adb binary."}
	case "MISSING_SEPARATOR":
		return []string{"Insert -- before adb arguments, for example: agr instance mobile adb <id> -- devices."}
	case "MISSING_ACTION":
		return []string{"Run: agr api call <Action> --request '{\"...\":\"...\"}'."}
	case "CONFLICTING_FLAGS":
		return []string{"Review the command help and provide only one option from each mutually exclusive flag set."}
	case "INVALID_CLEANUP":
		return []string{"Use one of: always, success, never."}
	case "UNIMPLEMENTED_COMMAND":
		return []string{"Update the CLI implementation so the generated command has a runtime hook."}
	case "PARTIAL_DELETE_FAILED":
		return []string{"Inspect the warnings and retry the failed resource deletions individually."}
	case "TOOL_NOT_FOUND":
		return []string{"agr tool list", "Verify the tool ID is correct and the tool has not been deleted."}
	case "INVALID_REQUEST_INPUT":
		return []string{"Review the command schema or request skeleton and retry with a matching JSON shape.", "agr schema -o json"}
	default:
		return []string{"agr schema -o json", "Review the command help and retry with valid flags or arguments."}
	}
}
