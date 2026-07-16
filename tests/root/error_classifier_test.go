// Package root_test contains credential-free smoke tests for the unified error
// classifier. These tests run in any CI environment because they do not call
// the remote API and do not require AGR_LIVE_TEST or cloud credentials.
//
// They use the standard go test framework (not Ginkgo) so they are not bound
// to testutil.SetupSuite, which ginkgo.Skip()s the entire suite when
// credentials are missing.
package root_test

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"testing"
)

// errorClassifierTestBinary builds the agr binary into a temp dir.
func errorClassifierTestBinary(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD failed: %v", err)
	}
	mod := string(bytes.TrimSpace(out))
	repoRoot := mod[:len(mod)-len("/go.mod")]

	dir := t.TempDir()
	bin := dir + "/agr"
	// #nosec G204 -- test-only command with controlled args.
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/agr")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}

func runAgr(t *testing.T, bin, home string, args ...string) (stdout, stderr string) {
	t.Helper()
	cmd := exec.Command(bin, args...) // #nosec G204 -- test-only.
	cmd.Env = append([]string{"HOME=" + home, "PATH=/usr/bin:/bin"}, "")
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	_ = cmd.Run()
	return so.String(), se.String()
}

func TestErrorClassifierNoCredentials(t *testing.T) {
	bin := errorClassifierTestBinary(t)
	home := t.TempDir()

	t.Run("FixAndExplainShownForRealError", func(t *testing.T) {
		// Use a resource that doesn't exist. The error path may be:
		//  - not_found (resource missing in cloud) → Fix + explain
		//  - auth (no credentials) → Fix + explain
		// Both should show Fix and agr explain in stderr.
		_, stderr := runAgr(t, bin, home, "instance", "get", "ins-nonexistent-test-id-zzz")
		if !bytes.Contains([]byte(stderr), []byte("Fix:")) {
			t.Fatalf("expected 'Fix:' in stderr, got: %q", stderr)
		}
		if !bytes.Contains([]byte(stderr), []byte("agr explain")) {
			t.Fatalf("expected 'agr explain' suggestion, got: %q", stderr)
		}
	})

	t.Run("JSONFailureHasFixField", func(t *testing.T) {
		stdout, _ := runAgr(t, bin, home, "--output", "json", "instance", "get", "ins-nonexistent-test-id-zzz")
		var env struct {
			Failure struct {
				Code string `json:"Code"`
				Kind string `json:"Kind"`
				Fix  string `json:"Fix"`
			} `json:"Failure"`
		}
		if err := json.Unmarshal([]byte(stdout), &env); err != nil {
			t.Fatalf("invalid JSON envelope: %v\nstdout=%q", err, stdout)
		}
		if env.Failure.Code == "" {
			t.Fatal("expected non-empty Code in JSON Failure")
		}
		// Fix should be populated for non-usage errors.
		if env.Failure.Kind != "usage" && env.Failure.Fix == "" {
			t.Fatalf("expected Fix to be set for kind=%q, got empty", env.Failure.Kind)
		}
	})

	t.Run("UsageErrorDoesNotShowExplain", func(t *testing.T) {
		_, stderr := runAgr(t, bin, home, "instance", "create")
		if bytes.Contains([]byte(stderr), []byte("agr explain")) {
			t.Fatalf("usage error should not show explain suggestion, got: %q", stderr)
		}
	})

	t.Run("UnknownCommandDoesNotShowExplain", func(t *testing.T) {
		_, stderr := runAgr(t, bin, home, "nonexistent-cmd-xyz")
		if bytes.Contains([]byte(stderr), []byte("agr explain")) {
			t.Fatalf("unknown command should not show explain, got: %q", stderr)
		}
	})
}
