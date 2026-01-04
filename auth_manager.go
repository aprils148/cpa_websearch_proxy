package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AuthEntry represents a single auth file entry
type AuthEntry struct {
	FilePath     string
	RefreshToken string
	ProjectID    string // GCP project ID from auth file metadata
	FailCount    int
	LastFail     time.Time
}

// AuthManager manages multiple auth files with rotation on failure
type AuthManager struct {
	mu           sync.RWMutex
	entries      []*AuthEntry
	currentIndex int
	failCooldown time.Duration // cooldown period before retrying a failed auth
}

func expandHomePath(p string) (string, error) {
	if p == "" {
		return p, nil
	}
	if p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		return home, nil
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}

// NewAuthManager creates a new auth manager
func NewAuthManager(cooldown time.Duration) *AuthManager {
	if cooldown == 0 {
		cooldown = 5 * time.Minute
	}
	return &AuthManager{
		entries:      make([]*AuthEntry, 0),
		failCooldown: cooldown,
	}
}

// LoadFromDirectory loads all antigravity auth files from a directory
func (am *AuthManager) LoadFromDirectory(dirPath string) error {
	expanded, err := expandHomePath(dirPath)
	if err != nil {
		return err
	}
	dirPath = expanded

	// Check if path is a directory
	info, err := os.Stat(dirPath)
	if err != nil {
		return fmt.Errorf("failed to stat path %s: %w", dirPath, err)
	}

	if !info.IsDir() {
		// If it's a file, load it as a single auth file
		return am.LoadFromFile(dirPath)
	}

	// Read directory entries
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", dirPath, err)
	}

	loadedCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Match antigravity*.json files
		if strings.HasPrefix(name, "antigravity") && strings.HasSuffix(name, ".json") {
			filePath := filepath.Join(dirPath, name)
			if err := am.LoadFromFile(filePath); err != nil {
				log.Printf("Warning: failed to load auth file %s: %v", filePath, err)
				continue
			}
			loadedCount++
		}
	}

	if loadedCount == 0 {
		return fmt.Errorf("no valid antigravity auth files found in %s", dirPath)
	}

	// Shuffle entries for random initial selection
	am.shuffle()

	log.Printf("Loaded %d auth files from %s", loadedCount, dirPath)
	return nil
}

// LoadFromFile loads a single auth file
func (am *AuthManager) LoadFromFile(filePath string) error {
	expanded, err := expandHomePath(filePath)
	if err != nil {
		return err
	}
	filePath = expanded

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	var authData map[string]interface{}
	if err := json.Unmarshal(data, &authData); err != nil {
		return fmt.Errorf("failed to parse JSON %s: %w", filePath, err)
	}

	refreshToken, ok := authData["refresh_token"].(string)
	if !ok || refreshToken == "" {
		return fmt.Errorf("no refresh_token found in %s", filePath)
	}

	// Extract project ID from metadata if available (like CLIProxyAPI)
	var projectID string
	if metadata, ok := authData["metadata"].(map[string]interface{}); ok {
		if p, ok := metadata["project"].(string); ok {
			projectID = strings.TrimSpace(p)
		}
	}
	// Also check top-level project field
	if projectID == "" {
		if p, ok := authData["project"].(string); ok {
			projectID = strings.TrimSpace(p)
		}
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	// Check for duplicates
	for _, entry := range am.entries {
		if entry.RefreshToken == refreshToken {
			return nil // Already loaded
		}
	}

	am.entries = append(am.entries, &AuthEntry{
		FilePath:     filePath,
		RefreshToken: refreshToken,
		ProjectID:    projectID,
	})

	return nil
}

// shuffle randomizes the order of entries
func (am *AuthManager) shuffle() {
	am.mu.Lock()
	defer am.mu.Unlock()

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(am.entries), func(i, j int) {
		am.entries[i], am.entries[j] = am.entries[j], am.entries[i]
	})
}

// GetCurrentRefreshToken returns the current refresh token
func (am *AuthManager) GetCurrentRefreshToken() (string, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if len(am.entries) == 0 {
		return "", fmt.Errorf("no auth entries available")
	}

	entry := am.entries[am.currentIndex]
	return entry.RefreshToken, nil
}

// GetCurrentAuthPath returns the current auth file path (for logging)
func (am *AuthManager) GetCurrentAuthPath() string {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if len(am.entries) == 0 {
		return ""
	}

	return am.entries[am.currentIndex].FilePath
}

// GetCurrentProjectID returns the project ID for the current auth entry
func (am *AuthManager) GetCurrentProjectID() string {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if len(am.entries) == 0 {
		return ""
	}

	return am.entries[am.currentIndex].ProjectID
}

// MarkCurrentFailed marks the current auth as failed and switches to next
// Returns true if successfully switched to a new auth, false if all auths failed
func (am *AuthManager) MarkCurrentFailed() bool {
	am.mu.Lock()
	defer am.mu.Unlock()

	if len(am.entries) == 0 {
		return false
	}

	// Mark current as failed
	entry := am.entries[am.currentIndex]
	entry.FailCount++
	entry.LastFail = time.Now()
	log.Printf("Auth failed for %s (fail count: %d)", filepath.Base(entry.FilePath), entry.FailCount)

	// Find next available auth
	startIndex := am.currentIndex
	for {
		am.currentIndex = (am.currentIndex + 1) % len(am.entries)

		// Checked all entries, back to start
		if am.currentIndex == startIndex {
			// Check if cooldown has passed for current entry
			if time.Since(entry.LastFail) >= am.failCooldown {
				log.Printf("All auths failed, retrying %s after cooldown", filepath.Base(entry.FilePath))
				return true
			}
			return false
		}

		nextEntry := am.entries[am.currentIndex]
		// Check if this entry is available (not in cooldown)
		if nextEntry.FailCount == 0 || time.Since(nextEntry.LastFail) >= am.failCooldown {
			log.Printf("Switched to auth: %s", filepath.Base(nextEntry.FilePath))
			return true
		}
	}
}

// ResetCurrentFailCount resets the fail count for the current auth (on success)
func (am *AuthManager) ResetCurrentFailCount() {
	am.mu.Lock()
	defer am.mu.Unlock()

	if len(am.entries) == 0 {
		return
	}

	am.entries[am.currentIndex].FailCount = 0
}

// Count returns the number of loaded auth entries
func (am *AuthManager) Count() int {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return len(am.entries)
}

// ListAuthFiles returns a list of loaded auth file paths
func (am *AuthManager) ListAuthFiles() []string {
	am.mu.RLock()
	defer am.mu.RUnlock()

	paths := make([]string, len(am.entries))
	for i, entry := range am.entries {
		paths[i] = entry.FilePath
	}
	return paths
}
