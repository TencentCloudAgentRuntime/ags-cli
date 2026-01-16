package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config represents the CLI configuration
type Config struct {
	Backend string      `mapstructure:"backend"`
	Output  string      `mapstructure:"output"`
	E2B     E2BConfig   `mapstructure:"e2b"`
	Cloud   CloudConfig `mapstructure:"cloud"`
}

// E2BConfig represents E2B API configuration
type E2BConfig struct {
	APIKey string `mapstructure:"api_key"`
	Domain string `mapstructure:"domain"`
	Region string `mapstructure:"region"`
}

// CloudConfig represents Tencent Cloud API configuration
type CloudConfig struct {
	SecretID  string `mapstructure:"secret_id"`
	SecretKey string `mapstructure:"secret_key"`
	Region    string `mapstructure:"region"`
	Internal  bool   `mapstructure:"internal"` // Use internal endpoints for both control plane and data plane
}

// ControlPlaneEndpoint returns the control plane API endpoint
func (c *CloudConfig) ControlPlaneEndpoint() string {
	if c.Internal {
		return "ags.internal.tencentcloudapi.com"
	}
	return "ags.tencentcloudapi.com"
}

// DataPlaneDomain returns the data plane domain
func (c *CloudConfig) DataPlaneDomain() string {
	if c.Internal {
		return "internal.tencentags.com"
	}
	return "tencentags.com"
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

	// Set default values
	viper.SetDefault("backend", "e2b")
	viper.SetDefault("output", "text")
	viper.SetDefault("e2b.domain", "tencentags.com")
	viper.SetDefault("e2b.region", "ap-guangzhou")
	viper.SetDefault("cloud.region", "ap-guangzhou")
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

	// Bind specific environment variables
	_ = viper.BindEnv("backend", "AGS_BACKEND")
	_ = viper.BindEnv("output", "AGS_OUTPUT")
	_ = viper.BindEnv("e2b.api_key", "AGS_E2B_API_KEY")
	_ = viper.BindEnv("e2b.domain", "AGS_E2B_DOMAIN")
	_ = viper.BindEnv("e2b.region", "AGS_E2B_REGION")
	_ = viper.BindEnv("cloud.secret_id", "AGS_CLOUD_SECRET_ID")
	_ = viper.BindEnv("cloud.secret_key", "AGS_CLOUD_SECRET_KEY")
	_ = viper.BindEnv("cloud.region", "AGS_CLOUD_REGION")
	_ = viper.BindEnv("cloud.internal", "AGS_CLOUD_INTERNAL")

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

	return nil
}

// Get returns the current configuration
func Get() *Config {
	if cfg == nil {
		cfg = &Config{
			Backend: "e2b",
			Output:  "text",
			E2B: E2BConfig{
				Domain: "tencentags.com",
				Region: "ap-guangzhou",
			},
			Cloud: CloudConfig{
				Region:   "ap-guangzhou",
				Internal: false,
			},
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

// GetE2BConfig returns E2B configuration
func GetE2BConfig() E2BConfig {
	return Get().E2B
}

// SetE2BAPIKey sets E2B API key (for command line override)
func SetE2BAPIKey(key string) {
	Get().E2B.APIKey = key
}

// SetE2BDomain sets E2B domain (for command line override)
func SetE2BDomain(domain string) {
	Get().E2B.Domain = domain
}

// SetE2BRegion sets E2B region (for command line override)
func SetE2BRegion(region string) {
	Get().E2B.Region = region
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
func SetCloudRegion(region string) {
	Get().Cloud.Region = region
}

// SetCloudInternal sets whether to use internal endpoints (for command line override)
func SetCloudInternal(internal bool) {
	Get().Cloud.Internal = internal
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
			return fmt.Errorf("Cloud API credentials are required (set AGS_CLOUD_SECRET_ID/AGS_CLOUD_SECRET_KEY or cloud.secret_id/cloud.secret_key in config)")
		}
	}

	return nil
}
