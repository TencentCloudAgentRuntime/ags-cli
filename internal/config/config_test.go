package config

import (
	"testing"

	"github.com/spf13/viper"
)

// resetGlobals resets package-level state for test isolation.
func resetGlobals() {
	cfg = nil
	cfgFile = ""
	viper.Reset()
}

// TestResolveDeprecatedFields_RegionPriority tests region resolution priority:
// top-level > backend-specific legacy > cross-backend fallback > default
func TestResolveDeprecatedFields_RegionPriority(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		expectedRegion string
	}{
		{
			name:           "default region when nothing set",
			config:         Config{Backend: "e2b"},
			expectedRegion: defaultRegion,
		},
		{
			name: "top-level region takes priority over legacy",
			config: Config{
				Backend: "cloud",
				Region:  "ap-shanghai",
				Cloud:   CloudConfig{Region: "ap-beijing"},
			},
			expectedRegion: "ap-shanghai",
		},
		{
			name: "cloud backend uses cloud.region as legacy",
			config: Config{
				Backend: "cloud",
				Cloud:   CloudConfig{Region: "ap-beijing"},
			},
			expectedRegion: "ap-beijing",
		},
		{
			name: "e2b backend uses e2b.region as legacy",
			config: Config{
				Backend: "e2b",
				E2B:     E2BConfig{Region: "ap-tokyo"},
			},
			expectedRegion: "ap-tokyo",
		},
		{
			name: "cross-backend fallback: e2b backend falls back to cloud.region",
			config: Config{
				Backend: "e2b",
				Cloud:   CloudConfig{Region: "ap-beijing"},
			},
			expectedRegion: "ap-beijing",
		},
		{
			name: "cross-backend fallback: cloud backend falls back to e2b.region",
			config: Config{
				Backend: "cloud",
				E2B:     E2BConfig{Region: "ap-tokyo"},
			},
			expectedRegion: "ap-tokyo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetGlobals()
			c := tt.config
			resolveDeprecatedFields(&c)
			if c.Region != tt.expectedRegion {
				t.Errorf("expected region %q, got %q", tt.expectedRegion, c.Region)
			}
		})
	}
}

// TestResolveDeprecatedFields_DomainPriority tests domain resolution priority:
// top-level > e2b.domain > default
func TestResolveDeprecatedFields_DomainPriority(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		expectedDomain string
	}{
		{
			name:           "default domain when nothing set",
			config:         Config{Backend: "e2b"},
			expectedDomain: defaultDomain,
		},
		{
			name: "top-level domain takes priority over e2b.domain",
			config: Config{
				Backend: "e2b",
				Domain:  "custom.com",
				E2B:     E2BConfig{Domain: "legacy.com"},
			},
			expectedDomain: "custom.com",
		},
		{
			name: "e2b.domain used as legacy fallback",
			config: Config{
				Backend: "e2b",
				E2B:     E2BConfig{Domain: "legacy.com"},
			},
			expectedDomain: "legacy.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetGlobals()
			c := tt.config
			resolveDeprecatedFields(&c)
			if c.Domain != tt.expectedDomain {
				t.Errorf("expected domain %q, got %q", tt.expectedDomain, c.Domain)
			}
		})
	}
}

// TestResolveDeprecatedFields_InternalResolution tests internal flag resolution,
// including the viper.IsSet distinction between explicit false and default false.
func TestResolveDeprecatedFields_InternalResolution(t *testing.T) {
	tests := []struct {
		name             string
		config           Config
		viperSetInternal bool // whether to call viper.Set("internal", ...)
		viperInternalVal bool // value to set if viperSetInternal is true
		expectedInternal bool
		expectedDomain   string
	}{
		{
			name:             "cloud.internal=true promotes to top-level when internal not set",
			config:           Config{Backend: "cloud", Cloud: CloudConfig{Internal: true}},
			expectedInternal: true,
			expectedDomain:   "internal." + defaultDomain,
		},
		{
			name:             "cloud.internal=false does not change default",
			config:           Config{Backend: "cloud", Cloud: CloudConfig{Internal: false}},
			expectedInternal: false,
			expectedDomain:   defaultDomain,
		},
		{
			name:             "explicit internal=false in viper blocks cloud.internal=true override",
			config:           Config{Backend: "cloud", Cloud: CloudConfig{Internal: true}},
			viperSetInternal: true,
			viperInternalVal: false,
			expectedInternal: false,
			expectedDomain:   defaultDomain,
		},
		{
			name:             "explicit internal=true in viper with cloud.internal=false",
			config:           Config{Backend: "cloud", Internal: true, Cloud: CloudConfig{Internal: false}},
			viperSetInternal: true,
			viperInternalVal: true,
			expectedInternal: true,
			expectedDomain:   "internal." + defaultDomain,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetGlobals()
			if tt.viperSetInternal {
				viper.Set("internal", tt.viperInternalVal)
			}
			c := tt.config
			resolveDeprecatedFields(&c)
			if c.Internal != tt.expectedInternal {
				t.Errorf("expected internal=%v, got %v", tt.expectedInternal, c.Internal)
			}
			if c.Domain != tt.expectedDomain {
				t.Errorf("expected domain %q, got %q", tt.expectedDomain, c.Domain)
			}
		})
	}
}

// TestResolveDeprecatedFields_InternalDomainNormalization tests domain normalization
// when internal=true, including the "internal." prefix handling.
func TestResolveDeprecatedFields_InternalDomainNormalization(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		expectedDomain string
	}{
		{
			name: "internal=true adds internal. prefix to default domain",
			config: Config{
				Backend:  "e2b",
				Internal: true,
			},
			expectedDomain: "internal." + defaultDomain,
		},
		{
			name: "internal=true adds internal. prefix to custom domain",
			config: Config{
				Backend:  "e2b",
				Domain:   "custom.com",
				Internal: true,
			},
			expectedDomain: "internal.custom.com",
		},
		{
			name: "internal=true does not double-prepend if domain already has prefix",
			config: Config{
				Backend:  "e2b",
				Domain:   "internal.custom.com",
				Internal: true,
			},
			expectedDomain: "internal.custom.com",
		},
		{
			name: "internal=false leaves domain unchanged",
			config: Config{
				Backend:  "e2b",
				Domain:   "custom.com",
				Internal: false,
			},
			expectedDomain: "custom.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetGlobals()
			c := tt.config
			resolveDeprecatedFields(&c)
			if c.Domain != tt.expectedDomain {
				t.Errorf("expected domain %q, got %q", tt.expectedDomain, c.Domain)
			}
		})
	}
}

// TestSetInternal tests the SetInternal function's domain normalization behavior.
func TestSetInternal(t *testing.T) {
	tests := []struct {
		name           string
		initialDomain  string
		setInternal    bool
		expectedDomain string
	}{
		{
			name:           "SetInternal(true) adds prefix",
			initialDomain:  "tencentags.com",
			setInternal:    true,
			expectedDomain: "internal.tencentags.com",
		},
		{
			name:           "SetInternal(true) does not double-prepend",
			initialDomain:  "internal.tencentags.com",
			setInternal:    true,
			expectedDomain: "internal.tencentags.com",
		},
		{
			name:           "SetInternal(false) removes prefix",
			initialDomain:  "internal.tencentags.com",
			setInternal:    false,
			expectedDomain: "tencentags.com",
		},
		{
			name:           "SetInternal(false) leaves domain without prefix unchanged",
			initialDomain:  "tencentags.com",
			setInternal:    false,
			expectedDomain: "tencentags.com",
		},
		{
			name:           "SetInternal(true) with custom domain",
			initialDomain:  "custom.example.com",
			setInternal:    true,
			expectedDomain: "internal.custom.example.com",
		},
		{
			name:           "SetInternal(false) removes prefix from custom domain",
			initialDomain:  "internal.custom.example.com",
			setInternal:    false,
			expectedDomain: "custom.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetGlobals()
			cfg = &Config{
				Domain: tt.initialDomain,
			}
			SetInternal(tt.setInternal)
			if cfg.Domain != tt.expectedDomain {
				t.Errorf("expected domain %q, got %q", tt.expectedDomain, cfg.Domain)
			}
			if cfg.Internal != tt.setInternal {
				t.Errorf("expected internal=%v, got %v", tt.setInternal, cfg.Internal)
			}
		})
	}
}

// TestSetDomain tests the SetDomain function's normalization with internal flag.
func TestSetDomain(t *testing.T) {
	tests := []struct {
		name            string
		initialInternal bool
		initialDomain   string
		newDomain       string
		expectedDomain  string
	}{
		{
			name:            "SetDomain with internal=false keeps domain as-is",
			initialInternal: false,
			initialDomain:   defaultDomain,
			newDomain:       "custom.com",
			expectedDomain:  "custom.com",
		},
		{
			name:            "SetDomain with internal=true adds prefix",
			initialInternal: true,
			initialDomain:   "internal." + defaultDomain,
			newDomain:       "custom.com",
			expectedDomain:  "internal.custom.com",
		},
		{
			name:            "SetDomain with internal=true and domain already has prefix",
			initialInternal: true,
			initialDomain:   "internal." + defaultDomain,
			newDomain:       "internal.custom.com",
			expectedDomain:  "internal.custom.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetGlobals()
			cfg = &Config{
				Internal: tt.initialInternal,
				Domain:   tt.initialDomain,
			}
			SetDomain(tt.newDomain)
			if cfg.Domain != tt.expectedDomain {
				t.Errorf("expected domain %q, got %q", tt.expectedDomain, cfg.Domain)
			}
		})
	}
}

// TestSetCloudInternal tests that SetCloudInternal syncs to top-level internal.
func TestSetCloudInternal(t *testing.T) {
	tests := []struct {
		name             string
		initialDomain    string
		setInternal      bool
		expectedInternal bool
		expectedDomain   string
	}{
		{
			name:             "SetCloudInternal(true) syncs to top-level",
			initialDomain:    defaultDomain,
			setInternal:      true,
			expectedInternal: true,
			expectedDomain:   "internal." + defaultDomain,
		},
		{
			name:             "SetCloudInternal(false) syncs to top-level and removes prefix",
			initialDomain:    "internal." + defaultDomain,
			setInternal:      false,
			expectedInternal: false,
			expectedDomain:   defaultDomain,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetGlobals()
			cfg = &Config{
				Domain: tt.initialDomain,
			}
			SetCloudInternal(tt.setInternal)
			if cfg.Internal != tt.expectedInternal {
				t.Errorf("expected internal=%v, got %v", tt.expectedInternal, cfg.Internal)
			}
			if cfg.Cloud.Internal != tt.setInternal {
				t.Errorf("expected cloud.internal=%v, got %v", tt.setInternal, cfg.Cloud.Internal)
			}
			if cfg.Domain != tt.expectedDomain {
				t.Errorf("expected domain %q, got %q", tt.expectedDomain, cfg.Domain)
			}
		})
	}
}

// TestEndpointMethods tests the endpoint construction methods.
func TestEndpointMethods(t *testing.T) {
	tests := []struct {
		name                    string
		config                  Config
		expectedControlPlane    string
		expectedDataPlaneDomain string
		expectedDataPlaneRegion string
		expectedE2BControlPlane string
	}{
		{
			name: "default config",
			config: Config{
				Region: defaultRegion,
				Domain: defaultDomain,
			},
			expectedControlPlane:    "ags.tencentcloudapi.com",
			expectedDataPlaneDomain: defaultDomain,
			expectedDataPlaneRegion: "ap-guangzhou.tencentags.com",
			expectedE2BControlPlane: "https://api.ap-guangzhou.tencentags.com",
		},
		{
			name: "internal mode",
			config: Config{
				Region:   defaultRegion,
				Domain:   "internal." + defaultDomain,
				Internal: true,
			},
			expectedControlPlane:    "ags.internal.tencentcloudapi.com",
			expectedDataPlaneDomain: "internal." + defaultDomain,
			expectedDataPlaneRegion: "ap-guangzhou.internal.tencentags.com",
			expectedE2BControlPlane: "https://api.ap-guangzhou.internal.tencentags.com",
		},
		{
			name: "custom region and domain",
			config: Config{
				Region: "ap-shanghai",
				Domain: "custom.com",
			},
			expectedControlPlane:    "ags.tencentcloudapi.com",
			expectedDataPlaneDomain: "custom.com",
			expectedDataPlaneRegion: "ap-shanghai.custom.com",
			expectedE2BControlPlane: "https://api.ap-shanghai.custom.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.config
			if got := c.ControlPlaneEndpoint(); got != tt.expectedControlPlane {
				t.Errorf("ControlPlaneEndpoint() = %q, want %q", got, tt.expectedControlPlane)
			}
			if got := c.DataPlaneDomain(); got != tt.expectedDataPlaneDomain {
				t.Errorf("DataPlaneDomain() = %q, want %q", got, tt.expectedDataPlaneDomain)
			}
			if got := c.DataPlaneRegionDomain(); got != tt.expectedDataPlaneRegion {
				t.Errorf("DataPlaneRegionDomain() = %q, want %q", got, tt.expectedDataPlaneRegion)
			}
			if got := c.E2BControlPlaneEndpoint(); got != tt.expectedE2BControlPlane {
				t.Errorf("E2BControlPlaneEndpoint() = %q, want %q", got, tt.expectedE2BControlPlane)
			}
		})
	}
}

// TestValidate tests the config validation logic.
func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		expectErr bool
	}{
		{
			name:      "invalid backend",
			config:    Config{Backend: "invalid", Output: "text"},
			expectErr: true,
		},
		{
			name:      "invalid output",
			config:    Config{Backend: "e2b", Output: "xml"},
			expectErr: true,
		},
		{
			name:      "e2b missing api key",
			config:    Config{Backend: "e2b", Output: "text"},
			expectErr: true,
		},
		{
			name:      "e2b valid",
			config:    Config{Backend: "e2b", Output: "text", E2B: E2BConfig{APIKey: "key"}},
			expectErr: false,
		},
		{
			name:      "cloud missing credentials",
			config:    Config{Backend: "cloud", Output: "text"},
			expectErr: true,
		},
		{
			name: "cloud valid",
			config: Config{
				Backend: "cloud", Output: "text",
				Cloud: CloudConfig{SecretID: "id", SecretKey: "key"},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetGlobals()
			cfg = &tt.config
			err := Validate()
			if tt.expectErr && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("expected no error but got: %v", err)
			}
		})
	}
}
