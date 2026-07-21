package updatecheck

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// helper to set up cache dir and write cache file in tests.
func setupCache(t *testing.T, data *cacheData) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := filepath.Join(tmp, ".agr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, cacheFile), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return tmp
}

// --- CompareVersions ---

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    int
	}{
		// latest is newer
		{"v0.6.2", "v0.7.0", 1},
		{"0.6.2", "0.7.0", 1},
		{"v1.0.0", "v2.0.0", 1},
		{"v1.0.0", "v1.1.0", 1},
		{"v1.0.0", "v1.0.1", 1},
		{"v0.0.1", "v0.0.2", 1},
		// equal
		{"v0.7.0", "v0.7.0", 0},
		{"1.0.0", "1.0.0", 0},
		{"v0.0.0", "v0.0.0", 0},
		// current is newer
		{"v0.8.0", "v0.7.0", -1},
		{"v2.0.0", "v1.9.9", -1},
		{"v1.0.1", "v1.0.0", -1},
		// pre-release handling
		{"v1.0.0-beta.1", "v1.0.0", 1},        // stable is newer than pre-release
		{"v1.0.0", "v1.0.0-beta.1", -1},       // current stable > latest pre-release
		{"v1.0.0-beta.1", "v1.0.0-beta.2", 1}, // beta.2 > beta.1
		{"v1.0.0-alpha", "v1.0.0-beta", 1},    // lexicographic: beta > alpha
		{"v1.0.0-rc.1", "v1.0.0-rc.1", 0},     // equal pre-release
		{"v1.0.0-beta.2", "v1.0.0-beta.1", -1},
		// dev version (parses as 0.0.0)
		{"dev", "v0.7.0", 1},
		{"dev", "v0.0.1", 1},
		// mixed v prefix
		{"v1.2.3", "1.2.3", 0},
	}
	for _, tt := range tests {
		t.Run(tt.current+"_vs_"+tt.latest, func(t *testing.T) {
			got := CompareVersions(tt.current, tt.latest)
			if got != tt.want {
				t.Fatalf("CompareVersions(%q, %q) = %d, want %d", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

// --- FetchLatestVersion ---

func TestFetchLatestVersionSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("v0.7.0\n"))
	}))
	defer ts.Close()

	restore := SetVersionURLForTest(ts.URL)
	defer restore()

	version, err := FetchLatestVersion()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "v0.7.0" {
		t.Fatalf("version = %q, want v0.7.0", version)
	}
}

func TestFetchLatestVersionTrimsWhitespace(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("  v0.8.1  \n"))
	}))
	defer ts.Close()

	restore := SetVersionURLForTest(ts.URL)
	defer restore()

	version, err := FetchLatestVersion()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "v0.8.1" {
		t.Fatalf("version = %q, want v0.8.1", version)
	}
}

func TestFetchLatestVersionHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	restore := SetVersionURLForTest(ts.URL)
	defer restore()

	_, err := FetchLatestVersion()
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestFetchLatestVersionEmptyResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(""))
	}))
	defer ts.Close()

	restore := SetVersionURLForTest(ts.URL)
	defer restore()

	_, err := FetchLatestVersion()
	if err == nil {
		t.Fatal("expected error for empty response")
	}
}

func TestFetchLatestVersionNetworkError(t *testing.T) {
	restore := SetVersionURLForTest("http://localhost:1") // port 1 — unreachable
	defer restore()

	_, err := FetchLatestVersion()
	if err == nil {
		t.Fatal("expected network error")
	}
}

// --- Cache I/O ---

func TestCacheReadWrite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir := filepath.Join(tmp, ".agr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	data := &cacheData{LastCheck: time.Now().Unix(), LatestVersion: "v0.7.0"}
	writeCache(data)

	got := readCache()
	if got == nil {
		t.Fatal("readCache returned nil")
	}
	if got.LatestVersion != "v0.7.0" {
		t.Fatalf("LatestVersion = %q, want v0.7.0", got.LatestVersion)
	}
	if got.LastCheck != data.LastCheck {
		t.Fatalf("LastCheck = %d, want %d", got.LastCheck, data.LastCheck)
	}
}

func TestCacheWriteCreatesDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// ~/.agr does not exist yet.
	data := &cacheData{LastCheck: 12345, LatestVersion: "v1.0.0"}
	writeCache(data)

	got := readCache()
	if got == nil {
		t.Fatal("expected cache to be readable after writeCache created dir")
	}
	if got.LatestVersion != "v1.0.0" {
		t.Fatalf("LatestVersion = %q, want v1.0.0", got.LatestVersion)
	}
}

func TestCacheReadReturnsNilOnMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	got := readCache()
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestCacheReadReturnsNilOnInvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir := filepath.Join(tmp, ".agr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cacheFile), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readCache()
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

// --- checkCache() / checkRemote() ---

func TestCheckCacheUsesCacheWhenFresh(t *testing.T) {
	setupCache(t, &cacheData{LastCheck: time.Now().Unix(), LatestVersion: "v0.8.0"})

	result := checkCache("v0.7.0")
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if !result.UpdateAvailable {
		t.Fatal("expected UpdateAvailable=true")
	}
	if result.LatestVersion != "v0.8.0" {
		t.Fatalf("LatestVersion = %q, want v0.8.0", result.LatestVersion)
	}
	if result.CurrentVersion != "v0.7.0" {
		t.Fatalf("CurrentVersion = %q, want v0.7.0", result.CurrentVersion)
	}
}

func TestCheckCacheReturnsNilWhenExpired(t *testing.T) {
	// Stale cache → checkCache yields nil so Start falls through to remote fetch.
	setupCache(t, &cacheData{LastCheck: time.Now().Add(-25 * time.Hour).Unix(), LatestVersion: "v0.5.0"})

	if result := checkCache("v0.7.0"); result != nil {
		t.Fatalf("expected nil for stale cache, got %+v", result)
	}
}

func TestCheckCacheReturnsNilWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if result := checkCache("v0.7.0"); result != nil {
		t.Fatalf("expected nil for missing cache, got %+v", result)
	}
}

func TestCheckCacheSkipsWhenVersionsEqual(t *testing.T) {
	setupCache(t, &cacheData{LastCheck: time.Now().Unix(), LatestVersion: "v0.7.0"})

	result := checkCache("v0.7.0")
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.UpdateAvailable {
		t.Fatal("expected UpdateAvailable=false")
	}
}

func TestCheckCacheCurrentIsNewer(t *testing.T) {
	setupCache(t, &cacheData{LastCheck: time.Now().Unix(), LatestVersion: "v0.6.0"})

	result := checkCache("v0.7.0")
	if result == nil {
		t.Fatal("expected result")
	}
	if result.UpdateAvailable {
		t.Fatal("expected UpdateAvailable=false (current is newer)")
	}
}

func TestCheckRemoteFetchesAndUpdatesCache(t *testing.T) {
	// Stale cache present; checkRemote ignores it and fetches fresh.
	setupCache(t, &cacheData{LastCheck: time.Now().Add(-25 * time.Hour).Unix(), LatestVersion: "v0.5.0"})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("v0.9.0\n"))
	}))
	defer ts.Close()
	restore := SetVersionURLForTest(ts.URL)
	defer restore()

	result := checkRemote("v0.7.0")
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if !result.UpdateAvailable {
		t.Fatal("expected UpdateAvailable=true (v0.9.0 > v0.7.0)")
	}
	if result.LatestVersion != "v0.9.0" {
		t.Fatalf("LatestVersion = %q, want v0.9.0 (fetched, not cached v0.5.0)", result.LatestVersion)
	}

	// Verify cache was updated.
	newCache := readCache()
	if newCache == nil || newCache.LatestVersion != "v0.9.0" {
		t.Fatalf("cache not updated: %+v", newCache)
	}
}

func TestCheckRemoteFetchesWhenNoCache(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("v1.0.0\n"))
	}))
	defer ts.Close()
	restore := SetVersionURLForTest(ts.URL)
	defer restore()

	result := checkRemote("v0.7.0")
	if result == nil {
		t.Fatal("expected result")
	}
	if !result.UpdateAvailable {
		t.Fatal("expected UpdateAvailable=true")
	}
	if result.LatestVersion != "v1.0.0" {
		t.Fatalf("LatestVersion = %q, want v1.0.0", result.LatestVersion)
	}
}

func TestCheckRemoteReturnsNilOnFetchError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	restore := SetVersionURLForTest("http://localhost:1")
	defer restore()

	result := checkRemote("v0.7.0")
	if result != nil {
		t.Fatalf("expected nil on fetch error, got %+v", result)
	}
}

// --- Start() ---

func TestStartReturnsChannelWithResult(t *testing.T) {
	setupCache(t, &cacheData{LastCheck: time.Now().Unix(), LatestVersion: "v0.9.0"})

	ch := Start("v0.7.0")
	result := <-ch
	if result == nil {
		t.Fatal("expected result")
	}
	if !result.UpdateAvailable {
		t.Fatal("expected UpdateAvailable=true")
	}
}

// Regression: on a warm cache the result must be ready before Start returns,
// so a fast command (which calls PrintNotice immediately) still observes it.
// Previously Start always spawned a goroutine, and for microsecond-fast
// commands PrintNotice's non-blocking select ran before the goroutine was
// scheduled → the notice was silently dropped.
func TestStartWarmCacheResultReadyImmediately(t *testing.T) {
	setupCache(t, &cacheData{LastCheck: time.Now().Unix(), LatestVersion: "v0.9.0"})

	ch := Start("v0.7.0")
	// Non-blocking receive — simulates PrintNotice's select without waiting.
	select {
	case result := <-ch:
		if result == nil {
			t.Fatal("expected non-nil result on warm cache")
		}
		if !result.UpdateAvailable {
			t.Fatal("expected UpdateAvailable=true")
		}
	default:
		t.Fatal("warm-cache result was not ready immediately; fast commands would drop the notice")
	}
}

// Cold cache must NOT block Start from returning. We verify this indirectly:
// Start returns a channel, and TestStartChannelClosesOnError below drains that
// channel to completion (on an unreachable URL), proving the cold-cache path
// runs in a goroutine. Asserting "not immediately ready" here would be flaky
// on fast machines and, more importantly, racing the goroutine against the
// test's deferred SetVersionURLForTest restore — so we don't.

func TestStartChannelClosesOnError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	restore := SetVersionURLForTest("http://localhost:1")
	defer restore()

	ch := Start("v0.7.0")
	result, ok := <-ch
	// Channel should close; result is nil.
	if ok && result != nil {
		t.Fatalf("expected nil/closed, got %+v", result)
	}
}

// --- PrintNotice ---

func TestPrintNoticeOutputsWhenUpdateAvailable(t *testing.T) {
	ch := make(chan *Result, 1)
	ch <- &Result{UpdateAvailable: true, LatestVersion: "v0.8.0", CurrentVersion: "v0.7.0"}
	close(ch)

	var buf bytes.Buffer
	PrintNotice(&buf, ch)

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("Update available")) {
		t.Fatalf("expected update notice, got: %q", output)
	}
	if !bytes.Contains([]byte(output), []byte("v0.7.0")) {
		t.Fatalf("expected current version, got: %q", output)
	}
	if !bytes.Contains([]byte(output), []byte("v0.8.0")) {
		t.Fatalf("expected latest version, got: %q", output)
	}
	if !bytes.Contains([]byte(output), []byte("curl")) {
		t.Fatalf("expected install command, got: %q", output)
	}
}

func TestPrintNoticeNoOutputWhenUpToDate(t *testing.T) {
	ch := make(chan *Result, 1)
	ch <- &Result{UpdateAvailable: false, LatestVersion: "v0.7.0", CurrentVersion: "v0.7.0"}
	close(ch)

	var buf bytes.Buffer
	PrintNotice(&buf, ch)

	if buf.Len() != 0 {
		t.Fatalf("expected no output, got: %q", buf.String())
	}
}

func TestPrintNoticeNoOutputOnNilChannel(t *testing.T) {
	var buf bytes.Buffer
	PrintNotice(&buf, nil)
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got: %q", buf.String())
	}
}

func TestPrintNoticeNoOutputOnClosedEmptyChannel(t *testing.T) {
	ch := make(chan *Result)
	close(ch)

	var buf bytes.Buffer
	PrintNotice(&buf, ch)
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got: %q", buf.String())
	}
}

func TestPrintNoticeNoOutputWhenResultIsNil(t *testing.T) {
	ch := make(chan *Result, 1)
	ch <- nil
	close(ch)

	var buf bytes.Buffer
	PrintNotice(&buf, ch)
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got: %q", buf.String())
	}
}

func TestPrintNoticeNonBlockingWhenChannelNotReady(t *testing.T) {
	// Unbuffered channel with no sender — PrintNotice must not block.
	ch := make(chan *Result)
	// Do NOT close or send — simulates slow network.

	var buf bytes.Buffer
	PrintNotice(&buf, ch)
	// Should return immediately without output.
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got: %q", buf.String())
	}
}

// --- Integration: full flow with httptest server ---

func TestFullFlowFetchAndCache(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		_, _ = w.Write([]byte("v1.2.0\n"))
	}))
	defer ts.Close()
	restore := SetVersionURLForTest(ts.URL)
	defer restore()

	// First call: no cache → fetches from server.
	ch := Start("v1.0.0")
	result := <-ch
	if result == nil || !result.UpdateAvailable {
		t.Fatalf("first call: expected update available, got %+v", result)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", callCount)
	}

	// Second call: cache is fresh → no HTTP call.
	ch = Start("v1.0.0")
	result = <-ch
	if result == nil || !result.UpdateAvailable {
		t.Fatalf("second call: expected update available, got %+v", result)
	}
	if callCount != 1 {
		t.Fatalf("expected still 1 HTTP call (cached), got %d", callCount)
	}

	// Expire cache and call again → should fetch.
	cache := readCache()
	cache.LastCheck = time.Now().Add(-25 * time.Hour).Unix()
	writeCache(cache)

	ch = Start("v1.0.0")
	result = <-ch
	if result == nil || !result.UpdateAvailable {
		t.Fatalf("third call: expected update available, got %+v", result)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 HTTP calls after cache expired, got %d", callCount)
	}
}
