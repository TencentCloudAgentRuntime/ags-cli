package patchscenarios

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/harness_web.cjs
var harnessWebBrowser string

func TestHarnessWebLive(t *testing.T) {
	r := newHarnessRuntime(t, "AGR_HARNESS_WEB_E2E")
	runner, version := os.Getenv("AGR_HARNESS_WEB_RUNNER"), os.Getenv("AGR_HARNESS_WEB_ENGINE_VERSION")
	if runtime.GOOS == "windows" || !filepath.IsAbs(runner) || version == "" {
		t.Fatal("Web E2E requires a Unix runner, absolute AGR_HARNESS_WEB_RUNNER (Node-compatible with Playwright), and pinned AGR_HARNESS_WEB_ENGINE_VERSION")
	}
	if info, err := os.Stat(runner); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		t.Fatal("AGR_HARNESS_WEB_RUNNER must be an executable Node-compatible runner")
	}
	// Use a separate owned DSH fixture; runtime/MCP cases retain their engine.
	t.Setenv("AGR_HARNESS_ENGINE", "dsh")
	t.Setenv("AGR_HARNESS_ENGINE_VERSION", version)
	r.create("Follow the user's instruction exactly. Never use tools.", "", 2)

	safe := r.must("harness", "web", r.harness, "--no-open")
	address, err := url.Parse(harnessText(safe.Data["WebURL"]))
	if err != nil || address.Scheme != "https" || !strings.HasPrefix(address.Host, "3080-"+r.deploy+"-webview.") || address.Query().Has("token") || safe.Data["Opened"] != false || safe.Data["AccessURL"] != nil {
		t.Fatal("--no-open did not return a safe 3080 Web URL")
	}
	explicit := r.must("harness", "web", r.harness, "--no-open", "--show-token")
	login, err := url.Parse(harnessText(explicit.Data["AccessURL"]))
	if err != nil || login.Host != address.Host || login.Query().Get("token") == "" || explicit.Data["ExpiresAt"] == nil || explicit.Data["Opened"] != false {
		t.Fatal("headless Web output omitted the explicit login link/expiry")
	}
	if ids, err := r.instanceIDs(t.Context()); err != nil || len(ids) != 0 {
		t.Fatal("--no-open started an instance, or instance readback failed")
	}

	// Capture only the OS launch request. The real CLI must finish readiness
	// first; Chromium then uses exactly that URL. Protect and promptly remove
	// the temporary credential file, and pass browser input through stdin.
	dir := t.TempDir()
	launchPath := filepath.Join(dir, "login-url")
	script := "#!/bin/sh\numask 077\nprintf '%s' \"$1\" > '" + strings.ReplaceAll(launchPath, "'", "'\\''") + "'\n"
	for _, name := range []string{"open", "xdg-open"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal("could not prepare the Web launch capture")
		}
	}
	r.env = append(r.env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	opened := r.must("harness", "web", r.harness, "--timeout", "90s")
	if opened.Data["Opened"] != true || opened.Data["AccessURL"] != nil {
		t.Fatal("Web readiness/launch did not succeed with safe output")
	}
	var raw []byte
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for len(raw) == 0 {
		raw, _ = os.ReadFile(launchPath)
		if len(raw) != 0 {
			break
		}
		select {
		case <-t.Context().Done():
			t.Fatal("Web launch capture canceled")
		case <-deadline.C:
			t.Fatal("Web launcher did not receive a URL")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := os.Remove(launchPath); err != nil {
		t.Fatal("could not remove the temporary Web login link")
	}
	launch, err := url.Parse(string(raw))
	if err != nil || launch.Host != address.Host || launch.Query().Get("token") == "" || launch.Query().Get("affinity_id") == "" {
		t.Fatal("Web launcher did not receive the authenticated, confirmed-affinity URL")
	}
	before, err := r.instanceIDs(t.Context())
	if err != nil || len(before) != 1 {
		t.Fatal("readiness must warm exactly one owned instance")
	}
	t.Log("safe/headless Web output had no instance side effects; default opening confirmed readiness and affinity")

	input, _ := json.Marshal(map[string]string{"accessURL": launch.String()})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, runner, "-e", harnessWebBrowser)
	cmd.Stdin = bytes.NewReader(input)
	// The browser needs only its login link, not cloud credentials.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "NODE_PATH=" + os.Getenv("NODE_PATH")}
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	raw, runErr := cmd.Output()
	var report struct {
		Passed, Reply, CookieReentry, TokenRemoved, SecureCookies bool
		PageStatus, AnonymousStatus, WebSocketStatus              int
		Phase                                                     string
	}
	if err := json.Unmarshal(raw, &report); err != nil || runErr != nil || !report.Passed || !report.Reply || !report.CookieReentry || !report.TokenRemoved || !report.SecureCookies || report.PageStatus != 200 || report.AnonymousStatus != 401 || report.WebSocketStatus != 101 {
		// Never print raw browser output/URLs/errors; Playwright errors can
		// include login URLs and rendered page content.
		if slices.Contains([]string{"input", "launch", "login", "anonymous", "cookie-reentry", "preview-notice", "workspace-path", "workspace-selection", "composer", "model-reply", "completed"}, report.Phase) {
			t.Log("native Web browser phase:", report.Phase)
		}
		t.Fatal("native Web browser E2E failed; check the reviewed runner and browser flow (browser output withheld)")
	}
	after, err := r.instanceIDs(t.Context())
	if err != nil || len(after) != 1 || after[0] != before[0] {
		t.Fatal("readiness and browser access did not retain the same owned instance")
	}
	t.Log("native Web passed: 307 login, first-page 200, secure cookies, anonymous 401, cookie re-entry, WebSocket 101, real assistant reply and same-instance affinity")
}
