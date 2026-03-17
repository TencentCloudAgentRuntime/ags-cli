package tunnelstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestStore creates a Store backed by a temp directory for testing.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "tunnels.json")
	lockPath := storePath + ".lock"
	return &Store{path: storePath, lockPath: lockPath}
}

func TestNewStore(t *testing.T) {
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() failed: %v", err)
	}
	if store == nil {
		t.Fatal("store should not be nil")
	}
	if store.path == "" {
		t.Error("store path should not be empty")
	}
}

func TestSaveAndGet(t *testing.T) {
	store := newTestStore(t)

	// Use current PID so the entry is not cleaned as zombie by List()
	entry := TunnelEntry{
		PID:       os.Getpid(),
		Port:      15555,
		CreatedAt: time.Now().Truncate(time.Second),
	}

	if err := store.Save("sandbox-aaa", entry); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// Verify we can get it back
	got, ok, err := store.Get("sandbox-aaa")
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if !ok {
		t.Fatal("Get() should find the entry")
	}
	if got.PID != entry.PID {
		t.Errorf("PID = %d, want %d", got.PID, entry.PID)
	}
	if got.Port != entry.Port {
		t.Errorf("Port = %d, want %d", got.Port, entry.Port)
	}
}

func TestSaveOverwrite(t *testing.T) {
	store := newTestStore(t)
	pid := os.Getpid()

	entry1 := TunnelEntry{PID: pid, Port: 15555, CreatedAt: time.Now()}
	entry2 := TunnelEntry{PID: pid, Port: 15556, CreatedAt: time.Now()}

	if err := store.Save("sandbox-aaa", entry1); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}
	if err := store.Save("sandbox-aaa", entry2); err != nil {
		t.Fatalf("Save() overwrite failed: %v", err)
	}

	got, ok, err := store.Get("sandbox-aaa")
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if !ok {
		t.Fatal("Get() should find the entry")
	}
	if got.Port != 15556 {
		t.Errorf("Port = %d, want 15556 (overwritten value)", got.Port)
	}
}

func TestRemove(t *testing.T) {
	store := newTestStore(t)

	entry := TunnelEntry{PID: os.Getpid(), Port: 15555, CreatedAt: time.Now()}
	if err := store.Save("sandbox-aaa", entry); err != nil {
		t.Fatal(err)
	}

	if err := store.Remove("sandbox-aaa"); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	_, ok, _ := store.Get("sandbox-aaa")
	if ok {
		t.Error("Get() should not find removed entry")
	}
}

func TestRemoveNonExistent(t *testing.T) {
	store := newTestStore(t)

	// Should not error when removing non-existent entry
	if err := store.Remove("nonexistent"); err != nil {
		t.Fatalf("Remove() of non-existent entry should not error: %v", err)
	}
}

func TestListEmpty(t *testing.T) {
	store := newTestStore(t)

	entries, err := store.List()
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestListMultiple(t *testing.T) {
	store := newTestStore(t)

	// Use current PID so they're detected as alive
	pid := os.Getpid()

	_ = store.Save("sandbox-aaa", TunnelEntry{PID: pid, Port: 15555, CreatedAt: time.Now()})
	_ = store.Save("sandbox-bbb", TunnelEntry{PID: pid, Port: 15556, CreatedAt: time.Now()})
	_ = store.Save("sandbox-ccc", TunnelEntry{PID: pid, Port: 15557, CreatedAt: time.Now()})

	entries, err := store.List()
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

func TestListCleansZombies(t *testing.T) {
	store := newTestStore(t)

	// Save an entry with a dead PID (PID 99999999 is extremely unlikely to exist)
	_ = store.Save("zombie-sandbox", TunnelEntry{PID: 99999999, Port: 15555, CreatedAt: time.Now()})

	// Also save a live entry (current process)
	_ = store.Save("live-sandbox", TunnelEntry{PID: os.Getpid(), Port: 15556, CreatedAt: time.Now()})

	entries, err := store.List()
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}

	if _, ok := entries["zombie-sandbox"]; ok {
		t.Error("zombie entry should have been cleaned")
	}
	if _, ok := entries["live-sandbox"]; !ok {
		t.Error("live entry should still exist")
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry after cleanup, got %d", len(entries))
	}

	// Verify the file was actually updated (zombie removed)
	rawData, _ := os.ReadFile(store.path)
	var raw map[string]TunnelEntry
	_ = json.Unmarshal(rawData, &raw)
	if _, ok := raw["zombie-sandbox"]; ok {
		t.Error("zombie should be removed from file after List()")
	}
}

func TestGetNonExistent(t *testing.T) {
	store := newTestStore(t)

	_, ok, err := store.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if ok {
		t.Error("Get() should return false for non-existent entry")
	}
}

func TestCleanup(t *testing.T) {
	store := newTestStore(t)

	// Save with a dead PID (99999999 is extremely unlikely to exist)
	// This avoids killing the test process when Cleanup calls killProcess
	_ = store.Save("sandbox-aaa", TunnelEntry{PID: 99999999, Port: 15555, CreatedAt: time.Now()})

	// Cleanup should remove the entry
	if err := store.Cleanup("sandbox-aaa"); err != nil {
		t.Fatalf("Cleanup() failed: %v", err)
	}

	// Verify removed - read file directly since Get() has zombie cleanup
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]TunnelEntry
	_ = json.Unmarshal(data, &entries)
	if _, ok := entries["sandbox-aaa"]; ok {
		t.Error("entry should be removed after Cleanup")
	}
}

func TestCleanupNonExistent(t *testing.T) {
	store := newTestStore(t)

	if err := store.Cleanup("nonexistent"); err != nil {
		t.Fatalf("Cleanup() of non-existent entry should not error: %v", err)
	}
}

func TestCleanupAll(t *testing.T) {
	store := newTestStore(t)

	// Use dead PIDs to avoid killing the test process
	_ = store.Save("sandbox-aaa", TunnelEntry{PID: 99999998, Port: 15555, CreatedAt: time.Now()})
	_ = store.Save("sandbox-bbb", TunnelEntry{PID: 99999997, Port: 15556, CreatedAt: time.Now()})

	if err := store.CleanupAll(); err != nil {
		t.Fatalf("CleanupAll() failed: %v", err)
	}

	// Read file directly to verify all entries removed
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]TunnelEntry
	_ = json.Unmarshal(data, &entries)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after CleanupAll, got %d", len(entries))
	}
}

func TestAtomicWrite(t *testing.T) {
	store := newTestStore(t)

	// Write an entry
	_ = store.Save("sandbox-aaa", TunnelEntry{PID: os.Getpid(), Port: 15555, CreatedAt: time.Now()})

	// Read the file directly and verify it's valid JSON
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatalf("failed to read store file: %v", err)
	}

	var entries map[string]TunnelEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("store file is not valid JSON: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("expected 1 entry in file, got %d", len(entries))
	}
}

func TestCorruptedFile(t *testing.T) {
	store := newTestStore(t)

	// Write garbage to the store file
	if err := os.WriteFile(store.path, []byte("not json{{{"), 0600); err != nil {
		t.Fatal(err)
	}

	// List should recover gracefully (start fresh)
	entries, err := store.List()
	if err != nil {
		t.Fatalf("List() should recover from corrupted file: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries from corrupted file, got %d", len(entries))
	}
}

func TestFilePermissions(t *testing.T) {
	store := newTestStore(t)

	_ = store.Save("sandbox-aaa", TunnelEntry{PID: os.Getpid(), Port: 15555, CreatedAt: time.Now()})

	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permission = %o, want 0600", perm)
	}
}

func TestSymlinkRejection(t *testing.T) {
	dir := t.TempDir()
	targetFile := filepath.Join(dir, "target.json")
	symlinkPath := filepath.Join(dir, "tunnels.json")
	lockPath := symlinkPath + ".lock"

	// Create a target file and a symlink pointing to it
	if err := os.WriteFile(targetFile, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetFile, symlinkPath); err != nil {
		t.Fatal(err)
	}

	store := &Store{path: symlinkPath, lockPath: lockPath}

	// Save should fail because store file is a symlink
	err := store.Save("sandbox-aaa", TunnelEntry{PID: os.Getpid(), Port: 15555, CreatedAt: time.Now()})
	if err == nil {
		t.Error("Save() should reject symlink store file")
	}

	// List should also fail
	_, err = store.List()
	if err == nil {
		t.Error("List() should reject symlink store file")
	}
}
