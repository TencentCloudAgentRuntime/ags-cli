package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/adbtunnel"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/tunnelstore"
)

var (
	// mobile tunnel flags
	daemonFlag bool
	portFlag   int

	// mobile disconnect flags
	disconnectAll bool
)

func init() {
	addMobileCommand(rootCmd)
}

// addMobileCommand adds the mobile command group to a parent command.
func addMobileCommand(parent *cobra.Command) {
	mobileCmd := &cobra.Command{
		Use:     "mobile",
		Aliases: []string{"m"},
		Short:   "Mobile sandbox ADB commands",
		Long: `Manage ADB connections to mobile sandboxes.

Provides secure ADB access to remote Android sandboxes through encrypted
WebSocket tunnels. Supports multiple concurrent connections with automatic
reconnection on network disruptions.

Examples:
  # Connect to a mobile sandbox (background tunnel + adb connect)
  ags mobile connect <sandbox_id>

  # List active connections
  ags mobile list

  # Execute adb command using sandbox ID
  ags mobile adb <sandbox_id> shell ls /sdcard

  # Disconnect
  ags mobile disconnect <sandbox_id>`,
	}

	// tunnel subcommand — foreground blocking tunnel (internal/debug)
	tunnelCmd := &cobra.Command{
		Use:   "tunnel <sandbox_id>",
		Short: "Run ADB tunnel in foreground (used internally by connect)",
		Long: `Run an ADB WebSocket tunnel in the foreground.

This is primarily used internally by 'ags mobile connect' to spawn a background
tunnel process. Can also be used directly for debugging.

The tunnel bridges local TCP connections to the remote adb-websockify server
via an encrypted WebSocket connection through SandPortal.`,
		Args: cobra.ExactArgs(1),
		RunE: runMobileTunnel,
	}
	tunnelCmd.Flags().BoolVar(&daemonFlag, "daemon", false, "Run in daemon mode (used by connect)")
	tunnelCmd.Flags().IntVar(&portFlag, "port", 0, "Local port to listen on (0 = auto-assign)")

	// connect subcommand — background tunnel + adb connect
	connectCmd := &cobra.Command{
		Use:   "connect <sandbox_id>",
		Short: "Connect to mobile sandbox (background tunnel + adb connect)",
		Long: `Connect to a mobile sandbox by establishing a background ADB tunnel.

This command:
1. Acquires an access token for the sandbox
2. Spawns a background tunnel process
3. Automatically runs 'adb connect' to the local tunnel port
4. Records the connection in ~/.ags/tunnels.json

After connecting, you can use native adb commands directly:
  adb -s 127.0.0.1:<port> shell
  adb -s 127.0.0.1:<port> push local.apk /sdcard/

Or use 'ags mobile adb' with the sandbox ID for convenience.`,
		Args: cobra.ExactArgs(1),
		RunE: runMobileConnect,
	}

	// disconnect subcommand
	disconnectCmd := &cobra.Command{
		Use:   "disconnect [sandbox_id]",
		Short: "Disconnect from mobile sandbox",
		Long: `Disconnect from a mobile sandbox by terminating the background tunnel
and running 'adb disconnect'.

Use --all to disconnect from all active sandboxes.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runMobileDisconnect,
	}
	disconnectCmd.Flags().BoolVar(&disconnectAll, "all", false, "Disconnect all active connections")

	// list subcommand
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List active mobile sandbox connections",
		Long:  `List all active ADB tunnel connections with their sandbox IDs, local ports, and status.`,
		Args:  cobra.NoArgs,
		RunE:  runMobileList,
	}

	// adb subcommand — execute adb commands by sandbox ID
	adbCmd := &cobra.Command{
		Use:   "adb <sandbox_id> [adb_args...]",
		Short: "Execute adb command on mobile sandbox by ID",
		Long: `Execute an adb command targeting a specific mobile sandbox using its ID.

This looks up the local port from the active tunnel mapping and passes the
command through to the native adb binary with the correct -s flag.

Examples:
  ags mobile adb sandbox-aaa shell ls /sdcard
  ags mobile adb sandbox-aaa install app.apk
  ags mobile adb sandbox-aaa logcat`,
		Args:               cobra.MinimumNArgs(1),
		RunE:               runMobileAdb,
		DisableFlagParsing: true, // Pass all args through to adb
	}

	mobileCmd.AddCommand(tunnelCmd, connectCmd, disconnectCmd, listCmd, adbCmd)
	parent.AddCommand(mobileCmd)
}

// readyMessage is the JSON protocol message sent by tunnel --daemon to stdout.
type readyMessage struct {
	Status  string `json:"status"`
	Port    int    `json:"port,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Message string `json:"message,omitempty"`
}

// runMobileTunnel runs a foreground ADB tunnel.
func runMobileTunnel(_ *cobra.Command, args []string) error {
	sandboxID := args[0]

	if err := config.Validate(); err != nil {
		return exitError(1, err)
	}

	// Build the token provider using the same pattern as acquireInstanceToken
	tokenProvider := func() (string, error) {
		return acquireInstanceToken(context.Background(), sandboxID)
	}

	cfg := config.Get()
	domain := cfg.DataPlaneRegionDomain()

	listenAddr := "127.0.0.1:0"
	if portFlag > 0 {
		listenAddr = fmt.Sprintf("127.0.0.1:%d", portFlag)
	}

	tunnel, err := adbtunnel.New(adbtunnel.TunnelOptions{
		InstanceID:    sandboxID,
		Domain:        domain,
		TokenProvider: tokenProvider,
		ListenAddress: listenAddr,
		Insecure:      false,
	})
	if err != nil {
		return exitError(1, fmt.Errorf("failed to create tunnel: %w", err))
	}

	addr, err := tunnel.Start()
	if err != nil {
		return exitError(3, fmt.Errorf("failed to start tunnel: %w", err))
	}

	// Probe upstream to verify full connectivity before declaring ready
	if err := tunnel.Probe(); err != nil {
		tunnel.Stop()
		if daemonFlag {
			errMsg := readyMessage{Status: "error", Message: err.Error()}
			_ = json.NewEncoder(os.Stderr).Encode(errMsg)
		}
		return exitError(2, fmt.Errorf("upstream probe failed: %w", err))
	}

	_, portStr, _ := strings.Cut(addr, ":")

	if daemonFlag {
		// Daemon mode: output JSON ready message on stdout
		msg := readyMessage{
			Status: "ready",
			Port:   mustAtoi(portStr),
			PID:    os.Getpid(),
		}
		if err := json.NewEncoder(os.Stdout).Encode(msg); err != nil {
			tunnel.Stop()
			return exitError(1, fmt.Errorf("failed to write ready message: %w", err))
		}
	} else {
		// Interactive mode: human-readable output
		fmt.Printf("[Ready] ADB Tunnel established at %s\n", addr)
		fmt.Println("[Ready] Press Ctrl+C to disconnect.")
	}

	// Wait for signal with graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	if !daemonFlag {
		fmt.Println("\n[INFO] Shutting down ADB tunnel...")
	}

	// Graceful shutdown with 5s timeout
	shutdownDone := make(chan struct{})
	go func() {
		tunnel.Stop()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
	case <-time.After(5 * time.Second):
		if !daemonFlag {
			fmt.Println("[WARN] Graceful shutdown timed out. Forcing exit.")
		}
	}

	return nil
}

// runMobileConnect spawns a background tunnel process and connects adb.
func runMobileConnect(_ *cobra.Command, args []string) error {
	sandboxID := args[0]

	if err := config.Validate(); err != nil {
		return err
	}

	// Check adb is available
	adbPath, err := requireAdb()
	if err != nil {
		return err
	}

	// Initialize tunnel store
	store, err := tunnelstore.NewStore()
	if err != nil {
		return fmt.Errorf("failed to initialize tunnel store: %w", err)
	}

	// Clean up any existing tunnel for this sandbox
	// First, disconnect old adb address if there was a previous tunnel
	if oldEntry, ok, _ := store.Get(sandboxID); ok {
		oldAddr := fmt.Sprintf("127.0.0.1:%d", oldEntry.Port)
		_ = runAdbCommand(adbPath, "disconnect", oldAddr)
	}
	if err := store.Cleanup(sandboxID); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to cleanup existing tunnel: %v\n", err)
	}

	// Spawn background tunnel process
	selfPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Build the command with the same global flags
	tunnelArgs := []string{"mobile", "tunnel", sandboxID, "--daemon", "--port=0"}
	// Pass through essential global flags (non-sensitive only via CLI args)
	if backend != "" {
		tunnelArgs = append(tunnelArgs, "--backend", backend)
	}
	if region != "" {
		tunnelArgs = append(tunnelArgs, "--region", region)
	}
	if domain != "" {
		tunnelArgs = append(tunnelArgs, "--domain", domain)
	}
	if internal {
		tunnelArgs = append(tunnelArgs, "--internal")
	}

	cmd := exec.Command(selfPath, tunnelArgs...)
	// Redirect tunnel stderr to a log file instead of parent terminal
	// to avoid background reconnection logs polluting the user's shell.
	if homeDir, homeErr := os.UserHomeDir(); homeErr == nil {
		logDir := filepath.Join(homeDir, ".ags")
		_ = os.MkdirAll(logDir, 0700)
		logFile, logErr := os.OpenFile(
			filepath.Join(logDir, fmt.Sprintf("tunnel-%s.log", sandboxID)),
			os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600,
		)
		if logErr == nil {
			cmd.Stderr = logFile
		}
	}

	// Pass sensitive credentials via environment variables instead of CLI args
	// to avoid exposure in process listing (ps aux).
	// The child process reads these via viper.BindEnv in config.go.
	cmd.Env = os.Environ()
	if e2bAPIKey != "" {
		cmd.Env = append(cmd.Env, "AGS_E2B_API_KEY="+e2bAPIKey)
	}
	if cloudSecretID != "" {
		cmd.Env = append(cmd.Env, "AGS_CLOUD_SECRET_ID="+cloudSecretID)
	}
	if cloudSecretKey != "" {
		cmd.Env = append(cmd.Env, "AGS_CLOUD_SECRET_KEY="+cloudSecretKey)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start tunnel process: %w", err)
	}

	// Reap the child process in the background to prevent zombie accumulation
	go func() { _ = cmd.Wait() }()

	// Read ready message with timeout
	readyCh := make(chan readyMessage, 1)
	errCh := make(chan error, 1)

	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			var msg readyMessage
			if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
				errCh <- fmt.Errorf("failed to parse tunnel ready message: %w", err)
				return
			}
			readyCh <- msg
		} else {
			if err := scanner.Err(); err != nil {
				errCh <- fmt.Errorf("failed to read tunnel output: %w", err)
			} else {
				errCh <- fmt.Errorf("tunnel process exited without ready message")
			}
		}
	}()

	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	var ready readyMessage
	select {
	case ready = <-readyCh:
		if ready.Status != "ready" || ready.Port == 0 {
			_ = cmd.Process.Kill()
			return fmt.Errorf("tunnel reported error: %s", ready.Message)
		}
	case err := <-errCh:
		_ = cmd.Process.Kill()
		return err
	case <-timer.C:
		_ = cmd.Process.Kill()
		return fmt.Errorf("tunnel did not become ready within 30s")
	}

	// Save to tunnel store (include exe path for PID reuse protection)
	if err := store.Save(sandboxID, tunnelstore.TunnelEntry{
		PID:       ready.PID,
		Port:      ready.Port,
		CreatedAt: time.Now(),
		ExePath:   selfPath,
	}); err != nil {
		// Non-fatal: tunnel is running, just can't track it
		fmt.Fprintf(os.Stderr, "Warning: failed to save tunnel mapping: %v\n", err)
	}

	// Run adb connect with retries
	adbAddr := fmt.Sprintf("127.0.0.1:%d", ready.Port)
	if err := adbConnectWithRetry(adbPath, adbAddr, 3); err != nil {
		output.PrintInfo(fmt.Sprintf("tunnel ready for %s at %s (adb connect failed: %v; use 'adb connect %s' manually)", sandboxID, adbAddr, err, adbAddr))
	} else {
		output.PrintInfo(fmt.Sprintf("connected to %s (%s)", sandboxID, adbAddr))
	}
	if homeDir, homeErr := os.UserHomeDir(); homeErr == nil {
		logPath := filepath.Join(homeDir, ".ags", fmt.Sprintf("tunnel-%s.log", sandboxID))
		output.PrintInfo(fmt.Sprintf("tunnel log: %s", logPath))
	}
	return nil
}

// runMobileDisconnect stops a tunnel and runs adb disconnect.
func runMobileDisconnect(_ *cobra.Command, args []string) error {
	store, err := tunnelstore.NewStore()
	if err != nil {
		return fmt.Errorf("failed to initialize tunnel store: %w", err)
	}

	if disconnectAll {
		return disconnectAllTunnels(store)
	}

	if len(args) == 0 {
		return fmt.Errorf("must specify sandbox_id or use --all")
	}

	sandboxID := args[0]

	// Look up the tunnel entry
	entry, ok, err := store.Get(sandboxID)
	if err != nil {
		return fmt.Errorf("failed to read tunnel store: %w", err)
	}
	if !ok {
		return fmt.Errorf("no active tunnel for %s", sandboxID)
	}

	// Try adb disconnect (best-effort, don't fail if adb is not available)
	if adbPath, err := requireAdb(); err == nil {
		adbAddr := fmt.Sprintf("127.0.0.1:%d", entry.Port)
		_ = runAdbCommand(adbPath, "disconnect", adbAddr)
	}

	// Kill the tunnel process and remove from store
	if err := store.Cleanup(sandboxID); err != nil {
		return fmt.Errorf("failed to cleanup tunnel: %w", err)
	}

	output.PrintInfo(fmt.Sprintf("disconnected from %s", sandboxID))
	return nil
}

func disconnectAllTunnels(store *tunnelstore.Store) error {
	entries, err := store.List()
	if err != nil {
		return fmt.Errorf("failed to list tunnels: %w", err)
	}

	if len(entries) == 0 {
		output.PrintInfo("no active connections")
		return nil
	}

	// Try adb disconnect for each (best-effort)
	adbPath, _ := requireAdb()

	for id, entry := range entries {
		if adbPath != "" {
			adbAddr := fmt.Sprintf("127.0.0.1:%d", entry.Port)
			_ = runAdbCommand(adbPath, "disconnect", adbAddr)
		}
		output.PrintInfo(fmt.Sprintf("disconnected from %s", id))
	}

	// Kill all and clear store
	if err := store.CleanupAll(); err != nil {
		return fmt.Errorf("failed to cleanup tunnels: %w", err)
	}

	return nil
}

// runMobileList displays active tunnel connections.
func runMobileList(_ *cobra.Command, _ []string) error {
	store, err := tunnelstore.NewStore()
	if err != nil {
		return fmt.Errorf("failed to initialize tunnel store: %w", err)
	}

	entries, err := store.List()
	if err != nil {
		return fmt.Errorf("failed to list tunnels: %w", err)
	}

	f := output.NewFormatter()

	if f.IsJSON() {
		items := make([]map[string]any, 0, len(entries))
		for id, entry := range entries {
			addr := fmt.Sprintf("127.0.0.1:%d", entry.Port)
			items = append(items, map[string]any{
				"sandbox_id":  id,
				"adb_address": addr,
				"port":        entry.Port,
				"pid":         entry.PID,
				"created_at":  entry.CreatedAt.Format(time.RFC3339),
				"status":      "connected",
			})
		}
		return f.PrintJSON(map[string]any{"items": items, "total": len(items)})
	}

	if len(entries) == 0 {
		fmt.Println("No active connections.")
		fmt.Println("Use 'ags mobile connect <sandbox_id>' to connect to a mobile sandbox.")
		return nil
	}

	// Text output: formatted table
	fmt.Printf("%-24s %-22s %s\n", "SANDBOX", "ADB ADDRESS", "STATUS")
	for id, entry := range entries {
		addr := fmt.Sprintf("127.0.0.1:%d", entry.Port)
		// Note: we do NOT probe the tunnel port here (e.g., via net.Dial) because
		// each TCP connection to the tunnel opens a new WebSocket to the server,
		// and the server only allows one WS connection per sandbox. A probe would
		// preempt the active ADB connection, causing "error: closed" on the next
		// user command. The store.List() already filters out zombie entries (dead PIDs).
		fmt.Printf("%-24s %-22s %s\n", id, addr, "connected")
	}

	return nil
}

// runMobileAdb executes an adb command targeting a specific sandbox by ID.
func runMobileAdb(_ *cobra.Command, args []string) error {
	sandboxID := args[0]
	adbArgs := args[1:]

	adbPath, err := requireAdb()
	if err != nil {
		return err
	}

	store, err := tunnelstore.NewStore()
	if err != nil {
		return fmt.Errorf("failed to initialize tunnel store: %w", err)
	}

	entry, ok, err := store.Get(sandboxID)
	if err != nil {
		return fmt.Errorf("failed to read tunnel store: %w", err)
	}
	if !ok {
		return fmt.Errorf("no active tunnel for %s; run 'ags mobile connect %s' first", sandboxID, sandboxID)
	}

	adbAddr := fmt.Sprintf("127.0.0.1:%d", entry.Port)

	// Build adb command: adb -s <addr> <user_args...>
	fullArgs := append([]string{"-s", adbAddr}, adbArgs...)
	cmd := exec.Command(adbPath, fullArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// requireAdb finds the adb binary, checking ADB_PATH env var first, then PATH.
func requireAdb() (string, error) {
	// Check ADB_PATH environment variable
	if p := os.Getenv("ADB_PATH"); p != "" {
		info, err := os.Lstat(p)
		if err != nil {
			return "", fmt.Errorf("ADB_PATH=%q not accessible: %w", p, err)
		}
		// Reject symlinks to prevent path hijacking
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("ADB_PATH=%q is a symlink (not allowed for security)", p)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("ADB_PATH=%q is not a regular file", p)
		}
		if runtime.GOOS == "windows" {
			if !strings.HasSuffix(strings.ToLower(p), ".exe") {
				return "", fmt.Errorf("ADB_PATH=%q does not have .exe extension (required on Windows)", p)
			}
		} else {
			if info.Mode()&0111 == 0 {
				return "", fmt.Errorf("ADB_PATH=%q is not executable", p)
			}
		}
		return p, nil
	}

	path, err := exec.LookPath("adb")
	if err != nil {
		return "", fmt.Errorf("adb not found in PATH; install Android SDK Platform-Tools or set ADB_PATH")
	}
	return path, nil
}

// adbConnectWithRetry runs 'adb connect <addr>' with bounded retries.
func adbConnectWithRetry(adbPath, addr string, maxRetries int) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(500 * time.Millisecond)
		}
		out, err := exec.Command(adbPath, "connect", addr).CombinedOutput()
		if err != nil {
			lastErr = err
			continue
		}
		outStr := strings.TrimSpace(string(out))
		fmt.Println(outStr)
		// adb connect returns "connected to <addr>" or "already connected to <addr>" on success.
		if strings.Contains(outStr, "connected") {
			// Wait for ADB protocol handshake to complete.
			// adb connect establishes TCP and starts CNXN exchange asynchronously.
			// Without this wait, the first user command may arrive before the
			// handshake finishes, causing "error: closed".
			// We cannot use get-state or shell commands to verify because each
			// adb command opens a new TCP connection through the tunnel, triggering
			// server-side preemption (close 4001) of the previous connection.
			time.Sleep(2 * time.Second)
			return nil
		}
		lastErr = fmt.Errorf("adb connect: %s", outStr)
	}
	return fmt.Errorf("adb connect failed after %d attempts: %w", maxRetries, lastErr)
}

// runAdbCommand executes an adb command and returns any error.
func runAdbCommand(adbPath string, args ...string) error {
	cmd := exec.Command(adbPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// mustAtoi converts a string to int, returning 0 on error.
func mustAtoi(s string) int {
	var n int
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

// exitError is a sentinel error type that carries an exit code.
type exitCodeError struct {
	code int
	err  error
}

func (e *exitCodeError) Error() string { return e.err.Error() }
func (e *exitCodeError) Unwrap() error { return e.err }

func exitError(code int, err error) error {
	return &exitCodeError{code: code, err: err}
}
