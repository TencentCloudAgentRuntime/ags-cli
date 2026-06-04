package download

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
		ID:    "instance.file.download",
		Path:  []string{"instance", "file", "download"},
		Use:   "download <instance-id> <remote-path> <local-path|->",
		Short: "Download a file from sandbox",
		Args: []command.ArgSpec{
			{Name: "instance-id", Required: true},
			{Name: "remote-path", Required: true},
			{Name: "local-path", Required: true},
		},
		Flags: []command.FlagSpec{
			{Name: "user", Usage: "User for file operations", Type: command.FlagString},
		},
		SupportsJSON: true,
		Output:       command.OutputSpec{DataType: "FileTransferResult"},
	}
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
					return runDownload(ctx, req, deps)
				}),
			}, nil
		},
	}
}

func runDownload(ctx context.Context, req command.Request, deps command.Deps) (*command.Result, error) {
	instanceID := req.ArgValues["instance-id"]
	remotePath := req.ArgValues["remote-path"]
	localPath := req.ArgValues["local-path"]
	if instanceID == "" && len(req.Args) > 0 {
		instanceID = req.Args[0]
	}
	if remotePath == "" && len(req.Args) > 1 {
		remotePath = req.Args[1]
	}
	if localPath == "" && len(req.Args) > 2 {
		localPath = req.Args[2]
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}
	if localPath == "-" && cli.IsJSON() {
		return nil, output.NewUsageError("STDOUT_CONFLICT",
			"cannot use -o json with stdout download (-)",
			"Use a file path instead of - when using -o json.")
	}
	if testDP := cli.TestDataPlane(); testDP != nil {
		reader, size, err := testDP.Download(ctx, instanceID, remotePath)
		if err != nil {
			return nil, err
		}
		return writeDownloadResult(reader, size, remotePath, localPath, deps.IO.Out)
	}

	sandbox, err := cli.ConnectSandboxWithCache(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to instance %s: %w", instanceID, err)
	}
	reader, err := sandbox.Files.Read(ctx, remotePath, &filesystem.ReadConfig{User: cli.ResolveUser(stringFlag(req, "user"))})
	if err != nil {
		return nil, fmt.Errorf("failed to read remote file: %w", err)
	}
	return writeDownloadResult(reader, -1, remotePath, localPath, deps.IO.Out)
}

func writeDownloadResult(reader io.Reader, size int64, remotePath, localPath string, stdout io.Writer) (*command.Result, error) {
	if localPath == "-" {
		_, _ = io.Copy(stdout, reader)
		return &command.Result{StreamDone: true}, nil
	}
	f, err := os.Create(localPath)
	if err != nil {
		return nil, output.NewUsageError("INVALID_LOCAL_PATH", fmt.Sprintf("failed to create local file: %v", err), "Ensure the destination path is writable.")
	}
	defer func() { _ = f.Close() }()
	n, err := io.Copy(f, reader)
	if err != nil {
		return nil, err
	}
	if size >= 0 {
		n = size
	}
	data := map[string]any{"Operation": "download", "Path": remotePath, "LocalPath": localPath, "Size": n}
	return &command.Result{Data: data, Text: func(w io.Writer) {
		fmt.Fprintf(w, "Downloaded %s -> %s (%s)\n", remotePath, localPath, output.FormatSize(n))
	}}, nil
}

func stringFlag(req command.Request, name string) string {
	flag, ok := req.Flags[name]
	if !ok {
		return ""
	}
	return flag.String
}
