package cli

import (
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

func TestVersionMatchesCompiledChannel(t *testing.T) {
	previous := Version
	defer func() { Version = previous }()
	for _, tc := range []struct {
		version string
		channel apimeta.Channel
	}{
		{"v1.2.3", apimeta.Stable}, {"v1.2.3-preview.1", apimeta.Preview},
	} {
		Version = tc.version
		err := ValidateBuildChannel()
		if (err == nil) != (tc.channel == apimeta.BuildChannel) {
			t.Fatalf("version=%s channel=%s err=%v", Version, apimeta.BuildChannel, err)
		}
	}
	Version = "dev"
	if err := ValidateBuildChannel(); err != nil {
		t.Fatal(err)
	}
}
