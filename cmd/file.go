package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-go-sdk/sandbox/code"
	"github.com/spf13/cobra"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

var (
	// file command flags
	fileInstance  string
	fileTool      string
	fileKeepAlive bool
	fileTime      bool

	// file list flags
	fileListDepth int
)

func init() {
	addFileCommand(rootCmd)
}

// addFileCommand adds the file command to a parent command
func addFileCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:     "file",
		Aliases: []string{"f", "fs"},
		Short:   "File operations in sandbox",
		Long: `Manage files in a sandbox instance.

Supports upload, download, list, remove, and other file operations.

Examples:
  # List files in sandbox
  ags file ls /home/user --instance <id>

  # Upload a file
  ags file upload local.txt /home/user/remote.txt --instance <id>

  # Download a file
  ags file download /home/user/remote.txt local.txt --instance <id>

  # Remove a file
  ags file rm /home/user/file.txt --instance <id>

  # Create a directory
  ags file mkdir /home/user/newdir --instance <id>`,
	}

	// Common flags for all subcommands
	cmd.PersistentFlags().StringVarP(&fileInstance, "instance", "i", "", "Instance ID to use (required)")
	cmd.PersistentFlags().StringVarP(&fileTool, "tool-name", "t", "code-interpreter-v1", "Tool for temporary instance (if --instance not specified)")
	cmd.PersistentFlags().StringVar(&fileTool, "tool", "code-interpreter-v1", "Tool for temporary instance (alias for --tool-name)")
	cmd.PersistentFlags().BoolVar(&fileKeepAlive, "keep-alive", false, "Keep temporary instance alive")
	cmd.PersistentFlags().BoolVar(&fileTime, "time", false, "Print elapsed time")

	// file list
	listCmd := &cobra.Command{
		Use:     "list <path>",
		Aliases: []string{"ls"},
		Short:   "List files in a directory",
		Args:    cobra.ExactArgs(1),
		RunE:    fileListCommand,
	}
	listCmd.Flags().IntVar(&fileListDepth, "depth", 1, "Directory depth (1 = current dir only)")
	cmd.AddCommand(listCmd)

	// file upload
	uploadCmd := &cobra.Command{
		Use:     "upload <local-path> <remote-path>",
		Aliases: []string{"up", "put"},
		Short:   "Upload a file to sandbox",
		Args:    cobra.ExactArgs(2),
		RunE:    fileUploadCommand,
	}
	cmd.AddCommand(uploadCmd)

	// file download
	downloadCmd := &cobra.Command{
		Use:     "download <remote-path> [local-path]",
		Aliases: []string{"down", "get"},
		Short:   "Download a file from sandbox",
		Args:    cobra.RangeArgs(1, 2),
		RunE:    fileDownloadCommand,
	}
	cmd.AddCommand(downloadCmd)

	// file remove
	removeCmd := &cobra.Command{
		Use:     "remove <path> [path...]",
		Aliases: []string{"rm", "del"},
		Short:   "Remove files or directories",
		Args:    cobra.MinimumNArgs(1),
		RunE:    fileRemoveCommand,
	}
	cmd.AddCommand(removeCmd)

	// file mkdir
	mkdirCmd := &cobra.Command{
		Use:   "mkdir <path>",
		Short: "Create a directory",
		Args:  cobra.ExactArgs(1),
		RunE:  fileMkdirCommand,
	}
	cmd.AddCommand(mkdirCmd)

	// file stat
	statCmd := &cobra.Command{
		Use:   "stat <path>",
		Short: "Get file or directory info",
		Args:  cobra.ExactArgs(1),
		RunE:  fileStatCommand,
	}
	cmd.AddCommand(statCmd)

	// file cat
	catCmd := &cobra.Command{
		Use:   "cat <path>",
		Short: "Print file contents to stdout",
		Args:  cobra.ExactArgs(1),
		RunE:  fileCatCommand,
	}
	cmd.AddCommand(catCmd)

	parent.AddCommand(cmd)
}

// getSandboxForFile gets or creates a sandbox for file operations
// Returns sandbox, cleanup function, create duration (0 if connecting to existing), and error
func getSandboxForFile(ctx context.Context) (*code.Sandbox, func(), time.Duration, error) {
	// Validate parameters
	if fileInstance != "" && fileTool != "code-interpreter-v1" {
		return nil, nil, 0, fmt.Errorf("cannot specify both --instance and --tool-name/--tool")
	}

	if fileInstance != "" {
		sandbox, err := code.Connect(ctx, fileInstance, getConnectOptions()...)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("failed to connect to instance %s: %w", fileInstance, err)
		}
		return sandbox, func() {}, 0, nil
	}

	createStart := time.Now()
	sandbox, err := code.Create(ctx, fileTool, getCreateOptions()...)
	createDuration := time.Since(createStart)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to create sandbox: %w", err)
	}

	cleanup := func() {}
	if fileKeepAlive {
		output.PrintInfo(fmt.Sprintf("Created instance: %s (kept alive)", sandbox.SandboxId))
	} else {
		cleanup = func() {
			_ = sandbox.Kill(ctx)
		}
	}

	return sandbox, cleanup, createDuration, nil
}

func fileListCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	start := time.Now()

	if err := config.Validate(); err != nil {
		return err
	}

	sandbox, cleanup, createDuration, err := getSandboxForFile(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	path := args[0]
	execStart := time.Now()
	entries, err := sandbox.Files.List(ctx, path, nil)
	if err != nil {
		return fmt.Errorf("failed to list directory: %w", err)
	}
	execDuration := time.Since(execStart)
	totalDuration := time.Since(start)

	var timing *output.Timing
	if fileTime {
		if createDuration > 0 {
			timing = output.NewTimingWithPhases(totalDuration, createDuration, execDuration)
		} else {
			timing = output.NewTiming(totalDuration)
		}
	}

	f := output.NewFormatter()

	if len(entries) == 0 {
		output.PrintInfo("Directory is empty")
		if fileTime && !f.IsJSON() {
			f.PrintTiming(timing)
		}
		return nil
	}

	// Build table output
	headers := []string{"TYPE", "SIZE", "PERMISSIONS", "MODIFIED", "NAME"}
	rows := make([][]string, len(entries))
	for i, e := range entries {
		fileType := "file"
		if e.Type != nil {
			fileType = string(*e.Type)
		}
		modified := e.ModifiedTime.Format("2006-01-02 15:04")
		rows[i] = []string{fileType, output.FormatSize(e.Size), e.Permissions, modified, e.Name}
	}

	if err := f.PrintTable(headers, rows, nil); err != nil {
		return err
	}

	if fileTime && !f.IsJSON() {
		f.PrintTiming(timing)
	}

	return nil
}

func fileUploadCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	start := time.Now()

	if err := config.Validate(); err != nil {
		return err
	}

	localPath := args[0]
	remotePath := args[1]

	// Get file info for size
	localInfo, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("failed to stat local file: %w", err)
	}

	// Open local file
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open local file: %w", err)
	}
	defer file.Close()

	sandbox, cleanup, createDuration, err := getSandboxForFile(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	execStart := time.Now()
	info, err := sandbox.Files.Write(ctx, remotePath, file, nil)
	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}
	execDuration := time.Since(execStart)
	totalDuration := time.Since(start)

	var timing *output.Timing
	if fileTime {
		if createDuration > 0 {
			timing = output.NewTimingWithPhases(totalDuration, createDuration, execDuration)
		} else {
			timing = output.NewTiming(totalDuration)
		}
	}

	f := output.NewFormatter()
	op := &output.FileOperation{
		Operation: "upload",
		Path:      info.Path,
		LocalPath: localPath,
		Size:      localInfo.Size(),
		Timing:    timing,
	}

	if err := f.PrintFileOperation(op); err != nil {
		return err
	}

	if fileTime && !f.IsJSON() {
		f.PrintTiming(timing)
	}

	return nil
}

func fileDownloadCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	start := time.Now()

	if err := config.Validate(); err != nil {
		return err
	}

	remotePath := args[0]
	localPath := filepath.Base(remotePath)
	if len(args) > 1 {
		localPath = args[1]
	}

	sandbox, cleanup, createDuration, err := getSandboxForFile(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	execStart := time.Now()
	reader, err := sandbox.Files.Read(ctx, remotePath, nil)
	if err != nil {
		return fmt.Errorf("failed to read remote file: %w", err)
	}

	// Create local file
	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer file.Close()

	n, err := io.Copy(file, reader)
	if err != nil {
		return fmt.Errorf("failed to write local file: %w", err)
	}
	execDuration := time.Since(execStart)
	totalDuration := time.Since(start)

	var timing *output.Timing
	if fileTime {
		if createDuration > 0 {
			timing = output.NewTimingWithPhases(totalDuration, createDuration, execDuration)
		} else {
			timing = output.NewTiming(totalDuration)
		}
	}

	f := output.NewFormatter()
	op := &output.FileOperation{
		Operation: "download",
		Path:      remotePath,
		LocalPath: localPath,
		Size:      n,
		Timing:    timing,
	}

	if err := f.PrintFileOperation(op); err != nil {
		return err
	}

	if fileTime && !f.IsJSON() {
		f.PrintTiming(timing)
	}

	return nil
}

func fileRemoveCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	start := time.Now()

	if err := config.Validate(); err != nil {
		return err
	}

	sandbox, cleanup, createDuration, err := getSandboxForFile(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	f := output.NewFormatter()
	var failed []string

	execStart := time.Now()
	for _, path := range args {
		if err := sandbox.Files.Remove(ctx, path, nil); err != nil {
			output.PrintWarning(fmt.Sprintf("Failed to remove %s: %v", path, err))
			failed = append(failed, path)
		} else {
			op := &output.FileOperation{
				Operation: "remove",
				Path:      path,
			}
			_ = f.PrintFileOperation(op)
		}
	}
	execDuration := time.Since(execStart)
	totalDuration := time.Since(start)

	var timing *output.Timing
	if fileTime {
		if createDuration > 0 {
			timing = output.NewTimingWithPhases(totalDuration, createDuration, execDuration)
		} else {
			timing = output.NewTiming(totalDuration)
		}
	}

	if fileTime && !f.IsJSON() {
		f.PrintTiming(timing)
	}

	if len(failed) > 0 {
		return fmt.Errorf("failed to remove %d file(s)", len(failed))
	}
	return nil
}

func fileMkdirCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	start := time.Now()

	if err := config.Validate(); err != nil {
		return err
	}

	sandbox, cleanup, createDuration, err := getSandboxForFile(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	path := args[0]
	execStart := time.Now()
	_, err = sandbox.Files.MakeDir(ctx, path, nil)
	if err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	execDuration := time.Since(execStart)
	totalDuration := time.Since(start)

	var timing *output.Timing
	if fileTime {
		if createDuration > 0 {
			timing = output.NewTimingWithPhases(totalDuration, createDuration, execDuration)
		} else {
			timing = output.NewTiming(totalDuration)
		}
	}

	f := output.NewFormatter()
	op := &output.FileOperation{
		Operation: "mkdir",
		Path:      path,
		Timing:    timing,
	}

	if err := f.PrintFileOperation(op); err != nil {
		return err
	}

	if fileTime && !f.IsJSON() {
		f.PrintTiming(timing)
	}

	return nil
}

func fileStatCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	start := time.Now()

	if err := config.Validate(); err != nil {
		return err
	}

	sandbox, cleanup, createDuration, err := getSandboxForFile(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	path := args[0]
	execStart := time.Now()
	info, err := sandbox.Files.GetInfo(ctx, path, nil)
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}
	execDuration := time.Since(execStart)
	totalDuration := time.Since(start)

	var timing *output.Timing
	if fileTime {
		if createDuration > 0 {
			timing = output.NewTimingWithPhases(totalDuration, createDuration, execDuration)
		} else {
			timing = output.NewTiming(totalDuration)
		}
	}

	f := output.NewFormatter()

	fileType := "file"
	if info.Type != nil {
		fileType = string(*info.Type)
	}

	if f.IsJSON() {
		// JSON mode: include all fields
		data := map[string]any{
			"name":        info.Name,
			"path":        info.Path,
			"type":        fileType,
			"size":        info.Size,
			"permissions": info.Permissions,
			"owner":       info.Owner,
			"group":       info.Group,
			"modified":    info.ModifiedTime.Format(time.RFC3339),
		}
		if info.SymlinkTarget != nil {
			data["symlink_target"] = *info.SymlinkTarget
		}
		if timing != nil {
			data["timing"] = timing
		}
		return f.PrintJSON(data)
	}

	// Text mode: key-value pairs
	result := []output.KeyValue{
		{Key: "Name", Value: info.Name},
		{Key: "Path", Value: info.Path},
		{Key: "Type", Value: fileType},
		{Key: "Size", Value: output.FormatSize(info.Size)},
		{Key: "Permissions", Value: info.Permissions},
		{Key: "Owner", Value: info.Owner},
		{Key: "Group", Value: info.Group},
		{Key: "Modified", Value: info.ModifiedTime.Format(time.RFC3339)},
	}

	if info.SymlinkTarget != nil {
		result = append(result, output.KeyValue{Key: "SymlinkTarget", Value: *info.SymlinkTarget})
	}

	if err := f.PrintKeyValue(result); err != nil {
		return err
	}

	if fileTime {
		f.PrintTiming(timing)
	}

	return nil
}

func fileCatCommand(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	start := time.Now()

	if err := config.Validate(); err != nil {
		return err
	}

	sandbox, cleanup, createDuration, err := getSandboxForFile(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	path := args[0]
	execStart := time.Now()
	reader, err := sandbox.Files.Read(ctx, path, nil)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Read all content
	content, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read file content: %w", err)
	}
	execDuration := time.Since(execStart)
	totalDuration := time.Since(start)

	var timing *output.Timing
	if fileTime {
		if createDuration > 0 {
			timing = output.NewTimingWithPhases(totalDuration, createDuration, execDuration)
		} else {
			timing = output.NewTiming(totalDuration)
		}
	}

	f := output.NewFormatter()

	fileContent := &output.FileContent{
		Path:    path,
		Content: string(content),
		Size:    int64(len(content)),
		Timing:  timing,
	}

	if err := f.PrintFileContent(fileContent); err != nil {
		return err
	}

	if fileTime && !f.IsJSON() {
		f.PrintTiming(timing)
	}

	return nil
}
