package upload

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/TencentCloudAgentRuntime/ags-go-sdk/tool/filesystem"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module returns this package's command module.
func Module() command.Module {
	spec := command.Spec{
		ID:    "instance.file.upload",
		Path:  []string{"instance", "file", "upload"},
		Use:   "upload <instance-id> <local-path|-> <remote-path>",
		Short: "Upload a file to sandbox",
		Args: []command.ArgSpec{
			{Name: "instance-id", Required: true},
			{Name: "local-path", Required: true},
			{Name: "remote-path", Required: true},
		},
		Flags: []command.FlagSpec{
			{Name: "user", Usage: "User for file operations", Type: command.FlagString},
		},
		SupportsJSON: true,
		Output:       command.OutputSpec{DataType: "FileTransferResult"},
	}
	return module(spec)
}

func module(spec command.Spec) command.Module {
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Groups: []command.GroupSpec{
				{
					Path:    []string{"instance"},
					Use:     "instance",
					Short:   "Manage sandbox instances",
					Long:    "Manage sandbox instances and related data-plane workflows.",
					Aliases: []string{"i"},
				},
				{
					Path:  []string{"instance", "file"},
					Use:   "file",
					Short: "File operations in sandbox",
					Long: `Upload and download files in a sandbox instance.

Examples:
  agr instance file upload ins-xxxx local.txt /home/user/remote.txt
  agr instance file download ins-xxxx /home/user/remote.txt local.txt
  echo "data" | agr instance file upload ins-xxxx - /home/user/data.txt
  agr instance file download ins-xxxx /home/user/data.txt -`,
				},
			},
			Source: "workflow",
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			return command.Runtime{
				Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
					return runUpload(ctx, req, deps)
				}),
			}, nil
		},
	}
}

func runUpload(ctx context.Context, req command.Request, deps command.Deps) (*command.Result, error) {
	instanceID := req.ArgValues["instance-id"]
	localPath := req.ArgValues["local-path"]
	remotePath := req.ArgValues["remote-path"]
	if instanceID == "" && len(req.Args) > 0 {
		instanceID = req.Args[0]
	}
	if localPath == "" && len(req.Args) > 1 {
		localPath = req.Args[1]
	}
	if remotePath == "" && len(req.Args) > 2 {
		remotePath = req.Args[2]
	}

	reader, localSize, cleanup, err := uploadReader(localPath, req.Stdin)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if err := config.Validate(); err != nil {
		return nil, err
	}
	if testDP := cli.TestDataPlane(); testDP != nil {
		path, size, err := testDP.Upload(ctx, instanceID, localPath, remotePath, reader)
		if err != nil {
			return nil, err
		}
		data := map[string]any{"Operation": "upload", "Path": path, "LocalPath": localPath, "Size": size}
		return &command.Result{Data: data, Text: func(w io.Writer) { fmt.Fprintf(w, "Uploaded %s -> %s\n", localPath, path) }}, nil
	}

	sandbox, err := cli.ConnectSandboxWithCache(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to instance %s: %w", instanceID, err)
	}
	info, err := sandbox.Files.Write(ctx, remotePath, reader, &filesystem.WriteConfig{User: cli.ResolveUser(stringFlag(req, "user"))})
	if err != nil {
		return nil, fmt.Errorf("failed to upload file: %w", err)
	}
	data := map[string]any{
		"Operation": "upload",
		"Path":      info.Path,
		"LocalPath": localPath,
		"Size":      localSize,
	}
	return &command.Result{Data: data, Text: func(w io.Writer) { fmt.Fprintf(w, "Uploaded %s -> %s\n", localPath, info.Path) }}, nil
}

func uploadReader(localPath string, stdin io.Reader) (io.Reader, int64, func(), error) {
	if localPath == "-" {
		if stdin == nil {
			stdin = os.Stdin
		}
		return stdin, -1, func() {}, nil
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return nil, 0, func() {}, output.NewUsageError("INVALID_LOCAL_PATH", fmt.Sprintf("failed to stat local file: %v", err), "Provide an existing local file path or use - for stdin.")
	}
	f, err := os.Open(localPath)
	if err != nil {
		return nil, 0, func() {}, output.NewUsageError("INVALID_LOCAL_PATH", fmt.Sprintf("failed to open local file: %v", err), "Ensure the local file exists and is readable.")
	}
	return f, info.Size(), func() { _ = f.Close() }, nil
}

func stringFlag(req command.Request, name string) string {
	flag, ok := req.Flags[name]
	if !ok {
		return ""
	}
	return flag.String
}
