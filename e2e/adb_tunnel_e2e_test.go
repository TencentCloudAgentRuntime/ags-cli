//go:build e2e

// Package e2e provides end-to-end tests for the ADB tunnel feature.
//
// These tests verify the full chain: ags-cli adbtunnel library → mock-sandportal-proxy
// (WSS + Bearer auth) → SSH tunnel → adb-websockify → adbd (redroid).
//
// Prerequisites:
//   - Remote docker-compose environment running (android + android-sidecar)
//   - SSH tunnel to adb-websockify port (e.g., ssh -L 15556:localhost:5556 root@<server>)
//   - mock_sandportal_proxy.py running (python mock_sandportal_proxy.py --adb-backend localhost:15556 --adb-proxy-port 9443 --tls --token test-token)
//   - adb binary in PATH
//
// Run:
//
//	AGS_E2E_ENABLED=true go test -v -tags e2e -timeout 120s ./e2e/...
package e2e

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/adbtunnel"
)

const (
	defaultProxyEndpoint = "localhost:9443"
	defaultToken         = "test-token"
	defaultInstanceID    = "e2e-test-sandbox"
	defaultDomain        = "mock.local"
)

// testEnv holds the resolved E2E test environment configuration.
type testEnv struct {
	proxyEndpoint string
	token         string
	adbPath       string
}

// getEnv resolves test environment from environment variables with defaults.
func getEnv(t *testing.T) testEnv {
	t.Helper()

	if os.Getenv("AGS_E2E_ENABLED") != "true" {
		t.Skip("E2E tests disabled: set AGS_E2E_ENABLED=true to enable")
	}

	endpoint := os.Getenv("AGS_E2E_PROXY_ENDPOINT")
	if endpoint == "" {
		endpoint = defaultProxyEndpoint
	}

	token := os.Getenv("AGS_E2E_TOKEN")
	if token == "" {
		token = defaultToken
	}

	adbPath, err := exec.LookPath("adb")
	if err != nil {
		t.Fatalf("adb not found in PATH: %v", err)
	}

	return testEnv{
		proxyEndpoint: endpoint,
		token:         token,
		adbPath:       adbPath,
	}
}

// startTunnel creates and starts an ADB tunnel using the ags-cli adbtunnel library.
// Returns the tunnel and the local address (e.g., "127.0.0.1:12345").
func startTunnel(t *testing.T, env testEnv) (*adbtunnel.Tunnel, string) {
	t.Helper()

	tunnel, err := adbtunnel.New(adbtunnel.TunnelOptions{
		InstanceID: defaultInstanceID,
		Domain:     defaultDomain,
		TokenProvider: func() (string, error) {
			return env.token, nil
		},
		Endpoint:      env.proxyEndpoint,
		Insecure:      true, // Self-signed TLS on mock proxy
		ListenAddress: "127.0.0.1:0",
		Logger:        log.New(os.Stderr, "[tunnel] ", log.LstdFlags),
	})
	if err != nil {
		t.Fatalf("adbtunnel.New failed: %v", err)
	}

	addr, err := tunnel.Start()
	if err != nil {
		t.Fatalf("tunnel.Start failed: %v", err)
	}

	t.Cleanup(func() {
		tunnel.Stop()
	})

	return tunnel, addr
}

// runAdb executes an adb command and returns combined output.
func runAdb(t *testing.T, adbPath string, args ...string) string {
	t.Helper()
	cmd := exec.Command(adbPath, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Logf("adb %s output: %s", strings.Join(args, " "), out.String())
		t.Fatalf("adb %s failed: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(out.String())
}

// runAdbNoFail executes an adb command, returning output and error (does not fail test).
func runAdbNoFail(adbPath string, args ...string) (string, error) {
	cmd := exec.Command(adbPath, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

// adbConnect connects adb to the tunnel address with retries.
func adbConnect(t *testing.T, adbPath, addr string) {
	t.Helper()
	var lastErr error
	for i := 0; i < 5; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i) * 500 * time.Millisecond)
		}
		output, err := runAdbNoFail(adbPath, "connect", addr)
		if err == nil && (strings.Contains(output, "connected") || strings.Contains(output, "already connected")) {
			t.Logf("adb connect: %s", output)
			return
		}
		lastErr = fmt.Errorf("output=%q err=%v", output, err)
	}
	t.Fatalf("adb connect %s failed after retries: %v", addr, lastErr)
}

// adbDisconnect disconnects adb from the tunnel address (best-effort).
func adbDisconnect(adbPath, addr string) {
	_, _ = runAdbNoFail(adbPath, "disconnect", addr)
}

// ============================================================================
// E2E Test Cases
// ============================================================================

// TestProbe verifies the tunnel's Probe() method can reach the upstream
// adb-websockify through the mock proxy chain.
func TestProbe(t *testing.T) {
	env := getEnv(t)
	tunnel, _ := startTunnel(t, env)

	if err := tunnel.Probe(); err != nil {
		t.Fatalf("Probe failed: %v", err)
	}
	t.Log("Probe succeeded: upstream adb-websockify is reachable")
}

// TestBasicConnectivity establishes an ADB connection through the tunnel
// and verifies basic shell commands work.
func TestBasicConnectivity(t *testing.T) {
	env := getEnv(t)
	_, addr := startTunnel(t, env)

	t.Cleanup(func() { adbDisconnect(env.adbPath, addr) })

	// Connect
	adbConnect(t, env.adbPath, addr)

	// Run a basic shell command
	output := runAdb(t, env.adbPath, "-s", addr, "shell", "getprop", "ro.build.version.sdk")
	t.Logf("Android SDK version: %s", output)

	// SDK version should be a number (e.g., "33", "34")
	if output == "" {
		t.Fatal("getprop ro.build.version.sdk returned empty")
	}
	for _, c := range output {
		if c < '0' || c > '9' {
			t.Fatalf("unexpected SDK version: %q (expected numeric)", output)
		}
	}
}

// TestShellEcho verifies bidirectional data transfer through the tunnel
// by echoing a string through the Android shell.
func TestShellEcho(t *testing.T) {
	env := getEnv(t)
	_, addr := startTunnel(t, env)

	t.Cleanup(func() { adbDisconnect(env.adbPath, addr) })

	adbConnect(t, env.adbPath, addr)

	testStr := "hello from ags-cli adbtunnel e2e test"
	output := runAdb(t, env.adbPath, "-s", addr, "shell", "echo", testStr)
	if output != testStr {
		t.Fatalf("echo mismatch:\n  got:  %q\n  want: %q", output, testStr)
	}
	t.Logf("Shell echo verified: %q", output)
}

// TestLogcat verifies streaming data (server → client) works by reading logcat entries.
func TestLogcat(t *testing.T) {
	env := getEnv(t)
	_, addr := startTunnel(t, env)

	t.Cleanup(func() { adbDisconnect(env.adbPath, addr) })

	adbConnect(t, env.adbPath, addr)

	output := runAdb(t, env.adbPath, "-s", addr, "shell", "logcat", "-d", "-t", "5")
	lines := strings.Split(output, "\n")
	if len(lines) < 1 || output == "" {
		t.Fatal("logcat returned no output")
	}
	t.Logf("Logcat returned %d lines (first: %s)", len(lines), truncate(lines[0], 100))
}

// TestFileTransferMD5 pushes a 1MB random file and verifies MD5 integrity.
func TestFileTransferMD5(t *testing.T) {
	env := getEnv(t)
	_, addr := startTunnel(t, env)

	t.Cleanup(func() { adbDisconnect(env.adbPath, addr) })

	adbConnect(t, env.adbPath, addr)

	// Generate 1MB random file
	data := make([]byte, 1*1024*1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("failed to generate random data: %v", err)
	}
	localMD5 := fmt.Sprintf("%x", md5.Sum(data))

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "ags-e2e-*.bin")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(data); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	remotePath := "/data/local/tmp/ags_e2e_test.bin"

	// Push file
	runAdb(t, env.adbPath, "-s", addr, "push", tmpFile.Name(), remotePath)
	t.Logf("Pushed 1MB file to %s", remotePath)

	// Get remote MD5
	remoteMD5Output := runAdb(t, env.adbPath, "-s", addr, "shell", "md5sum", remotePath)
	// md5sum output format: "<hash>  <path>"
	parts := strings.Fields(remoteMD5Output)
	if len(parts) < 1 {
		t.Fatalf("md5sum returned unexpected output: %q", remoteMD5Output)
	}
	remoteMD5 := parts[0]

	if localMD5 != remoteMD5 {
		t.Fatalf("MD5 mismatch:\n  local:  %s\n  remote: %s", localMD5, remoteMD5)
	}
	t.Logf("MD5 verified: %s (1MB file transfer integrity OK)", localMD5)

	// Cleanup remote file
	runAdb(t, env.adbPath, "-s", addr, "shell", "rm", "-f", remotePath)
}

// TestKeepalive verifies the connection survives idle periods (>30s, exceeding
// one Ping interval) by sleeping and then executing a command.
func TestKeepalive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping keepalive test in short mode")
	}

	env := getEnv(t)
	_, addr := startTunnel(t, env)

	t.Cleanup(func() { adbDisconnect(env.adbPath, addr) })

	adbConnect(t, env.adbPath, addr)

	// Verify connection works before sleep
	before := runAdb(t, env.adbPath, "-s", addr, "shell", "echo", "before-sleep")
	if before != "before-sleep" {
		t.Fatalf("pre-sleep echo failed: %q", before)
	}

	// Sleep for 35s (exceeds 30s Ping interval)
	t.Log("Sleeping 35s to test keepalive across Ping interval...")
	time.Sleep(35 * time.Second)

	// Verify connection still works after sleep
	after := runAdb(t, env.adbPath, "-s", addr, "shell", "echo", "after-sleep")
	if after != "after-sleep" {
		t.Fatalf("post-sleep echo failed: %q", after)
	}
	t.Log("Keepalive verified: connection survived 35s idle period")
}

// TestLargeFileTransfer pushes a 50MB file to stress-test the tunnel's streaming
// capability and verify no data corruption.
func TestLargeFileTransfer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file transfer test in short mode")
	}

	env := getEnv(t)
	_, addr := startTunnel(t, env)

	t.Cleanup(func() { adbDisconnect(env.adbPath, addr) })

	adbConnect(t, env.adbPath, addr)

	// Generate 50MB random file
	const fileSize = 50 * 1024 * 1024
	data := make([]byte, fileSize)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("failed to generate random data: %v", err)
	}
	localMD5 := fmt.Sprintf("%x", md5.Sum(data))

	tmpFile, err := os.CreateTemp("", "ags-e2e-large-*.bin")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(data); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	remotePath := "/data/local/tmp/ags_e2e_large_test.bin"

	start := time.Now()
	runAdb(t, env.adbPath, "-s", addr, "push", tmpFile.Name(), remotePath)
	elapsed := time.Since(start)
	throughput := float64(fileSize) / elapsed.Seconds() / (1024 * 1024)
	t.Logf("Pushed 50MB in %v (%.1f MB/s)", elapsed.Round(time.Millisecond), throughput)

	// Verify MD5
	remoteMD5Output := runAdb(t, env.adbPath, "-s", addr, "shell", "md5sum", remotePath)
	parts := strings.Fields(remoteMD5Output)
	if len(parts) < 1 {
		t.Fatalf("md5sum returned unexpected output: %q", remoteMD5Output)
	}
	remoteMD5 := parts[0]

	if localMD5 != remoteMD5 {
		t.Fatalf("MD5 mismatch on 50MB file:\n  local:  %s\n  remote: %s", localMD5, remoteMD5)
	}
	t.Logf("50MB file transfer integrity verified: %s", localMD5)

	// Cleanup
	runAdb(t, env.adbPath, "-s", addr, "shell", "rm", "-f", remotePath)
}

// TestAuthRejection verifies the mock proxy rejects connections with invalid tokens.
func TestAuthRejection(t *testing.T) {
	env := getEnv(t)

	tunnel, err := adbtunnel.New(adbtunnel.TunnelOptions{
		InstanceID: defaultInstanceID,
		Domain:     defaultDomain,
		TokenProvider: func() (string, error) {
			return "wrong-token", nil // Invalid token
		},
		Endpoint:      env.proxyEndpoint,
		Insecure:      true,
		ListenAddress: "127.0.0.1:0",
		Logger:        log.New(os.Stderr, "[tunnel-bad-auth] ", log.LstdFlags),
	})
	if err != nil {
		t.Fatalf("adbtunnel.New failed: %v", err)
	}

	_, err = tunnel.Start()
	if err != nil {
		t.Fatalf("tunnel.Start failed: %v", err)
	}
	defer tunnel.Stop()

	// Probe should fail with unauthorized
	err = tunnel.Probe()
	if err == nil {
		t.Fatal("Probe with wrong token should have failed, but succeeded")
	}
	t.Logf("Auth rejection verified: %v", err)
}

// ============================================================================
// Helpers
// ============================================================================

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
