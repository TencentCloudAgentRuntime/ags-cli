package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
	Long:  "View and modify AGR CLI configuration stored in ~/.agr/config.toml.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var configShowCmd = &cobra.Command{Use: "show", Short: "Show current configuration values and sources"}
var configSetCmd = &cobra.Command{
	Use:     "set <key> <value>",
	Short:   "Set a configuration value",
	Example: exampleBlocks("agr config set region ap-guangzhou", "agr config set output json"),
	Args:    cobra.RangeArgs(0, 2),
}
var configPathCmd = &cobra.Command{
	Use:     "path",
	Short:   "Print the configuration file path",
	Example: exampleBlocks("agr config path", "agr config path -o json"),
}

func init() {
	configShowCmd.Example = exampleBlocks("agr config show", "agr config show -o json")
	configShowCmd.RunE = Wrap("config.show", configShowFn)
	configSetCmd.RunE = Wrap("config.set", configSetFn)
	configPathCmd.RunE = Wrap("config.path", configPathFn)
	configCmd.AddCommand(configShowCmd, configSetCmd, configPathCmd)
	rootCmd.AddCommand(configCmd)
}

func configPathFn(cmd *cobra.Command, args []string) (*CmdResult, error) {
	path := config.ConfigFilePath()
	_, err := os.Stat(path)
	exists := err == nil
	data := map[string]any{"Path": path, "Exists": exists}
	return OK(data, func(w io.Writer) {
		fmt.Fprintln(w, path)
		if !exists {
			fmt.Fprintln(w, "(file does not exist; run 'agr init' to create)")
		}
	}), nil
}

func configShowFn(cmd *cobra.Command, args []string) (*CmdResult, error) {
	cfg := config.Get()
	secretIDPresent, secretIDSource := detectAuthSource("secret_id")
	secretKeyPresent, secretKeySource := detectAuthSource("secret_key")
	tokenPresent, tokenSource := detectAuthSource("token")

	data := map[string]any{
		"ConfigFile": config.ConfigFilePath(),
		"Loaded":     config.ConfigFileLoaded(),
		"Values": map[string]any{
			"output":         map[string]any{"Value": cfg.Output, "Source": config.GetSource("output")},
			"region":         map[string]any{"Value": cfg.Region, "Source": config.GetSource("region")},
			"domain":         map[string]any{"Value": cfg.Domain, "Source": config.GetSource("domain")},
			"cloud_endpoint": map[string]any{"Value": cfg.ControlPlaneEndpoint(), "Source": config.GetSource("cloud_endpoint")},
			"secret_id":      map[string]any{"Present": secretIDPresent, "Source": secretIDSource},
			"secret_key":     map[string]any{"Present": secretKeyPresent, "Source": secretKeySource},
			"token":          map[string]any{"Present": tokenPresent, "Source": tokenSource, "Value": maskCredential(config.GetToken())},
			"default_user":   map[string]any{"Value": cfg.Sandbox.DefaultUser, "Source": config.GetSource("default_user")},
		},
	}
	return OK(data, func(w io.Writer) {
		fmt.Fprintf(w, "Config file: %s (loaded: %t)\n\n", config.ConfigFilePath(), config.ConfigFileLoaded())
		pairs := []KeyValue{
			{Key: "output", Value: fmtSourced(cfg.Output, config.GetSource("output"))},
			{Key: "region", Value: fmtSourced(cfg.Region, config.GetSource("region"))},
			{Key: "domain", Value: fmtSourced(cfg.Domain, config.GetSource("domain"))},
			{Key: "cloud_endpoint", Value: fmtSourced(cfg.ControlPlaneEndpoint(), config.GetSource("cloud_endpoint"))},
			{Key: "secret_id", Value: formatAuthStatus(secretIDPresent, secretIDSource)},
			{Key: "secret_key", Value: formatAuthStatus(secretKeyPresent, secretKeySource)},
			{Key: "token", Value: formatSensitiveAuthStatus(tokenPresent, tokenSource, config.GetToken())},
			{Key: "default_user", Value: fmtSourced(cfg.Sandbox.DefaultUser, config.GetSource("default_user"))},
		}
		printKV(w, pairs)
	}), nil
}

func configSetFn(cmd *cobra.Command, args []string) (*CmdResult, error) {
	key, value, err := parseConfigSetArgs(cmd, args)
	if err != nil {
		return nil, err
	}

	validKeys := map[string]string{
		"output":         "output",
		"region":         "region",
		"domain":         "domain",
		"cloud_endpoint": "cloud_endpoint",
		"secret_id":      "auth.secret_id",
		"secret_key":     "auth.secret_key",
		"token":          "auth.token",
		"default_user":   "sandbox.default_user",
	}
	if _, ok := validKeys[key]; !ok {
		keys := make([]string, 0, len(validKeys))
		for k := range validKeys {
			keys = append(keys, k)
		}
		return nil, output.NewUsageError("INVALID_CONFIG_KEY",
			fmt.Sprintf("unknown config key: %s", key),
			fmt.Sprintf("Valid keys: %s", strings.Join(keys, ", ")))
	}

	path := config.ConfigFilePath()
	if path == "" {
		return nil, output.NewUsageError("CONFIG_INIT_FAILED", "cannot determine config file path", "Set HOME or pass --config <path>.")
	}

	// Read existing config or start fresh
	var cfg configFile
	if data, err := os.ReadFile(path); err == nil {
		_ = toml.Unmarshal(data, &cfg)
	}

	// Apply the change
	switch key {
	case "output":
		if value == "ndjson" {
			return nil, output.NewUsageError(
				"INVALID_CONFIG",
				"ndjson is only supported for streaming when passed explicitly with -o ndjson",
				"Set output to 'text' or 'json' in config, and use explicit -o ndjson with 'instance code run --stream' or 'instance exec --stream'.",
			)
		}
		cfg.Output = value
	case "region":
		cfg.Region = value
	case "domain":
		cfg.Domain = value
	case "cloud_endpoint":
		cfg.CloudEndpoint = value
	case "secret_id":
		cfg.Auth.SecretID = value
	case "secret_key":
		cfg.Auth.SecretKey = value
	case "token":
		cfg.Auth.Token = value
	case "default_user":
		cfg.Sandbox.DefaultUser = value
	}

	candidate := config.Config{
		Output:        cfg.Output,
		Region:        cfg.Region,
		Domain:        cfg.Domain,
		CloudEndpoint: cfg.CloudEndpoint,
		Auth: config.AuthConfig{
			SecretID:  cfg.Auth.SecretID,
			SecretKey: cfg.Auth.SecretKey,
			Token:     cfg.Auth.Token,
		},
		Sandbox: config.SandboxConfig{
			DefaultUser: cfg.Sandbox.DefaultUser,
		},
	}
	if err := config.ValidateCandidate(candidate); err != nil {
		return nil, newConfigUsageError(err)
	}

	// Marshal and write
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, output.NewUsageError("CONFIG_WRITE_FAILED", err.Error(), "Check directory permissions.")
	}
	out, err := toml.Marshal(cfg)
	if err != nil {
		return nil, output.NewUsageError("CONFIG_WRITE_FAILED", err.Error(), "")
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return nil, output.NewUsageError("CONFIG_WRITE_FAILED", err.Error(), "Check file permissions.")
	}

	data := map[string]any{"Key": key, "Value": configSetDisplayValue(key, value), "Path": path}
	return OK(data, func(w io.Writer) {
		fmt.Fprintf(w, "Set %s = %s\n", key, configSetDisplayValue(key, value))
		fmt.Fprintf(w, "Written to %s\n", path)
	}), nil
}

func parseConfigSetArgs(_ *cobra.Command, args []string) (string, string, error) {
	if tokenFlag != "" && len(args) == 0 {
		return "token", tokenFlag, nil
	}
	if tokenFlag != "" && len(args) != 0 {
		return "", "", output.NewUsageError("INVALID_USAGE", "--token cannot be combined with positional config set arguments", "Run: agr config set --token <token>")
	}
	switch len(args) {
	case 1:
		key, value, ok := strings.Cut(args[0], "=")
		if !ok || key == "" {
			return "", "", output.NewUsageError("INVALID_USAGE", "config set requires <key> <value> or <key>=<value>", "Run: agr config set token=<token>")
		}
		return key, value, nil
	case 2:
		return args[0], args[1], nil
	default:
		return "", "", output.NewUsageError("INVALID_USAGE", "config set requires <key> <value> or <key>=<value>", "Run: agr config set secret_id <id>")
	}
}

// configFile mirrors the TOML structure for marshal/unmarshal.
type configFile struct {
	Output        string            `toml:"output,omitempty"`
	Region        string            `toml:"region,omitempty"`
	Domain        string            `toml:"domain,omitempty"`
	CloudEndpoint string            `toml:"cloud_endpoint,omitempty"`
	Auth          configFileAuth    `toml:"auth"`
	Sandbox       configFileSandbox `toml:"sandbox,omitempty"`
}

type configFileAuth struct {
	SecretID  string `toml:"secret_id,omitempty"`
	SecretKey string `toml:"secret_key,omitempty"`
	Token     string `toml:"token,omitempty"`
}

type configFileSandbox struct {
	DefaultUser string `toml:"default_user,omitempty"`
}

func fmtSourced(value, source string) string {
	if source == "" || source == "default" {
		return value + " (default)"
	}
	return fmt.Sprintf("%s (source: %s)", value, source)
}

func configSetDisplayValue(key, value string) string {
	switch key {
	case "secret_id", "secret_key", "token":
		return maskCredential(value)
	default:
		return value
	}
}
