package experiment

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

type cacheKey struct {
	apiKeyID uuid.UUID
	ownerID  uuid.UUID // userID or orgID
}

type cachedEntry struct {
	experiments []*Experiment
	expiresAt   time.Time
}

// experimentCache provides a TTL cache for active experiments per API key.
type experimentCache struct {
	mu    sync.RWMutex
	items map[cacheKey]*cachedEntry
	ttl   time.Duration
}

func newExperimentCache(ttl time.Duration) *experimentCache {
	return &experimentCache{
		items: make(map[cacheKey]*cachedEntry),
		ttl:   ttl,
	}
}

// get returns cached experiments if present and not expired.
func (c *experimentCache) get(apiKeyID uuid.UUID, ownerID uuid.UUID) ([]*Experiment, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := cacheKey{apiKeyID: apiKeyID, ownerID: ownerID}
	entry, ok := c.items[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.experiments, true
}

// set stores experiments in the cache with TTL.
func (c *experimentCache) set(apiKeyID uuid.UUID, ownerID uuid.UUID, experiments []*Experiment) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey{apiKeyID: apiKeyID, ownerID: ownerID}
	c.items[key] = &cachedEntry{
		experiments: experiments,
		expiresAt:   time.Now().Add(c.ttl),
	}
}

// invalidateAll clears the entire cache. Called when experiment status changes.
func (c *experimentCache) invalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[cacheKey]*cachedEntry)
}
