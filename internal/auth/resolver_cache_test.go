package auth

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

// cacheTestAPIKeyStore is a minimal in-memory APIKeyStorage that returns
// any key by its hash. Defined locally so this test file can land
// independently of any other test mocks in this package.
type cacheTestAPIKeyStore struct {
	keys map[string]*models.APIKey
}

func newCacheTestAPIKeyStore() *cacheTestAPIKeyStore {
	return &cacheTestAPIKeyStore{keys: make(map[string]*models.APIKey)}
}

func (m *cacheTestAPIKeyStore) put(plaintext string) *models.APIKey {
	hash := HashAPIKey(plaintext)
	k := &models.APIKey{
		ID:        uuid.New(),
		KeyHash:   hash,
		Name:      "k",
		IsActive:  true,
		CreatedAt: time.Now(),
	}
	m.keys[hash] = k
	return k
}

func (m *cacheTestAPIKeyStore) GetAPIKeyByHash(_ context.Context, hash string) (*models.APIKey, error) {
	if k, ok := m.keys[hash]; ok {
		return k, nil
	}
	return nil, nil
}

// Stubs — unused by these tests.
func (m *cacheTestAPIKeyStore) CreateAPIKey(context.Context, string, *models.CreateAPIKeyInput) (*models.APIKey, error) {
	return nil, nil
}
func (m *cacheTestAPIKeyStore) GetAPIKeyByID(context.Context, uuid.UUID) (*models.APIKey, error) {
	return nil, errors.New("unused")
}
func (m *cacheTestAPIKeyStore) ListAPIKeys(context.Context) ([]*models.APIKey, error) {
	return nil, nil
}
func (m *cacheTestAPIKeyStore) UpdateAPIKey(context.Context, uuid.UUID, *models.UpdateAPIKeyInput) (*models.APIKey, error) {
	return nil, nil
}
func (m *cacheTestAPIKeyStore) RevokeAPIKey(context.Context, uuid.UUID) error { return nil }
func (m *cacheTestAPIKeyStore) UpdateAPIKeyLastUsed(context.Context, uuid.UUID) error {
	return nil
}
func (m *cacheTestAPIKeyStore) ListAPIKeysByUserID(context.Context, uuid.UUID) ([]*models.APIKey, error) {
	return nil, nil
}
func (m *cacheTestAPIKeyStore) ListAPIKeysByOrgID(context.Context, uuid.UUID) ([]*models.APIKey, error) {
	return nil, nil
}

// TestResolverCache_BoundedUnderLoad verifies that the cache stays at or
// below resolverCacheSize even when far more unique keys are resolved —
// the bug fixed by this change. The previous map-based implementation
// would grow without bound.
func TestResolverCache_BoundedUnderLoad(t *testing.T) {
	store := newCacheTestAPIKeyStore()

	// 5x the cache size of unique keys, all valid, all resolved once.
	keys := make([]string, 0, resolverCacheSize*5)
	for i := 0; i < resolverCacheSize*5; i++ {
		plaintext := fmt.Sprintf("mdm_sk_load_%d", i)
		store.put(plaintext)
		keys = append(keys, plaintext)
	}

	r := NewResolver(store)
	for _, k := range keys {
		if _, err := r.ResolveAPIKey(context.Background(), k); err != nil {
			t.Fatalf("resolve %q: %v", k, err)
		}
	}

	if got := r.cache.Len(); got > resolverCacheSize {
		t.Fatalf("cache exceeded bound: got %d, want <= %d", got, resolverCacheSize)
	}
}

// TestResolverCache_EvictsExpired verifies stale entries are evicted on
// read so the LRU's recency tracking stays in sync with logical validity.
func TestResolverCache_EvictsExpired(t *testing.T) {
	store := newCacheTestAPIKeyStore()
	store.put("mdm_sk_freshly_stale")

	r := NewResolver(store)
	r.cacheTTL = 0 // any insert is immediately stale

	if _, err := r.ResolveAPIKey(context.Background(), "mdm_sk_freshly_stale"); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	// First resolve writes the entry. Now resolve again — getFromCache
	// should treat it as expired and remove it.
	if _, err := r.ResolveAPIKey(context.Background(), "mdm_sk_freshly_stale"); err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	// On the second resolve we re-cached, so size is 1 (the new fresh entry).
	if got := r.cache.Len(); got != 1 {
		t.Fatalf("expected exactly 1 entry after re-cache, got %d", got)
	}
}

// TestResolverCache_LRUOrderingFavorsRecent verifies that hot keys
// stay cached even when many cold keys arrive — the practical benefit
// of LRU over plain capacity-limited maps.
func TestResolverCache_LRUOrderingFavorsRecent(t *testing.T) {
	store := newCacheTestAPIKeyStore()
	hot := store.put("mdm_sk_hot")

	r := NewResolver(store)

	if _, err := r.ResolveAPIKey(context.Background(), "mdm_sk_hot"); err != nil {
		t.Fatalf("seed hot: %v", err)
	}

	// Flood the cache with cold one-shot keys, but keep touching hot
	// every so often so it stays at the front of the LRU.
	for i := 0; i < resolverCacheSize*2; i++ {
		plaintext := fmt.Sprintf("mdm_sk_cold_%d", i)
		store.put(plaintext)
		if _, err := r.ResolveAPIKey(context.Background(), plaintext); err != nil {
			t.Fatalf("cold %d: %v", i, err)
		}
		if i%32 == 0 {
			if _, err := r.ResolveAPIKey(context.Background(), "mdm_sk_hot"); err != nil {
				t.Fatalf("touch hot: %v", err)
			}
		}
	}

	if !r.cache.Contains(HashAPIKey("mdm_sk_hot")) {
		t.Fatalf("expected hot key to remain cached under LRU pressure")
	}
	_ = hot
}
