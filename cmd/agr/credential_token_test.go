package main

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialTokenOwnership(t *testing.T) {
	const workloadToken = "eyJ.fixture-workload.signature"
	const cloudToken = "fixture-sts-token"
	for _, operation := range []struct {
		name, action string
		args         []string
	}{
		{"secret", "GetManagedSecret", []string{"credential", "secret", "get", "--credential-provider-id", "agc-fixture"}},
		{"oauth2", "AcquireOAuth2AccessToken", []string{"credential", "oauth2", "acquire", "--credential-provider-id", "agc-fixture", "--flow", "AUTHORIZATION_CODE"}},
	} {
		for _, tc := range []struct {
			name, auth            string
			inline, before, debug bool
		}{
			{name: "no_sts"},
			{name: "env_sts", auth: "env"},
			{name: "config_sts", auth: "config"},
			{name: "inline_env_sts", auth: "env", inline: true},
			{name: "inline_config_sts", auth: "config", inline: true},
			{name: "token_before_command", auth: "env", before: true},
			{name: "inline_token_before_command", auth: "config", before: true, inline: true},
			{name: "debug_failure", auth: "env", debug: true},
		} {
			t.Run(operation.name+"/"+tc.name, func(t *testing.T) {
				type request struct {
					action, token, signature string
					body                     map[string]any
				}
				requests := make(chan request, 1)
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					requests <- request{r.Header.Get("X-TC-Action"), r.Header.Get("X-TC-Token"), r.Header.Get("Authorization"), body}
					response := map[string]any{"RequestId": "fixture-request"}
					if tc.debug {
						response["Error"] = map[string]any{"Code": "AuthFailure.TokenFailure", "Message": "fixture rejected " + workloadToken + " and " + cloudToken}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"Response": response})
				}))
				defer server.Close()
				home := t.TempDir()
				args := append([]string{}, operation.args...)
				tokenArgs := []string{"--token", workloadToken}
				if tc.inline {
					tokenArgs = []string{"--token=" + workloadToken}
				}
				if tc.before {
					args = append(tokenArgs, args...)
				} else {
					args = append(args, tokenArgs...)
				}
				env := []string{}
				switch tc.auth {
				case "env":
					env = append(env, "TENCENTCLOUD_TOKEN="+cloudToken)
				case "config":
					configPath := filepath.Join(home, "config.toml")
					if err := os.WriteFile(configPath, []byte("[auth]\ntoken = \""+cloudToken+"\"\n"), 0600); err != nil {
						t.Fatal(err)
					}
					args = append(args, "--config", configPath)
				}
				logPath := filepath.Join(home, "debug.log")
				if tc.debug {
					args = append(args, "--debug-log", logPath)
				}
				args = append(args, "-o", "json")
				stdout, stderr, err := runTokenCLI(t, server, home, env, args)
				if (err != nil) != tc.debug {
					t.Fatalf("exit=%v stdout=%s stderr=%s", err, stdout, stderr)
				}
				select {
				case got := <-requests:
					wantCloudToken := ""
					if tc.auth != "" {
						wantCloudToken = cloudToken
					}
					if got.action != operation.action || got.token != wantCloudToken || got.signature == "" || got.body["WorkloadIdentityToken"] != workloadToken {
						t.Fatalf("incorrect token routing: %+v", got)
					}
				default:
					t.Fatal("no API request received")
				}
				if tc.debug {
					log, err := os.ReadFile(logPath)
					if err != nil {
						t.Fatal(err)
					}
					for name, output := range map[string]string{"stdout": stdout, "stderr": stderr, "debug log": string(log)} {
						if strings.Contains(output, workloadToken) || strings.Contains(output, cloudToken) {
							t.Errorf("%s leaked a token", name)
						}
						if !strings.Contains(output, "[REDACTED]") {
							t.Errorf("%s missing redacted diagnostic: %s", name, output)
						}
					}
					if !strings.Contains(stdout, "AuthFailure.TokenFailure") || !strings.Contains(stderr, operation.action) {
						t.Error("lost error code or API diagnostic context")
					}
				}
			})
		}
		for _, output := range []string{"text", "json"} {
			t.Run(operation.name+"/help_"+output, func(t *testing.T) {
				args := append(append([]string{}, operation.args[:3]...), "--help", "-o", output)
				stdout, stderr, err := runTokenCLI(t, nil, t.TempDir(), nil, args)
				if err != nil {
					t.Fatalf("help failed: %v %s", err, stderr)
				}
				for _, want := range []string{"WorkloadIdentityToken", "TENCENTCLOUD_TOKEN", "auth.token"} {
					if !strings.Contains(stdout, want) {
						t.Errorf("help missing %q: %s", want, stdout)
					}
				}
			})
		}
	}
}

func TestUnshadowedTokenStillAuthenticates(t *testing.T) {
	for _, args := range [][]string{
		{"credential", "secret", "list", "--credential-provider-id", "agc-fixture", "--token", "fixture-sts"},
		{"--token=fixture-sts", "credential", "secret", "list", "--credential-provider-id", "agc-fixture"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			tokens := make(chan string, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tokens <- r.Header.Get("X-TC-Token")
				_ = json.NewEncoder(w).Encode(map[string]any{"Response": map[string]any{"RequestId": "fixture"}})
			}))
			defer server.Close()
			stdout, stderr, err := runTokenCLI(t, server, t.TempDir(), []string{"TENCENTCLOUD_TOKEN=env-sts"}, append(args, "-o", "json"))
			if err != nil {
				t.Fatalf("exit=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
			select {
			case got := <-tokens:
				if got != "fixture-sts" {
					t.Errorf("cloud token=%q", got)
				}
			default:
				t.Fatal("no API request received")
			}
		})
	}
}

// Reuse the production-main subprocess helper, including trust for the local CA.
func runTokenCLI(t *testing.T, server *httptest.Server, home string, env, args []string) (string, string, error) {
	t.Helper()
	if server == nil {
		server = httptest.NewTLSServer(http.NotFoundHandler())
		defer server.Close()
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], append([]string{"-test.run=^TestTransportLifecycleHelper$", "--"}, args...)...)
	cmd.Env = append([]string{
		"HOME=" + home, "USERPROFILE=" + home, "PATH=" + os.Getenv("PATH"),
		"AGR_TRANSPORT_LIFECYCLE_HELPER=cli", "GORACE=atexit_sleep_ms=0",
		"TENCENTCLOUD_SECRET_ID=fixture-id", "TENCENTCLOUD_SECRET_KEY=fixture-key",
	}, env...)
	cmd.Env = append(cmd.Env,
		"AGR_CLOUD_ENDPOINT="+strings.TrimPrefix(server.URL, "https://"),
		"AGR_TRANSPORT_TEST_CERT="+string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
