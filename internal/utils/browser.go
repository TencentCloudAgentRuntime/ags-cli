package utils

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser opens the specified URL in the system default browser
func OpenBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		// Try multiple common browser launch commands
		browsers := []string{"xdg-open", "sensible-browser", "x-www-browser", "firefox", "chromium", "google-chrome"}
		for _, browser := range browsers {
			if _, err := exec.LookPath(browser); err == nil {
				cmd = browser
				args = []string{url}
				break
			}
		}
		if cmd == "" {
			return fmt.Errorf("no suitable browser found on Linux system")
		}
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	return exec.Command(cmd, args...).Start()
}

// IsBrowserAvailable checks if the system has an available browser
func IsBrowserAvailable() bool {
	switch runtime.GOOS {
	case "windows":
		// Windows systems usually have a default browser
		return true
	case "darwin":
		// macOS systems usually have a default browser
		return true
	case "linux":
		// Check if there are available browsers on Linux system
		browsers := []string{"xdg-open", "sensible-browser", "x-www-browser", "firefox", "chromium", "google-chrome"}
		for _, browser := range browsers {
			if _, err := exec.LookPath(browser); err == nil {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// GetBrowserCommand gets the system default browser command
func GetBrowserCommand() (string, []string, error) {
	switch runtime.GOOS {
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler"}, nil
	case "darwin":
		return "open", []string{}, nil
	case "linux":
		browsers := []string{"xdg-open", "sensible-browser", "x-www-browser", "firefox", "chromium", "google-chrome"}
		for _, browser := range browsers {
			if _, err := exec.LookPath(browser); err == nil {
				return browser, []string{}, nil
			}
		}
		return "", nil, fmt.Errorf("no suitable browser found on Linux system")
	default:
		return "", nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}
