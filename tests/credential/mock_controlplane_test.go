// Package credential_test contains mock-server integration tests for the
// credential and identity workflow adapter commands. They exercise the full
// CLI binary pipeline: flag parsing → params assembly → SDK.Call fallback →
// raw HTTP request → JSON response → text/JSON output rendering.
//
// These tests do NOT require live credentials — they inject a local httptest
// TLS server via --cloud-endpoint and provide fake secret-id/key via env vars.
package credential_test

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

func ensureBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		out, err := exec.Command("go", "env", "GOMOD").Output()
		if err != nil {
			buildErr = fmt.Errorf("go env GOMOD: %w", err)
			return
		}
		repoRoot := filepath.Dir(strings.TrimSpace(string(out)))
		dir, err := os.MkdirTemp("", "agr-mock-integ-*")
		if err != nil {
			buildErr = err
			return
		}
		bin := filepath.Join(dir, "agr")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "./cmd/agr")
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("build failed: %w\n%s", err, out)
			return
		}
		binPath = bin
	})
	if buildErr != nil {
		t.Fatalf("build: %v", buildErr)
	}
	return binPath
}

type runResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func runAGR(t *testing.T, serverURL string, args ...string) runResult {
	t.Helper()
	bin := ensureBinary(t)
	home := t.TempDir()

	// serverURL is "https://127.0.0.1:PORT" — extract host:port for --cloud-endpoint.
	endpoint := strings.TrimPrefix(serverURL, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")

	fullArgs := append([]string{
		"--cloud-endpoint", endpoint,
	}, args...)

	cmd := exec.Command(bin, fullArgs...) // #nosec G204
	cmd.Env = []string{
		"HOME=" + home,
		"USERPROFILE=" + home,
		"PATH=" + os.Getenv("PATH"),
		"TENCENTCLOUD_SECRET_ID=test-sid",
		"TENCENTCLOUD_SECRET_KEY=test-skey",
		// Allow self-signed certs from httptest TLS server.
		"AGR_INSECURE_SKIP_VERIFY=1",
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	return runResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
}

// mockAPIServer creates an httptest TLS server that responds to TencentCloud
// API requests. The handler receives the Action header and request body.
func mockAPIServer(t *testing.T, handler func(action string, body map[string]any) map[string]any) *httptest.Server {
	t.Helper()
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.Header.Get("X-TC-Action")
		bodyBytes, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		_ = json.Unmarshal(bodyBytes, &reqBody)

		respData := handler(action, reqBody)
		resp := map[string]any{
			"Response": respData,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	// Override the TLS client to skip verification (needed for httptest certs).
	ts.Client().Transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}
	return ts
}

// =============================================================================
// Identity command tests
// =============================================================================

func TestMock_IdentityCreate(t *testing.T) {
	var gotAction string
	var gotBody map[string]any
	ts := mockAPIServer(t, func(action string, body map[string]any) map[string]any {
		gotAction = action
		gotBody = body
		return map[string]any{"WorkloadIdentityId": "wi-new-123", "RequestId": "req-1"}
	})
	defer ts.Close()

	r := runAGR(t, ts.URL, "identity", "create", "--name", "my-agent", "-o", "json")
	if r.exitCode != 0 {
		t.Fatalf("exit %d\nstderr: %s\nstdout: %s", r.exitCode, r.stderr, r.stdout)
	}
	if gotAction != "CreateWorkloadIdentity" {
		t.Fatalf("action = %q, want CreateWorkloadIdentity", gotAction)
	}
	if gotBody["Name"] != "my-agent" {
		t.Fatalf("body Name = %v, want my-agent", gotBody["Name"])
	}
	// Verify JSON envelope.
	if !strings.Contains(r.stdout, "wi-new-123") {
		t.Fatalf("stdout missing identity ID:\n%s", r.stdout)
	}
}

func TestMock_IdentityList(t *testing.T) {
	ts := mockAPIServer(t, func(action string, body map[string]any) map[string]any {
		if action != "DescribeWorkloadIdentityList" {
			t.Fatalf("unexpected action: %s", action)
		}
		return map[string]any{
			"WorkloadIdentitySet": []any{
				map[string]any{"WorkloadIdentityId": "wi-1", "Name": "agent-1"},
				map[string]any{"WorkloadIdentityId": "wi-2", "Name": "agent-2"},
			},
			"TotalCount": 2,
			"RequestId":  "req-2",
		}
	})
	defer ts.Close()

	r := runAGR(t, ts.URL, "identity", "list", "-o", "json")
	if r.exitCode != 0 {
		t.Fatalf("exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "wi-1") || !strings.Contains(r.stdout, "wi-2") {
		t.Fatalf("stdout missing identities:\n%s", r.stdout)
	}
}

func TestMock_IdentityGet(t *testing.T) {
	ts := mockAPIServer(t, func(action string, body map[string]any) map[string]any {
		return map[string]any{
			"WorkloadIdentitySet": []any{
				map[string]any{"WorkloadIdentityId": "wi-get", "Name": "found-agent"},
			},
			"RequestId": "req-3",
		}
	})
	defer ts.Close()

	// Text mode.
	r := runAGR(t, ts.URL, "identity", "get", "wi-get")
	if r.exitCode != 0 {
		t.Fatalf("exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "wi-get") || !strings.Contains(r.stdout, "found-agent") {
		t.Fatalf("stdout missing identity details:\n%s", r.stdout)
	}
}

func TestMock_IdentityGetNotFound(t *testing.T) {
	ts := mockAPIServer(t, func(action string, body map[string]any) map[string]any {
		return map[string]any{
			"WorkloadIdentitySet": []any{},
			"RequestId":           "req-4",
		}
	})
	defer ts.Close()

	r := runAGR(t, ts.URL, "identity", "get", "wi-nope", "-o", "json")
	if r.exitCode == 0 {
		t.Fatal("expected non-zero exit code for not found")
	}
	// Verify structured not_found error.
	if !strings.Contains(r.stdout, "not_found") {
		t.Fatalf("Failure.Kind should be 'not_found':\n%s", r.stdout)
	}
	if !strings.Contains(r.stdout, "IDENTITY_NOT_FOUND") {
		t.Fatalf("Failure.Code should be 'IDENTITY_NOT_FOUND':\n%s", r.stdout)
	}
}

func TestMock_IdentityDelete(t *testing.T) {
	var gotBody map[string]any
	ts := mockAPIServer(t, func(action string, body map[string]any) map[string]any {
		gotBody = body
		return map[string]any{"RequestId": "req-5"}
	})
	defer ts.Close()

	r := runAGR(t, ts.URL, "identity", "delete", "wi-del")
	if r.exitCode != 0 {
		t.Fatalf("exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if gotBody["WorkloadIdentityId"] != "wi-del" {
		t.Fatalf("body = %v", gotBody)
	}
	if !strings.Contains(r.stdout, "deleted") {
		t.Fatalf("stdout missing confirmation:\n%s", r.stdout)
	}
}

func TestMock_IdentityTokenCreate(t *testing.T) {
	ts := mockAPIServer(t, func(action string, body map[string]any) map[string]any {
		if action != "CreateWorkloadAccessTokenForUserId" {
			t.Fatalf("action = %q", action)
		}
		return map[string]any{
			"WorkloadAccessToken": "eyJ-super-secret-token-value",
			"RequestId":           "req-6",
		}
	})
	defer ts.Close()

	// Text mode should mask the token.
	r := runAGR(t, ts.URL, "identity", "token", "create", "--identity-id", "wi-tok", "--user-id", "u1")
	if r.exitCode != 0 {
		t.Fatalf("exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if strings.Contains(r.stdout, "eyJ-super-secret-token-value") {
		t.Fatalf("text mode should mask token, got full value in stdout:\n%s", r.stdout)
	}
	if !strings.Contains(r.stdout, "...") {
		t.Fatalf("text mode should show masked token:\n%s", r.stdout)
	}

	// JSON mode should show full token.
	r2 := runAGR(t, ts.URL, "identity", "token", "create", "--identity-id", "wi-tok", "--user-id", "u1", "-o", "json")
	if r2.exitCode != 0 {
		t.Fatalf("exit %d", r2.exitCode)
	}
	if !strings.Contains(r2.stdout, "eyJ-super-secret-token-value") {
		t.Fatalf("JSON mode should show full token:\n%s", r2.stdout)
	}
}

// =============================================================================
// Credential Provider command tests
// =============================================================================

func TestMock_CredentialProviderCreate(t *testing.T) {
	var gotBody map[string]any
	ts := mockAPIServer(t, func(action string, body map[string]any) map[string]any {
		gotBody = body
		return map[string]any{"ProviderId": "agc-new", "RequestId": "req-7"}
	})
	defer ts.Close()

	r := runAGR(t, ts.URL, "credential", "provider", "create",
		"--name", "my-oauth", "--type", "OAuth2",
		"--provider-config", `[{"Key":"client_id","Value":"xxx"}]`,
		"-o", "json")
	if r.exitCode != 0 {
		t.Fatalf("exit %d\nstderr: %s\nstdout: %s", r.exitCode, r.stderr, r.stdout)
	}
	if gotBody["Name"] != "my-oauth" || gotBody["Type"] != "OAuth2" {
		t.Fatalf("body = %v", gotBody)
	}
	if !strings.Contains(r.stdout, "agc-new") {
		t.Fatalf("stdout missing provider ID:\n%s", r.stdout)
	}
}

func TestMock_CredentialProviderList(t *testing.T) {
	ts := mockAPIServer(t, func(action string, body map[string]any) map[string]any {
		return map[string]any{
			"ProviderSet": []any{
				map[string]any{"ProviderId": "agc-1", "Name": "p1", "Type": "OAuth2", "Status": "ACTIVE"},
			},
			"TotalCount": 1,
			"RequestId":  "req-8",
		}
	})
	defer ts.Close()

	r := runAGR(t, ts.URL, "credential", "provider", "list")
	if r.exitCode != 0 {
		t.Fatalf("exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "agc-1") || !strings.Contains(r.stdout, "OAuth2") {
		t.Fatalf("stdout missing provider info:\n%s", r.stdout)
	}
}

func TestMock_CredentialProviderGetNotFound(t *testing.T) {
	ts := mockAPIServer(t, func(action string, body map[string]any) map[string]any {
		return map[string]any{
			"ProviderSet": []any{},
			"RequestId":   "req-nf",
		}
	})
	defer ts.Close()

	r := runAGR(t, ts.URL, "credential", "provider", "get", "agc-nope", "-o", "json")
	if r.exitCode == 0 {
		t.Fatal("expected non-zero exit code for not found")
	}
	if !strings.Contains(r.stdout, "not_found") {
		t.Fatalf("Failure.Kind should be 'not_found':\n%s", r.stdout)
	}
	if !strings.Contains(r.stdout, "PROVIDER_NOT_FOUND") {
		t.Fatalf("Failure.Code should be 'PROVIDER_NOT_FOUND':\n%s", r.stdout)
	}
}

// =============================================================================
// Credential Secret command tests
// =============================================================================

func TestMock_CredentialSecretSet(t *testing.T) {
	var gotBody map[string]any
	ts := mockAPIServer(t, func(action string, body map[string]any) map[string]any {
		gotBody = body
		return map[string]any{"RequestId": "req-9"}
	})
	defer ts.Close()

	r := runAGR(t, ts.URL, "credential", "secret", "set",
		"--credential-provider-id", "agc-s",
		"--user-id", "u1",
		"--secret", "my-password",
		"--scope", "read:user")
	if r.exitCode != 0 {
		t.Fatalf("exit %d\nstderr: %s\nstdout: %s", r.exitCode, r.stderr, r.stdout)
	}
	if gotBody["Secret"] != "my-password" {
		t.Fatalf("Secret = %v", gotBody["Secret"])
	}
	if gotBody["Scope"] != "read:user" {
		t.Fatalf("Scope = %v", gotBody["Scope"])
	}
}

func TestMock_CredentialSecretGet_MasksInText(t *testing.T) {
	ts := mockAPIServer(t, func(action string, body map[string]any) map[string]any {
		return map[string]any{
			"Secret":    "super-secret-value-1234",
			"Metadata":  []any{},
			"RequestId": "req-10",
		}
	})
	defer ts.Close()

	// Text mode should mask.
	r := runAGR(t, ts.URL, "credential", "secret", "get",
		"--credential-provider-id", "agc-g",
		"--token", "eyJ-tok")
	if r.exitCode != 0 {
		t.Fatalf("exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if strings.Contains(r.stdout, "super-secret-value-1234") {
		t.Fatalf("text mode leaked full secret:\n%s", r.stdout)
	}
	if !strings.Contains(r.stdout, "****") {
		t.Fatalf("text mode should mask secret:\n%s", r.stdout)
	}

	// JSON mode should show full value.
	r2 := runAGR(t, ts.URL, "credential", "secret", "get",
		"--credential-provider-id", "agc-g",
		"--token", "eyJ-tok", "-o", "json")
	if !strings.Contains(r2.stdout, "super-secret-value-1234") {
		t.Fatalf("JSON mode should show full secret:\n%s", r2.stdout)
	}
}

// =============================================================================
// Error handling
// =============================================================================

func TestMock_APIError_RendersStructuredFailure(t *testing.T) {
	ts := mockAPIServer(t, func(action string, body map[string]any) map[string]any {
		return map[string]any{
			"Error": map[string]any{
				"Code":    "ResourceNotFound.CredentialProvider",
				"Message": "Provider not found",
			},
			"RequestId": "req-err-1",
		}
	})
	defer ts.Close()

	r := runAGR(t, ts.URL, "credential", "provider", "get", "agc-nope", "-o", "json")
	if r.exitCode == 0 {
		t.Fatal("expected error exit code")
	}
	// Should have structured failure in JSON output.
	if !strings.Contains(r.stdout, "ResourceNotFound") || !strings.Contains(r.stdout, "failed") {
		t.Fatalf("stdout missing error info:\n%s", r.stdout)
	}
}
