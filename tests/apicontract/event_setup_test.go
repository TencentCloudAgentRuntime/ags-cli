package apicontract_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/tests/testutil"
)

// This runs without live credentials. A warm update cache makes the original
// stderr pollution deterministic, without contacting the version endpoint.
func TestOperationalEventLiveCLISuppressesUpdateNotices(t *testing.T) {
	t.Setenv("TENCENTCLOUD_SECRET_ID", "")
	t.Setenv("TENCENTCLOUD_SECRET_KEY", "")
	t.Setenv("TENCENTCLOUD_TOKEN", "")
	t.Setenv("TERM", "xterm")
	t.Setenv("AGR_NON_INTERACTIVE", "1") // Simulate the runner; testutil drops this ambient value.
	repo, err := testutil.FindRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	name := "agr"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	build := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-o", binary, "./cmd/agr")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v: %s", err, out)
	}
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".agr"), 0700); err != nil {
		t.Fatal(err)
	}
	cache, err := json.Marshal(map[string]any{"last_check": time.Now().Unix(), "latest": "999.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agr", "update-check.json"), cache, 0600); err != nil {
		t.Fatal(err)
	}
	cli := &testutil.CLI{BinaryPath: binary, Home: home, Config: testutil.LiveConfig{
		SecretID: "fake", SecretKey: "fake", Region: "ap-guangzhou",
	}, Timeout: 15 * time.Second}
	before := cli.Run(t.Context(), "status", "-o", "text")
	if before.ExitCode != 0 || !strings.Contains(before.Stderr, "Update available:") {
		t.Fatalf("warm-cache control did not reproduce notice: exit=%d stderr=%s", before.ExitCode, before.Stderr)
	}
	after := operationalEventLiveCLI(cli).Run(t.Context(), "status", "-o", "text")
	if after.ExitCode != 0 || after.Stderr != "" || !strings.Contains(after.Stdout, "Region:") {
		t.Fatalf("explicit non-interactive setup failed: exit=%d stderr=%s", after.ExitCode, after.Stderr)
	}
}
