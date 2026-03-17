//go:build !windows

package tunnelstore

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// isProcessAlive checks if a process is still running by sending signal 0.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// isOurTunnelProcess checks if the process at pid is actually our tunnel process
// by comparing its executable path against the expected path. This prevents
// killing unrelated processes when PIDs are reused by the OS.
func isOurTunnelProcess(pid int, expectedExePath string) bool {
	if pid <= 0 {
		return false
	}
	if !isProcessAlive(pid) {
		return false
	}
	if expectedExePath == "" {
		// Legacy entry without exe path: cannot verify identity.
		// Conservative approach: refuse to kill to prevent PID reuse misidentification.
		// The caller should keep the entry and warn the user.
		return false
	}

	// Try /proc/<pid>/exe first (Linux)
	exeLink, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err == nil {
		return exeLink == expectedExePath
	}

	// Fallback for macOS: use ps to get the full command line (argv[0] is typically
	// the absolute executable path). "args" gives the complete command with arguments,
	// so we extract the first field as the executable path for exact comparison.
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "args=").Output()
	if err != nil {
		// Can't verify; assume it's ours to avoid leaving orphan tunnels
		return true
	}
	cmdLine := strings.TrimSpace(string(out))
	if cmdLine == "" {
		return true
	}
	// argv[0] is the first space-delimited field
	argv0 := cmdLine
	if idx := strings.IndexByte(cmdLine, ' '); idx > 0 {
		argv0 = cmdLine[:idx]
	}
	return argv0 == expectedExePath
}

// killProcess sends SIGTERM to the process, waits 5s, then SIGKILL if still alive.
// exePath is used to verify the process identity before killing (PID reuse protection).
// Returns true if the process was confirmed dead (killed or already exited),
// false if the process could not be killed (identity mismatch, still alive after SIGKILL).
func killProcess(pid int, exePath string) bool {
	if pid <= 0 {
		return true // Invalid PID, treat as "nothing to kill"
	}
	if !isOurTunnelProcess(pid, exePath) {
		// Process either doesn't exist (dead) or identity doesn't match (PID reused).
		// Check if the PID is alive at all to distinguish the two cases.
		if !isProcessAlive(pid) {
			return true // Process is dead, safe to clean up entry
		}
		return false // PID reused by different process, cannot kill
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return true // Can't find process, treat as dead
	}

	// Try graceful shutdown first
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return true // Process likely already dead
	}

	// Poll for exit using identity-aware check. We use isOurTunnelProcess
	// (not just isProcessAlive) so that if the PID is reused by another process
	// during this window, we detect the identity change and stop instead of
	// sending SIGKILL to an unrelated process.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !isOurTunnelProcess(pid, exePath) {
			return true // Process exited (or PID reused, but original is gone)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Still alive after timeout — verify identity one final time before SIGKILL
	if isOurTunnelProcess(pid, exePath) {
		_ = process.Signal(syscall.SIGKILL)
		// Give SIGKILL a moment to take effect
		time.Sleep(100 * time.Millisecond)
		return !isProcessAlive(pid)
	}
	return true // Identity changed during final check, original process is gone
}
