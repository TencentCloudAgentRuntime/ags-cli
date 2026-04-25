package cmd

import (
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/spf13/cobra"
)

// TestBuildWebshellURL verifies that buildWebshellURL correctly includes
// the access_token query parameter when a token is provided, and omits it
// entirely when the token is empty (i.e., --no-auth mode).
func TestBuildWebshellURL(t *testing.T) {
	// Snapshot current config and restore after the test to avoid cross-test
	// pollution.
	original := *config.Get()
	t.Cleanup(func() {
		*config.Get() = original
	})

	cfg := config.Get()
	cfg.Region = "ap-guangzhou"
	cfg.Domain = "tencentags.com"
	cfg.Internal = false

	tests := []struct {
		name        string
		instanceID  string
		accessToken string
		want        string
	}{
		{
			name:        "with token",
			instanceID:  "sbi-abc123",
			accessToken: "tok-xyz",
			want:        "https://8080-sbi-abc123.ap-guangzhou.tencentags.com/?access_token=tok-xyz",
		},
		{
			name:        "empty token omits access_token parameter",
			instanceID:  "sbi-abc123",
			accessToken: "",
			want:        "https://8080-sbi-abc123.ap-guangzhou.tencentags.com/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildWebshellURL(tt.instanceID, tt.accessToken)
			if got != tt.want {
				t.Errorf("buildWebshellURL(%q, %q) = %q, want %q",
					tt.instanceID, tt.accessToken, got, tt.want)
			}
			if tt.accessToken == "" && strings.Contains(got, "access_token") {
				t.Errorf("expected no access_token parameter when token is empty, got %q", got)
			}
		})
	}
}

// TestInstanceLoginNoAuthFlag verifies that the --no-auth flag is wired up on
// the instance login command with the expected default and usage.
func TestInstanceLoginNoAuthFlag(t *testing.T) {
	instCmd := findSubcommand(rootCmd, "instance")
	if instCmd == nil {
		t.Fatal("instance command not registered on rootCmd")
	}
	loginCmd := findSubcommand(instCmd, "login")
	if loginCmd == nil {
		t.Fatal("instance login command not registered")
	}

	flag := loginCmd.Flags().Lookup("no-auth")
	if flag == nil {
		t.Fatal("--no-auth flag not registered on instance login")
	}
	if flag.DefValue != "false" {
		t.Errorf("--no-auth default = %q, want \"false\"", flag.DefValue)
	}
	if !strings.Contains(strings.ToLower(flag.Usage), "token") {
		t.Errorf("--no-auth usage should mention token, got %q", flag.Usage)
	}
}

// findSubcommand returns the direct child command with the given name, or nil.
func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}
