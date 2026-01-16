package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/client"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

var (
	apikeyName string
)

// apikeyCreateCmd represents the apikey create command
var apikeyCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new API key",
	Long: `Create a new API key for Agent Sandbox.

The API key is only displayed once upon creation and cannot be retrieved later.
Make sure to save it securely.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		if err := config.Validate(); err != nil {
			return err
		}

		if config.GetBackend() != "cloud" {
			return fmt.Errorf("API key management is only supported with cloud backend")
		}

		apiClient, err := client.NewClient(config.GetBackend())
		if err != nil {
			return fmt.Errorf("failed to create API client: %w", err)
		}

		if apikeyName == "" {
			return fmt.Errorf("API key name is required (use --name)")
		}

		result, err := apiClient.CreateAPIKey(ctx, apikeyName)
		if err != nil {
			return fmt.Errorf("failed to create API key: %w", err)
		}

		output.PrintSuccess(fmt.Sprintf("API key created: %s", result.KeyID))
		output.PrintWarning("Save this API key securely - it will not be shown again!")

		return output.Print(map[string]string{
			"KeyID":  result.KeyID,
			"Name":   result.Name,
			"APIKey": result.APIKey,
		})
	},
}

// apikeyListCmd represents the apikey list command
var apikeyListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List API keys",
	Long:    `List all API keys for Agent Sandbox.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		if err := config.Validate(); err != nil {
			return err
		}

		if config.GetBackend() != "cloud" {
			return fmt.Errorf("API key management is only supported with cloud backend")
		}

		apiClient, err := client.NewClient(config.GetBackend())
		if err != nil {
			return fmt.Errorf("failed to create API client: %w", err)
		}

		keys, err := apiClient.ListAPIKeys(ctx)
		if err != nil {
			return fmt.Errorf("failed to list API keys: %w", err)
		}

		if len(keys) == 0 {
			output.PrintInfo("No API keys found")
			return nil
		}

		headers := []string{"KEY ID", "NAME", "STATUS", "MASKED KEY", "CREATED"}
		rows := make([][]string, len(keys))
		for i, k := range keys {
			rows[i] = []string{k.KeyID, k.Name, k.Status, k.MaskedKey, k.CreatedAt}
		}

		return output.PrintTable(headers, rows)
	},
}

// apikeyDeleteCmd represents the apikey delete command
var apikeyDeleteCmd = &cobra.Command{
	Use:     "delete <key-id>",
	Aliases: []string{"rm", "del"},
	Short:   "Delete an API key",
	Long:    `Delete an API key by its ID.`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		keyID := args[0]

		if err := config.Validate(); err != nil {
			return err
		}

		if config.GetBackend() != "cloud" {
			return fmt.Errorf("API key management is only supported with cloud backend")
		}

		apiClient, err := client.NewClient(config.GetBackend())
		if err != nil {
			return fmt.Errorf("failed to create API client: %w", err)
		}

		if err := apiClient.DeleteAPIKey(ctx, keyID); err != nil {
			return fmt.Errorf("failed to delete API key: %w", err)
		}

		output.PrintSuccess(fmt.Sprintf("API key deleted: %s", keyID))
		return nil
	},
}

func init() {
	addAPIKeyCommand(rootCmd)
}

// addAPIKeyCommand adds the apikey command to a parent command
func addAPIKeyCommand(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:     "apikey",
		Aliases: []string{"ak", "key"},
		Short:   "Manage API keys",
		Long: `Manage API keys for Agent Sandbox.

API keys can be used to authenticate with Agent Sandbox APIs instead of
using Tencent Cloud SecretID/SecretKey. Note that API keys have limited
permissions compared to cloud credentials.

This feature is only available with the cloud backend.`,
	}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API key",
		Long: `Create a new API key for Agent Sandbox.

The API key is only displayed once upon creation and cannot be retrieved later.
Make sure to save it securely.`,
		RunE: apikeyCreateCmd.RunE,
	}
	createCmd.Flags().StringVarP(&apikeyName, "name", "n", "", "Name for the API key (required)")
	_ = createCmd.MarkFlagRequired("name")
	cmd.AddCommand(createCmd)

	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List API keys",
		Long:    `List all API keys for Agent Sandbox.`,
		RunE:    apikeyListCmd.RunE,
	})

	cmd.AddCommand(&cobra.Command{
		Use:     "delete <key-id>",
		Aliases: []string{"rm", "del"},
		Short:   "Delete an API key",
		Long:    `Delete an API key by its ID.`,
		Args:    cobra.ExactArgs(1),
		RunE:    apikeyDeleteCmd.RunE,
	})

	parent.AddCommand(cmd)
}
