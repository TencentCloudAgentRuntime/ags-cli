// Package token provides access token caching for data plane operations.
//
// Access tokens are required for data plane operations (code execution, file operations, etc.)
// but acquiring them has different behaviors depending on the control plane backend:
//   - Cloud backend: Can call AcquireSandboxInstanceToken API to get a token at any time
//   - E2B backend: Token is only returned during instance creation
//
// This cache provides a persistent file-based storage to save instance ID to access token
// mappings, allowing CLI commands to retrieve tokens across invocations.
// Cross-process safety is ensured via flock file locking and atomic writes.
package token

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

const (
	// CacheDir is the directory name under user home for storing cache files
	CacheDir = ".ags"
	// CacheFile is the filename for token cache
	CacheFile = "tokens.json"
	// CacheVersion is the current version of cache file format
	CacheVersion = 1
	// lockTimeout is the maximum wait time to acquire the file lock.
	lockTimeout = 3 * time.Second
	// lockRetryDelay is the interval between lock acquisition attempts.
	lockRetryDelay = 100 * time.Millisecond
)

// TokenEntry represents a cached access token
type TokenEntry struct {
	AccessToken string    `json:"access_token"`
	CreatedAt   time.Time `json:"created_at"`
}

// CacheData represents the structure of the cache file
type CacheData struct {
	Version int                    `json:"version"`
	Tokens  map[string]*TokenEntry `json:"tokens"`
}

// Cache manages instance access tokens with file-based persistence.
// It is safe for concurrent use both within a single process and across
// multiple processes via flock-based file locking.
type Cache struct {
	path     string // path to tokens.json
	lockPath string // path to tokens.json.lock
}

// NewCache creates a new token cache.
// The cache file is stored at ~/.ags/tokens.json
func NewCache() (*Cache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	cacheDir := filepath.Join(homeDir, CacheDir)
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	cachePath := filepath.Join(cacheDir, CacheFile)

	return &Cache{
		path:     cachePath,
		lockPath: cachePath + ".lock",
	}, nil
}

// withLock acquires the file lock, runs fn, then releases the lock.
func (c *Cache) withLock(fn func() error) error {
	fl := flock.New(c.lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	locked, err := fl.TryLockContext(ctx, lockRetryDelay)
	if err != nil || !locked {
		return fmt.Errorf("failed to acquire token cache lock: %w", err)
	}
	defer func() { _ = fl.Unlock() }()
	return fn()
}

// loadLocked reads the cache file and returns the cache data.
// Must be called while holding the file lock.
func (c *Cache) loadLocked() (*CacheData, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &CacheData{
				Version: CacheVersion,
				Tokens:  make(map[string]*TokenEntry),
			}, nil
		}
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	var cache CacheData
	if err := json.Unmarshal(data, &cache); err != nil {
		// If the file is corrupted, start fresh
		return &CacheData{
			Version: CacheVersion,
			Tokens:  make(map[string]*TokenEntry),
		}, nil
	}

	// Handle version migration if needed
	if cache.Version < CacheVersion {
		cache.Version = CacheVersion
	}

	if cache.Tokens == nil {
		cache.Tokens = make(map[string]*TokenEntry)
	}

	return &cache, nil
}

// saveLocked writes the cache data to a temp file then atomically renames it.
// Must be called while holding the file lock.
func (c *Cache) saveLocked(cache *CacheData) error {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %w", err)
	}

	// Atomic write: write to temp file in same directory, then rename.
	dir := filepath.Dir(c.path)
	tmpFile, err := os.CreateTemp(dir, "tokens-*.json.tmp")
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

	if err := os.Rename(tmpPath, c.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file to cache file: %w", err)
	}

	return nil
}

// Get retrieves the access token for an instance.
// Returns the token and true if found, empty string and false otherwise.
func (c *Cache) Get(instanceID string) (string, bool) {
	var token string
	var found bool
	_ = c.withLock(func() error {
		cache, err := c.loadLocked()
		if err != nil {
			return err
		}
		entry, ok := cache.Tokens[instanceID]
		if ok && entry != nil {
			token = entry.AccessToken
			found = true
		}
		return nil
	})
	return token, found
}

// Set stores the access token for an instance.
func (c *Cache) Set(instanceID, accessToken string) error {
	return c.withLock(func() error {
		cache, err := c.loadLocked()
		if err != nil {
			return err
		}
		cache.Tokens[instanceID] = &TokenEntry{
			AccessToken: accessToken,
			CreatedAt:   time.Now(),
		}
		return c.saveLocked(cache)
	})
}

// Delete removes the access token for an instance.
func (c *Cache) Delete(instanceID string) error {
	return c.withLock(func() error {
		cache, err := c.loadLocked()
		if err != nil {
			return err
		}
		delete(cache.Tokens, instanceID)
		return c.saveLocked(cache)
	})
}

// Clear removes all cached tokens.
func (c *Cache) Clear() error {
	return c.withLock(func() error {
		cache := &CacheData{
			Version: CacheVersion,
			Tokens:  make(map[string]*TokenEntry),
		}
		return c.saveLocked(cache)
	})
}

// List returns all cached instance IDs.
func (c *Cache) List() ([]string, error) {
	var ids []string
	err := c.withLock(func() error {
		cache, err := c.loadLocked()
		if err != nil {
			return err
		}
		ids = make([]string, 0, len(cache.Tokens))
		for id := range cache.Tokens {
			ids = append(ids, id)
		}
		return nil
	})
	return ids, err
}
