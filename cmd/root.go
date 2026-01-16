package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/repl"
)

var (
	cfgFile     string
	backend     string
	outputFmt   string
	showVersion bool
	// E2B flags
	e2bAPIKey string
	e2bDomain string
	e2bRegion string
	// Cloud flags
	cloudSecretID  string
	cloudSecretKey string
	cloudRegion    string
	cloudInternal  bool
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "ags",
	Short: "AGS CLI - Agent Sandbox Command Line Interface",
	Long: `AGS CLI is a command line tool for managing Agent Sandbox tools and instances.

It supports both E2B API and Tencent Cloud API backends, allowing you to:
  - Manage sandbox tools (templates)
  - Create, list, and delete sandbox instances
  - Execute code in sandbox instances
  - Interactive REPL mode for management commands

Examples:
  # List available tools
  ags tool list

  # Create/start a new instance
  ags instance create --tool code-interpreter-v1
  ags instance start --tool code-interpreter-v1

  # Delete/stop an instance
  ags instance delete <instance-id>
  ags instance stop <instance-id>

  # Execute code
  ags run -c "print('Hello, World!')"

  # Enter REPL mode (default when no command is given)
  ags`,
	Run: func(cmd *cobra.Command, args []string) {
		if showVersion {
			printVersion()
			return
		}
		runREPL(cmd, args)
	},
}

func runREPL(_ *cobra.Command, _ []string) {
	// Set up REPL command executor
	repl.ExecuteCommand = executeREPLCommand

	// If no subcommand is given, enter REPL mode
	if err := repl.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "Error starting REPL:", err)
		os.Exit(1)
	}
}

func executeREPLCommand(args []string) error {
	// Create a fresh command tree for REPL execution
	newRoot := &cobra.Command{
		Use:           "ags",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Add subcommands
	addToolCommand(newRoot)
	addInstanceCommand(newRoot)
	addRunCommand(newRoot)
	addAPIKeyCommand(newRoot)
	addExecCommand(newRoot)
	addFileCommand(newRoot)
	addBrowserCommand(newRoot)

	newRoot.SetArgs(args)
	return newRoot.Execute()
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.ags/config.toml)")
	rootCmd.PersistentFlags().StringVar(&backend, "backend", "", "API backend: e2b or cloud")
	rootCmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "", "output format: text or json")

	// Version flag (local to root command only)
	rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "Print version information")

	// E2B flags
	rootCmd.PersistentFlags().StringVar(&e2bAPIKey, "e2b-api-key", "", "E2B API key")
	rootCmd.PersistentFlags().StringVar(&e2bDomain, "e2b-domain", "", "E2B domain (default: tencentags.com)")
	rootCmd.PersistentFlags().StringVar(&e2bRegion, "e2b-region", "", "E2B region (default: ap-guangzhou)")

	// Cloud flags
	rootCmd.PersistentFlags().StringVar(&cloudSecretID, "cloud-secret-id", "", "Tencent Cloud SecretID")
	rootCmd.PersistentFlags().StringVar(&cloudSecretKey, "cloud-secret-key", "", "Tencent Cloud SecretKey")
	rootCmd.PersistentFlags().StringVar(&cloudRegion, "cloud-region", "", "Tencent Cloud region (default: ap-guangzhou)")
	rootCmd.PersistentFlags().BoolVar(&cloudInternal, "cloud-internal", false, "Use internal endpoints (for Tencent Cloud internal network)")
}

func initConfig() {
	// Set config file if provided
	if cfgFile != "" {
		config.SetConfigFile(cfgFile)
	}

	// Initialize config
	if err := config.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "Warning: failed to load config:", err)
	}

	// Apply command line overrides
	if backend != "" {
		config.SetBackend(backend)
	}
	if outputFmt != "" {
		config.SetOutput(outputFmt)
	}

	// E2B overrides
	if e2bAPIKey != "" {
		config.SetE2BAPIKey(e2bAPIKey)
	}
	if e2bDomain != "" {
		config.SetE2BDomain(e2bDomain)
	}
	if e2bRegion != "" {
		config.SetE2BRegion(e2bRegion)
	}

	// Cloud overrides
	if cloudSecretID != "" {
		config.SetCloudSecretID(cloudSecretID)
	}
	if cloudSecretKey != "" {
		config.SetCloudSecretKey(cloudSecretKey)
	}
	if cloudRegion != "" {
		config.SetCloudRegion(cloudRegion)
	}
	if cloudInternal {
		config.SetCloudInternal(cloudInternal)
	}
}
