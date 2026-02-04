package storage

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type activeKeysEntry struct {
	keys      map[string]bool
	expiresAt time.Time
}

// ActiveKeysCache caches active metadata keys per Majordomo API key with TTL.
// This reduces database queries when splitting metadata into raw and indexed.
type ActiveKeysCache struct {
	mu    sync.RWMutex
	cache map[uuid.UUID]activeKeysEntry
	ttl   time.Duration
	db    *sqlx.DB
}

// NewActiveKeysCache creates a new cache with the specified TTL.
func NewActiveKeysCache(db *sqlx.DB, ttl time.Duration) *ActiveKeysCache {
	return &ActiveKeysCache{
		cache: make(map[uuid.UUID]activeKeysEntry),
		ttl:   ttl,
		db:    db,
	}
}

// GetActiveKeys returns the set of active metadata keys for a Majordomo API key ID.
// Results are cached with TTL to reduce database load.
func (c *ActiveKeysCache) GetActiveKeys(ctx context.Context, apiKeyID uuid.UUID) (map[string]bool, error) {
	// Check cache first
	c.mu.RLock()
	entry, ok := c.cache[apiKeyID]
	if ok && time.Now().Before(entry.expiresAt) {
		c.mu.RUnlock()
		return entry.keys, nil
	}
	c.mu.RUnlock()

	// Cache miss or expired, query database
	keys, err := c.fetchActiveKeys(ctx, apiKeyID)
	if err != nil {
		return nil, err
	}

	// Update cache
	c.mu.Lock()
	c.cache[apiKeyID] = activeKeysEntry{
		keys:      keys,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()

	return keys, nil
}

func (c *ActiveKeysCache) fetchActiveKeys(ctx context.Context, apiKeyID uuid.UUID) (map[string]bool, error) {
	query := `
		SELECT key_name
		FROM llm_requests_metadata_keys
		WHERE majordomo_api_key_id = $1 AND is_active = true`

	rows, err := c.db.QueryContext(ctx, query, apiKeyID)
	if err != nil {
		slog.Warn("failed to fetch active keys", "error", err, "api_key_id", apiKeyID)
		return make(map[string]bool), nil // Return empty on error to not block logging
	}
	defer rows.Close()

	keys := make(map[string]bool)
	for rows.Next() {
		var keyName string
		if err := rows.Scan(&keyName); err != nil {
			continue
		}
		keys[keyName] = true
	}

	return keys, rows.Err()
}

// InvalidateAPIKey removes the cached entry for a Majordomo API key ID.
// Call this when keys are activated/deactivated.
func (c *ActiveKeysCache) InvalidateAPIKey(apiKeyID uuid.UUID) {
	c.mu.Lock()
	delete(c.cache, apiKeyID)
	c.mu.Unlock()
}

// InvalidateAll clears the entire cache.
func (c *ActiveKeysCache) InvalidateAll() {
	c.mu.Lock()
	c.cache = make(map[uuid.UUID]activeKeysEntry)
	c.mu.Unlock()
}
