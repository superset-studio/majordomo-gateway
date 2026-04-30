package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

var (
	ErrInvalidAPIKey  = errors.New("invalid API key")
	ErrAPIKeyRevoked  = errors.New("API key has been revoked")
	ErrAPIKeyInactive = errors.New("API key is not active")
)

// resolverCacheSize bounds the in-memory api-key resolver cache. Sized
// for ~hundreds of active customers per pod with comfortable headroom
// for transient probes / rotated keys before LRU eviction kicks in.
//
// At ~200 bytes per entry (hash key + cachedKey struct + APIKeyInfo),
// 4096 entries is ~800 KB — trivial — and gives 8x slack over a typical
// hundreds-of-customers fleet.
const resolverCacheSize = 4096

type cachedKey struct {
	info      *models.APIKeyInfo
	expiresAt time.Time
	isValid   bool
}

type Resolver struct {
	storage  storage.APIKeyStorage
	cache    *lru.Cache[string, *cachedKey]
	cacheTTL time.Duration
}

func NewResolver(storage storage.APIKeyStorage) *Resolver {
	// lru.New only fails for size <= 0; size is a compile-time constant > 0.
	cache, _ := lru.New[string, *cachedKey](resolverCacheSize)
	return &Resolver{
		storage:  storage,
		cache:    cache,
		cacheTTL: 5 * time.Minute,
	}
}

func (r *Resolver) ResolveAPIKey(ctx context.Context, apiKey string) (*models.APIKeyInfo, error) {
	if apiKey == "" {
		return nil, ErrInvalidAPIKey
	}

	hash := HashAPIKey(apiKey)

	// Check cache first
	if cached := r.getFromCache(hash); cached != nil {
		if !cached.isValid {
			return nil, ErrAPIKeyInactive
		}
		return cached.info, nil
	}

	// Database lookup
	key, err := r.storage.GetAPIKeyByHash(ctx, hash)
	if err != nil {
		return nil, err
	}

	if key == nil {
		r.cacheInvalid(hash)
		return nil, ErrInvalidAPIKey
	}

	if !key.IsActive {
		r.cacheInvalid(hash)
		if key.RevokedAt != nil {
			return nil, ErrAPIKeyRevoked
		}
		return nil, ErrAPIKeyInactive
	}

	info := &models.APIKeyInfo{
		ID:     key.ID,
		Hash:   hash,
		Alias:  &key.Name,
		UserID: key.UserID,
		OrgID:  key.OrgID,
	}

	r.cacheValid(hash, info)

	// Update last_used_at asynchronously
	go func() {
		if err := r.storage.UpdateAPIKeyLastUsed(context.Background(), key.ID); err != nil {
			slog.Warn("failed to update API key last_used_at", "error", err, "key_id", key.ID)
		}
	}()

	return info, nil
}

// getFromCache returns the cached entry for hash if present and unexpired.
// Stale entries are evicted on access so the LRU's recency tracking stays
// in sync with logical validity (an expired entry shouldn't keep a "fresh"
// slot warm just because it was recently accessed).
func (r *Resolver) getFromCache(hash string) *cachedKey {
	cached, ok := r.cache.Get(hash)
	if !ok {
		return nil
	}
	if time.Now().After(cached.expiresAt) {
		r.cache.Remove(hash)
		return nil
	}
	return cached
}

func (r *Resolver) cacheValid(hash string, info *models.APIKeyInfo) {
	r.cache.Add(hash, &cachedKey{
		info:      info,
		expiresAt: time.Now().Add(r.cacheTTL),
		isValid:   true,
	})
}

func (r *Resolver) cacheInvalid(hash string) {
	r.cache.Add(hash, &cachedKey{
		info:      nil,
		expiresAt: time.Now().Add(r.cacheTTL),
		isValid:   false,
	})
}

// InvalidateCache removes a specific key from the cache (call after revocation)
func (r *Resolver) InvalidateCache(hash string) {
	r.cache.Remove(hash)
}

// HashAPIKey computes SHA256 hash of an API key
func HashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
