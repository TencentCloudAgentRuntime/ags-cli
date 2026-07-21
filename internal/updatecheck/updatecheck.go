// Package updatecheck implements a background version check with 24-hour cache.
// Inspired by AgentCore CLI's update-notifier.ts.
//
// Usage (from the root command flow):
//
//	ch := updatecheck.Start(currentVersion)  // non-blocking goroutine
//	... execute command ...
//	updatecheck.PrintNotice(ch)              // after command exits
package updatecheck

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// VersionURL is the official endpoint that returns the latest version string.
	VersionURL = "https://dl.tencentags.com/agr-cli/latest/VERSION"
	// InstallURL is the one-liner shown to users for upgrading.
	InstallURL = "https://dl.tencentags.com/agr-cli/latest/install.sh"
	// checkInterval is how long between remote checks (24 hours).
	checkInterval = 24 * time.Hour
	// httpTimeout prevents the background goroutine from blocking indefinitely.
	httpTimeout = 5 * time.Second
	// cacheFile is the name stored under ~/.agr/.
	cacheFile = "update-check.json"
	// maxVersionBytes limits the amount of data read from the VERSION endpoint.
	maxVersionBytes = 128
)

// Result is the outcome of a background version check.
type Result struct {
	UpdateAvailable bool
	LatestVersion   string
	CurrentVersion  string
}

// cacheData is the JSON structure persisted to disk.
type cacheData struct {
	LastCheck     int64  `json:"last_check"`
	LatestVersion string `json:"latest"`
}

// Start kicks off a non-blocking background check and returns a channel that
// will receive at most one *Result (or nil on error/skip). The channel is
// closed when done.
//
// To make warm-cache results survive fast commands (e.g. `agr status`, which
// finish in microseconds, before a goroutine can be scheduled), the cache is
// read synchronously here. On a hit, the result is placed on the channel
// before Start returns, so PrintNotice always observes it. Only a cache miss
// or a stale cache spawns a goroutine for the remote fetch — which remains
// non-blocking (PrintNotice's default branch handles the not-yet-ready case).
func Start(currentVersion string) <-chan *Result {
	ch := make(chan *Result, 1)

	// Synchronous warm-cache path: result is ready before we return.
	if result := checkCache(currentVersion); result != nil {
		ch <- result
		close(ch)
		return ch
	}

	// Cold/stale cache: fetch remotely in the background without blocking.
	go func() {
		defer close(ch)
		result := checkRemote(currentVersion)
		if result != nil {
			ch <- result
		}
	}()
	return ch
}

// PrintNotice attempts a non-blocking receive from the background check
// channel. If the result is ready and a newer version is available, it prints
// a notice to w (typically stderr). If the check has not completed yet (e.g.
// slow network on cold cache), it returns immediately without blocking — the
// fresh result will be cached for the next invocation.
func PrintNotice(w io.Writer, ch <-chan *Result) {
	if ch == nil {
		return
	}
	// Non-blocking: only print if the result is already available.
	select {
	case result, ok := <-ch:
		if !ok || result == nil || !result.UpdateAvailable {
			return
		}
		fmt.Fprintf(w, "\nUpdate available: %s → %s\n", result.CurrentVersion, result.LatestVersion)
		fmt.Fprintf(w, "Run: curl -fsSL %s | sh\n", InstallURL)
	default:
		// Check still in progress — do not block command exit.
	}
}

// checkCache returns a Result from a fresh warm cache, or nil if the cache is
// missing/stale (in which case checkRemote should fetch remotely). It never
// touches the network.
func checkCache(currentVersion string) *Result {
	cache := readCache()
	if cache == nil {
		return nil
	}
	now := time.Now()
	if now.Unix()-cache.LastCheck >= int64(checkInterval.Seconds()) {
		return nil
	}
	cmp := CompareVersions(currentVersion, cache.LatestVersion)
	return &Result{
		UpdateAvailable: cmp > 0,
		LatestVersion:   cache.LatestVersion,
		CurrentVersion:  currentVersion,
	}
}

// checkRemote fetches the latest version from the remote endpoint, writes the
// cache, and returns a Result (or nil on fetch failure).
func checkRemote(currentVersion string) *Result {
	latest, err := FetchLatestVersion()
	if err != nil {
		return nil
	}
	now := time.Now()
	writeCache(&cacheData{LastCheck: now.Unix(), LatestVersion: latest})

	cmp := CompareVersions(currentVersion, latest)
	return &Result{
		UpdateAvailable: cmp > 0,
		LatestVersion:   latest,
		CurrentVersion:  currentVersion,
	}
}

// versionURLOverride allows tests to point at a local httptest server.
// When empty, the production VersionURL is used.
var versionURLOverride string

// SetVersionURLForTest overrides the version check URL and returns a restore function.
func SetVersionURLForTest(url string) func() {
	prev := versionURLOverride
	versionURLOverride = url
	return func() { versionURLOverride = prev }
}

func resolveVersionURL() string {
	if versionURLOverride != "" {
		return versionURLOverride
	}
	if env := os.Getenv("_TEST_VERSION_URL"); env != "" {
		return env
	}
	return VersionURL
}

// FetchLatestVersion queries the remote VERSION endpoint.
func FetchLatestVersion() (string, error) {
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(resolveVersionURL())
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxVersionBytes))
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(body))
	if version == "" {
		return "", fmt.Errorf("empty version response")
	}
	return version, nil
}

// CompareVersions returns:
//
//	> 0 if latest is newer than current
//	  0 if equal
//	< 0 if current is newer (or latest is a pre-release vs stable current)
//
// Handles semver with optional "v" prefix and pre-release suffixes.
func CompareVersions(current, latest string) int {
	current = strings.TrimPrefix(current, "v")
	latest = strings.TrimPrefix(latest, "v")

	currCore, currPre := splitPrerelease(current)
	latCore, latPre := splitPrerelease(latest)

	currNums := parseNums(currCore)
	latNums := parseNums(latCore)

	// Compare major.minor.patch
	for i := 0; i < 3; i++ {
		c := safeIndex(currNums, i)
		l := safeIndex(latNums, i)
		if l > c {
			return 1
		}
		if l < c {
			return -1
		}
	}

	// Equal core versions — compare pre-release.
	if currPre == "" && latPre == "" {
		return 0
	}
	// No pre-release is "greater" than having one (1.0.0 > 1.0.0-beta).
	if currPre == "" {
		return -1
	}
	if latPre == "" {
		return 1
	}

	// Both have pre-release — compare segments.
	return comparePrerelease(currPre, latPre)
}

func splitPrerelease(v string) (core, pre string) {
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		return v[:idx], v[idx+1:]
	}
	return v, ""
}

func parseNums(core string) []int {
	parts := strings.Split(core, ".")
	nums := make([]int, len(parts))
	for i, p := range parts {
		nums[i], _ = strconv.Atoi(p)
	}
	return nums
}

func safeIndex(nums []int, i int) int {
	if i < len(nums) {
		return nums[i]
	}
	return 0
}

func comparePrerelease(a, b string) int {
	aSeg := strings.Split(a, ".")
	bSeg := strings.Split(b, ".")
	n := len(aSeg)
	if len(bSeg) > n {
		n = len(bSeg)
	}
	for i := 0; i < n; i++ {
		var as, bs string
		if i < len(aSeg) {
			as = aSeg[i]
		}
		if i < len(bSeg) {
			bs = bSeg[i]
		}
		if as == "" {
			return 1 // fewer segments = earlier version
		}
		if bs == "" {
			return -1
		}
		an, aerr := strconv.Atoi(as)
		bn, berr := strconv.Atoi(bs)
		if aerr == nil && berr == nil {
			if bn > an {
				return 1
			}
			if bn < an {
				return -1
			}
		} else {
			if bs > as {
				return 1
			}
			if bs < as {
				return -1
			}
		}
	}
	return 0
}

// --- Cache I/O ---

func cacheFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agr", cacheFile)
}

func readCache() *cacheData {
	path := cacheFilePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cache cacheData
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}
	return &cache
}

func writeCache(data *cacheData) {
	path := cacheFilePath()
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	_ = os.MkdirAll(dir, 0o755)
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o644)
}
