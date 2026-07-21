package cli

import (
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/updatecheck"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for CLI updates",
	Long: `Check whether a newer version of AGR CLI is available.

By default, checks the remote version and reports current vs latest.
Use --check to perform an explicit check (same as running without arguments).`,
	Example: exampleBlocks("agr update", "agr update --check", "agr update -o json"),
}

func init() {
	updateCmd.Flags().Bool("check", false, "Explicitly check for updates (default behavior)")
	updateCmd.RunE = Wrap("update", updateFn)
	rootCmd.AddCommand(updateCmd)
}

func updateFn(cmd *cobra.Command, args []string) (*CmdResult, error) {
	latest, err := updatecheck.FetchLatestVersion()
	if err != nil {
		return nil, output.NewCLIError(&output.Failure{
			Code:    "UPDATE_CHECK_FAILED",
			Kind:    output.KindGenericError,
			Message: fmt.Sprintf("failed to check for updates: %v", err),
			Hint:    "Check your network connection and try again.",
		})
	}

	version, _, _ := resolvedVersionInfo()
	cmp := updatecheck.CompareVersions(version, latest)

	data := map[string]any{
		"current":          version,
		"latest":           latest,
		"update_available": cmp > 0,
	}
	if cmp > 0 {
		data["install_command"] = fmt.Sprintf("curl -fsSL %s | sh", updatecheck.InstallURL)
	}

	return OK(data, func(w io.Writer) {
		fmt.Fprintf(w, "Current: %s\n", version)
		fmt.Fprintf(w, "Latest:  %s\n", latest)
		if cmp > 0 {
			fmt.Fprintf(w, "\nUpdate available: %s → %s\n", version, latest)
			fmt.Fprintf(w, "Run: curl -fsSL %s | sh\n", updatecheck.InstallURL)
		} else if cmp == 0 {
			fmt.Fprintf(w, "\nYou are up to date.\n")
		} else {
			fmt.Fprintf(w, "\nYour version is newer than the latest release.\n")
		}
	}), nil
}
