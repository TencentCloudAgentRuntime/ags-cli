// Package tunnelstore manages persistent tunnel state in ~/.ags/tunnels.json.
//
// It provides cross-process safe read/write access to the tunnel registry using
// file-level locking (flock on Unix, LockFileEx on Windows) and atomic writes.
// Zombie tunnel entries (where the backing process has died) are automatically
// cleaned on List and Get operations.
package tunnelstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

const (
	// storeDir is the directory name under user home for storing tunnel state.
	storeDir = ".ags"
	// storeFile is the filename for tunnel registry.
	storeFile = "tunnels.json"
	// lockTimeout is the maximum wait time to acquire the file lock.
	lockTimeout = 3 * time.Second
	// lockRetryDelay is the interval between lock acquisition attempts.
	lockRetryDelay = 100 * time.Millisecond
)

// TunnelEntry represents a single active tunnel mapping.
type TunnelEntry struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	CreatedAt time.Time `json:"created_at"`
	ExePath   string    `json:"exe_path,omitempty"` // Executable path for PID reuse protection
}

// Store manages the tunnel registry file with cross-process locking.
type Store struct {
	path     string // path to tunnels.json
	lockPath string // path to tunnels.json.lock
}

// NewStore creates a Store instance. The registry is stored at ~/.ags/tunnels.json.
func NewStore() (*Store, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	dir := filepath.Join(homeDir, storeDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %w", err)
	}

	storePath := filepath.Join(dir, storeFile)
	lockPath := storePath + ".lock"

	return &Store{
		path:     storePath,
		lockPath: lockPath,
	}, nil
}

// Save registers or updates a tunnel entry for the given sandbox ID.
// Uses exclusive file lock + atomic write for cross-process safety.
func (s *Store) Save(sandboxID string, entry TunnelEntry) error {
	fl := flock.New(s.lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	locked, err := fl.TryLockContext(ctx, lockRetryDelay)
	if err != nil || !locked {
		return fmt.Errorf("failed to acquire store lock: %w", err)
	}
	defer func() { _ = fl.Unlock() }()

	entries, err := s.loadLocked()
	if err != nil {
		return err
	}

	entries[sandboxID] = entry
	return s.saveLocked(entries)
}

// Remove deletes the tunnel entry for the given sandbox ID.
// Uses exclusive file lock + atomic write.
func (s *Store) Remove(sandboxID string) error {
	fl := flock.New(s.lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	locked, err := fl.TryLockContext(ctx, lockRetryDelay)
	if err != nil || !locked {
		return fmt.Errorf("failed to acquire store lock: %w", err)
	}
	defer func() { _ = fl.Unlock() }()

	entries, err := s.loadLocked()
	if err != nil {
		return err
	}

	delete(entries, sandboxID)
	return s.saveLocked(entries)
}

// Get retrieves a single tunnel entry. Returns the entry and true if found,
// zero value and false if not found, or an error if the store cannot be read.
// Automatically removes zombie entries via List().
func (s *Store) Get(sandboxID string) (TunnelEntry, bool, error) {
	entries, err := s.List()
	if err != nil {
		return TunnelEntry{}, false, err
	}
	entry, ok := entries[sandboxID]
	return entry, ok, nil
}

// List returns all live tunnel entries. Dead entries (where PID is no longer
// alive) are automatically cleaned up.
func (s *Store) List() (map[string]TunnelEntry, error) {
	fl := flock.New(s.lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	locked, err := fl.TryLockContext(ctx, lockRetryDelay)
	if err != nil || !locked {
		return nil, fmt.Errorf("failed to acquire store lock: %w", err)
	}
	defer func() { _ = fl.Unlock() }()

	entries, err := s.loadLocked()
	if err != nil {
		return nil, err
	}

	// Clean zombies
	cleaned := false
	for id, entry := range entries {
		if !isProcessAlive(entry.PID) {
			delete(entries, id)
			cleaned = true
		}
	}

	if cleaned {
		_ = s.saveLocked(entries) // best-effort cleanup
	}

	return entries, nil
}

// Cleanup kills the tunnel process for the given sandbox ID (if alive)
// and removes its entry from the store. If the process cannot be confirmed
// dead (e.g. PID reused by another process), the entry is preserved and
// an error is returned.
func (s *Store) Cleanup(sandboxID string) error {
	fl := flock.New(s.lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	locked, err := fl.TryLockContext(ctx, lockRetryDelay)
	if err != nil || !locked {
		return fmt.Errorf("failed to acquire store lock: %w", err)
	}
	defer func() { _ = fl.Unlock() }()

	entries, err := s.loadLocked()
	if err != nil {
		return err
	}

	if entry, ok := entries[sandboxID]; ok {
		if !killProcess(entry.PID, entry.ExePath) {
			// Process could not be killed (PID reused or still alive).
			// Keep the entry so the user knows the tunnel may still be running.
			return fmt.Errorf("tunnel process (PID %d) could not be terminated — it may have been replaced by another process; entry preserved for manual cleanup", entry.PID)
		}
		delete(entries, sandboxID)
		return s.saveLocked(entries)
	}

	return nil
}

// CleanupAll kills all tunnel processes and clears the store.
// Entries whose processes cannot be confirmed dead are preserved.
func (s *Store) CleanupAll() error {
	fl := flock.New(s.lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	locked, err := fl.TryLockContext(ctx, lockRetryDelay)
	if err != nil || !locked {
		return fmt.Errorf("failed to acquire store lock: %w", err)
	}
	defer func() { _ = fl.Unlock() }()

	entries, err := s.loadLocked()
	if err != nil {
		return err
	}

	var warnings []string
	for id, entry := range entries {
		if killProcess(entry.PID, entry.ExePath) {
			delete(entries, id)
		} else {
			warnings = append(warnings, fmt.Sprintf("PID %d (%s)", entry.PID, id))
		}
	}

	if err := s.saveLocked(entries); err != nil {
		return err
	}

	if len(warnings) > 0 {
		return fmt.Errorf("could not terminate some tunnel processes (entries preserved): %s", strings.Join(warnings, ", "))
	}
	return nil
}

// loadLocked reads the store file. Must be called while holding the lock.
func (s *Store) loadLocked() (map[string]TunnelEntry, error) {
	// Defense-in-depth: reject symlinks to prevent redirection attacks
	if info, err := os.Lstat(s.path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("store file is a symlink (rejected for security): %s", s.path)
		}
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]TunnelEntry), nil
		}
		return nil, fmt.Errorf("failed to read store file: %w", err)
	}

	entries := make(map[string]TunnelEntry)
	if err := json.Unmarshal(data, &entries); err != nil {
		// Corrupted file: start fresh
		return make(map[string]TunnelEntry), nil
	}

	return entries, nil
}

// saveLocked writes the entries to a temp file then atomically renames it.
// Must be called while holding the lock.
func (s *Store) saveLocked(entries map[string]TunnelEntry) error {
	// Defense-in-depth: reject symlinks to prevent redirection attacks
	if info, err := os.Lstat(s.path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("store file is a symlink (rejected for security): %s", s.path)
		}
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal store data: %w", err)
	}

	// Atomic write: write to temp file in same directory, then rename.
	dir := filepath.Dir(s.path)
	tmpFile, err := os.CreateTemp(dir, "tunnels-*.json.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := tmpFile.Chmod(0600); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to set temp file permissions: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}
