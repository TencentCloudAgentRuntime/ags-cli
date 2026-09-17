package main

import (
	"fmt"
	"os"
	"sync"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands"
)

// Version information - set by ldflags at build time
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

var (
	attachOnce sync.Once
	attachErr  error
)

func attachCommands() error {
	attachOnce.Do(func() {
		registry, err := commands.Registry()
		if err != nil {
			attachErr = err
			return
		}
		attachErr = cli.AttachCommandRegistry(registry)
	})
	return attachErr
}

func main() {
	cli.SetVersionInfo(Version, Commit, BuildTime)
	if err := cli.ValidateBuildChannel(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := attachCommands(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build command tree: %v\n", err)
		os.Exit(1)
	}
	cli.Execute()
}
