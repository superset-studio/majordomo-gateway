package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/secrets"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

var (
	ErrProxyKeyNotFound     = errors.New("proxy key not found")
	ErrProxyKeyRevoked      = errors.New("proxy key has been revoked")
	ErrProxyKeyInactive     = errors.New("proxy key is not active")
	ErrProxyKeyWrongOwner   = errors.New("proxy key does not belong to this Majordomo key")
	ErrNoProviderMapping    = errors.New("no provider key configured")
)

type cachedProxyKey struct {
	proxyKeyID        uuid.UUID
	majordomoAPIKeyID uuid.UUID
	isActive          bool
	revokedAt         *time.Time
	expiresAt         time.Time
}

// ProxyResolver validates proxy keys and resolves them to decrypted provider API keys.
type ProxyResolver struct {
	storage   storage.ProxyKeyStorage
	secrets   secrets.SecretStore
	cache     map[string]*cachedProxyKey // key_hash → proxy key info
	provCache map[string]string          // key_hash:provider → decrypted provider key
	cacheMu   sync.RWMutex
	cacheTTL  time.Duration
}

// NewProxyResolver creates a new ProxyResolver.
func NewProxyResolver(storage storage.ProxyKeyStorage, secretStore secrets.SecretStore) *ProxyResolver {
	return &ProxyResolver{
		storage:   storage,
		secrets:   secretStore,
		cache:     make(map[string]*cachedProxyKey),
		provCache: make(map[string]string),
		cacheTTL:  5 * time.Minute,
	}
}

// ResolveProxyKey validates a proxy key and returns the decrypted provider API key
// for the given provider. Returns ("", nil, nil) if the key is not a proxy key (no mdm_pk_ prefix).
func (r *ProxyResolver) ResolveProxyKey(ctx context.Context, authKey string, provider string, majordomoKeyID uuid.UUID) (providerKey string, proxyKeyID *uuid.UUID, err error) {
	if !strings.HasPrefix(authKey, ProxyKeyPrefix) {
		return "", nil, nil
	}

	hash := HashAPIKey(authKey)

	// Check provider key cache
	provCacheKey := hash + ":" + provider
	r.cacheMu.RLock()
	if cached, ok := r.provCache[provCacheKey]; ok {
		if pkc, ok := r.cache[hash]; ok && time.Now().Before(pkc.expiresAt) {
			id := pkc.proxyKeyID
			r.cacheMu.RUnlock()
			return cached, &id, nil
		}
	}
	r.cacheMu.RUnlock()

	// DB lookup
	proxyKey, err := r.storage.GetProxyKeyByHash(ctx, hash)
	if err != nil {
		return "", nil, fmt.Errorf("failed to look up proxy key: %w", err)
	}
	if proxyKey == nil {
		return "", nil, ErrProxyKeyNotFound
	}

	if !proxyKey.IsActive {
		if proxyKey.RevokedAt != nil {
			return "", nil, ErrProxyKeyRevoked
		}
		return "", nil, ErrProxyKeyInactive
	}

	if proxyKey.MajordomoAPIKeyID != majordomoKeyID {
		return "", nil, ErrProxyKeyWrongOwner
	}

	// Look up provider mapping
	mapping, err := r.storage.GetProviderMapping(ctx, proxyKey.ID, provider)
	if err != nil {
		return "", nil, fmt.Errorf("failed to look up provider mapping: %w", err)
	}
	if mapping == nil {
		return "", nil, fmt.Errorf("%w for %s", ErrNoProviderMapping, provider)
	}

	// Decrypt provider API key
	decrypted, err := r.secrets.Decrypt(mapping.EncryptedKey)
	if err != nil {
		return "", nil, fmt.Errorf("failed to decrypt provider key: %w", err)
	}

	// Cache the results
	r.cacheMu.Lock()
	r.cache[hash] = &cachedProxyKey{
		proxyKeyID:        proxyKey.ID,
		majordomoAPIKeyID: proxyKey.MajordomoAPIKeyID,
		isActive:          proxyKey.IsActive,
		revokedAt:         proxyKey.RevokedAt,
		expiresAt:         time.Now().Add(r.cacheTTL),
	}
	r.provCache[provCacheKey] = decrypted
	r.cacheMu.Unlock()

	// Update last_used_at asynchronously
	go func() {
		if err := r.storage.UpdateProxyKeyLastUsed(context.Background(), proxyKey.ID); err != nil {
			slog.Warn("failed to update proxy key last_used_at", "error", err, "proxy_key_id", proxyKey.ID)
		}
	}()

	id := proxyKey.ID
	return decrypted, &id, nil
}

// InvalidateCache removes a specific proxy key from the cache.
func (r *ProxyResolver) InvalidateCache(hash string) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	delete(r.cache, hash)
	// Also remove all provider cache entries for this hash
	prefix := hash + ":"
	for k := range r.provCache {
		if strings.HasPrefix(k, prefix) {
			delete(r.provCache, k)
		}
	}
}
