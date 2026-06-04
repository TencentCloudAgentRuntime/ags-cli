package exec

import (
	"context"
	"fmt"
	"io"
	"strings"

	sdkcommand "github.com/TencentCloudAgentRuntime/ags-go-sdk/tool/command"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	cmdcore "github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module returns this package's command module.
func Module() cmdcore.Module {
	spec := cmdcore.Spec{
		ID:    "instance.exec",
		Path:  []string{"instance", "exec"},
		Use:   "exec [instance-id] -- <command> [args...]",
		Short: "Execute a shell command in an instance (existing or temporary)",
		Long: `Execute a shell command in a sandbox instance.

Provide an instance id, or use --create-temp-instance to spin up a
temporary sandbox for this single execution.

Use '--' to separate flags from the remote command.

Examples:
  agr instance exec ins-xxxx -- ls -la
  agr instance exec ins-xxxx -s -- ping -c 5 localhost
  agr instance exec ins-xxxx --env FOO=bar -- echo $FOO
  # Create the tool first, then reuse its name or id here.
  agr instance exec --create-temp-instance --tool-name my-tool -- python -V
  agr instance exec --create-temp-instance --tool-id sdt-xxxx --cleanup never -- bash`,
		Args: []cmdcore.ArgSpec{
			{Name: "args", Repeatable: true, Description: "Optional instance id followed by remote command arguments."},
		},
		Flags: []cmdcore.FlagSpec{
			{Name: "stream", Shorthand: "s", Usage: "Stream output in real-time", Type: cmdcore.FlagBool},
			{Name: "cwd", Usage: "Working directory", Type: cmdcore.FlagString},
			{Name: "env", Usage: "Environment variables (KEY=VALUE format)", Type: cmdcore.FlagStringArray},
			{Name: "user", Usage: "User to run commands as (default: \"user\")", Type: cmdcore.FlagString},
			{Name: "create-temp-instance", Usage: "Create a temporary sandbox instance, run, then clean up per --cleanup", Type: cmdcore.FlagBool, Workflow: true},
			{Name: "cleanup", Usage: "Cleanup policy for temporary instance: always|success|never", Type: cmdcore.FlagString, Default: "always", Workflow: true},
			{Name: "tool-name", Shorthand: "t", Usage: "Tool name for temporary instance", Type: cmdcore.FlagString, Workflow: true},
			{Name: "tool-id", Usage: "Tool ID for temporary instance", Type: cmdcore.FlagString, Workflow: true},
		},
		SupportsJSON:   true,
		SupportsNDJSON: true,
		Output: cmdcore.OutputSpec{
			DataType:    "CommandExecutionResult",
			Description: "Shell command execution result.",
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
			},
			Source: "workflow",
		},
		Build: func(deps cmdcore.Deps) (cmdcore.Runtime, error) {
			deps = deps.WithDefaults()
			return cmdcore.Runtime{
				Handler: cmdcore.HandlerFunc(func(ctx context.Context, req cmdcore.Request) (*cmdcore.Result, error) {
					return runExec(ctx, req, deps)
				}),
			}, nil
		},
	}
}

func runExec(ctx context.Context, req cmdcore.Request, deps cmdcore.Deps) (*cmdcore.Result, error) {
	opts := execOptionsFromRequest(req)

	if err := cli.ValidateNDJSONOnlyForStream(opts.Stream); err != nil {
		return nil, err
	}
	if opts.Stream {
		if err := cli.ValidateStreamNotJSON(); err != nil {
			return nil, err
		}
	}

	instanceArgs, remoteArgs := splitExecArgs(req.Args, req.DashPos)
	if len(remoteArgs) == 0 {
		return nil, output.NewUsageError(
			"MISSING_COMMAND_SEPARATOR",
			"usage: agr instance exec [<instance-id>|--create-temp-instance ...] -- <command> [args...]",
			"Use '--' to separate the remote command from flags.",
		)
	}
	cmdStr := shellJoin(remoteArgs)
	envs, err := parseExecEnv(opts.Env)
	if err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	resolved, err := cli.ResolveOverlay(ctx, opts.Overlay, instanceArgs, cli.OverlayCloudCreate, cli.OverlayCloudDelete)
	if err != nil {
		return nil, err
	}
	instanceID := resolved.InstanceID

	if opts.Stream && cli.IsNDJSON() {
		return runExecStreamNDJSON(ctx, deps, opts, resolved, instanceID, cmdStr, envs)
	}

	if testDP := cli.TestDataPlane(); testDP != nil && !opts.Stream {
		stdout, stderrText, exitCode, remoteErr, err := testDP.Exec(ctx, instanceID, remoteArgs)
		if err != nil {
			resolved.CleanupForPreExecutionFailure()
			return nil, err
		}
		data := &output.ExecData{Stdout: stdout, Stderr: stderrText, ExitCode: exitCode, Error: remoteErr, ExecutionContext: resolved.ExecContext}
		resolved.Cleanup(exitCode == 0)
		return &cmdcore.Result{
			Data:     data,
			ExitCode: exitCode,
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

	procConfig := &sdkcommand.ProcessConfig{
		User: cli.ResolveUser(opts.User),
		Envs: envs,
	}
	if opts.Cwd != "" {
		procConfig.Cwd = &opts.Cwd
	}

	if opts.Stream {
		callbacks := &sdkcommand.OnOutputConfig{
			OnStdout: func(data []byte) { fmt.Fprint(deps.IO.Out, string(data)) },
			OnStderr: func(data []byte) { fmt.Fprint(deps.IO.ErrOut, string(data)) },
		}
		result, err := sandbox.Commands.Run(ctx, cmdStr, procConfig, callbacks)
		if err != nil {
			resolved.Cleanup(false)
			return nil, fmt.Errorf("failed to execute command: %w", err)
		}
		resolved.Cleanup(result.ExitCode == 0)
		return &cmdcore.Result{StreamDone: true, ExitCode: int(result.ExitCode)}, nil
	}

	result, err := sandbox.Commands.Run(ctx, cmdStr, procConfig, nil)
	if err != nil {
		resolved.Cleanup(false)
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	data := &output.ExecData{
		Stdout:           string(result.Stdout),
		Stderr:           string(result.Stderr),
		ExitCode:         int(result.ExitCode),
		ExecutionContext: resolved.ExecContext,
	}
	if result.Error != nil {
		data.Error = map[string]any{"Message": *result.Error}
	}

	if result.ExitCode != 0 {
		resolved.Cleanup(false)
		return &cmdcore.Result{
			Data:     data,
			ExitCode: int(result.ExitCode),
			Text: func(w io.Writer) {
				if len(result.Stdout) > 0 {
					fmt.Fprint(w, string(result.Stdout))
				}
				if len(result.Stderr) > 0 {
					fmt.Fprintln(deps.IO.ErrOut, "--- stderr ---")
					fmt.Fprint(deps.IO.ErrOut, string(result.Stderr))
				}
				if result.Error != nil {
					fmt.Fprintf(deps.IO.ErrOut, "--- error ---\n%s\n", *result.Error)
				}
			},
		}, nil
	}

	resolved.Cleanup(true)
	return &cmdcore.Result{
		Data: data,
		Text: func(w io.Writer) {
			if len(result.Stdout) > 0 {
				fmt.Fprint(w, string(result.Stdout))
			}
			if len(result.Stderr) > 0 {
				fmt.Fprintln(deps.IO.ErrOut, "--- stderr ---")
				fmt.Fprint(deps.IO.ErrOut, string(result.Stderr))
			}
		},
	}, nil
}

func runExecStreamNDJSON(ctx context.Context, deps cmdcore.Deps, opts execOptions, resolved *cli.ResolvedOverlay, instanceID, cmdStr string, envs map[string]string) (*cmdcore.Result, error) {
	nw := output.NewNDJSONWriter(deps.IO.Out, "instance.exec")
	_ = nw.WriteStarted(map[string]any{"InstanceId": instanceID, "ExecutionContext": resolved.ExecContext})
	sandbox, err := cli.ConnectSandboxWithCache(ctx, instanceID)
	if err != nil {
		cliErr := cli.ClassifyCLIError(err)
		resolved.CleanupForPreExecutionFailure()
		_ = nw.WriteFailed(map[string]any{"ExecutionContext": resolved.ExecContext}, cliErr.Failure)
		return &cmdcore.Result{StreamDone: true, ExitCode: cliErr.ExitCode}, nil
	}
	procConfig := &sdkcommand.ProcessConfig{User: cli.ResolveUser(opts.User), Envs: envs}
	if opts.Cwd != "" {
		procConfig.Cwd = &opts.Cwd
	}
	callbacks := &sdkcommand.OnOutputConfig{
		OnStdout: func(data []byte) { _ = nw.WriteStdout(string(data)) },
		OnStderr: func(data []byte) { _ = nw.WriteStderr(string(data)) },
	}
	result, err := sandbox.Commands.Run(ctx, cmdStr, procConfig, callbacks)
	if err != nil {
		cliErr := cli.ClassifyCLIError(err)
		resolved.Cleanup(false)
		_ = nw.WriteFailed(map[string]any{"ExecutionContext": resolved.ExecContext}, cliErr.Failure)
		return &cmdcore.Result{StreamDone: true, ExitCode: cliErr.ExitCode}, nil
	}
	if result.ExitCode != 0 {
		resolved.Cleanup(false)
		_ = nw.WriteFailed(
			map[string]any{"ExitCode": int(result.ExitCode), "ExecutionContext": resolved.ExecContext},
			cli.RemoteCommandFailure(),
		)
		return &cmdcore.Result{StreamDone: true, ExitCode: int(result.ExitCode)}, nil
	}
	resolved.Cleanup(true)
	_ = nw.WriteCompleted(map[string]any{"ExitCode": 0, "ExecutionContext": resolved.ExecContext})
	return &cmdcore.Result{StreamDone: true}, nil
}

type execOptions struct {
	Stream  bool
	Cwd     string
	Env     []string
	User    string
	Overlay cli.OverlayFlags
}

func execOptionsFromRequest(req cmdcore.Request) execOptions {
	return execOptions{
		Stream: boolFlag(req, "stream"),
		Cwd:    stringFlag(req, "cwd"),
		Env:    stringsFlag(req, "env"),
		User:   stringFlag(req, "user"),
		Overlay: cli.OverlayFlags{
			CreateTempInstance: boolFlag(req, "create-temp-instance"),
			Cleanup:            stringFlag(req, "cleanup"),
			ToolName:           stringFlag(req, "tool-name"),
			ToolID:             stringFlag(req, "tool-id"),
		},
	}
}

func splitExecArgs(args []string, dashPos int) ([]string, []string) {
	if dashPos < 0 {
		return args, nil
	}
	if dashPos > len(args) {
		dashPos = len(args)
	}
	return args[:dashPos], args[dashPos:]
}

func parseExecEnv(values []string) (map[string]string, error) {
	envs := make(map[string]string)
	for _, env := range values {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, output.NewUsageError("INVALID_ENV", fmt.Sprintf("invalid environment variable format: %s (expected KEY=VALUE, key must be non-empty)", env), "Use --env KEY=VALUE.")
		}
		envs[strings.TrimSpace(parts[0])] = parts[1]
	}
	return envs, nil
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if arg == "" {
			quoted[i] = "''"
			continue
		}
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", `'"'"'`) + "'"
	}
	return strings.Join(quoted, " ")
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
