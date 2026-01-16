package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-go-sdk/sandbox/code"
	toolcode "github.com/TencentCloudAgentRuntime/ags-go-sdk/tool/code"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"

	"github.com/spf13/cobra"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

var (
	runCode        string
	runFiles       []string
	runInstance    string
	runTool        string
	runLanguage    string
	runKeepAlive   bool
	runStream      bool
	runTime        bool
	runRepeat      int
	runParallel    bool
	runMaxParallel int
)

// executionTask represents a single execution task
type executionTask struct {
	id         int
	code       string
	source     string // filename or "<code>"
	instanceNo int    // instance number when repeat > 1
	totalInst  int    // total instances for this source
}

// taskResult represents the result of a task execution
type taskResult struct {
	task           executionTask
	result         *toolcode.Execution
	err            error
	createDuration time.Duration
	execDuration   time.Duration
	totalDuration  time.Duration
}

// getCredential returns the credential from config
func getCredential() common.CredentialIface {
	cloudCfg := config.GetCloudConfig()
	return common.NewCredential(cloudCfg.SecretID, cloudCfg.SecretKey)
}

// getCreateOptions returns the common create options for sandbox
func getCreateOptions() []code.CreateOption {
	cloudCfg := config.GetCloudConfig()
	opts := []code.CreateOption{
		code.WithCredential(getCredential()),
		code.WithRegion(cloudCfg.Region),
		code.WithSandboxTimeout(300 * time.Second),
	}
	if cloudCfg.Internal {
		opts = append(opts, code.WithDataPlaneDomain(cloudCfg.DataPlaneDomain()))
	}
	return opts
}

// getConnectOptions returns the common connect options for sandbox
func getConnectOptions() []code.ConnectOption {
	cloudCfg := config.GetCloudConfig()
	opts := []code.ConnectOption{
		code.WithCredential(getCredential()),
		code.WithRegion(cloudCfg.Region),
	}
	if cloudCfg.Internal {
		opts = append(opts, code.WithDataPlaneDomain(cloudCfg.DataPlaneDomain()))
	}
	return opts
}

func runCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if err := config.Validate(); err != nil {
		return err
	}

	// Validate parameters
	if runInstance != "" && runTool != "code-interpreter-v1" {
		return fmt.Errorf("cannot specify both --instance and --tool-name/--tool")
	}
	if runCode != "" && len(runFiles) > 0 {
		return fmt.Errorf("cannot use both -c and -f flags")
	}

	if runRepeat > 1 && runInstance != "" {
		return fmt.Errorf("cannot use --repeat with --instance (existing instance doesn't support multiple executions)")
	}

	// Build execution tasks
	tasks, err := buildTasks(runLanguage)
	if err != nil {
		return err
	}

	if len(tasks) == 0 {
		return fmt.Errorf("no code provided")
	}

	// Single task: use original simple logic
	if len(tasks) == 1 && runRepeat <= 1 {
		return runSingleTask(ctx, tasks[0])
	}

	// Multi-task execution
	return runMultiTasks(ctx, tasks)
}

// buildTasks builds execution tasks from input
func buildTasks(language string) ([]executionTask, error) {
	var tasks []executionTask
	taskID := 1

	if runCode != "" {
		// From -c flag
		for i := 0; i < max(runRepeat, 1); i++ {
			tasks = append(tasks, executionTask{
				id:         taskID,
				code:       runCode,
				source:     "<code>",
				instanceNo: i + 1,
				totalInst:  max(runRepeat, 1),
			})
			taskID++
		}
		return tasks, nil
	}

	if len(runFiles) > 0 {
		// From -f flags
		for _, file := range runFiles {
			data, err := os.ReadFile(file)
			if err != nil {
				return nil, fmt.Errorf("failed to read file %s: %w", file, err)
			}
			codeStr := string(data)

			for i := 0; i < max(runRepeat, 1); i++ {
				tasks = append(tasks, executionTask{
					id:         taskID,
					code:       codeStr,
					source:     file,
					instanceNo: i + 1,
					totalInst:  max(runRepeat, 1),
				})
				taskID++
			}
		}
		return tasks, nil
	}

	// Check for pipe input
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		reader := bufio.NewReader(os.Stdin)
		var builder strings.Builder
		for {
			line, err := reader.ReadString('\n')
			builder.WriteString(line)
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("failed to read from stdin: %w", err)
			}
		}
		codeStr := builder.String()
		if codeStr != "" {
			for i := 0; i < max(runRepeat, 1); i++ {
				tasks = append(tasks, executionTask{
					id:         taskID,
					code:       codeStr,
					source:     "<stdin>",
					instanceNo: i + 1,
					totalInst:  max(runRepeat, 1),
				})
				taskID++
			}
		}
		return tasks, nil
	}

	// No input provided, open editor
	codeStr, err := openEditor(language)
	if err != nil {
		return nil, err
	}
	if codeStr != "" {
		for i := 0; i < max(runRepeat, 1); i++ {
			tasks = append(tasks, executionTask{
				id:         taskID,
				code:       codeStr,
				source:     "<editor>",
				instanceNo: i + 1,
				totalInst:  max(runRepeat, 1),
			})
			taskID++
		}
	}
	return tasks, nil
}

// runSingleTask runs a single task using ags-go-sdk
func runSingleTask(ctx context.Context, task executionTask) error {
	start := time.Now()
	var createDuration time.Duration

	var sandbox *code.Sandbox
	var err error

	if runInstance != "" {
		// Connect to existing instance
		sandbox, err = code.Connect(ctx, runInstance, getConnectOptions()...)
		if err != nil {
			return fmt.Errorf("failed to connect to instance %s: %w", runInstance, err)
		}
	} else {
		// Create new sandbox
		createStart := time.Now()
		sandbox, err = code.Create(ctx, runTool, getCreateOptions()...)
		createDuration = time.Since(createStart)
		if err != nil {
			return fmt.Errorf("failed to create sandbox: %w", err)
		}

		if runKeepAlive {
			output.PrintInfo(fmt.Sprintf("Created instance: %s (kept alive)", sandbox.SandboxId))
		} else {
			defer func() {
				_ = sandbox.Kill(ctx)
			}()
		}
	}

	// Execute code
	execStart := time.Now()
	var result *toolcode.Execution

	runConfig := &toolcode.RunCodeConfig{
		Language: runLanguage,
	}

	if runStream {
		callbacks := &toolcode.OnOutputConfig{
			OnStdout: func(s string) {
				fmt.Print(s)
			},
			OnStderr: func(s string) {
				fmt.Fprint(os.Stderr, s)
			},
		}
		result, err = sandbox.Code.RunCode(ctx, task.code, runConfig, callbacks)
	} else {
		result, err = sandbox.Code.RunCode(ctx, task.code, runConfig, nil)
	}

	execDuration := time.Since(execStart)
	totalDuration := time.Since(start)

	if err != nil {
		return fmt.Errorf("failed to execute code: %w", err)
	}

	// Build timing info
	var timing *output.Timing
	if runTime {
		if createDuration > 0 {
			timing = output.NewTimingWithPhases(totalDuration, createDuration, execDuration)
		} else {
			timing = output.NewTiming(totalDuration)
		}
	}

	if runStream {
		if result.Error != nil {
			fmt.Fprintln(os.Stderr, "\n--- error ---")
			fmt.Fprintf(os.Stderr, "%s: %s\n", result.Error.Name, result.Error.Value)
			if result.Error.Traceback != "" {
				fmt.Fprintln(os.Stderr, result.Error.Traceback)
			}
		}
		if runTime {
			fmt.Fprintf(os.Stderr, "Time: %v\n", totalDuration)
		}
		return nil
	}

	// Build execution result
	var execErr *output.ExecError
	if result.Error != nil {
		execErr = &output.ExecError{
			Name:      result.Error.Name,
			Value:     result.Error.Value,
			Traceback: result.Error.Traceback,
		}
	}

	execResult := &output.ExecResult{
		Stdout:  result.Logs.Stdout,
		Stderr:  result.Logs.Stderr,
		Results: convertResults(result.Results),
		Error:   execErr,
		Timing:  timing,
	}

	// Add instance ID if kept alive
	if runKeepAlive {
		execResult.InstanceID = sandbox.SandboxId
	}

	f := output.NewFormatter()
	if err := f.PrintExecResult(execResult); err != nil {
		return err
	}

	if runTime && !f.IsJSON() {
		f.PrintTiming(timing)
	}

	return nil
}

// convertResults converts SDK results to output format
func convertResults(sdkResults []toolcode.Result) []map[string]any {
	results := make([]map[string]any, 0, len(sdkResults))
	for _, r := range sdkResults {
		m := make(map[string]any)
		if r.Text != nil {
			m["text"] = *r.Text
		}
		if r.Html != nil {
			m["html"] = *r.Html
		}
		if r.Markdown != nil {
			m["markdown"] = *r.Markdown
		}
		if r.Svg != nil {
			m["svg"] = *r.Svg
		}
		if r.Png != nil {
			m["png"] = *r.Png
		}
		if r.Jpeg != nil {
			m["jpeg"] = *r.Jpeg
		}
		if r.Pdf != nil {
			m["pdf"] = *r.Pdf
		}
		if r.Latex != nil {
			m["latex"] = *r.Latex
		}
		if r.Json != nil {
			m["json"] = r.Json
		}
		if r.Javascript != nil {
			m["javascript"] = *r.Javascript
		}
		if r.Data != nil {
			m["data"] = r.Data
		}
		if r.Chart != nil {
			m["chart"] = r.Chart
		}
		m["is_main_result"] = r.IsMainResult
		if r.Extra != nil {
			m["extra"] = r.Extra
		}
		if len(m) > 1 { // has more than just is_main_result
			results = append(results, m)
		}
	}
	return results
}

// runMultiTasks runs multiple tasks
func runMultiTasks(ctx context.Context, tasks []executionTask) error {
	start := time.Now()

	var results []taskResult

	if runParallel {
		results = runTasksParallel(ctx, tasks)
	} else {
		results = runTasksSequential(ctx, tasks)
	}

	totalDuration := time.Since(start)

	// Build output
	return printMultiTaskResults(results, totalDuration)
}

// runTasksSequential runs tasks sequentially, reusing a single sandbox
func runTasksSequential(ctx context.Context, tasks []executionTask) []taskResult {
	results := make([]taskResult, len(tasks))

	var sandbox *code.Sandbox
	var err error
	var sandboxCreateDuration time.Duration

	if runInstance != "" {
		sandbox, err = code.Connect(ctx, runInstance, getConnectOptions()...)
		if err != nil {
			for i, task := range tasks {
				results[i] = taskResult{
					task: task,
					err:  fmt.Errorf("failed to connect to instance: %w", err),
				}
			}
			return results
		}
	} else {
		createStart := time.Now()
		sandbox, err = code.Create(ctx, runTool, getCreateOptions()...)
		sandboxCreateDuration = time.Since(createStart)
		if err != nil {
			for i, task := range tasks {
				results[i] = taskResult{
					task: task,
					err:  fmt.Errorf("failed to create sandbox: %w", err),
				}
			}
			return results
		}

		if runKeepAlive {
			output.PrintInfo(fmt.Sprintf("Created instance: %s (kept alive)", sandbox.SandboxId))
		} else {
			defer func() {
				_ = sandbox.Kill(ctx)
			}()
		}
	}

	runConfig := &toolcode.RunCodeConfig{
		Language: runLanguage,
	}

	for i, task := range tasks {
		taskStart := time.Now()

		var result *toolcode.Execution

		if runStream {
			callbacks := &toolcode.OnOutputConfig{
				OnStdout: func(s string) {
					output.PrintStreamPrefix(task.id, task.source, getInstanceNo(task), false, s)
				},
				OnStderr: func(s string) {
					output.PrintStreamPrefix(task.id, task.source, getInstanceNo(task), true, s)
				},
			}
			result, err = sandbox.Code.RunCode(ctx, task.code, runConfig, callbacks)
		} else {
			result, err = sandbox.Code.RunCode(ctx, task.code, runConfig, nil)
		}

		execDuration := time.Since(taskStart)

		r := taskResult{
			task:          task,
			result:        result,
			err:           err,
			execDuration:  execDuration,
			totalDuration: execDuration,
		}

		// First task includes sandbox creation time
		if i == 0 && sandboxCreateDuration > 0 {
			r.createDuration = sandboxCreateDuration
			r.totalDuration = sandboxCreateDuration + execDuration
		}

		results[i] = r
	}

	return results
}

// runTasksParallel runs tasks in parallel
func runTasksParallel(ctx context.Context, tasks []executionTask) []taskResult {
	results := make([]taskResult, len(tasks))
	var wg sync.WaitGroup

	// Semaphore for limiting parallelism
	maxParallel := runMaxParallel
	if maxParallel <= 0 || maxParallel > len(tasks) {
		maxParallel = len(tasks)
	}
	sem := make(chan struct{}, maxParallel)

	// Channel for streaming results as they complete (text mode only)
	isTextMode := config.GetOutput() != "json"
	var resultChan chan taskResult
	var printWg sync.WaitGroup
	if isTextMode && !runStream {
		resultChan = make(chan taskResult, len(tasks))
		// Start a goroutine to print results as they arrive
		printWg.Add(1)
		go func() {
			defer printWg.Done()
			for r := range resultChan {
				printSingleTaskResult(r)
			}
		}()
	}

	// Track sandboxes for cleanup
	var sandboxes []*code.Sandbox
	var sandboxesMu sync.Mutex
	var resultsMu sync.Mutex

	runConfig := &toolcode.RunCodeConfig{
		Language: runLanguage,
	}

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t executionTask) {
			defer wg.Done()

			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			taskStart := time.Now()

			// Each parallel task needs its own sandbox
			createStart := time.Now()
			sandbox, err := code.Create(ctx, runTool, getCreateOptions()...)
			createDuration := time.Since(createStart)

			if err != nil {
				r := taskResult{
					task:          t,
					err:           fmt.Errorf("failed to create sandbox: %w", err),
					totalDuration: time.Since(taskStart),
				}
				resultsMu.Lock()
				results[idx] = r
				resultsMu.Unlock()
				if resultChan != nil {
					resultChan <- r
				}
				return
			}

			sandboxesMu.Lock()
			sandboxes = append(sandboxes, sandbox)
			sandboxesMu.Unlock()

			var result *toolcode.Execution

			execStart := time.Now()
			if runStream {
				callbacks := &toolcode.OnOutputConfig{
					OnStdout: func(s string) {
						output.PrintStreamPrefix(t.id, t.source, getInstanceNo(t), false, s)
					},
					OnStderr: func(s string) {
						output.PrintStreamPrefix(t.id, t.source, getInstanceNo(t), true, s)
					},
				}
				result, err = sandbox.Code.RunCode(ctx, t.code, runConfig, callbacks)
			} else {
				result, err = sandbox.Code.RunCode(ctx, t.code, runConfig, nil)
			}
			execDuration := time.Since(execStart)

			r := taskResult{
				task:           t,
				result:         result,
				err:            err,
				createDuration: createDuration,
				execDuration:   execDuration,
				totalDuration:  time.Since(taskStart),
			}
			resultsMu.Lock()
			results[idx] = r
			resultsMu.Unlock()

			// Send to channel for immediate printing (text mode only)
			if resultChan != nil {
				resultChan <- r
			}
		}(i, task)
	}

	wg.Wait()

	// Close result channel and wait for printing to finish
	if resultChan != nil {
		close(resultChan)
		printWg.Wait()
	}

	// Cleanup sandboxes
	if !runKeepAlive {
		for _, sb := range sandboxes {
			_ = sb.Kill(ctx)
		}
	} else if len(sandboxes) > 0 {
		ids := make([]string, len(sandboxes))
		for i, sb := range sandboxes {
			ids[i] = sb.SandboxId
		}
		output.PrintInfo(fmt.Sprintf("Created %d instances (kept alive): %s", len(sandboxes), strings.Join(ids, ", ")))
	}

	return results
}

// printSingleTaskResult prints a single task result immediately (text mode)
func printSingleTaskResult(r taskResult) {
	t := r.task

	// Print task header
	var status string
	if r.err != nil || (r.result != nil && r.result.Error != nil) {
		status = " [FAILED]"
	}

	var header string
	if t.totalInst > 1 {
		header = fmt.Sprintf("━━━ Task %d: %s (%d/%d)%s ━━━",
			t.id, t.source, t.instanceNo, t.totalInst, status)
	} else {
		header = fmt.Sprintf("━━━ Task %d: %s%s ━━━", t.id, t.source, status)
	}
	fmt.Println(header)

	if r.err != nil {
		fmt.Println("--- error ---")
		fmt.Println(r.err.Error())
		fmt.Println()
		return
	}

	if r.result != nil {
		// Print stdout
		if len(r.result.Logs.Stdout) > 0 {
			for _, line := range r.result.Logs.Stdout {
				fmt.Print(line)
			}
		}

		// Print stderr
		if len(r.result.Logs.Stderr) > 0 {
			fmt.Println("--- stderr ---")
			for _, line := range r.result.Logs.Stderr {
				fmt.Print(line)
			}
		}

		// Print error
		if r.result.Error != nil {
			fmt.Println("--- error ---")
			fmt.Printf("%s: %s\n", r.result.Error.Name, r.result.Error.Value)
			if r.result.Error.Traceback != "" {
				fmt.Println(r.result.Error.Traceback)
			}
		}
	}

	fmt.Println() // Empty line between tasks
}

// getInstanceNo returns instance number for display (0 if only 1 instance)
func getInstanceNo(task executionTask) int {
	if task.totalInst <= 1 {
		return 0
	}
	return task.instanceNo
}

// printMultiTaskResults prints the results of multiple tasks
func printMultiTaskResults(results []taskResult, totalDuration time.Duration) error {
	success := 0
	failed := 0
	for _, r := range results {
		if r.err != nil || (r.result != nil && r.result.Error != nil) {
			failed++
		} else {
			success++
		}
	}

	f := output.NewFormatter()

	// Build timing
	var timing *output.Timing
	if runTime {
		timing = output.NewTiming(totalDuration)
	}

	summary := output.TaskSummary{
		Total:   len(results),
		Success: success,
		Failed:  failed,
		Timing:  timing,
	}

	// In streaming mode, output already printed, just print summary
	if runStream {
		f.PrintSummaryToStderr(summary)
		if failed > 0 {
			if failed == len(results) {
				os.Exit(2)
			}
			os.Exit(1)
		}
		return nil
	}

	// Text mode with parallel execution: results already printed via channel, just print summary
	if !f.IsJSON() && runParallel {
		f.PrintSummary(summary)
		if failed > 0 {
			if failed == len(results) {
				os.Exit(2)
			}
			os.Exit(1)
		}
		return nil
	}

	// Build task results for formatter (JSON mode or sequential text mode)
	taskResults := make([]output.TaskResult, len(results))

	// Reset counters for accurate counting
	success = 0
	failed = 0

	for i, r := range results {
		var taskTiming *output.Timing
		if runTime {
			if r.createDuration > 0 {
				taskTiming = output.NewTimingWithPhases(r.totalDuration, r.createDuration, r.execDuration)
			} else {
				taskTiming = output.NewTiming(r.totalDuration)
			}
		}

		t := output.TaskResult{
			ID:        r.task.id,
			Source:    r.task.source,
			Instance:  r.task.instanceNo,
			TotalInst: r.task.totalInst,
			Timing:    taskTiming,
			Success:   true,
		}

		if r.err != nil {
			t.Success = false
			t.ErrorMsg = r.err.Error()
			failed++
		} else if r.result != nil {
			t.Stdout = r.result.Logs.Stdout
			t.Stderr = r.result.Logs.Stderr
			t.Results = convertResults(r.result.Results)
			if r.result.Error != nil {
				t.Success = false
				t.Error = &output.ExecError{
					Name:      r.result.Error.Name,
					Value:     r.result.Error.Value,
					Traceback: r.result.Error.Traceback,
				}
				failed++
			} else {
				success++
			}
		} else {
			success++
		}

		taskResults[i] = t
	}

	// Update summary with accurate counts
	summary.Success = success
	summary.Failed = failed

	multiResult := &output.MultiTaskResult{
		Tasks:   taskResults,
		Summary: summary,
	}

	if err := f.PrintMultiTaskResult(multiResult); err != nil {
		return err
	}

	// Set exit code based on results
	if failed > 0 {
		if failed == len(results) {
			os.Exit(2)
		}
		os.Exit(1)
	}

	return nil
}

// openEditor opens the default editor for the user to write code
func openEditor(language string) (string, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		if _, err := exec.LookPath("vim"); err == nil {
			editor = "vim"
		} else if _, err := exec.LookPath("vi"); err == nil {
			editor = "vi"
		} else if _, err := exec.LookPath("nano"); err == nil {
			editor = "nano"
		} else {
			return "", fmt.Errorf("no editor found: set $EDITOR environment variable")
		}
	}

	ext := getFileExtension(language)
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("ags-*%s", ext))
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	template := getEditorTemplate(language)
	if _, err := tmpFile.WriteString(template); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to write template: %w", err)
	}
	tmpFile.Close()

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor exited with error: %w", err)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to read edited file: %w", err)
	}

	codeStr := string(data)
	codeStr = strings.TrimSpace(codeStr)
	if codeStr == "" || codeStr == strings.TrimSpace(template) {
		return "", nil
	}

	return codeStr, nil
}

// getFileExtension returns the file extension for a language
func getFileExtension(language string) string {
	switch language {
	case "python":
		return ".py"
	case "javascript":
		return ".js"
	case "typescript":
		return ".ts"
	case "bash":
		return ".sh"
	case "r":
		return ".r"
	case "java":
		return ".java"
	default:
		return ".py"
	}
}

// getEditorTemplate returns a template comment for the editor
func getEditorTemplate(language string) string {
	var comment string
	switch language {
	case "python":
		comment = "# "
	case "javascript", "typescript", "java":
		comment = "// "
	case "bash":
		comment = "# "
	case "r":
		comment = "# "
	default:
		comment = "# "
	}

	return fmt.Sprintf(`%sAGS Code Editor
%sWrite your %s code below, save and exit to execute.
%sLeave empty or unchanged to cancel.

`, comment, comment, language, comment)
}

func init() {
	addRunCommand(rootCmd)
}

// addRunCommand adds the run command to a parent command
func addRunCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:     "run",
		Aliases: []string{"r"},
		Short:   "Execute code in a sandbox",
		Long: `Execute code in a sandbox instance.

Code can be provided in four ways:
  1. Direct string: ags run -c "print('Hello')"
  2. From file: ags run -f script.py
  3. From pipe: echo "print('Hello')" | ags run
  4. Editor: ags run (opens $EDITOR to write code)

Multiple files can be specified with multiple -f flags:
  ags run -f a.py -f b.py -f c.py

Parallel options:
  -n, --repeat       Run the same code N times
  -p, --parallel     Execute tasks in parallel (default: sequential)
  --max-parallel     Limit maximum parallel executions

Supported languages: python (default), javascript, typescript, r, java, bash

By default, a temporary instance is created and destroyed after execution.
Use --instance to specify an existing instance, or --keep-alive to preserve
the temporary instance.`,
		RunE: runCommand,
	}

	cmd.Flags().StringVarP(&runCode, "code", "c", "", "Code to execute")
	cmd.Flags().StringArrayVarP(&runFiles, "file", "f", nil, "File(s) containing code to execute (can be specified multiple times)")
	cmd.Flags().StringVarP(&runInstance, "instance", "i", "", "Existing instance ID to use")
	cmd.Flags().StringVarP(&runTool, "tool-name", "t", "code-interpreter-v1", "Tool to use for temporary instance")
	cmd.Flags().StringVar(&runTool, "tool", "code-interpreter-v1", "Tool to use for temporary instance (alias for --tool-name)")
	cmd.Flags().StringVarP(&runLanguage, "language", "l", "python", "Programming language (python, javascript, typescript, r, java, bash)")
	cmd.Flags().BoolVar(&runKeepAlive, "keep-alive", false, "Keep temporary instance alive after execution")
	cmd.Flags().BoolVarP(&runStream, "stream", "s", false, "Stream output in real-time")
	cmd.Flags().BoolVar(&runTime, "time", false, "Print elapsed time to stderr")
	cmd.Flags().IntVarP(&runRepeat, "repeat", "n", 1, "Run the same code N times")
	cmd.Flags().BoolVarP(&runParallel, "parallel", "p", false, "Execute tasks in parallel (default: sequential)")
	cmd.Flags().IntVar(&runMaxParallel, "max-parallel", 0, "Maximum parallel executions (0 = unlimited)")

	parent.AddCommand(cmd)
}

// Ensure profile package is imported for potential future use
var _ = profile.NewClientProfile
