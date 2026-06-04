package cli

import (
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/cobra"
)

func init() {
	statusCmd.RunE = Wrap("status", statusFn)
	rootCmd.AddCommand(statusCmd)
}

var statusCmd = &cobra.Command{
	Use:     "status",
	Short:   "Show current CLI configuration status",
	Example: exampleBlocks("agr status", "agr status -o json"),
}

func statusFn(cmd *cobra.Command, args []string) (*CmdResult, error) {
	permWarning := config.CheckConfigFilePermissions()
	warnings := collectConfigWarnings(permWarning)
	statusErr := configInitErr
	if statusErr == nil {
		statusErr = configBasicsErr
	}
	if statusErr == nil {
		statusErr = configCommandErr
	}

	cfg := config.Get()
	secretIDPresent, secretIDSource := detectAuthSource("secret_id")
	secretKeyPresent, secretKeySource := detectAuthSource("secret_key")
	tokenPresent, tokenSource := detectAuthSource("token")
	configLoaded := config.ConfigFileLoaded() && statusErr == nil

	data := map[string]any{
		"ConfigFile":       config.ConfigFilePath(),
		"ConfigFound":      config.ConfigFileFound(),
		"ConfigLoaded":     configLoaded,
		"ConfigError":      "",
		"ConfiguredOutput": configuredOutput,
		"EffectiveOutput":  effectiveOutput(),
		"Auth": map[string]any{
			"SecretId":  map[string]any{"Present": secretIDPresent, "Source": secretIDSource},
			"SecretKey": map[string]any{"Present": secretKeyPresent, "Source": secretKeySource},
			"Token":     map[string]any{"Present": tokenPresent, "Source": tokenSource},
		},
	}
	if statusErr == nil {
		data["Region"] = cfg.Region
		data["Domain"] = cfg.Domain
		data["Output"] = configuredOutput
	} else {
		data["ConfigError"] = statusErr.Error()
	}

	renderText := func(w io.Writer) {
		if statusErr == nil {
			fmt.Fprintf(w, "Region:       %s\n", cfg.Region)
			fmt.Fprintf(w, "Domain:       %s\n", cfg.Domain)
			fmt.Fprintf(w, "Output:       %s\n", configuredOutput)
		}
		fmt.Fprintf(w, "Config file:  %s\n", config.ConfigFilePath())
		fmt.Fprintf(w, "Config load:  %t\n", configLoaded)
		if configuredOutput != "" && configuredOutput != effectiveOutput() {
			fmt.Fprintf(w, "Render out:   %s\n", effectiveOutput())
		}
		if statusErr != nil {
			fmt.Fprintf(w, "Config error: %s\n", statusErr.Error())
		}
		fmt.Fprintln(w, "\nAuth:")
		fmt.Fprintf(w, "  Secret ID:  %s\n", formatAuthStatus(secretIDPresent, secretIDSource))
		fmt.Fprintf(w, "  Secret Key: %s\n", formatAuthStatus(secretKeyPresent, secretKeySource))
		fmt.Fprintf(w, "  Token:      %s\n", formatSensitiveAuthStatus(tokenPresent, tokenSource, config.GetToken()))
		if permWarning != "" {
			fmt.Fprintf(ios.ErrOut, "\nWarning: %s\n", permWarning)
		}
	}

	if statusErr != nil {
		cliErr := output.ClassifyError(statusErr)
		return &CmdResult{Data: data, Warnings: warnings, Failure: cliErr.Failure, ExitCode: cliErr.ExitCode, RenderText: renderText}, nil
	}
	return &CmdResult{Data: data, Warnings: warnings, RenderText: renderText}, nil
}

func detectAuthSource(field string) (bool, string) {
	cfg := config.Get()
	switch field {
	case "secret_id":
		if cfg.Auth.SecretID != "" {
			return true, config.GetSource("secret_id")
		}
	case "secret_key":
		if cfg.Auth.SecretKey != "" {
			return true, config.GetSource("secret_key")
		}
	case "token":
		if cfg.Auth.Token != "" {
			return true, config.GetSource("token")
		}
	}
	return false, ""
}

func formatAuthStatus(present bool, source string) string {
	if !present {
		return "not configured"
	}
	return fmt.Sprintf("configured (source: %s)", source)
}

func formatSensitiveAuthStatus(present bool, source, value string) string {
	if !present {
		return "not configured"
	}
	if source == "" {
		return fmt.Sprintf("configured (%s)", maskCredential(value))
	}
	return fmt.Sprintf("configured (%s, source: %s)", maskCredential(value), source)
}

func maskCredential(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "..." + value[len(value)-4:]
}
