package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRegionUsesProvidedDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AGR_REGION", "")

	if got := ResolveRegion("ap-shanghai"); got != "ap-shanghai" {
		t.Fatalf("ResolveRegion() = %q, want ap-shanghai", got)
	}
}

func TestResolveRegionUsesEnvironmentOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AGR_REGION", "ap-beijing")

	if got := ResolveRegion("ap-shanghai"); got != "ap-beijing" {
		t.Fatalf("ResolveRegion() = %q, want ap-beijing", got)
	}
}

func TestResolveRegionUsesConfigFileOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGR_REGION", "")

	configDir := filepath.Join(home, ".agr")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("region = \"ap-chengdu\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if got := ResolveRegion("ap-shanghai"); got != "ap-chengdu" {
		t.Fatalf("ResolveRegion() = %q, want ap-chengdu", got)
	}
}
