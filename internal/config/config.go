package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config represents the CLI configuration
type Config struct {
	Backend  string        `mapstructure:"backend"`
	Output   string        `mapstructure:"output"`
	Region   string        `mapstructure:"region"`   // Unified region for both control plane and data plane
	Domain   string        `mapstructure:"domain"`   // Unified base domain (normalized: includes "internal." prefix when internal mode is enabled)
	Internal bool          `mapstructure:"internal"` // Use internal endpoints for both control plane and data plane
	E2B      E2BConfig     `mapstructure:"e2b"`
	Cloud    CloudConfig   `mapstructure:"cloud"`
	Sandbox  SandboxConfig `mapstructure:"sandbox"`
}

// E2BConfig represents E2B API configuration
type E2BConfig struct {
	APIKey string `mapstructure:"api_key"`
	// Deprecated: Use top-level "domain" instead. Will be removed in a future version.
	Domain string `mapstructure:"domain"`
	// Deprecated: Use top-level "region" instead. Will be removed in a future version.
	Region string `mapstructure:"region"`
}

// CloudConfig represents Tencent Cloud API configuration
type CloudConfig struct {
	SecretID  string `mapstructure:"secret_id"`
	SecretKey string `mapstructure:"secret_key"`
	// Deprecated: Use top-level "region" instead. Will be removed in a future version.
	Region string `mapstructure:"region"`
	// Deprecated: Use top-level "internal" instead. Will be removed in a future version.
	Internal bool `mapstructure:"internal"`
}

// SandboxConfig represents sandbox-level configuration
type SandboxConfig struct {
	DefaultUser string `mapstructure:"default_user"`
}

const (
	defaultRegion = "ap-guangzhou"
	defaultDomain = "tencentags.com"
)

// ControlPlaneEndpoint returns the control plane API endpoint for cloud backend.
// Note: Cloud control plane uses "tencentcloudapi.com" domain system, which is separate from
// the data plane "tencentags.com" domain. Therefore this endpoint is hardcoded and does not
// use c.Domain (which only applies to data plane).
func (c *Config) ControlPlaneEndpoint() string {
	if c.Internal {
		return "ags.internal.tencentcloudapi.com"
	}
	return "ags.tencentcloudapi.com"
}

// DataPlaneDomain returns the base data plane domain.
// After normalization in resolveDeprecatedFields, Domain already includes "internal." prefix
// when internal mode is enabled (e.g., "internal.tencentags.com").
func (c *Config) DataPlaneDomain() string {
	return c.Domain
}

// DataPlaneRegionDomain returns the region-qualified data plane domain
// (e.g., "ap-guangzhou.tencentags.com" or "ap-guangzhou.internal.tencentags.com").
// This is used for constructing sandbox connection URLs.
func (c *Config) DataPlaneRegionDomain() string {
	return fmt.Sprintf("%s.%s", c.Region, c.Domain)
}

// E2BControlPlaneEndpoint returns the E2B control plane API endpoint
// (e.g., "https://api.ap-guangzhou.tencentags.com" or "https://api.ap-guangzhou.internal.tencentags.com").
func (c *Config) E2BControlPlaneEndpoint() string {
	return fmt.Sprintf("https://api.%s.%s", c.Region, c.Domain)
}

var (
	cfg     *Config
	cfgFile string
)

// SetConfigFile sets the config file path
func SetConfigFile(path string) {
	cfgFile = path
}

// Init initializes the configuration
func Init() error {
	viper.SetConfigType("toml")

	// Set default values - top-level unified fields
	viper.SetDefault("backend", "e2b")
	viper.SetDefault("output", "text")
	viper.SetDefault("region", "")
	viper.SetDefault("domain", "")
	viper.SetDefault("internal", false)

	// Legacy defaults for backward compatibility
	viper.SetDefault("e2b.domain", "")
	viper.SetDefault("e2b.region", "")
	viper.SetDefault("cloud.region", "")
	viper.SetDefault("cloud.internal", false)

	// Config file path
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		configDir := filepath.Join(home, ".ags")
		viper.AddConfigPath(configDir)
		viper.SetConfigName("config")
	}

	// Environment variable bindings
	viper.SetEnvPrefix("AGS")
	viper.AutomaticEnv()

	// Bind specific environment variables - top-level unified fields
	_ = viper.BindEnv("backend", "AGS_BACKEND")
	_ = viper.BindEnv("output", "AGS_OUTPUT")
	_ = viper.BindEnv("region", "AGS_REGION")
	_ = viper.BindEnv("domain", "AGS_DOMAIN")
	_ = viper.BindEnv("internal", "AGS_INTERNAL")

	// Legacy environment variable bindings (for backward compatibility)
	_ = viper.BindEnv("e2b.api_key", "AGS_E2B_API_KEY")
	_ = viper.BindEnv("e2b.domain", "AGS_E2B_DOMAIN")
	_ = viper.BindEnv("e2b.region", "AGS_E2B_REGION")
	_ = viper.BindEnv("cloud.secret_id", "AGS_CLOUD_SECRET_ID")
	_ = viper.BindEnv("cloud.secret_key", "AGS_CLOUD_SECRET_KEY")
	_ = viper.BindEnv("cloud.region", "AGS_CLOUD_REGION")
	_ = viper.BindEnv("cloud.internal", "AGS_CLOUD_INTERNAL")
	_ = viper.BindEnv("sandbox.default_user", "AGS_SANDBOX_DEFAULT_USER")

	// Read config file (ignore if not found)
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("failed to read config file: %w", err)
		}
	}

	cfg = &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Resolve unified fields from legacy fields with deprecation warnings
	resolveDeprecatedFields(cfg)

	return nil
}

// resolveDeprecatedFields merges deprecated [e2b]/[cloud] fields into top-level unified fields.
// Priority: top-level > legacy (based on current backend) > default
func resolveDeprecatedFields(c *Config) {
	// Resolve Region: top-level > cloud.region / e2b.region (based on backend) > default
	if c.Region == "" {
		switch c.Backend {
		case "cloud":
			if c.Cloud.Region != "" {
				c.Region = c.Cloud.Region
				printDeprecationWarning("cloud.region", "region")
			}
		case "e2b":
			if c.E2B.Region != "" {
				c.Region = c.E2B.Region
				printDeprecationWarning("e2b.region", "region")
			}
		}
		// Also check the other backend's region as fallback (silent, no deprecation warning
		// since the field belongs to a different backend than the one currently in use)
		if c.Region == "" {
			if c.Cloud.Region != "" {
				c.Region = c.Cloud.Region
			} else if c.E2B.Region != "" {
				c.Region = c.E2B.Region
			}
		}
		if c.Region == "" {
			c.Region = defaultRegion
		}
	}

	// Resolve Domain: top-level > e2b.domain > default
	if c.Domain == "" {
		if c.E2B.Domain != "" {
			c.Domain = c.E2B.Domain
			printDeprecationWarning("e2b.domain", "domain")
		} else {
			c.Domain = defaultDomain
		}
	}

	// Resolve Internal: top-level > cloud.internal
	// Use viper.IsSet to distinguish "user explicitly set internal=false" from "default false".
	// Without this check, cloud.internal=true would incorrectly override an explicit internal=false.
	if !viper.IsSet("internal") && c.Cloud.Internal {
		c.Internal = true
		printDeprecationWarning("cloud.internal", "internal")
	}

	// Normalize: merge Internal flag into Domain.
	// This ensures all endpoint functions can simply use c.Domain without branching on c.Internal.
	// The "internal" segment sits between region and base domain: {prefix}.{region}.internal.{domain}
	// By prepending "internal." to Domain here, all downstream fmt.Sprintf("%s.%s", region, domain)
	// calls automatically produce the correct result.
	if c.Internal && !strings.HasPrefix(c.Domain, "internal.") {
		c.Domain = "internal." + c.Domain
	}
}

// printDeprecationWarning prints a deprecation warning for a legacy config field
func printDeprecationWarning(oldField, newField string) {
	fmt.Fprintf(os.Stderr, "Warning: config field \"%s\" is deprecated, please use top-level \"%s\" instead.\n", oldField, newField)
}

// Get returns the current configuration
func Get() *Config {
	if cfg == nil {
		cfg = &Config{
			Backend:  "e2b",
			Output:   "text",
			Region:   defaultRegion,
			Domain:   defaultDomain,
			Internal: false,
		}
	}
	return cfg
}

// GetBackend returns the current backend type
func GetBackend() string {
	return Get().Backend
}

// SetBackend sets the backend type (for command line override)
func SetBackend(backend string) {
	Get().Backend = backend
}

// GetOutput returns the current output format
func GetOutput() string {
	return Get().Output
}

// SetOutput sets the output format (for command line override)
func SetOutput(output string) {
	Get().Output = output
}

// GetRegion returns the unified region
func GetRegion() string {
	return Get().Region
}

// SetRegion sets the unified region (for command line override)
func SetRegion(region string) {
	Get().Region = region
}

// GetDomain returns the unified domain
func GetDomain() string {
	return Get().Domain
}

// SetDomain sets the unified domain (for command line override)
func SetDomain(domain string) {
	c := Get()
	c.Domain = domain
	// Re-normalize: if internal is enabled, ensure domain has the prefix
	if c.Internal && !strings.HasPrefix(c.Domain, "internal.") {
		c.Domain = "internal." + c.Domain
	}
}

// GetInternal returns whether to use internal endpoints
func GetInternal() bool {
	return Get().Internal
}

// SetInternal sets whether to use internal endpoints (for command line override)
func SetInternal(internal bool) {
	c := Get()
	c.Internal = internal
	// Normalize domain based on new internal flag
	if internal && !strings.HasPrefix(c.Domain, "internal.") {
		c.Domain = "internal." + c.Domain
	} else if !internal && strings.HasPrefix(c.Domain, "internal.") {
		c.Domain = strings.TrimPrefix(c.Domain, "internal.")
	}
}

// GetE2BConfig returns E2B configuration
func GetE2BConfig() E2BConfig {
	return Get().E2B
}

// SetE2BAPIKey sets E2B API key (for command line override)
func SetE2BAPIKey(key string) {
	Get().E2B.APIKey = key
}

// SetE2BDomain sets E2B domain (for command line override)
// Deprecated: Use SetDomain instead.
// Note: This function is only called from the legacy --e2b-domain flag path in initConfig.
// The condition below checks whether the top-level domain is still at its default value;
// if so, it syncs to the unified field. Even if it incorrectly syncs (e.g., user explicitly
// set domain to the default), the unified --domain flag is always applied after legacy flags,
// so it will correct any over-write.
func SetE2BDomain(domain string) {
	Get().E2B.Domain = domain
	// Also update top-level domain if not explicitly set
	if Get().Domain == defaultDomain || Get().Domain == "internal."+defaultDomain {
		SetDomain(domain)
	}
}

// SetE2BRegion sets E2B region (for command line override)
// Deprecated: Use SetRegion instead.
func SetE2BRegion(region string) {
	Get().E2B.Region = region
	// Also update top-level region if not explicitly set
	if Get().Region == defaultRegion {
		Get().Region = region
	}
}

// GetCloudConfig returns Cloud API configuration
func GetCloudConfig() CloudConfig {
	return Get().Cloud
}

// SetCloudSecretID sets Cloud API SecretID (for command line override)
func SetCloudSecretID(id string) {
	Get().Cloud.SecretID = id
}

// SetCloudSecretKey sets Cloud API SecretKey (for command line override)
func SetCloudSecretKey(key string) {
	Get().Cloud.SecretKey = key
}

// SetCloudRegion sets Cloud API region (for command line override)
// Deprecated: Use SetRegion instead.
func SetCloudRegion(region string) {
	Get().Cloud.Region = region
	// Also update top-level region if not explicitly set
	if Get().Region == defaultRegion {
		Get().Region = region
	}
}

// SetCloudInternal sets whether to use internal endpoints (for command line override)
// Deprecated: Use SetInternal instead.
func SetCloudInternal(internal bool) {
	Get().Cloud.Internal = internal
	// Always sync to top-level internal
	SetInternal(internal)
}

// GetSandboxUser returns the default sandbox user
func GetSandboxUser() string {
	return Get().Sandbox.DefaultUser
}

// SetSandboxUser sets the default sandbox user (for command line override)
func SetSandboxUser(user string) {
	Get().Sandbox.DefaultUser = user
}

// Validate validates the configuration
func Validate() error {
	c := Get()
	if c.Backend != "e2b" && c.Backend != "cloud" {
		return fmt.Errorf("invalid backend: %s (must be 'e2b' or 'cloud')", c.Backend)
	}
	if c.Output != "text" && c.Output != "json" {
		return fmt.Errorf("invalid output format: %s (must be 'text' or 'json')", c.Output)
	}

	switch c.Backend {
	case "e2b":
		if c.E2B.APIKey == "" {
			return fmt.Errorf("E2B API key is required (set AGS_E2B_API_KEY or e2b.api_key in config)")
		}
	case "cloud":
		if c.Cloud.SecretID == "" || c.Cloud.SecretKey == "" {
			return fmt.Errorf("cloud API credentials are required (set AGS_CLOUD_SECRET_ID/AGS_CLOUD_SECRET_KEY or cloud.secret_id/cloud.secret_key in config)")
		}
	}

	return nil
}
