package main

import (
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
	runBinary := func(binary, head string) (Report, error) {
		t.Helper()
		b, err := exec.CommandContext(t.Context(), binary, "--repository", "example/repo", "--pr", "1", "--head", head, "--base", headA, "--environment", "fixture").Output()
		var report Report
		if e := json.Unmarshal(b, &report); e != nil {
			t.Fatalf("report: %s %v (process: %v)", b, e, err)
		}
		return report, err
	}
	if r, err := runBinary(launcher, headA); err != nil || r.Status != "pass" {
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
}
