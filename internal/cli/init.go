package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/cobra"
)

var initOverwrite bool

var initCmd = &cobra.Command{
	Use:     "init",
	Short:   "Initialize local AGR CLI configuration",
	Long:    "Initialize local AGR CLI configuration and Tencent Cloud credentials without creating remote resources.",
	Example: exampleBlocks("agr init --secret-id <id> --secret-key <key>", "agr init --overwrite --secret-id <id> --secret-key <key>"),
	Args:    cobra.NoArgs,
}

func init() {
	initCmd.RunE = Wrap("init", initFn)
	initCmd.Flags().BoolVar(&initOverwrite, "overwrite", false, "overwrite existing config file")
	rootCmd.AddCommand(initCmd)
}

func initFn(cmd *cobra.Command, args []string) (*CmdResult, error) {
	sid := secretID
	if sid == "" {
		sid = os.Getenv("TENCENTCLOUD_SECRET_ID")
	}
	skey := secretKey
	if skey == "" {
		skey = os.Getenv("TENCENTCLOUD_SECRET_KEY")
	}
	sessionToken := tokenFlag
	if sessionToken == "" {
		sessionToken = os.Getenv("TENCENTCLOUD_TOKEN")
	}
	if sid == "" || skey == "" {
		return nil, output.NewUsageError("MISSING_CLOUD_CREDENTIALS", "cloud credentials are required", "Run: agr init --secret-id <id> --secret-key <key>")
	}

	path := config.ConfigFilePath()
	if path == "" {
		return nil, output.NewUsageError("CONFIG_INIT_FAILED", "failed to determine config file path", "Set HOME or pass --config <path>.")
	}
	if _, err := os.Stat(path); err == nil && !initOverwrite {
		return nil, output.NewUsageError("CONFIG_EXISTS", "config file already exists", "Run: agr init --overwrite --secret-id <id> --secret-key <key>")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, output.NewUsageError("CONFIG_INIT_FAILED", err.Error(), "Check the config file path and permissions.")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, output.NewUsageError("CONFIG_INIT_FAILED", err.Error(), "Check that the config directory is writable.")
	}

	content := fmt.Sprintf("output = \"text\"\nregion = \"ap-guangzhou\"\ndomain = \"tencentags.com\"\ncloud_endpoint = \"ags.tencentcloudapi.com\"\n\n[auth]\nsecret_id = %q\nsecret_key = %q\n", sid, skey)
	if sessionToken != "" {
		content += fmt.Sprintf("token = %q\n", sessionToken)
	}
	content += "\n[sandbox]\ndefault_user = \"user\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return nil, output.NewUsageError("CONFIG_INIT_FAILED", err.Error(), "Check that the config file is writable.")
	}
	_ = os.Chmod(path, 0o600)

	data := map[string]any{
		"ConfigFile": path,
		"Output":     "text",
		"Auth": map[string]any{
			"SecretId":  map[string]any{"Present": true, "Source": "config auth.secret_id"},
			"SecretKey": map[string]any{"Present": true, "Source": "config auth.secret_key"},
			"Token":     map[string]any{"Present": sessionToken != "", "Source": "config auth.token"},
		},
		"Written": true,
	}
	return OK(data, func(w io.Writer) {
		fmt.Fprintf(w, "Config initialized: %s\n", path)
		fmt.Fprintln(w, "Secret ID:  present")
		fmt.Fprintln(w, "Secret Key: present")
		if sessionToken != "" {
			fmt.Fprintf(w, "Token:      %s\n", maskCredential(sessionToken))
		}
		fmt.Fprintln(w, "Next: run 'agr status' or 'agr doctor'.")
	}), nil
}
