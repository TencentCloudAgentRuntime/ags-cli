package testutil

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// LiveConfig is the live-test configuration loaded from environment variables
// and the user's AGR config file.
type LiveConfig struct {
	SecretID          string
	SecretKey         string
	Region            string
	Domain            string
	CloudEndpoint     string
	ToolID            string
	ToolName          string
	DeploymentToolID  string
	KeepResources     bool
	KeepBinary        bool
	CommandTimeout    time.Duration
	EventuallyTimeout time.Duration
}

// LoadConfig resolves live-test configuration from environment variables first,
// then ~/.agr/config.toml, then test defaults.
func LoadConfig() LiveConfig {
	fileCfg := loadUserAGRConfig()
	return LiveConfig{
		SecretID:          firstNonEmpty(os.Getenv("TENCENTCLOUD_SECRET_ID"), fileCfg.SecretID),
		SecretKey:         firstNonEmpty(os.Getenv("TENCENTCLOUD_SECRET_KEY"), fileCfg.SecretKey),
		Region:            firstNonEmpty(os.Getenv("AGR_REGION"), fileCfg.Region, "ap-guangzhou"),
		Domain:            firstNonEmpty(os.Getenv("AGR_DOMAIN"), fileCfg.Domain),
		CloudEndpoint:     firstNonEmpty(os.Getenv("AGR_CLOUD_ENDPOINT"), fileCfg.CloudEndpoint),
		ToolID:            os.Getenv("AGR_TEST_TOOL_ID"),
		ToolName:          os.Getenv("AGR_TEST_TOOL_NAME"),
		DeploymentToolID:  os.Getenv("AGR_TEST_DEPLOYMENT_TOOL_ID"),
		KeepResources:     parseBoolEnv("AGR_TEST_KEEP_RESOURCES"),
		KeepBinary:        parseBoolEnv("AGR_TEST_KEEP_BINARY"),
		CommandTimeout:    getenvDuration("AGR_TEST_COMMAND_TIMEOUT", 2*time.Minute),
		EventuallyTimeout: getenvDuration("AGR_TEST_EVENTUALLY_TIMEOUT", 8*time.Minute),
	}
}

// Missing returns required live-test settings that were not provided.
func (c LiveConfig) Missing() []string {
	var missing []string
	if c.SecretID == "" {
		missing = append(missing, "TENCENTCLOUD_SECRET_ID")
	}
	if c.SecretKey == "" {
		missing = append(missing, "TENCENTCLOUD_SECRET_KEY")
	}
	return missing
}

type userAGRConfig struct {
	SecretID      string
	SecretKey     string
	Region        string
	Domain        string
	CloudEndpoint string
}

func loadUserAGRConfig() userAGRConfig {
	home, err := os.UserHomeDir()
	if err != nil {
		return userAGRConfig{}
	}
	path := filepath.Join(home, ".agr", "config.toml")
	if _, err := os.Stat(path); err != nil {
		return userAGRConfig{}
	}

	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return userAGRConfig{}
	}
	return userAGRConfig{
		SecretID:      strings.TrimSpace(v.GetString("auth.secret_id")),
		SecretKey:     strings.TrimSpace(v.GetString("auth.secret_key")),
		Region:        strings.TrimSpace(v.GetString("region")),
		Domain:        strings.TrimSpace(v.GetString("domain")),
		CloudEndpoint: strings.TrimSpace(v.GetString("cloud_endpoint")),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseBoolEnv(key string) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return false
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
