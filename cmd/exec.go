package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-go-sdk/sandbox/code"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/tool/command"
	"github.com/spf13/cobra"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

var (
	// exec command flags
	execInstance  string
	execTool      string
	execKeepAlive bool
	execTime      bool
	execStream    bool
	execCwd       string
	execEnv       []string
)

func init() {
	addExecCommand(rootCmd)
}

// addExecCommand adds the exec command to a parent command
func addExecCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:     "exec <command> [args...]",
		Aliases: []string{"x"},
		Short:   "Execute a shell command in sandbox",
		Long: `Execute a shell command in a sandbox instance.

The command runs in a shell environment and supports streaming output.

Examples:
  # Run a simple command
  ags exec "ls -la" --instance <id>

  # Run with streaming output
  ags exec -s "ping -c 5 localhost" --instance <id>

  # Run with environment variables
  ags exec --env FOO=bar --env BAZ=qux "echo \$FOO \$BAZ"

  # Run with working directory
  ags exec --cwd /home/user "pwd"

  # Create temporary instance and run command
  ags exec "uname -a"

  # Keep instance alive after execution
  ags exec --keep-alive "whoami"`,
		Args: cobra.MinimumNArgs(1),
		RunE: execCommand,
	}

	cmd.Flags().StringVarP(&execInstance, "instance", "i", "", "Instance ID to use")
	cmd.Flags().StringVarP(&execTool, "tool-name", "t", "code-interpreter-v1", "Tool for temporary instance")
	cmd.Flags().StringVar(&execTool, "tool", "code-interpreter-v1", "Tool for temporary instance (alias for --tool-name)")
	cmd.Flags().BoolVar(&execKeepAlive, "keep-alive", false, "Keep temporary instance alive")
	cmd.Flags().BoolVar(&execTime, "time", false, "Print elapsed time")
	cmd.Flags().BoolVarP(&execStream, "stream", "s", false, "Stream output in real-time")
	cmd.Flags().StringVar(&execCwd, "cwd", "", "Working directory")
	cmd.Flags().StringArrayVar(&execEnv, "env", nil, "Environment variables (KEY=VALUE format)")

	parent.AddCommand(cmd)

	// Also add 'ps' subcommand to list processes
	psCmd := &cobra.Command{
		Use:   "ps",
		Short: "List running processes in sandbox",
		Long: `List all running processes in a sandbox instance.

Examples:
  ags exec ps --instance <id>`,
		RunE: execPsCommand,
	}
	psCmd.Flags().StringVarP(&execInstance, "instance", "i", "", "Instance ID to use (required)")
	psCmd.Flags().StringVarP(&execTool, "tool-name", "t", "code-interpreter-v1", "Tool for temporary instance")
	psCmd.Flags().StringVar(&execTool, "tool", "code-interpreter-v1", "Tool for temporary instance (alias for --tool-name)")
	psCmd.Flags().BoolVar(&execKeepAlive, "keep-alive", false, "Keep temporary instance alive")
	psCmd.Flags().BoolVar(&execTime, "time", false, "Print elapsed time")

	cmd.AddCommand(psCmd)
}

// getSandboxForExec gets or creates a sandbox for exec operations
// Returns sandbox, cleanup function, create duration (0 if connecting to existing), and error
func getSandboxForExec(ctx context.Context) (*code.Sandbox, func(), time.Duration, error) {
	if execInstance != "" {
		sandbox, err := code.Connect(ctx, execInstance, getConnectOptions()...)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("failed to connect to instance %s: %w", execInstance, err)
		}
		return sandbox, func() {}, 0, nil
	}

	createStart := time.Now()
	sandbox, err := code.Create(ctx, execTool, getCreateOptions()...)
	createDuration := time.Since(createStart)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to create sandbox: %w", err)
	}

	cleanup := func() {}
	if execKeepAlive {
		output.PrintInfo(fmt.Sprintf("Created instance: %s (kept alive)", sandbox.SandboxId))
	} else {
		cleanup = func() {
			_ = sandbox.Kill(ctx)
		}
	}

	return sandbox, cleanup, createDuration, nil
}

func execCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	start := time.Now()

	if err := config.Validate(); err != nil {
		return err
	}

	// Validate parameters
	if execInstance != "" && execTool != "code-interpreter-v1" {
		return fmt.Errorf("cannot specify both --instance and --tool-name/--tool")
	}

	sandbox, cleanup, createDuration, err := getSandboxForExec(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	// Build command string
	cmdStr := strings.Join(args, " ")

	// Parse environment variables
	envs := make(map[string]string)
	for _, env := range execEnv {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid environment variable format: %s (expected KEY=VALUE)", env)
		}
		envs[parts[0]] = parts[1]
	}

	// Build process config
	procConfig := &command.ProcessConfig{
		User: "user",
		Envs: envs,
	}
	if execCwd != "" {
		procConfig.Cwd = &execCwd
	}

	if execStream {
		// Streaming mode
		callbacks := &command.OnOutputConfig{
			OnStdout: func(data []byte) {
				fmt.Print(string(data))
			},
			OnStderr: func(data []byte) {
				fmt.Fprint(os.Stderr, string(data))
			},
		}

		result, err := sandbox.Commands.Run(ctx, cmdStr, procConfig, callbacks)
		if err != nil {
			return fmt.Errorf("failed to execute command: %w", err)
		}

		if execTime {
			fmt.Fprintf(os.Stderr, "Time: %v\n", time.Since(start))
		}

		if result.ExitCode != 0 {
			if result.Error != nil {
				return fmt.Errorf("command failed with exit code %d: %s", result.ExitCode, *result.Error)
			}
			os.Exit(int(result.ExitCode))
		}

		return nil
	}

	// Non-streaming mode
	execStart := time.Now()
	result, err := sandbox.Commands.Run(ctx, cmdStr, procConfig, nil)
	if err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}
	execDuration := time.Since(execStart)
	totalDuration := time.Since(start)

	// Build timing
	var timing *output.Timing
	if execTime {
		if createDuration > 0 {
			timing = output.NewTimingWithPhases(totalDuration, createDuration, execDuration)
		} else {
			timing = output.NewTiming(totalDuration)
		}
	}

	// Build command result
	cmdResult := &output.CommandResult{
		Stdout:   string(result.Stdout),
		Stderr:   string(result.Stderr),
		ExitCode: int(result.ExitCode),
		Timing:   timing,
	}
	if result.Error != nil {
		cmdResult.Error = *result.Error
	}

	f := output.NewFormatter()
	if err := f.PrintCommandResult(cmdResult); err != nil {
		return err
	}

	if execTime && !f.IsJSON() {
		f.PrintTiming(timing)
	}

	if result.ExitCode != 0 {
		os.Exit(int(result.ExitCode))
	}

	return nil
}

func execPsCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	start := time.Now()

	if err := config.Validate(); err != nil {
		return err
	}

	// Validate parameters
	if execInstance != "" && execTool != "code-interpreter-v1" {
		return fmt.Errorf("cannot specify both --instance and --tool-name/--tool")
	}

	sandbox, cleanup, createDuration, err := getSandboxForExec(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	execStart := time.Now()
	processes, err := sandbox.Commands.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list processes: %w", err)
	}
	execDuration := time.Since(execStart)
	totalDuration := time.Since(start)

	// Build timing
	var timing *output.Timing
	if execTime {
		if createDuration > 0 {
			timing = output.NewTimingWithPhases(totalDuration, createDuration, execDuration)
		} else {
			timing = output.NewTiming(totalDuration)
		}
	}

	f := output.NewFormatter()

	if len(processes) == 0 {
		output.PrintInfo("No running processes")
		if execTime && !f.IsJSON() {
			f.PrintTiming(timing)
		}
		return nil
	}

	headers := []string{"PID", "CMD", "ARGS", "CWD"}
	rows := make([][]string, len(processes))
	for i, p := range processes {
		cwd := "-"
		if p.Cwd != nil {
			cwd = *p.Cwd
		}
		argsStr := strings.Join(p.Args, " ")
		if argsStr == "" {
			argsStr = "-"
		}
		rows[i] = []string{
			fmt.Sprintf("%d", p.Pid),
			p.Cmd,
			output.TruncateString(argsStr, 40),
			output.TruncateString(cwd, 30),
		}
	}

	if err := f.PrintTable(headers, rows, nil); err != nil {
		return err
	}

	if execTime && !f.IsJSON() {
		f.PrintTiming(timing)
	}

	return nil
}
