package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// All child processes use a local CLI fixture, never a cloud endpoint.
func TestArchivedRunnerUsesCommittedScenarios(t *testing.T) {
	source, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, dir := range []string{"cmd/internal/patch-e2e", "internal/apimeta", "internal/patchcoverage", "internal/patchtest", "internal/patchscenarios"} {
		if err := os.CopyFS(filepath.Join(root, dir), os.DirFS(filepath.Join(source, dir))); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, data string) {
		t.Helper()
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		write(name, string(data))
	}
	write("cmd/agr/main.go", "package main\nfunc main() {}\n")
	write("api/ags/v1/api.json", `{"actions":{},"objects":{"Req":{"members":[]}}}`)
	write("api/ags/v1/api.patch.json", `[{"op":"add","path":"/objects/Req/type","value":"object"}]`)
	write("api/ags/v1/e2e-coverage.yaml", "version: 1\ncoverage:\n  - entry: /objects/Req/type\n    scenario: fixture\n    assertions: [behavior]\n")
	registry := `package patchscenarios
import "github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
var Registry = patchtest.Registry{"fixture": {Assertions: []string{"behavior"}, Run: func(s *patchtest.Session) error {
 s.NoResources()
 if _, err := s.CLI(s.Context); err != nil { return err }
 return s.Assert("behavior", true)
}}}
`
	write("internal/patchscenarios/registry.go", registry)
	write(".gitignore", "internal/patchscenarios/ignored.go\n")
	t.Chdir(root)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	command := func(name string, args ...string) string {
		t.Helper()
		b, err := exec.CommandContext(t.Context(), name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %s %v", name, b, err)
		}
		return strings.TrimSpace(string(b))
	}
	command("git", "init")
	command("git", "config", "user.email", "fixture@example.com")
	command("git", "config", "user.name", "Fixture")
	command("git", "config", "commit.gpgsign", "false")
	command("git", "add", ".")
	command("git", "commit", "-m", "passing-scenario")
	headA := command("git", "rev-parse", "HEAD")
	binDir := t.TempDir()
	launcher := filepath.Join(binDir, "launcher")
	worker := filepath.Join(binDir, "worker")
	command("go", "build", "-o", worker, "-ldflags=-X main.runnerCommit="+headA, "./cmd/internal/patch-e2e")
	// The launcher itself includes ignored source. It must still execute the
	// archived registry, not this locally replaced assertion.
	write("internal/patchscenarios/ignored.go", `package patchscenarios
import "github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
func init() { Registry["fixture"] = patchtest.Scenario{Assertions: []string{"behavior"}, Run: func(s *patchtest.Session) error {
 s.NoResources()
 if _, err := s.CLI(s.Context); err != nil { return err }
 return s.Assert("behavior", false)
}} }
`)
	command("go", "build", "-o", launcher, "./cmd/internal/patch-e2e")
	t.Setenv("TENCENTCLOUD_SECRET_ID", "fixture-only")
	t.Setenv("TENCENTCLOUD_SECRET_KEY", "fixture-only")
	t.Setenv("AGR_REGION", "fixture")
	var diagnostics bytes.Buffer
	runBinary := func(binary, head string) (Report, error) {
		t.Helper()
		diagnostics.Reset()
		cmd := exec.CommandContext(t.Context(), binary, "--repository", "example/repo", "--pr", "1", "--head", head, "--base", headA, "--environment", "fixture")
		cmd.Stderr = &diagnostics
		b, err := cmd.Output()
		var report Report
		if e := json.Unmarshal(b, &report); e != nil {
			t.Fatalf("report: %s %v (process: %v)", b, e, err)
		}
		return report, err
	}
	if r, err := runBinary(launcher, headA); err != nil || r.Status != "pass" || r.Channel != "preview" || r.BuildTags != "preview" || len(r.CandidateSHA256) != 64 {
		t.Fatalf("ignored source contaminated archived runner: %+v %v", r, err)
	}
	write("internal/patchscenarios/registry.go", strings.Replace(registry, `s.Assert("behavior", true)`, `s.Assert("behavior", false)`, 1))
	command("git", "add", "internal/patchscenarios/registry.go")
	command("git", "commit", "-m", "failing-scenario-same-IDs")
	headB := command("git", "rev-parse", "HEAD")
	if r, err := runBinary(worker, headB); err == nil || !strings.Contains(r.Reason, "runner commit") {
		t.Fatalf("old worker accepted new head: %+v %v", r, err)
	}
	if r, err := runBinary(launcher, headB); err == nil || r.Status != "fail" || r.Failed != 1 || r.Source != headB {
		t.Fatalf("launcher did not execute new committed assertion: %+v %v", r, err)
	}
	// Both archive builds must preserve compiler diagnostics on stderr only,
	// including the candidate build performed inside the launcher child process.
	for _, fixture := range []struct{ path, reason string }{
		{"cmd/agr/main.go", "committed runner failed: candidate CLI build failed"},
		{"cmd/internal/patch-e2e/broken.go", "committed runner build failed"},
	} {
		write(fixture.path, "package main\nvar _ = archiveBuildDiagnosticMarker\n")
		command("git", "add", fixture.path)
		command("git", "commit", "-m", "invalid-build-fixture")
		head := command("git", "rev-parse", "HEAD")
		r, err := runBinary(launcher, head)
		if err == nil || r.Status != "fail" || r.Reason != fixture.reason {
			t.Fatalf("build failure report: %+v %v", r, err)
		}
		if !strings.Contains(diagnostics.String(), "undefined: archiveBuildDiagnosticMarker") {
			t.Fatalf("missing compiler diagnostic: %s", &diagnostics)
		}
	}
}

func TestExtractCommitPaddedArchive(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	t.Chdir(repo)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	gitCommand := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(t.Context(), realGit, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	gitCommand("init")
	if err := os.WriteFile("payload.txt", []byte("committed fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitCommand("add", "payload.txt")
	gitCommand("-c", "user.name=Fixture", "-c", "user.email=fixture@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
	head := gitCommand("rev-parse", "HEAD")
	bin := t.TempDir()
	// Extra zero records are valid tar padding, which tar may leave unread.
	shim := `#!/bin/sh
"$AGR_TEST_REAL_GIT" "$@" || exit $?
if [ "$1" = archive ]; then
  dd if=/dev/zero bs=1024 count=1024 2>/dev/null || exit $?
  if [ "$AGR_TEST_ARCHIVE_FAIL" = 1 ]; then exit 1; fi
fi
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGR_TEST_REAL_GIT", realGit)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, fail := range []bool{false, true} {
		name := "padding"
		if fail {
			name = "producer_failure"
		}
		t.Run(name, func(t *testing.T) {
			value := "0"
			if fail {
				value = "1"
			}
			t.Setenv("AGR_TEST_ARCHIVE_FAIL", value)
			out := t.TempDir()
			err := extractCommit(t.Context(), head, out)
			if (err != nil) != fail {
				t.Fatalf("extract error=%v, want failure=%t", err, fail)
			}
			if !fail {
				got, err := os.ReadFile(filepath.Join(out, "payload.txt"))
				if err != nil || string(got) != "committed fixture\n" {
					t.Fatalf("payload=%q err=%v", got, err)
				}
			}
		})
	}
}
