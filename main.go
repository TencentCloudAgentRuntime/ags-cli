package main

import (
	"github.com/TencentCloudAgentRuntime/ags-cli/cmd"
)

// Version information - set by ldflags at build time
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	// Pass version info to cmd package
	cmd.SetVersionInfo(Version, Commit, BuildTime)
	cmd.Execute()
}
