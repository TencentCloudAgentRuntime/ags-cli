package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/repl"
	"github.com/spf13/cobra"
)

var (
	cfgFile     string
	backend     string
	outputFmt   string
	showVersion bool
	// Unified flags
	region   string
	domain   string
	internal bool
	// E2B flags
	e2bAPIKey string
	e2bDomain string // Deprecated: use --domain
	e2bRegion string // Deprecated: use --region
	// Cloud flags
	cloudSecretID  string
	cloudSecretKey string
	cloudRegion    string // Deprecated: use --region
	cloudInternal  bool   // Deprecated: use --internal
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
	addMobileCommand(newRoot)
	addProxyCommand(newRoot)

	newRoot.SetArgs(args)
	return newRoot.Execute()
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		// Respect custom exit codes from exitCodeError (used by mobile tunnel daemon)
		var exitErr *exitCodeError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.code)
		}
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

	// Unified flags (recommended)
	rootCmd.PersistentFlags().StringVar(&region, "region", "", "Region for API access (default: ap-guangzhou)")
	rootCmd.PersistentFlags().StringVar(&domain, "domain", "", "Base domain (default: tencentags.com)")
	rootCmd.PersistentFlags().BoolVar(&internal, "internal", false, "Use internal endpoints (for Tencent Cloud internal network)")

	// E2B flags
	rootCmd.PersistentFlags().StringVar(&e2bAPIKey, "e2b-api-key", "", "E2B API key")
	rootCmd.PersistentFlags().StringVar(&e2bDomain, "e2b-domain", "", "E2B domain (deprecated: use --domain)")
	rootCmd.PersistentFlags().StringVar(&e2bRegion, "e2b-region", "", "E2B region (deprecated: use --region)")

	// Cloud flags
	rootCmd.PersistentFlags().StringVar(&cloudSecretID, "cloud-secret-id", "", "Tencent Cloud SecretID")
	rootCmd.PersistentFlags().StringVar(&cloudSecretKey, "cloud-secret-key", "", "Tencent Cloud SecretKey")
	rootCmd.PersistentFlags().StringVar(&cloudRegion, "cloud-region", "", "Tencent Cloud region (deprecated: use --region)")
	rootCmd.PersistentFlags().BoolVar(&cloudInternal, "cloud-internal", false, "Use internal endpoints (deprecated: use --internal)")

	// Mark deprecated flags
	_ = rootCmd.PersistentFlags().MarkDeprecated("e2b-domain", "use --domain instead")
	_ = rootCmd.PersistentFlags().MarkDeprecated("e2b-region", "use --region instead")
	_ = rootCmd.PersistentFlags().MarkDeprecated("cloud-region", "use --region instead")
	_ = rootCmd.PersistentFlags().MarkDeprecated("cloud-internal", "use --internal instead")
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

	// Credential overrides (not subject to priority ordering)
	if e2bAPIKey != "" {
		config.SetE2BAPIKey(e2bAPIKey)
	}
	if cloudSecretID != "" {
		config.SetCloudSecretID(cloudSecretID)
	}
	if cloudSecretKey != "" {
		config.SetCloudSecretKey(cloudSecretKey)
	}

	// Legacy overrides (lower priority, applied first so unified flags can override)
	if e2bDomain != "" {
		config.SetE2BDomain(e2bDomain)
	}
	if e2bRegion != "" {
		config.SetE2BRegion(e2bRegion)
	}
	if cloudRegion != "" {
		config.SetCloudRegion(cloudRegion)
	}
	if rootCmd.PersistentFlags().Changed("cloud-internal") {
		config.SetCloudInternal(cloudInternal)
	}

	// Unified overrides (highest priority, applied last)
	if region != "" {
		config.SetRegion(region)
	}
	if domain != "" {
		config.SetDomain(domain)
	}
	if rootCmd.PersistentFlags().Changed("internal") {
		config.SetInternal(internal)
	}
}
