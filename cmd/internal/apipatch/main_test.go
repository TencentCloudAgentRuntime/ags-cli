package main

import (
	"bytes"
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
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
