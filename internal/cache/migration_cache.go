package cache

import (
	"sync"
	"time"

	"github.com/go-logr/logr"
)

// MigrationCacheEntry represents a cached migration record
type MigrationCacheEntry struct {
	// Migration ID (typically namespace/name)
	MigrationID string
	// Migration name
	Name string
	// Migration namespace
	Namespace string
	// Workspace directory path
	WorkspacePath string
	// Phase (Running, Completed, Failed, etc.)
	Phase string
	// Start time
	StartTime time.Time
	// Completion time (nil if not completed)
	CompletionTime *time.Time
	// Repository clone path
	ClonePath string
	// Size in MB
	SizeMB float64
	// Whether cleanup has been performed
	CleanedUp bool
	// Metadata
	Metadata map[string]string
}

// MigrationCache provides an in-memory cache for migration tracking
// with automatic cleanup after retention period
type MigrationCache struct {
	// Cache entries indexed by migration ID
	entries map[string]*MigrationCacheEntry
	// Mutex for thread-safe access
	mu sync.RWMutex
	// Retention duration after completion
	retentionDuration time.Duration
	// Maximum number of entries
	maxEntries int
	// Cleanup interval
	cleanupInterval time.Duration
	// Logger
	logger logr.Logger
	// Stop channel
	stopCh chan struct{}
	// Running flag
	running bool
}

// NewMigrationCache creates a new migration cache
func NewMigrationCache(retentionHours int, maxEntries int, cleanupIntervalMinutes int, logger logr.Logger) *MigrationCache {
	return &MigrationCache{
		entries:           make(map[string]*MigrationCacheEntry),
		retentionDuration: time.Duration(retentionHours) * time.Hour,
		maxEntries:        maxEntries,
		cleanupInterval:   time.Duration(cleanupIntervalMinutes) * time.Minute,
		logger:            logger,
		stopCh:            make(chan struct{}),
		running:           false,
	}
}

// Start starts the cache cleanup goroutine
func (c *MigrationCache) Start() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()

	c.logger.Info("Starting migration cache with automatic cleanup",
		"retentionDuration", c.retentionDuration,
		"maxEntries", c.maxEntries,
		"cleanupInterval", c.cleanupInterval)

	go c.cleanupLoop()
}

// Stop stops the cache cleanup goroutine
func (c *MigrationCache) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return
	}

	c.logger.Info("Stopping migration cache")
	close(c.stopCh)
	c.running = false
}

// Set adds or updates a migration cache entry
func (c *MigrationCache) Set(entry *MigrationCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we're at max capacity and need to evict
	if len(c.entries) >= c.maxEntries && c.entries[entry.MigrationID] == nil {
		c.evictOldest()
	}

	c.entries[entry.MigrationID] = entry
	c.logger.V(1).Info("Migration cache entry updated",
		"migrationID", entry.MigrationID,
		"phase", entry.Phase,
		"workspacePath", entry.WorkspacePath)
}

// Get retrieves a migration cache entry
func (c *MigrationCache) Get(migrationID string) (*MigrationCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[migrationID]
	return entry, exists
}

// Delete removes a migration cache entry
func (c *MigrationCache) Delete(migrationID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, migrationID)
	c.logger.V(1).Info("Migration cache entry deleted", "migrationID", migrationID)
}

// List returns all cache entries
func (c *MigrationCache) List() []*MigrationCacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entries := make([]*MigrationCacheEntry, 0, len(c.entries))
	for _, entry := range c.entries {
		entries = append(entries, entry)
	}
	return entries
}

// MarkCompleted marks a migration as completed
func (c *MigrationCache) MarkCompleted(migrationID string, phase string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, exists := c.entries[migrationID]; exists {
		now := time.Now()
		entry.CompletionTime = &now
		entry.Phase = phase
		c.logger.Info("Migration marked as completed in cache",
			"migrationID", migrationID,
			"phase", phase,
			"retentionUntil", now.Add(c.retentionDuration))
	}
}

// MarkCleanedUp marks a migration workspace as cleaned up
func (c *MigrationCache) MarkCleanedUp(migrationID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, exists := c.entries[migrationID]; exists {
		entry.CleanedUp = true
		c.logger.Info("Migration workspace marked as cleaned up",
			"migrationID", migrationID,
			"workspacePath", entry.WorkspacePath)
	}
}

// GetPendingCleanup returns migrations that are completed but not yet cleaned up
func (c *MigrationCache) GetPendingCleanup() []*MigrationCacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	pending := make([]*MigrationCacheEntry, 0)
	for _, entry := range c.entries {
		if entry.CompletionTime != nil && !entry.CleanedUp {
			pending = append(pending, entry)
		}
	}
	return pending
}

// cleanupLoop runs periodic cleanup of expired entries
func (c *MigrationCache) cleanupLoop() {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanup()
		case <-c.stopCh:
			return
		}
	}
}

// cleanup removes entries that have exceeded their retention period
func (c *MigrationCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	removed := 0

	for migrationID, entry := range c.entries {
		// Only cleanup completed migrations that have exceeded retention period
		if entry.CompletionTime != nil {
			expirationTime := entry.CompletionTime.Add(c.retentionDuration)
			if now.After(expirationTime) {
				delete(c.entries, migrationID)
				removed++
				c.logger.Info("Migration cache entry expired and removed",
					"migrationID", migrationID,
					"completionTime", entry.CompletionTime,
					"expirationTime", expirationTime)
			}
		}
	}

	if removed > 0 {
		c.logger.Info("Migration cache cleanup completed",
			"entriesRemoved", removed,
			"entriesRemaining", len(c.entries))
	}
}

// evictOldest evicts the oldest completed entry to make room
func (c *MigrationCache) evictOldest() {
	var oldestID string
	var oldestTime *time.Time

	// Find the oldest completed entry
	for id, entry := range c.entries {
		if entry.CompletionTime != nil {
			if oldestTime == nil || entry.CompletionTime.Before(*oldestTime) {
				oldestTime = entry.CompletionTime
				oldestID = id
			}
		}
	}

	// If we found a completed entry, evict it
	if oldestID != "" {
		delete(c.entries, oldestID)
		c.logger.Info("Evicted oldest migration cache entry to make room",
			"migrationID", oldestID,
			"completionTime", oldestTime)
	} else {
		// If no completed entries, evict the oldest running entry
		for id, entry := range c.entries {
			if oldestTime == nil || entry.StartTime.Before(*oldestTime) {
				oldestTime = &entry.StartTime
				oldestID = id
			}
		}
		if oldestID != "" {
			delete(c.entries, oldestID)
			c.logger.Info("Evicted oldest running migration cache entry to make room",
				"migrationID", oldestID,
				"startTime", oldestTime)
		}
	}
}

// Stats returns cache statistics
func (c *MigrationCache) Stats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	running := 0
	completed := 0
	pendingCleanup := 0

	for _, entry := range c.entries {
		if entry.CompletionTime == nil {
			running++
		} else {
			completed++
			if !entry.CleanedUp {
				pendingCleanup++
			}
		}
	}

	return map[string]interface{}{
		"totalEntries":    len(c.entries),
		"running":         running,
		"completed":       completed,
		"pendingCleanup":  pendingCleanup,
		"maxEntries":      c.maxEntries,
		"retentionHours":  c.retentionDuration.Hours(),
		"cleanupInterval": c.cleanupInterval.Minutes(),
	}
}
