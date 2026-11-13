//go:build preview

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

const webTestToken = "web-secret/+?&= with spaces"

type harnessWebFake struct {
	*harnessFake
	deploymentStatus string
	token            string
	failAction       string
	webCalls         int
	webStatus        int
}

func newHarnessWebFake(t *testing.T) *harnessWebFake {
	f := &harnessWebFake{harnessFake: newHarnessFake(t), deploymentStatus: "ACTIVE", token: webTestToken}
	f.seedHarness("PUBLISHED", "UNPUBLISHED")
	// The live revision, newer revision and draft intentionally disagree.
	f.revisions[1]["Spec"].(map[string]any)["HarnessEngine"] = "dsh"
	return f
}

func (f *harnessWebFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.webCalls++
		if r.URL.Query().Get("token") != "" {
			http.SetCookie(w, &http.Cookie{Name: "tencentags-deployment-token", Value: f.token, Path: "/", Secure: true, HttpOnly: true})
			http.SetCookie(w, &http.Cookie{Name: "tencentags-deployment-affinity-id", Value: "confirmed-instance", Path: "/", Secure: true, HttpOnly: true})
			http.Redirect(w, r, "https://"+r.Host+"/", http.StatusTemporaryRedirect)
			return
		}
		if cookie, err := r.Cookie("tencentags-deployment-token"); err != nil || cookie.Value != f.token || r.URL.Query().Get("affinity_id") != "confirmed-instance" || r.Referer() != "" {
			f.t.Error("readiness lost authentication/confirmed affinity or forwarded the credential Referer")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if f.webStatus != 0 {
			w.WriteHeader(f.webStatus)
			return
		}
		_, _ = w.Write([]byte("ready"))
		return
	}
	action := r.Header.Get("X-TC-Action")
	if r.Header.Get("Authorization") == "" {
		f.t.Error("unsigned Cloud API request")
	}
	var request map[string]any
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&request); err != nil {
		f.t.Error(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{action, request})
	var response map[string]any
	var failure string
	switch action {
	case f.failAction:
		response = map[string]any{"Error": map[string]any{"Code": "UnauthorizedOperation", "Message": "access denied"}}
	case "DescribeDeployment":
		response = map[string]any{"Deployment": map[string]any{"DeploymentId": "dpl-test", "Status": f.deploymentStatus}}
	case "AcquireDeploymentToken":
		response = map[string]any{"Token": f.token, "ExpiresAt": "2026-11-14T00:00:00Z"}
	case "DescribeHarnessRevisionList":
		// One revision per page makes the PUBLISHED revision a later-page result.
		n := int64(len(f.revisions)) - number(request["Offset"])
		var page []any
		if n > 0 {
			page = append(page, f.revisions[n])
		}
		response = map[string]any{"HarnessRevisionSet": page, "TotalCount": len(f.revisions)}
	default:
		response, failure = f.handle(action, request)
	}
	if failure != "" {
		code, message, _ := strings.Cut(failure, ":")
		response = map[string]any{"Error": map[string]any{"Code": code, "Message": message}}
	}
	response["RequestId"] = "req-" + action
	_ = json.NewEncoder(w).Encode(map[string]any{"Response": response})
}

func TestHarnessWebNoOpen(t *testing.T) {
	for _, showToken := range []bool{false, true} {
		t.Run(map[bool]string{false: "safe_url", true: "explicit_access_url"}[showToken], func(t *testing.T) {
			fake := newHarnessWebFake(t)
			server := httptest.NewTLSServer(fake)
			defer server.Close()
			affinity := "conversation /+&?=中文"
			args := []string{"harness", "web", "hns-test", "--no-open", "--region", "ap-chongqing", "--domain", "example.com", "--affinity-id", affinity}
			if showToken {
				args = append(args, "--show-token")
			}
			result, stderr, err := runHarness(t, server, args...)
			if err != nil || result.Status != "succeeded" || result.Data["Opened"] != false || result.Data["Revision"] != json.Number("1") {
				t.Fatalf("web: err=%v result=%+v stderr=%s", err, result, stderr)
			}
			webURL, err := url.Parse(result.Data["WebURL"].(string))
			if err != nil || webURL.Scheme != "https" || webURL.Host != "3080-dpl-test-webview.ap-chongqing.agents.example.com" || webURL.Path != "/" || webURL.Query().Get("affinity_id") != affinity || webURL.Query().Has("token") {
				t.Fatalf("safe Web URL = %v, err=%v", webURL, err)
			}
			if showToken {
				accessURL, err := url.Parse(result.Data["AccessURL"].(string))
				if err != nil || accessURL.Query().Get("token") != webTestToken || accessURL.Query().Get("affinity_id") != affinity || result.Data["ExpiresAt"] != "2026-11-14T00:00:00Z" {
					t.Fatalf("explicit token output = %+v, err=%v", result.Data, err)
				}
			} else if _, exists := result.Data["AccessURL"]; exists {
				t.Fatalf("default output includes AccessURL: %+v", result.Data)
			}
			actions := fake.actions()
			want := []string{"DescribeHarness", "DescribeDeployment", "DescribeHarnessRevisionList", "DescribeHarnessRevisionList"}
			if showToken {
				want = append(want, "AcquireDeploymentToken")
				if fake.last("AcquireDeploymentToken")["DeploymentId"] != "dpl-test" {
					t.Fatal("token requested for the wrong Deployment")
				}
				if actions[len(actions)-1] != "AcquireDeploymentToken" {
					t.Fatalf("token acquired before reads completed: %v", actions)
				}
			}
			slices.Sort(actions)
			slices.Sort(want)
			if !slices.Equal(actions, want) {
				t.Fatalf("side effects: got %v, want %v", actions, want)
			}
			if fake.webCalls != 0 {
				t.Fatal("--no-open contacted the data plane")
			}
			if strings.Contains(stderr, webTestToken) || strings.Contains(stderr, url.QueryEscape(webTestToken)) {
				t.Fatalf("token leaked to stderr: %s", stderr)
			}
		})
	}
}

func TestHarnessWebRejectsUnavailableTargets(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*harnessWebFake)
		code string
	}{
		{"missing deployment", func(f *harnessWebFake) { f.harness["ResourceInfo"] = map[string]any{} }, "HARNESS_NOT_DEPLOYED"},
		{"no published revision", func(f *harnessWebFake) { f.revisions[1]["Status"] = "SUPERSEDED" }, "HARNESS_NOT_PUBLISHED"},
		{"draft dsh but live pi", func(f *harnessWebFake) {
			f.harness["Spec"].(map[string]any)["HarnessEngine"] = "dsh"
			f.revisions[1]["Spec"].(map[string]any)["HarnessEngine"] = "pi"
		}, "HARNESS_WEB_UNSUPPORTED_ENGINE"},
		{"inactive deployment", func(f *harnessWebFake) { f.deploymentStatus = "CREATING" }, "DEPLOYMENT_NOT_ACTIVE"},
		{"missing deployment status", func(f *harnessWebFake) { f.deploymentStatus = "" }, "DEPLOYMENT_NOT_ACTIVE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newHarnessWebFake(t)
			tc.edit(fake)
			server := httptest.NewTLSServer(fake)
			defer server.Close()
			result, _, err := runHarness(t, server, "harness", "web", "hns-test", "--no-open", "--show-token")
			if err == nil || failureCode(result) != tc.code || result.Failure.Hint == "" {
				t.Fatalf("err=%v failure=%+v, want %s", err, result.Failure, tc.code)
			}
			for _, action := range fake.actions() {
				if !strings.HasPrefix(action, "Describe") {
					t.Fatalf("invalid target reached %s", action)
				}
			}
		})
	}
}

func TestHarnessWebCloudFailures(t *testing.T) {
	for _, action := range []string{"DescribeHarness", "DescribeDeployment", "DescribeHarnessRevisionList", "AcquireDeploymentToken", "empty token"} {
		t.Run(action, func(t *testing.T) {
			fake := newHarnessWebFake(t)
			fake.failAction = action
			if action == "empty token" {
				fake.token = ""
			}
			server := httptest.NewTLSServer(fake)
			defer server.Close()
			result, _, err := runHarness(t, server, "harness", "web", "hns-test", "--no-open", "--show-token")
			if err == nil || result.Status == "succeeded" || result.Failure == nil {
				t.Fatalf("cloud failure reported success: err=%v result=%+v", err, result)
			}
			if action != "empty token" && failureCode(result) != "UnauthorizedOperation" {
				t.Fatalf("Cloud API error lost: %+v", result.Failure)
			}
			if action == "empty token" && (failureCode(result) != "EMPTY_DEPLOYMENT_TOKEN" || result.Failure.Hint == "") {
				t.Fatalf("empty token missing actionable failure: %+v", result.Failure)
			}
			if strings.HasPrefix(action, "Describe") && slices.Contains(fake.actions(), "AcquireDeploymentToken") {
				t.Fatal("read failure still acquired a token")
			}
			if _, exists := result.Data["AccessURL"]; exists {
				t.Fatalf("failed request returned an access URL: %+v", result.Data)
			}
		})
	}
}

func TestHarnessWebBrowserLaunch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a Unix shell launcher; platform-independent launch behavior is covered by the command tests")
	}
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			fake := newHarnessWebFake(t)
			server := httptest.NewTLSServer(fake)
			defer server.Close()
			dir := t.TempDir()
			launchedPath := filepath.Join(dir, "launched-url")
			// Replace only the OS launcher in PATH; execute the actual CLI handler
			// and utils.OpenBrowser without opening a real browser in the test.
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > '%s'\n", strings.ReplaceAll(launchedPath, "'", "'\\''"))
			for _, name := range []string{"open", "xdg-open"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", dir)
			logPath := filepath.Join(dir, "debug.log")
			out, stderr, err := runSnapshotCLIWithOutput(t, server, strings.NewReader(""), format, "harness", "web", "hns-test", "--debug-log", logPath)
			if err != nil {
				t.Fatalf("browser launch: %v stdout=%s stderr=%s", err, out, stderr)
			}
			var launched []byte
			deadline := time.Now().Add(5 * time.Second)
			for {
				launched, err = os.ReadFile(launchedPath)
				if err == nil && len(launched) != 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("launcher did not capture a URL: %v", err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			address, err := url.Parse(strings.TrimSpace(string(launched)))
			if err != nil || address.Host != "3080-dpl-test-webview.ap-guangzhou.agents.tencentags.com" || address.Query().Get("token") != webTestToken || address.Query().Get("affinity_id") != "confirmed-instance" {
				t.Fatalf("launched URL=%v err=%v", address, err)
			}
			if fake.webCalls < 2 {
				t.Fatal("browser launched before native Web readiness was checked")
			}
			logs, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			for channel, value := range map[string]string{"stdout": string(out), "stderr": stderr, "debug log": string(logs)} {
				if strings.Contains(value, webTestToken) || strings.Contains(value, url.QueryEscape(webTestToken)) || strings.Contains(value, "token=") {
					t.Errorf("%s leaked token: %s", channel, value)
				}
			}
			if format == "json" {
				var result harnessRun
				if err := json.Unmarshal(out, &result); err != nil || result.Status != "succeeded" || result.Data["Opened"] != true {
					t.Fatalf("launch envelope: %s err=%v", out, err)
				}
			}
		})
	}
}

func TestHarnessWebBrowserLaunchFailure(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			fake := newHarnessWebFake(t)
			server := httptest.NewTLSServer(fake)
			defer server.Close()
			dir := t.TempDir()
			t.Setenv("PATH", dir) // No browser launcher is available.
			logPath := filepath.Join(dir, "debug.log")
			out, stderr, err := runSnapshotCLIWithOutput(t, server, strings.NewReader(""), format, "harness", "web", "hns-test", "--debug-log", logPath)
			if err == nil || !strings.Contains(string(out)+stderr, "BROWSER_OPEN_FAILED") || !strings.Contains(string(out)+stderr, "--no-open --show-token") {
				t.Fatalf("browser failure: err=%v stdout=%s stderr=%s", err, out, stderr)
			}
			logs, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			for channel, value := range map[string]string{"stdout": string(out), "stderr": stderr, "debug log": string(logs)} {
				if strings.Contains(value, webTestToken) || strings.Contains(value, url.QueryEscape(webTestToken)) || strings.Contains(value, "token=") {
					t.Errorf("%s leaked token on failure: %s", channel, value)
				}
			}
		})
	}
}

func TestHarnessWebReadinessFailureDoesNotLaunch(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		for _, status := range []int{401, 502} {
			t.Run(fmt.Sprintf("%s/%d", format, status), func(t *testing.T) {
				fake := newHarnessWebFake(t)
				fake.webStatus = status
				server := httptest.NewTLSServer(fake)
				defer server.Close()
				dir := t.TempDir()
				marker := filepath.Join(dir, "unexpected-launch")
				script := fmt.Sprintf("#!/bin/sh\nprintf launched > '%s'\n", strings.ReplaceAll(marker, "'", "'\\''"))
				for _, launcher := range []string{"open", "xdg-open"} {
					if err := os.WriteFile(filepath.Join(dir, launcher), []byte(script), 0o700); err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv("PATH", dir)
				logPath := filepath.Join(dir, "debug.log")
				out, stderr, err := runSnapshotCLIWithOutput(t, server, strings.NewReader(""), format,
					"harness", "web", "hns-test", "--timeout", "1s", "--debug-log", logPath)
				code := "WEB_READY_FAILED"
				if status == 502 {
					code = "WEB_READY_TIMEOUT"
				}
				if err == nil || !strings.Contains(string(out)+stderr, code) || fake.webCalls < 2 {
					t.Fatalf("readiness: err=%v stdout=%s stderr=%s calls=%d", err, out, stderr, fake.webCalls)
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("failed readiness still launched a browser")
				}
				logs, err := os.ReadFile(logPath)
				if err != nil {
					t.Fatal(err)
				}
				for _, value := range []string{string(out), stderr, string(logs)} {
					if strings.Contains(value, webTestToken) || strings.Contains(value, url.QueryEscape(webTestToken)) || strings.Contains(value, "token=") {
						t.Fatal("readiness failure leaked a credential")
					}
				}
				if format == "json" {
					var result harnessRun
					if err := json.Unmarshal(out, &result); err != nil || result.Status != "failed" || failureCode(result) != code {
						t.Fatalf("readiness failure lost its JSON envelope: %s", out)
					}
				}
			})
		}
	}
}

func TestHarnessWebSchemaAndHelp(t *testing.T) {
	fake := newHarnessWebFake(t)
	server := httptest.NewTLSServer(fake)
	defer server.Close()
	schema, _, err := runHarness(t, server, "schema", "harness.web")
	if err != nil || schema.Data["SupportsJson"] != true || schema.Data["SupportsNdjson"] != false || schema.Data["RequiresAuth"] != true || schema.Data["Mutation"] != false || schema.Data["CreatesResource"] != false {
		t.Fatalf("web schema: err=%v data=%+v", err, schema.Data)
	}
	flags, _ := schema.Data["Flags"].([]any)
	for name, kind := range map[string]string{"no-open": "bool", "show-token": "bool", "affinity-id": "string", "timeout": "string"} {
		if !slices.ContainsFunc(flags, func(flag any) bool {
			value := flag.(map[string]any)
			return value["Name"] == name && value["Type"] == kind
		}) {
			t.Errorf("final schema missing --%s (%s): %v", name, kind, flags)
		}
	}
	out, stderr, err := runSnapshotCLIWithOutput(t, server, strings.NewReader(""), "text", "harness", "web", "--help")
	if err != nil {
		t.Fatalf("help: %v stderr=%s", err, stderr)
	}
	for _, text := range []string{"web <harness-id>", "3080", "--no-open", "--show-token", "--affinity-id", "--timeout", "90s", "404/502/503"} {
		if !strings.Contains(string(out), text) {
			t.Errorf("help missing %q: %s", text, out)
		}
	}
	result, _, err := runHarness(t, server, "harness", "web", "--no-open")
	if err == nil || failureCode(result) != "INVALID_USAGE" {
		t.Fatalf("missing ID: err=%v failure=%+v", err, result.Failure)
	}
	if len(fake.actions()) != 0 {
		t.Fatalf("help/schema/invalid arguments reached the cloud: %v", fake.actions())
	}
}
