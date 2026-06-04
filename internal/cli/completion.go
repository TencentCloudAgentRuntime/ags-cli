package cli

import (
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/cobra"
)

func init() {
	completionCmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long:  "Generate shell completion scripts for agr.",
		Example: exampleBlocks(
			"agr completion bash > /etc/bash_completion.d/agr",
			`agr completion zsh > "${fpath[1]}/_agr"`,
			"agr completion fish > ~/.config/fish/completions/agr.fish",
			"agr completion powershell > agr.ps1",
		),
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	}
	completionCmd.RunE = WrapNoJSON(func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(ios.Out)
		case "zsh":
			return rootCmd.GenZshCompletion(ios.Out)
		case "fish":
			return rootCmd.GenFishCompletion(ios.Out, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(ios.Out)
		default:
			supported := strings.Join(cmd.ValidArgs, ", ")
			return output.NewUsageError("INVALID_SHELL", "unsupported shell: "+args[0]+" (supported: "+supported+")", "Supported shells: "+supported+".")
		}
	})
	rootCmd.AddCommand(completionCmd)
}
