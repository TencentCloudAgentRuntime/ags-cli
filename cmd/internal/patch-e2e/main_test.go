package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchscenarios"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

func TestStrictPreflight(t *testing.T) {
	oldCommit := runnerCommit
	t.Cleanup(func() { runnerCommit = oldCommit })
	root := t.TempDir()
	t.Chdir(root)
	gitRun := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git fixture: %s %v", b, err)
		}
		return strings.TrimSpace(string(b))
	}
	gitRun("init")
	gitRun("config", "user.email", "fixture@example.com")
	gitRun("config", "user.name", "Fixture")
	gitRun("config", "commit.gpgsign", "false")
	dir := filepath.Join(root, "api", "ags", "v1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("api.json", `{"actions":{},"objects":{"Req":{"members":[]}}}`)
	write("api.patch.json", "[]")
	write("e2e-coverage.yaml", "version: 1\ncoverage: []\n")
	gitRun("add", ".")
	gitRun("commit", "-m", "fixture")
	head := gitRun("rev-parse", "HEAD")
	args := func() []string {
		return []string{"--repository", "example/repo", "--pr", "1", "--head", head, "--base", head, "--environment", "test"}
	}
	runReport := func() (Report, error) {
		// Exercise worker preflight directly; archive_test drives the real launcher.
		runnerCommit = head
		var b bytes.Buffer
		err := run(t.Context(), args(), &b)
		var r Report
		if e := json.Unmarshal(b.Bytes(), &r); e != nil {
			t.Fatal(b.String(), e)
		}
		return r, err
	}
	r, err := runReport()
	if err != nil || r.Status != "not_applicable" || r.Tree == "" || r.Plan.PatchDigest == "" {
		t.Fatal(r, err)
	}
	alias := filepath.Join(t.TempDir(), "repo-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	t.Chdir(alias)
	t.Setenv("PWD", alias)
	if r, err := runReport(); err != nil || r.Status != "not_applicable" {
		t.Fatalf("repository alias rejected: %+v %v", r, err)
	}
	t.Chdir(dir)
	if r, err := runReport(); err == nil || r.Reason != "run from the repository root" {
		t.Fatalf("repository subdirectory accepted: %+v %v", r, err)
	}
	t.Chdir(root)
	write("extra.txt", "untracked")
	if _, err := runReport(); err == nil {
		t.Fatal("untracked source accepted")
	}
	gitRun("add", ".")
	if _, err := runReport(); err == nil {
		t.Fatal("staged source accepted")
	}
	gitRun("commit", "-m", "fixture2")
	if _, err := runReport(); err == nil {
		t.Fatal("stale head accepted")
	}
	head = gitRun("rev-parse", "HEAD")
	write("api.patch.json", `[{"op":"add","path":"/objects/Req/type","value":"object"}]`)
	write("e2e-coverage.yaml", "version: 1\ncoverage:\n  - entry: /objects/Req/type\n    scenario: fixture\n    assertions: [behavior]\n")
	gitRun("add", ".")
	gitRun("commit", "-m", "fixture3")
	head = gitRun("rev-parse", "HEAD")
	old := patchscenarios.Registry
	t.Cleanup(func() { patchscenarios.Registry = old })
	patchscenarios.Registry = patchtest.Registry{"fixture": {Assertions: []string{"behavior"}, Run: func(*patchtest.Session) error { t.Fatal("must not execute without credentials"); return nil }}}
	t.Setenv("TENCENTCLOUD_SECRET_ID", "")
	t.Setenv("TENCENTCLOUD_SECRET_KEY", "")
	r, err = runReport()
	if err == nil || r.Status != "fail" || !strings.Contains(r.Reason, "credentials") {
		t.Fatal(r, err)
	}
	// Exercise archive -> build -> subprocess -> report using a local fixture,
	// never a cloud endpoint. Dummy credentials go only to this fixture binary.
	if err := os.MkdirAll(filepath.Join(root, "cmd", "agr"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"go.mod":          "module example.com/fixture\n\ngo 1.25.0\n",
		"cmd/agr/main.go": "package main\nimport \"fmt\"\nfunc main(){fmt.Print(\"committed-fixture\")}\n",
		".gitignore":      "cmd/agr/ignored.go\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	gitRun("add", ".")
	gitRun("commit", "-m", "candidate-fixture")
	head = gitRun("rev-parse", "HEAD")
	// Ignored local source would break a worktree build; the archive excludes it.
	if err := os.WriteFile(filepath.Join(root, "cmd", "agr", "ignored.go"), []byte("not valid Go"), 0600); err != nil {
		t.Fatal(err)
	}
	patchscenarios.Registry = patchtest.Registry{"fixture": {Assertions: []string{"behavior"}, Run: func(s *patchtest.Session) error {
		s.NoResources()
		b, err := s.CLI(s.Context)
		if err != nil {
			return err
		}
		return s.Assert("behavior", string(b) == "committed-fixture")
	}}}
	t.Setenv("TENCENTCLOUD_SECRET_ID", "fixture-only")
	t.Setenv("TENCENTCLOUD_SECRET_KEY", "fixture-only")
	t.Setenv("AGR_REGION", "fixture")
	r, err = runReport()
	if err != nil || r.Status != "pass" || r.Passed != 1 || r.Results[0].Cleanup != "pass" {
		t.Fatal(r, err)
	}
}
