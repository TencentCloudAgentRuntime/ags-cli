package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

// TestSchemaJSONGoldenSnapshot pins the whole `agr schema -o json` contract for
// the channel this test binary was built for, so any drift in Args, Flags,
// Failures or SupportsDryRun shows up as a reviewable file diff instead of a
// silent agent-facing contract change. Refresh with UPDATE_GOLDEN=1.
func TestSchemaJSONGoldenSnapshot(t *testing.T) {
	result := runSchemaCLI(t, "schema", "-o", "json")
	if result.exitCode != 0 {
		t.Fatalf("agr schema -o json: exit=%d stderr=%s", result.exitCode, result.stderr)
	}
	if len(result.stderr) != 0 {
		t.Fatalf("stderr must stay empty in JSON mode, got %q", result.stderr)
	}
	got := normalizeSchemaGolden(t, result.stdout)
	if bytes.Contains(got, []byte(result.home)) {
		t.Fatal("golden snapshot must not contain absolute local paths")
	}

	path := filepath.Join("testdata", "schema_"+string(apimeta.BuildChannel)+".golden.json")
	if os.Getenv(updateGoldenEnv) == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run UPDATE_GOLDEN=1 to create it): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale; rerun with UPDATE_GOLDEN=1\nfirst difference: %s", path, firstLineDifference(want, got))
	}

	// A copy/paste between channels would leave both files plausible but wrong.
	hasHarness := bytes.Contains(got, []byte(`"Name": "harness.create"`))
	if wantHarness := apimeta.BuildChannel == apimeta.Preview; hasHarness != wantHarness {
		t.Fatalf("golden snapshot contains preview-only harness.create=%t for channel %s", hasHarness, apimeta.BuildChannel)
	}
	if !strings.Contains(string(got), `"SchemaVersion": "agr.v1"`) {
		t.Fatal("golden snapshot is not a standard agr.v1 envelope")
	}
}
