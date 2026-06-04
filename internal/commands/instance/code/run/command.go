package run

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	toolcode "github.com/TencentCloudAgentRuntime/ags-go-sdk/tool/code"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	cmdcore "github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module returns this package's command module.
func Module() cmdcore.Module {
	spec := cmdcore.Spec{
		ID:    "instance.code.run",
		Path:  []string{"instance", "code", "run"},
		Use:   "run [instance-id]",
		Short: "Execute code in an instance (existing or temporary)",
		Long: `Execute code in a sandbox instance.

Provide an instance id, or use --create-temp-instance to spin up a
temporary sandbox for this single execution.

Code can be provided as a direct string, from a file, or from stdin.

Supported languages: python (default), javascript, typescript, r, java, bash`,
		Examples: []string{
			`agr instance code run ins-xxxx -c "print('Hello')"`,
			"agr instance code run ins-xxxx -f script.py",
			`echo "print('Hello')" | agr instance code run ins-xxxx`,
			`agr instance code run --create-temp-instance --tool-name my-tool -c "print('hello')"`,
			"agr instance code run --create-temp-instance --tool-id sdt-xxxx -f script.py --cleanup never",
		},
		Args: []cmdcore.ArgSpec{
			{Name: "instance-id", Description: "Sandbox instance ID."},
		},
		Flags: []cmdcore.FlagSpec{
			{Name: "code", Shorthand: "c", Usage: "Code to execute", Type: cmdcore.FlagString},
			{Name: "file", Shorthand: "f", Usage: "File containing code to execute", Type: cmdcore.FlagStringArray},
			{Name: "language", Shorthand: "l", Usage: "Programming language (python, javascript, typescript, r, java, bash)", Type: cmdcore.FlagString, Default: "python"},
			{Name: "stream", Shorthand: "s", Usage: "Stream output in real-time", Type: cmdcore.FlagBool},
			{Name: "create-temp-instance", Usage: "Create a temporary sandbox instance, run, then clean up per --cleanup", Type: cmdcore.FlagBool, Workflow: true},
			{Name: "cleanup", Usage: "Cleanup policy for temporary instance: always|success|never", Type: cmdcore.FlagString, Default: "always", Workflow: true},
			{Name: "tool-name", Shorthand: "t", Usage: "Tool name for temporary instance", Type: cmdcore.FlagString, Workflow: true},
			{Name: "tool-id", Usage: "Tool ID for temporary instance", Type: cmdcore.FlagString, Workflow: true},
		},
		SupportsJSON:   true,
		SupportsNDJSON: true,
		Output: cmdcore.OutputSpec{
			DataType:    "CodeExecutionResult",
			Description: "Code execution result.",
		},
	}
	return cmdcore.Module{
		Descriptor: cmdcore.Descriptor{
			Spec: spec,
			Groups: []cmdcore.GroupSpec{
				{
					Path:    []string{"instance"},
					Use:     "instance",
					Short:   "Manage sandbox instances",
					Long:    "Manage sandbox instances and related data-plane workflows.",
					Aliases: []string{"i"},
				},
				{Path: []string{"instance", "code"}, Use: "code", Short: "Code execution commands"},
			},
			Source: "workflow",
		},
		Build: func(deps cmdcore.Deps) (cmdcore.Runtime, error) {
			deps = deps.WithDefaults()
			return cmdcore.Runtime{
				Handler: cmdcore.HandlerFunc(func(ctx context.Context, req cmdcore.Request) (*cmdcore.Result, error) {
					return runCode(ctx, req, deps)
				}),
			}, nil
		},
	}
}

func runCode(ctx context.Context, req cmdcore.Request, deps cmdcore.Deps) (*cmdcore.Result, error) {
	opts := codeOptionsFromRequest(req)
	if err := cli.ValidateNDJSONOnlyForStream(opts.Stream); err != nil {
		return nil, err
	}
	if opts.Stream {
		if err := cli.ValidateStreamNotJSON(); err != nil {
			return nil, err
		}
	}
	if err := validateRunLanguage(opts.Language); err != nil {
		return nil, err
	}
	codeStr, err := resolvePreflightCodeInput(req, deps, opts)
	if err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}

	resolved, err := cli.ResolveOverlay(ctx, opts.Overlay, req.Args, cli.OverlayCloudCreate, cli.OverlayCloudDelete)
	if err != nil {
		return nil, err
	}
	instanceID := resolved.InstanceID

	if opts.Stream && cli.IsNDJSON() {
		return runCodeStreamNDJSON(ctx, deps, opts, resolved, instanceID, codeStr)
	}
	if testDP := cli.TestDataPlane(); testDP != nil && !opts.Stream {
		stdout, stderrText, results, remoteErr, count, err := testDP.RunCode(ctx, instanceID, codeStr, opts.Language)
		if err != nil {
			resolved.CleanupForPreExecutionFailure()
			return nil, err
		}
		data := &output.CodeRunData{Stdout: stdout, Stderr: stderrText, Results: results, Error: remoteErr, ExecutionCount: count, ExecutionContext: resolved.ExecContext}
		if remoteErr != nil {
			resolved.Cleanup(false)
			return &cmdcore.Result{
				Data:     data,
				ExitCode: output.ExitRemoteExecFailed,
				Text: func(w io.Writer) {
					fmt.Fprint(w, stdout)
					fmt.Fprint(deps.IO.ErrOut, stderrText)
				},
			}, nil
		}
		resolved.Cleanup(true)
		return &cmdcore.Result{
			Data: data,
			Text: func(w io.Writer) {
				fmt.Fprint(w, stdout)
				fmt.Fprint(deps.IO.ErrOut, stderrText)
			},
		}, nil
	}

	sandbox, err := cli.ConnectSandboxWithCache(ctx, instanceID)
	if err != nil {
		resolved.CleanupForPreExecutionFailure()
		return nil, fmt.Errorf("failed to connect to instance %s: %w", instanceID, err)
	}

	runConfig := &toolcode.RunCodeConfig{Language: opts.Language}
	if opts.Stream {
		callbacks := &toolcode.OnOutputConfig{
			OnStdout: func(s string) { fmt.Fprint(deps.IO.Out, s) },
			OnStderr: func(s string) { fmt.Fprint(deps.IO.ErrOut, s) },
		}
		result, err := sandbox.Code.RunCode(ctx, codeStr, runConfig, callbacks)
		if err != nil {
			resolved.Cleanup(false)
			return nil, err
		}
		if result.Error != nil {
			fmt.Fprintf(deps.IO.ErrOut, "\n--- error ---\n%s: %s\n", result.Error.Name, result.Error.Value)
			if result.Error.Traceback != "" {
				fmt.Fprintln(deps.IO.ErrOut, result.Error.Traceback)
			}
			resolved.Cleanup(false)
			return &cmdcore.Result{StreamDone: true, ExitCode: output.ExitRemoteExecFailed}, nil
		}
		resolved.Cleanup(true)
		return &cmdcore.Result{StreamDone: true}, nil
	}

	result, err := sandbox.Code.RunCode(ctx, codeStr, runConfig, nil)
	if err != nil {
		resolved.Cleanup(false)
		return nil, fmt.Errorf("failed to execute code: %w", err)
	}

	codeData := &output.CodeRunData{
		Stdout:           strings.Join(result.Logs.Stdout, ""),
		Stderr:           strings.Join(result.Logs.Stderr, ""),
		Results:          convertResults(result.Results),
		ExecutionCount:   1,
		ExecutionContext: resolved.ExecContext,
	}

	textFn := func(w io.Writer) {
		for _, line := range result.Logs.Stdout {
			fmt.Fprint(w, line)
		}
		if len(result.Logs.Stderr) > 0 {
			fmt.Fprintln(deps.IO.ErrOut, "\n--- stderr ---")
			for _, line := range result.Logs.Stderr {
				fmt.Fprint(deps.IO.ErrOut, line)
			}
		}
	}

	if result.Error != nil {
		codeData.Error = map[string]any{
			"Name":      result.Error.Name,
			"Value":     result.Error.Value,
			"Traceback": result.Error.Traceback,
		}
		resolved.Cleanup(false)
		return &cmdcore.Result{
			Data:     codeData,
			ExitCode: output.ExitRemoteExecFailed,
			Text: func(w io.Writer) {
				textFn(w)
				fmt.Fprintf(deps.IO.ErrOut, "\n--- error ---\n%s: %s\n", result.Error.Name, result.Error.Value)
				if result.Error.Traceback != "" {
					fmt.Fprintln(deps.IO.ErrOut, result.Error.Traceback)
				}
			},
		}, nil
	}

	resolved.Cleanup(true)
	return &cmdcore.Result{Data: codeData, Text: textFn}, nil
}

func runCodeStreamNDJSON(ctx context.Context, deps cmdcore.Deps, opts codeOptions, resolved *cli.ResolvedOverlay, instanceID, codeStr string) (*cmdcore.Result, error) {
	nw := output.NewNDJSONWriter(deps.IO.Out, "instance.code.run")
	_ = nw.WriteStarted(map[string]any{"InstanceId": instanceID, "ExecutionContext": resolved.ExecContext})
	sandbox, err := cli.ConnectSandboxWithCache(ctx, instanceID)
	if err != nil {
		cliErr := cli.ClassifyCLIError(err)
		resolved.CleanupForPreExecutionFailure()
		_ = nw.WriteFailed(map[string]any{"ExecutionContext": resolved.ExecContext}, cliErr.Failure)
		return &cmdcore.Result{StreamDone: true, ExitCode: cliErr.ExitCode}, nil
	}
	runConfig := &toolcode.RunCodeConfig{Language: opts.Language}
	callbacks := &toolcode.OnOutputConfig{
		OnStdout: func(s string) { _ = nw.WriteStdout(s) },
		OnStderr: func(s string) { _ = nw.WriteStderr(s) },
	}
	result, err := sandbox.Code.RunCode(ctx, codeStr, runConfig, callbacks)
	if err != nil {
		cliErr := cli.ClassifyCLIError(err)
		resolved.CleanupForPreExecutionFailure()
		_ = nw.WriteFailed(map[string]any{"ExecutionContext": resolved.ExecContext}, cliErr.Failure)
		return &cmdcore.Result{StreamDone: true, ExitCode: cliErr.ExitCode}, nil
	}
	if result.Error != nil {
		resolved.Cleanup(false)
		_ = nw.WriteFailed(
			map[string]any{"Error": map[string]any{"Name": result.Error.Name, "Value": result.Error.Value, "Traceback": result.Error.Traceback}, "ExecutionContext": resolved.ExecContext},
			nil)
		return &cmdcore.Result{StreamDone: true, ExitCode: output.ExitRemoteExecFailed}, nil
	}
	resolved.Cleanup(true)
	_ = nw.WriteCompleted(map[string]any{"ExecutionCount": 1, "ExecutionContext": resolved.ExecContext})
	return &cmdcore.Result{StreamDone: true}, nil
}

func resolvePreflightCodeInput(req cmdcore.Request, deps cmdcore.Deps, opts codeOptions) (string, error) {
	stdinProvided := stdinHasData(req.Stdin, deps)
	sourceCount := 0
	if opts.Code != "" {
		sourceCount++
	}
	if len(opts.Files) > 0 {
		sourceCount++
	}
	if stdinProvided {
		sourceCount++
	}
	if sourceCount > 1 {
		return "", output.NewUsageError("CONFLICTING_INPUTS", "provide exactly one code source: -c, -f, or stdin", "Use only one of -c/--code, -f/--file, or piped stdin.")
	}
	stdin := io.Reader(nil)
	if stdinProvided {
		stdin = req.Stdin
	}
	codeStr, err := resolveCodeInput(stdin, opts.Code, opts.Files)
	if err != nil {
		return "", err
	}
	if codeStr == "" {
		return "", output.NewUsageError("MISSING_CODE", "no code provided: use -c, -f, or pipe via stdin", "Provide code with -c/--code, -f/--file, or stdin.")
	}
	return codeStr, nil
}

type codeOptions struct {
	Code     string
	Files    []string
	Language string
	Stream   bool
	Overlay  cli.OverlayFlags
}

func codeOptionsFromRequest(req cmdcore.Request) codeOptions {
	return codeOptions{
		Code:     stringFlag(req, "code"),
		Files:    stringsFlag(req, "file"),
		Language: stringFlag(req, "language"),
		Stream:   boolFlag(req, "stream"),
		Overlay: cli.OverlayFlags{
			CreateTempInstance: boolFlag(req, "create-temp-instance"),
			Cleanup:            stringFlag(req, "cleanup"),
			ToolName:           stringFlag(req, "tool-name"),
			ToolID:             stringFlag(req, "tool-id"),
		},
	}
}

func validateRunLanguage(language string) error {
	switch language {
	case "python", "javascript", "typescript", "r", "java", "bash":
		return nil
	default:
		return output.NewUsageError("UNSUPPORTED_LANGUAGE",
			fmt.Sprintf("unsupported language: %s (must be one of: python, javascript, typescript, r, java, bash)", language),
			"Use one of: python, javascript, typescript, r, java, bash.")
	}
}

func stdinHasData(stdin io.Reader, deps cmdcore.Deps) bool {
	if stdin == nil {
		return false
	}
	if sized, ok := stdin.(interface{ Len() int }); ok {
		return sized.Len() > 0
	}
	if file, ok := stdin.(*os.File); ok {
		stat, err := file.Stat()
		if err != nil {
			return false
		}
		return (stat.Mode() & os.ModeCharDevice) == 0
	}
	if deps.IO != nil {
		return !deps.IO.IsStdinTTY()
	}
	return true
}

func resolveCodeInput(stdin io.Reader, codeFlag string, files []string) (string, error) {
	if codeFlag != "" {
		return codeFlag, nil
	}
	if len(files) > 0 {
		if len(files) > 1 {
			return "", fmt.Errorf("only one file is supported; got %d", len(files))
		}
		data, err := os.ReadFile(files[0])
		if err != nil {
			return "", fmt.Errorf("failed to read file %s: %w", files[0], err)
		}
		return string(data), nil
	}
	if stdin != nil {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("failed to read from stdin: %w", err)
		}
		return string(data), nil
	}
	return "", nil
}

func convertResults(sdkResults []toolcode.Result) []map[string]any {
	results := make([]map[string]any, 0, len(sdkResults))
	for _, r := range sdkResults {
		m := make(map[string]any)
		if r.Text != nil {
			m["Text"] = *r.Text
		}
		if r.Html != nil {
			m["Html"] = *r.Html
		}
		if r.Markdown != nil {
			m["Markdown"] = *r.Markdown
		}
		if r.Svg != nil {
			m["Svg"] = *r.Svg
		}
		if r.Png != nil {
			m["Png"] = *r.Png
		}
		if r.Jpeg != nil {
			m["Jpeg"] = *r.Jpeg
		}
		if r.Pdf != nil {
			m["Pdf"] = *r.Pdf
		}
		if r.Latex != nil {
			m["Latex"] = *r.Latex
		}
		if r.Json != nil {
			m["Json"] = r.Json
		}
		if r.Javascript != nil {
			m["Javascript"] = *r.Javascript
		}
		if r.Data != nil {
			m["Data"] = r.Data
		}
		if r.Chart != nil {
			m["Chart"] = r.Chart
		}
		m["IsMainResult"] = r.IsMainResult
		if r.Extra != nil {
			m["Extra"] = r.Extra
		}
		if len(m) > 1 {
			results = append(results, m)
		}
	}
	return results
}

func stringFlag(req cmdcore.Request, name string) string {
	flag, ok := req.Flags[name]
	if !ok {
		return ""
	}
	return flag.String
}

func stringsFlag(req cmdcore.Request, name string) []string {
	flag, ok := req.Flags[name]
	if !ok {
		return nil
	}
	return flag.Strings
}

func boolFlag(req cmdcore.Request, name string) bool {
	flag, ok := req.Flags[name]
	return ok && flag.Bool
}
