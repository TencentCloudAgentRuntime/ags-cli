// Package config loads and validates AGR configuration from defaults, config
// files, environment variables, and command-line overrides.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/viper"
)

// Config is the resolved AGR configuration from defaults, config file, env, and flags.
type Config struct {
	Output        string        `mapstructure:"output"`
	Region        string        `mapstructure:"region"`
	Domain        string        `mapstructure:"domain"`
	CloudEndpoint string        `mapstructure:"cloud_endpoint"`
	Auth          AuthConfig    `mapstructure:"auth"`
	Sandbox       SandboxConfig `mapstructure:"sandbox"`
}

// AuthConfig holds TencentCloud credentials.
type AuthConfig struct {
	SecretID  string `mapstructure:"secret_id"`
	SecretKey string `mapstructure:"secret_key"`
	Token     string `mapstructure:"token"`
}

// SandboxConfig holds sandbox command defaults.
type SandboxConfig struct {
	DefaultUser string `mapstructure:"default_user"`
}

const (
	defaultRegion        = "ap-guangzhou"
	defaultDomain        = "tencentags.com"
	defaultCloudEndpoint = "ags.tencentcloudapi.com"
)

// ControlPlaneEndpoint returns the configured TencentCloud API endpoint.
func (c *Config) ControlPlaneEndpoint() string {
	if c.CloudEndpoint != "" {
		return c.CloudEndpoint
	}
	return defaultCloudEndpoint
}

// DataPlaneDomain returns the base data-plane domain.
func (c *Config) DataPlaneDomain() string { return c.Domain }

// DataPlaneRegionDomain returns the regional data-plane domain.
func (c *Config) DataPlaneRegionDomain() string {
	return fmt.Sprintf("%s.%s", c.Region, c.Domain)
}

var (
	cfg            *Config
	cfgFile        string
	sources        map[string]string
	configFileUsed string
	configFileSeen bool
)

var regionPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// SetConfigFile overrides the config file path used by Init.
func SetConfigFile(path string) { cfgFile = path }

// Init loads configuration from defaults, file, and environment variables.
func Init() error {
	viper.Reset()
	viper.SetConfigType("toml")
	configFileUsed = ""
	configFileSeen = false

	viper.SetDefault("output", "text")
	viper.SetDefault("region", "")
	viper.SetDefault("domain", "")
	viper.SetDefault("cloud_endpoint", "")

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		viper.AddConfigPath(filepath.Join(home, ".agr"))
		viper.SetConfigName("config")
	}

	viper.SetEnvPrefix("AGR")
	viper.AutomaticEnv()
	_ = viper.BindEnv("output", "AGR_OUTPUT")
	_ = viper.BindEnv("region", "AGR_REGION")
	_ = viper.BindEnv("domain", "AGR_DOMAIN")
	_ = viper.BindEnv("cloud_endpoint", "AGR_CLOUD_ENDPOINT")
	_ = viper.BindEnv("auth.secret_id", "TENCENTCLOUD_SECRET_ID")
	_ = viper.BindEnv("auth.secret_key", "TENCENTCLOUD_SECRET_KEY")
	_ = viper.BindEnv("auth.token", "TENCENTCLOUD_TOKEN")
	_ = viper.BindEnv("sandbox.default_user", "AGR_SANDBOX_DEFAULT_USER")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("failed to read config file: %w", err)
		}
	}
	configFileUsed = viper.ConfigFileUsed()
	configFileSeen = configFileUsed != ""

	nextCfg := &Config{}
	if err := viper.Unmarshal(nextCfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	applyDefaults(nextCfg)
	cfg = nextCfg
	initSources()
	return nil
}

func initSources() {
	sources = map[string]string{
		"output":         "default",
		"region":         "default",
		"domain":         "default",
		"cloud_endpoint": "default",
		"secret_id":      "",
		"secret_key":     "",
		"token":          "",
		"default_user":   "default",
	}
	assignSource("output", []string{"AGR_OUTPUT"}, "output")
	assignSource("region", []string{"AGR_REGION"}, "region")
	assignSource("domain", []string{"AGR_DOMAIN"}, "domain")
	assignSource("cloud_endpoint", []string{"AGR_CLOUD_ENDPOINT"}, "cloud_endpoint")
	assignSource("secret_id", []string{"TENCENTCLOUD_SECRET_ID"}, "auth.secret_id")
	assignSource("secret_key", []string{"TENCENTCLOUD_SECRET_KEY"}, "auth.secret_key")
	assignSource("token", []string{"TENCENTCLOUD_TOKEN"}, "auth.token")
	assignSource("default_user", []string{"AGR_SANDBOX_DEFAULT_USER"}, "sandbox.default_user")
}

func assignSource(key string, envVars []string, configKey string) {
	for _, envVar := range envVars {
		if os.Getenv(envVar) != "" {
			sources[key] = envVar
			return
		}
	}
	if viper.InConfig(configKey) {
		sources[key] = "config " + configKey
	}
}

func applyDefaults(c *Config) {
	if c.Output == "" {
		c.Output = "text"
	}
	if c.Region == "" {
		c.Region = defaultRegion
	}
	if c.Domain == "" {
		c.Domain = defaultDomain
	}
}

// CheckConfigFilePermissions returns a warning when a credential-bearing config file is too permissive.
func CheckConfigFilePermissions() string {
	path := configFileUsed
	if path == "" || !configFileContainsCredentials() {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Sprintf("config file %q contains credentials and is readable by group or others; consider chmod 600", path)
	}
	return ""
}

func configFileContainsCredentials() bool {
	return viper.InConfig("auth.secret_id") || viper.InConfig("auth.secret_key") || viper.InConfig("auth.token")
}

// ConfigFilePath returns the active or default config file path.
func ConfigFilePath() string {
	if configFileUsed != "" {
		return configFileUsed
	}
	if cfgFile != "" {
		return cfgFile
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agr", "config.toml")
}

// ConfigFileLoaded reports whether Init loaded a config file.
func ConfigFileLoaded() bool { return configFileUsed != "" }

// ConfigFileFound reports whether Init found a config file.
func ConfigFileFound() bool { return configFileSeen }

// Get returns the process-wide configuration, initializing defaults if needed.
func Get() *Config {
	if cfg == nil {
		cfg = &Config{Output: "text", Region: defaultRegion, Domain: defaultDomain}
	}
	return cfg
}

// GetBackend returns the active backend name.
func GetBackend() string { return "cloud" }

// SetBackend is retained for compatibility with older callers.
func SetBackend(_ string) {}

// GetOutput returns the configured output format.
func GetOutput() string { return Get().Output }

// SetOutput sets the configured output format.
func SetOutput(o string) { Get().Output = o; setSource("output", "flag") }

// GetRegion returns the configured TencentCloud region.
func GetRegion() string { return Get().Region }

// SetRegion sets the configured TencentCloud region.
func SetRegion(r string) { Get().Region = r; setSource("region", "flag") }

// GetDomain returns the configured data-plane domain.
func GetDomain() string { return Get().Domain }

// GetCloudEndpoint returns the resolved control-plane endpoint.
func GetCloudEndpoint() string { return Get().ControlPlaneEndpoint() }

// GetCloudEndpointRaw returns the explicitly configured control-plane endpoint.
func GetCloudEndpointRaw() string { return Get().CloudEndpoint }

// SetCloudEndpoint sets the control-plane endpoint override.
func SetCloudEndpoint(endpoint string) {
	Get().CloudEndpoint = endpoint
	setSource("cloud_endpoint", "flag")
}

// GetAPIKey returns the legacy API key value, which is no longer configured.
func GetAPIKey() string { return "" }

// SetAPIKey is retained for compatibility with older callers.
func SetAPIKey(_ string) {}

// GetSecretID returns the TencentCloud secret ID.
func GetSecretID() string { return Get().Auth.SecretID }

// SetSecretID sets the TencentCloud secret ID.
func SetSecretID(id string) { Get().Auth.SecretID = id; setSource("secret_id", "flag") }

// GetSecretKey returns the TencentCloud secret key.
func GetSecretKey() string { return Get().Auth.SecretKey }

// SetSecretKey sets the TencentCloud secret key.
func SetSecretKey(k string) { Get().Auth.SecretKey = k; setSource("secret_key", "flag") }

// GetToken returns the optional TencentCloud STS session token.
func GetToken() string {
	if GetSource("token") == "flag" {
		return Get().Auth.Token
	}
	if token := os.Getenv("TENCENTCLOUD_TOKEN"); token != "" {
		return token
	}
	return Get().Auth.Token
}

// SetToken sets the optional TencentCloud STS session token.
func SetToken(token string) { Get().Auth.Token = token; setSource("token", "flag") }

// GetSandboxUser returns the default sandbox user.
func GetSandboxUser() string { return Get().Sandbox.DefaultUser }

// SetSandboxUser sets the default sandbox user.
func SetSandboxUser(u string) {
	Get().Sandbox.DefaultUser = u
	setSource("default_user", "flag")
}

// GetSource returns where a config key's current value came from.
func GetSource(key string) string {
	if sources == nil {
		return ""
	}
	return sources[key]
}

// SetDomain sets the data-plane domain.
func SetDomain(domain string) {
	c := Get()
	c.Domain = domain
	setSource("domain", "flag")
}

func setSource(key, source string) {
	if sources == nil {
		sources = map[string]string{}
	}
	sources[key] = source
}

// Validate checks that the current process configuration is usable.
func Validate() error {
	if err := ValidateBasics(); err != nil {
		return err
	}
	c := Get()
	if c.Auth.SecretID == "" || c.Auth.SecretKey == "" {
		return output.NewAuthError("MISSING_CLOUD_CREDENTIALS",
			"cloud API credentials are required",
			"Run: agr init --secret-id <id> --secret-key <key>, or set TENCENTCLOUD_SECRET_ID and TENCENTCLOUD_SECRET_KEY.")
	}
	return nil
}

// ValidateBasics checks non-secret configuration values.
func ValidateBasics() error {
	return ValidateCandidate(*Get())
}

// ValidateCandidate checks a candidate config without making it process-global.
func ValidateCandidate(c Config) error {
	applyDefaults(&c)
	return validateBasics(&c)
}

func validateBasics(c *Config) error {
	if c.Output != "text" && c.Output != "json" && c.Output != "ndjson" {
		return fmt.Errorf("invalid output format: %s (must be 'text', 'json', or 'ndjson')", c.Output)
	}
	if !regionPattern.MatchString(c.Region) {
		return fmt.Errorf("invalid region: %s", c.Region)
	}
	if err := validateDomain(c.Domain); err != nil {
		return err
	}
	if c.CloudEndpoint != "" {
		if err := validateCloudEndpoint(c.CloudEndpoint); err != nil {
			return err
		}
	}
	return nil
}

func validateCloudEndpoint(endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("invalid cloud endpoint: empty (must be a complete hostname like ags.tencentcloudapi.com)")
	}
	if strings.Contains(endpoint, "://") {
		return fmt.Errorf("invalid cloud endpoint: %s (must be a hostname without scheme; use ags.tencentcloudapi.com)", endpoint)
	}
	if strings.Contains(endpoint, "/") {
		return fmt.Errorf("invalid cloud endpoint: %s (must be a hostname without path)", endpoint)
	}
	if strings.ContainsAny(endpoint, " \t\r\n") {
		return fmt.Errorf("invalid cloud endpoint: %s (must not contain whitespace)", endpoint)
	}
	if u, err := url.Parse("https://" + endpoint); err != nil || u.Hostname() == "" || u.Host != endpoint {
		return fmt.Errorf("invalid cloud endpoint: %s (must be a hostname)", endpoint)
	}
	return nil
}

func validateDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("invalid domain: empty")
	}
	if strings.Contains(domain, "/") || strings.Contains(domain, "://") {
		return fmt.Errorf("invalid domain: %s (must be a hostname without scheme or path)", domain)
	}
	if strings.ContainsAny(domain, " \t\r\n") {
		return fmt.Errorf("invalid domain: %s (must not contain whitespace)", domain)
	}
	if u, err := url.Parse("https://" + domain); err != nil || u.Hostname() == "" || u.Host != domain {
		return fmt.Errorf("invalid domain: %s (must be a hostname)", domain)
	}
	return nil
}
