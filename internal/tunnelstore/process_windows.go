//go:build windows

package tunnelstore

import (
	"os"
	"strings"
)

// isProcessAlive checks if a process is still running on Windows.
// On Windows, os.FindProcess always succeeds, so we try to open the process
// with limited access to verify its existence.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Windows, Signal(0) is not supported. Instead, try to wait with
	// a zero timeout — if the process exists, Wait would block (or fail
	// differently than for a non-existent process). Use a pragmatic approach:
	// attempt to send os.Interrupt which is a no-op on Windows for non-console
	// processes but returns an error if the process doesn't exist.
	err = process.Signal(os.Interrupt)
	// If error contains "not supported" or "Access is denied", process exists
	// (we just can't signal it). If "process has already finished", it's dead.
	if err == nil {
		return true
	}
	errStr := err.Error()
	// On Windows, "Access is denied" means the process exists but we can't signal it
	if strings.Contains(errStr, "Access is denied") || strings.Contains(errStr, "not supported") {
		return true
	}
	return false
}

// isOurTunnelProcess checks if the process at pid is actually our tunnel process.
// On Windows, we only check if the process is alive. ExePath is not yet used.
//
// KNOWN RISK: PID reuse on Windows can still cause misidentification. This is
// mitigated by the short-lived nature of disconnect operations, but not eliminated.
//
// TODO: Use golang.org/x/sys/windows.QueryFullProcessImageName or
// windows.OpenProcess + windows.GetProcessImageFileName to retrieve the process
// executable path and compare against expectedExePath for full PID reuse protection.
func isOurTunnelProcess(pid int, expectedExePath string) bool {
	return isProcessAlive(pid)
}

// killProcess terminates a process on Windows using TerminateProcess (via os.Process.Kill).
// exePath is used for identity verification on supported platforms.
// Returns true if the process was confirmed dead, false if it could not be killed.
func killProcess(pid int, exePath string) bool {
	if pid <= 0 {
		return true
	}
	if !isOurTunnelProcess(pid, exePath) {
		if !isProcessAlive(pid) {
			return true // Process is dead
		}
		return false // PID reused by different process
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	_ = process.Kill()
	return !isProcessAlive(pid)
}
