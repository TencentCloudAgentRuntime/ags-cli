package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
)

// Exercise the production entry point in a child process. Only context injection
// and trust for the local certificate are test-specific; no production seam is added.
func TestTransportLifecycleHelper(t *testing.T) {
	mode := os.Getenv("AGR_TRANSPORT_LIFECYCLE_HELPER")
	if mode == "" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing CLI arguments")
	}
	os.Args = append([]string{"agr"}, os.Args[separator+1:]...)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	switch mode {
	case "timeout":
		var deadlineCancel context.CancelFunc
		ctx, deadlineCancel = context.WithTimeout(ctx, 5*time.Second)
		defer deadlineCancel()
	case "cancel":
		go func() {
			var signal [1]byte
			if _, err := io.ReadFull(os.Stdin, signal[:]); err == nil {
				cancel()
			}
		}()
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(os.Getenv("AGR_TRANSPORT_TEST_CERT"))) {
		t.Fatal("invalid local test certificate")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	http.DefaultTransport = transport
	cli.RootCmd().SetContext(ctx)
	main()
	os.Exit(0)
}

func TestTransportLifecycleFailures(t *testing.T) {
	for _, tc := range []struct {
		name, mode, code, kind string
		debug                  bool
		wantActions            []string
	}{
		{"cancel_after_dispatch", "cancel", "CANCELED", "error", false, []string{"StartSandboxInstance"}},
		{"timeout_after_dispatch", "timeout", "TIMEOUT", "timeout", false, []string{"StartSandboxInstance"}},
		{"failed_poll_cleans_debug_resources", "remote_failure", "WAIT_FAILED", "error", true, []string{
			"DescribeSandboxToolList", "CreateSandboxTool", "DescribeSandboxToolList", "StartSandboxInstance",
			"DescribeSandboxInstanceList", "DescribeSandboxInstanceList", "StopSandboxInstance", "DeleteSandboxTool",
		}},
		{"canceled_poll_cleans_debug_resources", "cancel", "CANCELED", "error", true, []string{
			"DescribeSandboxToolList", "CreateSandboxTool", "DescribeSandboxToolList", "StartSandboxInstance",
			"DescribeSandboxInstanceList", "StopSandboxInstance", "DeleteSandboxTool",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var actions []string
			polls := 0
			dispatched := make(chan struct{}, 1)
			disconnected := make(chan struct{}, 1)
			release := make(chan struct{})
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "" || r.Header.Get("X-TC-Token") != "test-session" {
					t.Error("missing signature or temporary credential")
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					return
				}
				action := r.Header.Get("X-TC-Action")
				mu.Lock()
				actions = append(actions, action)
				if action == "DescribeSandboxInstanceList" {
					polls++
				}
				poll := polls
				mu.Unlock()
				if tc.mode != "remote_failure" && ((!tc.debug && action == "StartSandboxInstance") || (tc.debug && action == "DescribeSandboxInstanceList")) {
					select {
					case dispatched <- struct{}{}:
					default:
					}
					select {
					case <-r.Context().Done():
						select {
						case disconnected <- struct{}{}:
						default:
						}
					case <-release:
					}
					return
				}
				response := map[string]any{"RequestId": "fixture"}
				switch action {
				case "DescribeSandboxToolList":
					ids, _ := payload["ToolIds"].([]any)
					if len(ids) != 1 {
						t.Errorf("unexpected tool lookup: %v", payload)
						return
					}
					response["SandboxToolSet"] = []any{map[string]any{
						"ToolId": ids[0], "ToolName": "fixture", "ToolType": "custom", "Status": "ACTIVE",
						"RoleArn": "fixture-role", "NetworkConfiguration": map[string]any{"NetworkMode": "PUBLIC"},
						"CustomConfiguration": map[string]any{"Image": "example/image"},
					}}
				case "CreateSandboxTool":
					response["ToolId"] = "sdt-debug"
				case "StartSandboxInstance":
					response["Instance"] = map[string]any{"InstanceId": "ssi-debug", "Status": "STARTING", "AuthMode": "NONE"}
				case "DescribeSandboxInstanceList":
					status := "STARTING"
					if poll > 1 {
						status = "FAILED"
					}
					response["InstanceSet"] = []any{map[string]any{"InstanceId": "ssi-debug", "Status": status, "StatusReason": "fixture boot failed"}}
				case "StopSandboxInstance":
					if payload["InstanceId"] != "ssi-debug" {
						t.Errorf("cleanup targeted wrong instance: %v", payload)
					}
				case "DeleteSandboxTool":
					if payload["ToolId"] != "sdt-debug" {
						t.Errorf("cleanup targeted wrong tool: %v", payload)
					}
				default:
					t.Errorf("unexpected action %s", action)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"Response": response})
			}))
			defer func() { close(release); server.Close() }()
			args := []string{"instance", "create", "--tool-id", "sdt-source", "-o", "json"}
			if tc.debug {
				args[1] = "debug"
			}
			ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestTransportLifecycleHelper$", "--"}, args...)...)
			home := t.TempDir()
			cmd.Env = []string{
				"HOME=" + home, "USERPROFILE=" + home, "PATH=" + os.Getenv("PATH"),
				"AGR_TRANSPORT_LIFECYCLE_HELPER=" + tc.mode, "GORACE=atexit_sleep_ms=0",
				"TENCENTCLOUD_SECRET_ID=fake", "TENCENTCLOUD_SECRET_KEY=fake", "TENCENTCLOUD_TOKEN=test-session",
				"AGR_REGION=ap-guangzhou", "AGR_CLOUD_ENDPOINT=" + strings.TrimPrefix(server.URL, "https://"), "AGR_TRANSPORT_TEST_CERT=" + string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})),
			}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stdin.Close() }()
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			if tc.mode != "remote_failure" {
				select {
				case <-dispatched:
				case err := <-done:
					t.Fatalf("exited before request arrived: %v\n%s\n%s", err, stdout.String(), stderr.String())
				case <-ctx.Done():
					<-done
					t.Fatal("request never reached server")
				}
				if tc.mode == "cancel" {
					if _, err := stdin.Write([]byte{1}); err != nil {
						t.Fatal(err)
					}
				}
			}
			err = <-done
			var exitErr *exec.ExitError
			if ctx.Err() != nil || !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("exit=%v context=%v stdout=%s stderr=%s", err, ctx.Err(), stdout.String(), stderr.String())
			}
			var result struct {
				Status  string
				Data    any
				Failure struct {
					Code, Kind string
					Details    map[string]any
				}
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatalf("invalid failure JSON: %v\n%s\n%s", err, stdout.String(), stderr.String())
			}
			if result.Status != "failed" || result.Data != nil || result.Failure.Code != tc.code || result.Failure.Kind != tc.kind {
				t.Fatalf("failure lost: %s; stderr=%s", stdout.String(), stderr.String())
			}
			if tc.mode == "remote_failure" {
				for key, want := range map[string]any{"ResourceId": "ssi-debug", "Operation": "create", "LastStatus": "FAILED", "Attempts": float64(2)} {
					if result.Failure.Details[key] != want {
						t.Errorf("failure details[%s]=%v, want %v", key, result.Failure.Details[key], want)
					}
				}
			} else {
				select {
				case <-disconnected:
				case <-time.After(2 * time.Second):
					t.Error("server request context was not canceled")
				}
			}
			mu.Lock()
			gotActions := slices.Clone(actions)
			mu.Unlock()
			if !slices.Equal(gotActions, tc.wantActions) {
				t.Errorf("actions=%v, want %v (no duplicate writes; exact cleanup)", gotActions, tc.wantActions)
			}
			if strings.Contains(stderr.String(), "failed to cleanup") {
				t.Errorf("cleanup failed: %s", stderr.String())
			}
		})
	}
}
