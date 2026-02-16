package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

var (
	ErrInvalidAPIKey  = errors.New("invalid API key")
	ErrAPIKeyRevoked  = errors.New("API key has been revoked")
	ErrAPIKeyInactive = errors.New("API key is not active")
)

type cachedKey struct {
	info      *models.APIKeyInfo
	expiresAt time.Time
	isValid   bool
}

type Resolver struct {
	storage  storage.APIKeyStorage
	cache    map[string]*cachedKey
	cacheMu  sync.RWMutex
	cacheTTL time.Duration
}

func NewResolver(storage storage.APIKeyStorage) *Resolver {
	return &Resolver{
		storage:  storage,
		cache:    make(map[string]*cachedKey),
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

func (r *Resolver) getFromCache(hash string) *cachedKey {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()

	cached, ok := r.cache[hash]
	if !ok {
		return nil
	}

	if time.Now().After(cached.expiresAt) {
		return nil // Expired
	}

	return cached
}

func (r *Resolver) cacheValid(hash string, info *models.APIKeyInfo) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	r.cache[hash] = &cachedKey{
		info:      info,
		expiresAt: time.Now().Add(r.cacheTTL),
		isValid:   true,
	}
}

func (r *Resolver) cacheInvalid(hash string) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	r.cache[hash] = &cachedKey{
		info:      nil,
		expiresAt: time.Now().Add(r.cacheTTL),
		isValid:   false,
	}
}

// InvalidateCache removes a specific key from the cache (call after revocation)
func (r *Resolver) InvalidateCache(hash string) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	delete(r.cache, hash)
}

// HashAPIKey computes SHA256 hash of an API key
func HashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
