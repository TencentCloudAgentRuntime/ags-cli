// Package cli owns the top-level Cobra command tree, process-wide flags,
// output envelopes, and compatibility helpers used by command modules.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	cfgFile          string
	outputFmt        string
	showVersion      bool
	region           string
	domain           string
	cloudEndpoint    string
	secretID         string
	secretKey        string
	tokenFlag        string
	jqExpr           string
	nonInteractive   bool
	noColor          bool
	debugFlag        bool
	configInitErr    error
	configBasicsErr  error
	configCommandErr error
	configuredOutput string
)

var rootCmd = &cobra.Command{
	Use:           "agr",
	Short:         "AGR CLI - Agent Runtime Command Line Interface",
	SilenceUsage:  true,
	SilenceErrors: true,
	Long: `AGR CLI is the command-line interface for Tencent Cloud Agent Runtime (AGR).

Create sandbox instances, execute code, manage tools and API keys.
Run 'agr doctor' to diagnose configuration issues.`,
	Example: exampleBlocks(
		`tool_id=$(agr tool create --tool-name "quickstart-code-$(date +%s)-$$" --tool-type code-interpreter --network-configuration '{"NetworkMode":"SANDBOX"}' -o json --jq '.Data.ToolId')
instance_id=$(agr instance create --tool-id "$tool_id" -o json --jq '.Data.InstanceId')
agr instance code run "$instance_id" -c "print('Hello, World!')"
agr instance delete "$instance_id" --ignore-not-found
agr tool delete "$tool_id"`,
		"agr status",
		"agr doctor",
	),
	RunE: func(cmd *cobra.Command, args []string) error {
		if showVersion {
			return Wrap("version", versionFn)(cmd, args)
		}
		if isJSON() {
			return renderJSONSchemaEnvelope("schema", cmd, []string{})
		}
		return cmd.Help()
	},
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.SetHelpCommand(newHelpCommand())

	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		applyRawGlobalArgs(os.Args[1:])
		initConfig()
		wantJSON := effectiveOutput() == "json" || hasRawOutputFlag("json")
		wantNDJSON := effectiveOutput() == "ndjson" || hasRawOutputFlag("ndjson")
		hasJQ := jqExpr != "" || hasRawFlag("--jq")

		if hasJQ && !hasRawOutputFlag("json") {
			fmt.Fprintln(os.Stderr, "Error: --jq can only be used with explicit -o json")
			os.Exit(output.ExitUsage)
		}
		if wantNDJSON {
			fmt.Fprintln(os.Stderr, "Error: -o ndjson is not supported with --help")
			os.Exit(output.ExitUsage)
		}

		if wantJSON {
			cmdID := canonicalCommandID(cmd)
			if cmdID == "" {
				_ = renderJSONSchemaEnvelope("help", cmd, []string{})
			} else {
				_ = renderJSONSchemaEnvelope("help", cmd, []string{cmdID})
			}
			return
		}
		defaultHelp(cmd, args)
	})

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if jqExpr != "" && !hasRawOutputFlag("json") {
			return output.NewUsageError("JQ_REQUIRES_JSON", "--jq can only be used with explicit -o json", "Add -o json, for example: agr status -o json --jq '.Data'.")
		}
		if generateSkeleton && !supportsSkeleton(canonicalCommandID(cmd)) {
			return output.NewUsageError("SKELETON_UNSUPPORTED", "--generate-skeleton is only supported for request-based commands", "Run: agr schema -o json")
		}
		if effectiveOutput() == "ndjson" && !isNDJSONAllowedCommand(cmd) {
			err := invalidNDJSONOutputError(cmd)
			if shouldAllowDiagnosticOutputOverride(cmd) && outputFmt == "" {
				configCommandErr = err
				outputFmt = "text"
			} else {
				return err
			}
		}
		if shouldSkipConfigPreflight(cmd) {
			return nil
		}
		if configCommandErr != nil && !shouldAllowDiagnosticOutputOverride(cmd) {
			return configCommandErr
		}
		if cmd == rootCmd && showVersion {
			return nil
		}
		if configInitErr != nil {
			return configInitErr
		}
		if configBasicsErr != nil {
			return configBasicsErr
		}
		return nil
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.agr/config.toml)")
	rootCmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "", "output format: text, json, or ndjson")
	rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "Print version information")
	rootCmd.PersistentFlags().StringVar(&region, "region", "", "Region for API access (default: ap-guangzhou)")
	rootCmd.PersistentFlags().StringVar(&domain, "domain", "", "Data-plane base domain (default: tencentags.com)")
	rootCmd.PersistentFlags().StringVar(&cloudEndpoint, "cloud-endpoint", "", "Control-plane API endpoint (default: ags.tencentcloudapi.com)")
	rootCmd.PersistentFlags().StringVar(&secretID, "secret-id", "", "Tencent Cloud SecretID")
	rootCmd.PersistentFlags().StringVar(&secretKey, "secret-key", "", "Tencent Cloud SecretKey")
	rootCmd.PersistentFlags().StringVar(&tokenFlag, "token", "", "Tencent Cloud STS session token")
	rootCmd.PersistentFlags().StringVar(&jqExpr, "jq", "", "jq expression (only with -o json)")
	rootCmd.PersistentFlags().BoolVar(&nonInteractive, "non-interactive", false, "Disable interactive behaviors")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable ANSI color output")
	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "Write debug diagnostics to stderr")
	rootCmd.PersistentFlags().BoolVar(&generateSkeleton, "generate-skeleton", false, "print an empty JSON request skeleton for request-based commands")
}

func newHelpCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "help [command]",
		Short:   "Help about any command",
		Long:    "Help provides help for any command in the application.",
		Example: exampleBlocks("agr help instance create", "agr help schema"),
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Root().Help()
			}
			target, remaining, err := cmd.Root().Find(args)
			if err != nil || len(remaining) > 0 || target == nil {
				topic := strings.Join(args, " ")
				return output.NewUsageError("INVALID_USAGE",
					fmt.Sprintf("Unknown help topic [%s]", topic),
					fmt.Sprintf("Run '%s --help' to see available commands.", cmd.Root().CommandPath()))
			}
			return target.Help()
		},
	}
}

// Execute initializes IO/configuration, handles early help JSON paths, and runs
// the root Cobra command. It is the process entry point used by cmd/agr.
func Execute() {
	initIOStreams()
	applyRawGlobalArgs(os.Args[1:])
	initConfig()
	rootCmd.SetHelpCommand(newHelpCommand())
	if err := explicitHelpTopicError(os.Args[1:]); err != nil {
		renderExecuteError(rootCmd, err)
	}

	if hasRawFlag("--help") || hasRawFlag("-h") {
		if hasRawOutputFlag("json") {
			// Help JSON has to be resolved from the raw argv stream because Cobra
			// consumes help before the normal command RunE path is reached.
			targetCmd, _, _ := rootCmd.Find(stripHelpAndOutputFlags(os.Args[1:]))
			cmdID := ""
			if targetCmd != nil && targetCmd != rootCmd {
				var parts []string
				for c := targetCmd; c != nil && c != rootCmd; c = c.Parent() {
					parts = append([]string{c.Name()}, parts...)
				}
				cmdID = strings.Join(parts, ".")
			}
			if cmdID != "" {
				for _, s := range getAllSchemas() {
					if s.Name == cmdID && !s.SupportsJson {
						renderExecuteError(targetCmd, output.NewUsageError("INVALID_USAGE", fmt.Sprintf("%s does not support -o json", cmdID), "Use text help for this command, or run 'agr schema -o json' for machine-readable command metadata."))
					}
				}
			}
			var schemaErr error
			if cmdID == "" {
				schemaErr = renderJSONSchemaEnvelope("help", rootCmd, []string{})
			} else {
				schemaErr = renderJSONSchemaEnvelope("help", rootCmd, []string{cmdID})
			}
			if schemaErr != nil {
				renderExecuteError(targetCmd, schemaErr)
			}
			return
		}
	}

	if cmd, err := rootCmd.ExecuteC(); err != nil {
		renderExecuteError(cmd, err)
	}
}

func explicitHelpTopicError(args []string) error {
	if len(args) < 2 || args[0] != "help" {
		return nil
	}
	topics := extractHelpTopics(args[1:])
	if len(topics) == 0 {
		return nil
	}
	if len(topics) == 1 && topics[0] == "help" {
		return nil
	}
	target, remaining, err := rootCmd.Find(topics)
	if err == nil && len(remaining) == 0 && target != nil {
		return nil
	}
	topic := strings.Join(topics, " ")
	return output.NewUsageError(
		"INVALID_USAGE",
		fmt.Sprintf("Unknown help topic [%s]", topic),
		fmt.Sprintf("Run '%s --help' to see available commands.", rootCmd.CommandPath()),
	)
}

func extractHelpTopics(args []string) []string {
	var topics []string
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--" {
			break
		}
		switch arg {
		case "-o", "--output", "--config", "--secret-id", "--secret-key", "--token", "--region", "--domain", "--cloud-endpoint", "--jq":
			skipNext = true
			continue
		case "--no-color", "--non-interactive", "-h", "--help":
			continue
		}
		if strings.HasPrefix(arg, "-o") || strings.HasPrefix(arg, "--output=") ||
			strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "--secret-id=") ||
			strings.HasPrefix(arg, "--secret-key=") || strings.HasPrefix(arg, "--token=") || strings.HasPrefix(arg, "--region=") ||
			strings.HasPrefix(arg, "--domain=") || strings.HasPrefix(arg, "--cloud-endpoint=") || strings.HasPrefix(arg, "--jq=") {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		topics = append(topics, arg)
	}
	return topics
}

func classifyCLIError(err error) *output.CLIError {
	cliErr := output.ClassifyError(err)
	if cliErr.ExitCode == output.ExitGenericError && isCobraUsageError(err) {
		cliErr = output.NewUsageError("INVALID_USAGE", err.Error(), "Run 'agr --help' or 'agr schema -o json' to inspect valid commands and flags.")
	}
	return cliErr
}

// commandSpecificUsageHint returns a tailored hint for unknown-flag style usage
// errors on commands where the generic --help pointer is too vague to be
// actionable. Returns "" when no command-specific hint applies.
func commandSpecificUsageHint(cmd *cobra.Command, err error) string {
	if cmd == nil || err == nil {
		return ""
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown flag") && !strings.Contains(msg, "unknown shorthand") {
		return ""
	}
	switch canonicalCommandID(cmd) {
	case "instance.file.upload":
		return "upload uses positional paths; use: agr instance file upload <instance-id> <local-path|-> <remote-path>"
	case "instance.file.download":
		return "download uses positional paths; use: agr instance file download <instance-id> <remote-path> <local-path|->"
	}
	return ""
}

func renderExecuteError(cmd *cobra.Command, err error) {
	var envDone *envelopeAlreadyWritten
	if errors.As(err, &envDone) {
		os.Exit(envDone.code)
	}
	cliErr := classifyCLIError(err)
	if cliErr != nil && cliErr.Failure != nil && cliErr.Failure.Code == "INVALID_USAGE" {
		if hint := commandSpecificUsageHint(cmd, err); hint != "" {
			cliErr.Failure.Hint = hint
		}
	}
	debugf("Debug: error=%T: %v\n", err, err)
	if isJSON() || hasRawOutputFlag("json") {
		cmdID := commandIDForJSONError(cmd, os.Args[1:])
		env := output.NewFailedEnvelope(cmdID, withIdempotencyHint(cmdID, cliErr.Failure), config.GetBackend(), 0)
		errorJQ := jqExpr
		if cliErr.Failure.Code == "INVALID_JQ_EXPRESSION" {
			errorJQ = ""
		}
		if jqErr := output.RenderEnvelope(os.Stdout, env, errorJQ); jqErr != nil {
			jqFailure := output.NewUsageError("INVALID_JQ_EXPRESSION", jqErr.Error(), "Check your --jq expression syntax.")
			jqEnv := output.NewFailedEnvelope(cmdID, jqFailure.Failure, config.GetBackend(), 0)
			_ = output.RenderEnvelopeToStdout(jqEnv)
			os.Exit(output.ExitUsage)
		}
		os.Exit(cliErr.ExitCode)
	}
	failure := withIdempotencyHint(commandIDForJSONError(cmd, os.Args[1:]), cliErr.Failure)
	writeFailureText(ios.ErrOut, failure)
	os.Exit(cliErr.ExitCode)
}

// writeFailureText renders a Failure to a human-readable, line-per-field
// stderr block. Code/Message/RequestId are surfaced uniformly so all three
// service-side identifiers needed for a TencentCloud support handoff appear
// on their own labeled line. Hint and Retryable follow when present.
func writeFailureText(w io.Writer, failure *output.Failure) {
	if failure == nil {
		return
	}
	fmt.Fprintf(w, "Error: %s\n", failure.Message)
	if failure.Code != "" {
		fmt.Fprintf(w, "Code: %s\n", failure.Code)
	}
	if requestID := failureRequestID(failure); requestID != "" {
		fmt.Fprintf(w, "RequestId: %s\n", requestID)
	}
	if failure.Hint != "" {
		fmt.Fprintf(w, "Hint: %s\n", failure.Hint)
	}
	if failure.Retryable {
		fmt.Fprintln(w, "Retryable: yes")
	}
}

func failureRequestID(failure *output.Failure) string {
	if failure == nil || failure.Details == nil {
		return ""
	}
	requestID, ok := failure.Details["RequestId"].(string)
	if !ok {
		return ""
	}
	return requestID
}

func renderJSONSchemaEnvelope(commandID string, cmd *cobra.Command, args []string) error {
	start := time.Now()
	result, err := schemaFn(cmd, args)
	dm := time.Since(start).Milliseconds()

	if err != nil {
		cliErr := classifyCLIError(err)
		if jqErr := writeEnvelope(ios.Out, commandID, "failed", nil, cliErr.Failure, nil, nil, dm, nil); jqErr != nil {
			return output.NewUsageError("INVALID_JQ_EXPRESSION", jqErr.Error(), "Check your --jq expression syntax.")
		}
		return &envelopeAlreadyWritten{code: cliErr.ExitCode}
	}

	if result == nil {
		return nil
	}

	// Schema/help JSON shares the standard envelope contract so callers see the
	// same succeeded/failed/partial shape as regular command execution.
	status := "succeeded"
	if result.ExitCode == output.ExitPartialSuccess || (result.Failure != nil && result.Failure.Kind == output.KindPartialSuccess) {
		status = "partial"
	} else if result.Failure != nil {
		status = "failed"
	} else if result.ExitCode != 0 {
		status = "failed"
	}

	if jqErr := writeEnvelope(ios.Out, commandID, status, result.Data, result.Failure, result.Warnings, result.Effects, dm, result.MetaExtra); jqErr != nil {
		return output.NewUsageError("INVALID_JQ_EXPRESSION", jqErr.Error(), "Check your --jq expression syntax.")
	}
	if result.ExitCode != 0 {
		return &envelopeAlreadyWritten{code: result.ExitCode}
	}
	return nil
}

func effectiveOutput() string {
	if outputFmt != "" {
		return outputFmt
	}
	return config.GetOutput()
}

func canonicalCommandID(cmd *cobra.Command) string {
	var parts []string
	for c := cmd; c != nil && c.Parent() != nil; c = c.Parent() {
		parts = append([]string{c.Name()}, parts...)
	}
	return strings.Join(parts, ".")
}

func commandIDForJSONError(cmd *cobra.Command, args []string) string {
	if cmdID := canonicalCommandID(cmd); cmdID != "" {
		return cmdID
	}
	return inferRequestedCommandID(args)
}

func inferRequestedCommandID(args []string) string {
	commands := extractCommandTokens(args)
	if len(commands) == 0 {
		return "agr"
	}
	if commands[0] == "help" {
		if len(commands) == 1 {
			return "help"
		}
		return "help." + strings.Join(commands[1:], ".")
	}
	return strings.Join(commands, ".")
}

func extractCommandTokens(args []string) []string {
	var tokens []string
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--" {
			break
		}
		switch arg {
		case "-o", "--output", "--config", "--secret-id", "--secret-key", "--token", "--region", "--domain", "--cloud-endpoint", "--jq":
			skipNext = true
			continue
		case "--no-color", "--non-interactive", "-h", "--help", "--version", "-v":
			continue
		}
		if strings.HasPrefix(arg, "-o") || strings.HasPrefix(arg, "--output=") ||
			strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "--secret-id=") ||
			strings.HasPrefix(arg, "--secret-key=") || strings.HasPrefix(arg, "--token=") || strings.HasPrefix(arg, "--region=") ||
			strings.HasPrefix(arg, "--domain=") || strings.HasPrefix(arg, "--cloud-endpoint=") || strings.HasPrefix(arg, "--jq=") {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		tokens = append(tokens, arg)
	}
	return tokens
}

func stripHelpAndOutputFlags(args []string) []string {
	var result []string
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if a == "--help" || a == "-h" {
			continue
		}
		if a == "-o" || a == "--output" {
			skip = true
			continue
		}
		if strings.HasPrefix(a, "-o") || strings.HasPrefix(a, "--output=") {
			continue
		}
		if a == "--jq" {
			skip = true
			continue
		}
		if strings.HasPrefix(a, "--jq=") {
			continue
		}
		result = append(result, a)
	}
	return result
}

func hasRawOutputFlag(value string) bool {
	for i, arg := range os.Args {
		if (arg == "-o" || arg == "--output") && i+1 < len(os.Args) && os.Args[i+1] == value {
			return true
		}
		if arg == "-o"+value || arg == "--output="+value {
			return true
		}
	}
	return false
}

func hasRawFlag(flag string) bool {
	for _, arg := range os.Args {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}
	return false
}

func shouldAllowDiagnosticOutputOverride(cmd *cobra.Command) bool {
	switch canonicalCommandID(cmd) {
	case "status", "doctor", "schema":
		return true
	default:
		return false
	}
}

func applyRawGlobalArgs(args []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		switch {
		// This pre-parse keeps config/output/debug behavior available before
		// Cobra builds the full command path, which matters for help and errors.
		case arg == "--config" && i+1 < len(args):
			cfgFile = args[i+1]
			i++
		case strings.HasPrefix(arg, "--config="):
			cfgFile = strings.TrimPrefix(arg, "--config=")
		case (arg == "-o" || arg == "--output") && i+1 < len(args):
			outputFmt = args[i+1]
			i++
		case strings.HasPrefix(arg, "--output="):
			outputFmt = strings.TrimPrefix(arg, "--output=")
		case strings.HasPrefix(arg, "-o") && len(arg) > 2:
			outputFmt = strings.TrimPrefix(arg, "-o")
		case arg == "--jq" && i+1 < len(args):
			jqExpr = args[i+1]
			i++
		case strings.HasPrefix(arg, "--jq="):
			jqExpr = strings.TrimPrefix(arg, "--jq=")
		case arg == "--secret-id" && i+1 < len(args):
			secretID = args[i+1]
			i++
		case strings.HasPrefix(arg, "--secret-id="):
			secretID = strings.TrimPrefix(arg, "--secret-id=")
		case arg == "--secret-key" && i+1 < len(args):
			secretKey = args[i+1]
			i++
		case strings.HasPrefix(arg, "--secret-key="):
			secretKey = strings.TrimPrefix(arg, "--secret-key=")
		case arg == "--token" && i+1 < len(args):
			tokenFlag = args[i+1]
			i++
		case strings.HasPrefix(arg, "--token="):
			tokenFlag = strings.TrimPrefix(arg, "--token=")
		case arg == "--region" && i+1 < len(args):
			region = args[i+1]
			i++
		case strings.HasPrefix(arg, "--region="):
			region = strings.TrimPrefix(arg, "--region=")
		case arg == "--domain" && i+1 < len(args):
			domain = args[i+1]
			i++
		case strings.HasPrefix(arg, "--domain="):
			domain = strings.TrimPrefix(arg, "--domain=")
		case arg == "--cloud-endpoint" && i+1 < len(args):
			cloudEndpoint = args[i+1]
			i++
		case strings.HasPrefix(arg, "--cloud-endpoint="):
			cloudEndpoint = strings.TrimPrefix(arg, "--cloud-endpoint=")
		case arg == "--debug":
			debugFlag = true
		case arg == "--no-color":
			noColor = true
		case arg == "--non-interactive":
			nonInteractive = true
		case arg == "--version" || arg == "-v":
			showVersion = true
		}
	}
}

func invalidNDJSONOutputError(cmd *cobra.Command) *output.CLIError {
	if outputFmt == "ndjson" {
		if !commandSupportsJSON(canonicalCommandID(cmd)) {
			return output.NewUsageError(
				"UNSUPPORTED_OUTPUT",
				"this command does not support -o json or -o ndjson",
				"Use text output for this command, or run 'agr schema -o json' for machine-readable command metadata.",
			)
		}
		return output.NewUsageError(
			"NDJSON_REQUIRES_STREAM",
			"-o ndjson is only supported with 'instance code run --stream' and 'instance exec --stream'",
			"Use -o json for a single envelope, or add --stream on a supported streaming command.",
		)
	}
	return output.NewUsageError(
		"INVALID_CONFIG",
		"-o ndjson is only supported with 'instance code run --stream' and 'instance exec --stream'",
		"Set output to 'text' or 'json', or override with -o text/-o json for this command.",
	)
}

func commandSupportsJSON(commandID string) bool {
	if commandID == "" {
		return true
	}
	for _, schema := range getAllSchemas() {
		if schema.Name == commandID {
			return schema.SupportsJson
		}
	}
	return true
}

func isCobraUsageError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "accepts ") ||
		strings.Contains(msg, "required") ||
		strings.Contains(msg, "requires ") ||
		strings.Contains(msg, "unknown command") ||
		strings.Contains(msg, "unknown flag") ||
		strings.Contains(msg, "invalid argument") ||
		strings.Contains(msg, "unknown shorthand")
}

func isNDJSONAllowedCommand(cmd *cobra.Command) bool {
	switch canonicalCommandID(cmd) {
	case "instance.code.run", "instance.exec":
		return true
	default:
		return false
	}
}

func shouldSkipConfigPreflight(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "help", "version", "completion", "docs", "status", "schema", "doctor", "init", "explain", "config":
			return true
		}
	}
	return false
}

func initConfig() {
	configInitErr = nil
	configBasicsErr = nil
	configCommandErr = nil
	configuredOutput = ""
	config.SetConfigFile(cfgFile)
	if err := config.Init(); err != nil {
		configInitErr = output.NewUsageError("CONFIG_INIT_FAILED", err.Error(), "Fix the config file path or TOML syntax, then rerun the command.")
	}
	// Merge env vars into interactive/color flags
	if os.Getenv("NO_COLOR") != "" {
		noColor = true
	}
	if os.Getenv("TERM") == "dumb" {
		noColor = true
		nonInteractive = true
	}
	if os.Getenv("AGR_NON_INTERACTIVE") == "1" {
		nonInteractive = true
	}
	if os.Getenv("AGR_DEBUG") == "1" {
		debugFlag = true
	}

	if secretID != "" {
		config.SetSecretID(secretID)
	}
	if secretKey != "" {
		config.SetSecretKey(secretKey)
	}
	if tokenFlag != "" {
		config.SetToken(tokenFlag)
	}
	if region != "" {
		config.SetRegion(region)
	}
	if domain != "" {
		config.SetDomain(domain)
	}
	if rootCmd.PersistentFlags().Changed("cloud-endpoint") {
		config.SetCloudEndpoint(cloudEndpoint)
	}
	configuredOutput = config.GetOutput()
	if outputFmt == "" && configuredOutput == "ndjson" {
		configCommandErr = output.NewUsageError(
			"INVALID_CONFIG",
			"ndjson is only supported for streaming when passed explicitly with -o ndjson",
			"Set output to 'text' or 'json' in config/env, or use explicit -o ndjson with 'instance code run --stream' or 'instance exec --stream'.",
		)
		outputFmt = "text"
	}
	if configInitErr == nil {
		if outputFmt != "" {
			if outputFmt != "text" && outputFmt != "json" && outputFmt != "ndjson" {
				configBasicsErr = newConfigUsageError(fmt.Errorf("invalid output format: %s (must be 'text', 'json', or 'ndjson')", outputFmt))
			} else {
				config.SetOutput(outputFmt)
				if configCommandErr == nil {
					configuredOutput = outputFmt
				}
			}
		}
		if configBasicsErr == nil {
			if err := config.ValidateBasics(); err != nil {
				configBasicsErr = newConfigUsageError(err)
			}
		}
	}
}

// RootCmd returns the configured root cobra command. Maintainer-only
// tools (e.g. cmd/internal/docgen) use this entry point to render
// markdown / man pages without exposing a user-facing `agr docs`
// command (NextPlan §9.5).
func RootCmd() *cobra.Command { return rootCmd }
