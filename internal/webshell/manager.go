package webshell

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-go-sdk/sandbox/code"
	"github.com/TencentCloudAgentRuntime/ags-go-sdk/tool/command"
)

const (
	// ttyd version and download URL
	ttydVersion = "1.7.7"
	ttydBaseURL = "https://github.com/tsl0922/ttyd/releases/download"
	ttydPort    = 8080
)

// Manager defines the webshell manager interface
type Manager interface {
	// IsRunning checks if ttyd is running in the specified instance
	IsRunning(ctx context.Context, instanceID string) (bool, error)

	// Download downloads ttyd binary to the specified instance
	Download(ctx context.Context, instanceID string) error

	// UploadTTYD uploads a custom ttyd binary to the specified instance
	UploadTTYD(ctx context.Context, instanceID string, ttydPath string) error

	// Start starts ttyd service in the specified instance
	Start(ctx context.Context, instanceID string, accessToken string) error

	// Stop stops ttyd service in the specified instance
	Stop(ctx context.Context, instanceID string) error
}

// manager implements the Manager interface
type manager struct {
	connectOpts []code.ConnectOption
}

// NewManager creates a new webshell manager
func NewManager(opts ...code.ConnectOption) Manager {
	return &manager{
		connectOpts: opts,
	}
}

// getSandbox connects to the sandbox instance
func (m *manager) getSandbox(ctx context.Context, instanceID string) (*code.Sandbox, error) {
	sandbox, err := code.Connect(ctx, instanceID, m.connectOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to instance %s: %w", instanceID, err)
	}
	return sandbox, nil
}

// IsRunning checks if ttyd is running and responding
func (m *manager) IsRunning(ctx context.Context, instanceID string) (bool, error) {
	sandbox, err := m.getSandbox(ctx, instanceID)
	if err != nil {
		return false, err
	}

	// Check if process exists
	result, err := sandbox.Commands.Run(ctx, "pgrep -f 'ttyd.*--port 8080' >/dev/null && echo running || echo stopped", nil, nil)
	if err != nil {
		return false, fmt.Errorf("failed to check ttyd status: %w", err)
	}

	if strings.TrimSpace(string(result.Stdout)) != "running" {
		return false, nil
	}

	// Also check if service is actually responding using HEAD request
	// Try multiple tools for compatibility
	checkCmd := fmt.Sprintf(`
if command -v curl >/dev/null 2>&1; then
    curl -s -o /dev/null -w '%%{http_code}' http://localhost:%d/ 2>/dev/null
elif command -v wget >/dev/null 2>&1; then
    wget -q --spider -S http://localhost:%d/ 2>&1 | grep 'HTTP/' | awk '{print $2}' | tail -1
else
    # Use Perl LWP as fallback
    perl -MLWP::Simple -e 'my $r = head("http://localhost:%d/"); print $r ? "200" : "000"' 2>/dev/null
fi
`, ttydPort, ttydPort, ttydPort)

	result, err = sandbox.Commands.Run(ctx, checkCmd, nil, nil)
	if err != nil {
		return false, nil // Process exists but can't check HTTP, assume not running properly
	}

	httpCode := strings.TrimSpace(string(result.Stdout))
	// 200 or 401 means service is responding (401 = requires auth)
	return httpCode == "200" || httpCode == "401", nil
}

// Download downloads ttyd binary to the specified instance
func (m *manager) Download(ctx context.Context, instanceID string) error {
	sandbox, err := m.getSandbox(ctx, instanceID)
	if err != nil {
		return err
	}

	// Check if already downloaded
	result, err := sandbox.Commands.Run(ctx, "test -x /tmp/ttyd && echo exists || echo missing", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to check ttyd binary: %w", err)
	}

	if strings.TrimSpace(string(result.Stdout)) == "exists" {
		return nil
	}

	// Get system architecture
	result, err = sandbox.Commands.Run(ctx, "uname -m", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to get system architecture: %w", err)
	}

	arch := "x86_64"
	sysArch := strings.TrimSpace(string(result.Stdout))
	switch sysArch {
	case "aarch64", "arm64":
		arch = "aarch64"
	case "armv7l":
		arch = "armv7"
	case "x86_64", "amd64":
		arch = "x86_64"
	default:
		return fmt.Errorf("unsupported architecture: %s", sysArch)
	}

	// Download ttyd - try multiple download tools for compatibility
	downloadURL := fmt.Sprintf("%s/%s/ttyd.%s", ttydBaseURL, ttydVersion, arch)

	// Try curl first, then wget, then lwp-download (Perl)
	downloadCmd := fmt.Sprintf(`
if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o /tmp/ttyd '%s'
elif command -v wget >/dev/null 2>&1; then
    wget -q -O /tmp/ttyd '%s'
elif command -v lwp-download >/dev/null 2>&1; then
    lwp-download '%s' /tmp/ttyd
else
    echo "No download tool available (curl, wget, or lwp-download)" >&2
    exit 1
fi
chmod +x /tmp/ttyd
`, downloadURL, downloadURL, downloadURL)

	result, err = sandbox.Commands.Run(ctx, downloadCmd, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to download ttyd: %w", err)
	}

	if result.ExitCode != 0 {
		return fmt.Errorf("failed to download ttyd (exit code %d): %s", result.ExitCode, string(result.Stderr))
	}

	return nil
}

// Start starts ttyd service in the specified instance using Commands.Start for background execution
func (m *manager) Start(ctx context.Context, instanceID string, accessToken string) error {
	// Check if already running
	running, err := m.IsRunning(ctx, instanceID)
	if err != nil {
		return err
	}
	if running {
		return nil
	}

	sandbox, err := m.getSandbox(ctx, instanceID)
	if err != nil {
		return err
	}

	// Stop any existing process first
	_, _ = sandbox.Commands.Run(ctx, "pkill -f 'ttyd.*--port 8080' 2>/dev/null || true", nil, nil)

	// Start ttyd in background using Commands.Start
	// Note: ttyd doesn't need --credential when accessed through AGS proxy (proxy handles auth)
	ttydCmd := fmt.Sprintf("/tmp/ttyd --port %d --interface 0.0.0.0 --writable bash",
		ttydPort)

	_, err = sandbox.Commands.Start(ctx, ttydCmd, &command.ProcessConfig{
		User: "user",
	}, nil)
	if err != nil {
		return fmt.Errorf("failed to start ttyd: %w", err)
	}

	// Wait for service to be ready
	if err := m.waitForService(ctx, instanceID, 10*time.Second); err != nil {
		return err
	}

	return nil
}

// waitForService waits for ttyd service to be ready
func (m *manager) waitForService(ctx context.Context, instanceID string, timeout time.Duration) error {
	sandbox, err := m.getSandbox(ctx, instanceID)
	if err != nil {
		return err
	}

	// Build check command that works with curl, wget, or perl
	checkCmd := fmt.Sprintf(`
if command -v curl >/dev/null 2>&1; then
    curl -s -o /dev/null -w '%%{http_code}' http://localhost:%d/ 2>/dev/null
elif command -v wget >/dev/null 2>&1; then
    wget -q --spider -S http://localhost:%d/ 2>&1 | grep 'HTTP/' | awk '{print $2}' | tail -1
else
    perl -MLWP::Simple -e 'my $r = head("http://localhost:%d/"); print $r ? "200" : "000"' 2>/dev/null
fi
`, ttydPort, ttydPort, ttydPort)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		result, err := sandbox.Commands.Run(ctx, checkCmd, nil, nil)
		if err == nil {
			httpCode := strings.TrimSpace(string(result.Stdout))
			// 200 means ttyd is running (no auth), 401 means requires auth
			if httpCode == "200" || httpCode == "401" {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	return fmt.Errorf("ttyd service did not become ready within %v", timeout)
}

// Stop stops ttyd service in the specified instance
func (m *manager) Stop(ctx context.Context, instanceID string) error {
	sandbox, err := m.getSandbox(ctx, instanceID)
	if err != nil {
		return err
	}

	_, err = sandbox.Commands.Run(ctx, "pkill -f 'ttyd.*--port 8080' 2>/dev/null || true", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to stop ttyd: %w", err)
	}

	return nil
}

// UploadTTYD uploads a custom ttyd binary to the specified instance
func (m *manager) UploadTTYD(ctx context.Context, instanceID string, ttydPath string) error {
	// Validate local ttyd file
	if err := validateTTYDBinary(ttydPath); err != nil {
		return fmt.Errorf("ttyd binary validation failed: %w", err)
	}

	sandbox, err := m.getSandbox(ctx, instanceID)
	if err != nil {
		return err
	}

	// Check if ttyd already exists and is valid
	result, err := sandbox.Commands.Run(ctx, "test -x /tmp/ttyd && echo exists || echo missing", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to check existing ttyd binary: %w", err)
	}

	if strings.TrimSpace(string(result.Stdout)) == "exists" {
		return nil // ttyd already exists
	}

	// Open local ttyd file
	file, err := os.Open(ttydPath)
	if err != nil {
		return fmt.Errorf("failed to open ttyd binary file: %w", err)
	}
	defer file.Close()

	// Upload ttyd binary to sandbox
	_, err = sandbox.Files.Write(ctx, "/tmp/ttyd", file, nil)
	if err != nil {
		return fmt.Errorf("failed to upload ttyd binary: %w", err)
	}

	// Set executable permissions
	result, err = sandbox.Commands.Run(ctx, "chmod +x /tmp/ttyd", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to set ttyd executable permissions: %w", err)
	}

	if result.ExitCode != 0 {
		return fmt.Errorf("failed to set ttyd executable permissions (exit code %d): %s", result.ExitCode, string(result.Stderr))
	}

	return nil
}

// validateTTYDBinary validates the local ttyd binary file
func validateTTYDBinary(ttydPath string) error {
	// Check if file exists
	info, err := os.Stat(ttydPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("ttyd binary file does not exist: %s", ttydPath)
		}
		return fmt.Errorf("failed to stat ttyd binary file: %w", err)
	}

	// Check if it's a regular file
	if !info.Mode().IsRegular() {
		return fmt.Errorf("ttyd binary path is not a regular file: %s", ttydPath)
	}

	// Check if file is readable
	file, err := os.Open(ttydPath)
	if err != nil {
		return fmt.Errorf("ttyd binary file is not readable: %w", err)
	}
	file.Close()

	// Check file size (ttyd should be at least 1MB, but not larger than 50MB)
	if info.Size() < 1024*1024 {
		return fmt.Errorf("ttyd binary file is too small (< 1MB), might not be a valid binary: %s", ttydPath)
	}
	if info.Size() > 50*1024*1024 {
		return fmt.Errorf("ttyd binary file is too large (> 50MB), might not be a valid ttyd binary: %s", ttydPath)
	}

	return nil
}
