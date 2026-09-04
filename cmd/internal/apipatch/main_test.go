package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const commandTestAPI = `{
  "version":"1.0",
  "metadata":{},
  "actions":{"Ping":{"name":"Ping","input":"PingRequest","output":"PingResponse","status":"online"}},
  "objects":{"PingRequest":{"type":"object","members":[]},"PingResponse":{"type":"object","members":[]}}
}`

func TestRunCheckAndRender(t *testing.T) {
	dir := t.TempDir()
	writeCommandTestFile(t, filepath.Join(dir, "api.json"), commandTestAPI)
	writeCommandTestFile(t, filepath.Join(dir, "api.patch.json"), `[]`)

	var check bytes.Buffer
	if err := run([]string{"check", "--api", dir}, &check); err != nil {
		t.Fatalf("check returned error: %v", err)
	}
	if !strings.Contains(check.String(), "1 actions, 2 objects") {
		t.Fatalf("check output=%q", check.String())
	}

	var render bytes.Buffer
	if err := run([]string{"render", "--api", dir}, &render); err != nil {
		t.Fatalf("render returned error: %v", err)
	}
	if !strings.Contains(render.String(), `"Ping"`) || !strings.HasSuffix(render.String(), "\n") {
		t.Fatalf("render output=%q", render.String())
	}
}

func TestRunCheckAll(t *testing.T) {
	root := t.TempDir()
	for _, version := range []string{"v1", "v2"} {
		dir := filepath.Join(root, "ags", version)
		writeCommandTestFile(t, filepath.Join(dir, "api.json"), commandTestAPI)
		writeCommandTestFile(t, filepath.Join(dir, "api.patch.json"), `[]`)
	}

	var output bytes.Buffer
	if err := run([]string{"check-all", "--root", root, "--require-empty"}, &output); err != nil {
		t.Fatalf("check-all returned error: %v", err)
	}
	for _, version := range []string{"v1", "v2"} {
		if !strings.Contains(output.String(), filepath.ToSlash(filepath.Join(root, "ags", version))) {
			t.Fatalf("check-all output=%q, missing version %s", output.String(), version)
		}
	}
}

func TestRunCheckAllRequiresEmptyPatches(t *testing.T) {
	root := t.TempDir()
	emptyDir := filepath.Join(root, "ags", "v1")
	writeCommandTestFile(t, filepath.Join(emptyDir, "api.json"), commandTestAPI)
	writeCommandTestFile(t, filepath.Join(emptyDir, "api.patch.json"), `[]`)

	patchedDir := filepath.Join(root, "ags", "v2")
	writeCommandTestFile(t, filepath.Join(patchedDir, "api.json"), commandTestAPI)
	writeCommandTestFile(t, filepath.Join(patchedDir, "api.patch.json"), `[{
  "op":"add",
  "path":"/metadata/ready",
  "value":true
}]`)

	var output bytes.Buffer
	if err := run([]string{"check-all", "--root", root}, &output); err != nil {
		t.Fatalf("check-all returned error for valid non-empty patch: %v", err)
	}
	if !strings.Contains(output.String(), "1 operation") {
		t.Fatalf("check-all output=%q, want operation count", output.String())
	}

	err := run([]string{"check-all", "--root", root, "--require-empty"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "must be empty") {
		t.Fatalf("check-all --require-empty error=%v", err)
	}
}

func TestRunCheckAllRequiresPatchFiles(t *testing.T) {
	err := run([]string{"check-all", "--root", t.TempDir()}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no api.patch.json files") {
		t.Fatalf("check-all error=%v", err)
	}
}

func TestRunRebaseReportsMaintenance(t *testing.T) {
	dir := t.TempDir()
	upstream := strings.Replace(commandTestAPI, `"metadata":{}`, `"metadata":{"ready":true}`, 1)
	upstreamPath := filepath.Join(dir, "upstream.json")
	writeCommandTestFile(t, upstreamPath, upstream)
	writeCommandTestFile(t, filepath.Join(dir, "api.patch.json"), `[{"op":"add","path":"/metadata/ready","value":true}]`)

	var output bytes.Buffer
	err := run([]string{"rebase", "--api", dir, "--upstream", upstreamPath}, &output)
	if err == nil || !strings.Contains(err.Error(), "OBSOLETE") {
		t.Fatalf("rebase error=%v", err)
	}
	if !strings.Contains(output.String(), "OBSOLETE") {
		t.Fatalf("rebase output=%q", output.String())
	}
}

func writeCommandTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
