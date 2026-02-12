package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

// mockProxyKeyStorage implements storage.ProxyKeyStorage for testing
type mockProxyKeyStorage struct {
	proxyKeys        map[string]*models.ProxyKey   // key_hash → proxy key
	providerMappings map[string]*models.ProviderMapping // proxyKeyID:provider → mapping
	lastUsedCalls    []uuid.UUID
}

func newMockProxyKeyStorage() *mockProxyKeyStorage {
	return &mockProxyKeyStorage{
		proxyKeys:        make(map[string]*models.ProxyKey),
		providerMappings: make(map[string]*models.ProviderMapping),
	}
}

func (m *mockProxyKeyStorage) CreateProxyKey(_ context.Context, keyHash string, majordomoKeyID uuid.UUID, input *models.CreateProxyKeyInput) (*models.ProxyKey, error) {
	pk := &models.ProxyKey{
		ID:                uuid.New(),
		KeyHash:           keyHash,
		Name:              input.Name,
		Description:       input.Description,
		MajordomoAPIKeyID: majordomoKeyID,
		IsActive:          true,
		CreatedAt:         time.Now(),
	}
	m.proxyKeys[keyHash] = pk
	return pk, nil
}

func (m *mockProxyKeyStorage) GetProxyKeyByHash(_ context.Context, keyHash string) (*models.ProxyKey, error) {
	pk, ok := m.proxyKeys[keyHash]
	if !ok {
		return nil, nil
	}
	return pk, nil
}

func (m *mockProxyKeyStorage) GetProxyKeyByID(_ context.Context, id uuid.UUID) (*models.ProxyKey, error) {
	for _, pk := range m.proxyKeys {
		if pk.ID == id {
			return pk, nil
		}
	}
	return nil, errors.New("proxy key not found")
}

func (m *mockProxyKeyStorage) ListProxyKeys(_ context.Context, majordomoKeyID uuid.UUID) ([]*models.ProxyKey, error) {
	var result []*models.ProxyKey
	for _, pk := range m.proxyKeys {
		if pk.MajordomoAPIKeyID == majordomoKeyID {
			result = append(result, pk)
		}
	}
	return result, nil
}

func (m *mockProxyKeyStorage) RevokeProxyKey(_ context.Context, id uuid.UUID) error {
	for _, pk := range m.proxyKeys {
		if pk.ID == id {
			pk.IsActive = false
			now := time.Now()
			pk.RevokedAt = &now
			return nil
		}
	}
	return errors.New("proxy key not found")
}

func (m *mockProxyKeyStorage) UpdateProxyKeyLastUsed(_ context.Context, id uuid.UUID) error {
	m.lastUsedCalls = append(m.lastUsedCalls, id)
	return nil
}

func (m *mockProxyKeyStorage) SetProviderMapping(_ context.Context, proxyKeyID uuid.UUID, provider string, encryptedKey string) error {
	key := proxyKeyID.String() + ":" + provider
	m.providerMappings[key] = &models.ProviderMapping{
		ID:           uuid.New(),
		ProxyKeyID:   proxyKeyID,
		Provider:     provider,
		EncryptedKey: encryptedKey,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	return nil
}

func (m *mockProxyKeyStorage) GetProviderMapping(_ context.Context, proxyKeyID uuid.UUID, provider string) (*models.ProviderMapping, error) {
	key := proxyKeyID.String() + ":" + provider
	mapping, ok := m.providerMappings[key]
	if !ok {
		return nil, nil
	}
	return mapping, nil
}

func (m *mockProxyKeyStorage) ListProviderMappings(_ context.Context, proxyKeyID uuid.UUID) ([]*models.ProviderMapping, error) {
	var result []*models.ProviderMapping
	prefix := proxyKeyID.String() + ":"
	for k, v := range m.providerMappings {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			result = append(result, v)
		}
	}
	return result, nil
}

func (m *mockProxyKeyStorage) DeleteProviderMapping(_ context.Context, proxyKeyID uuid.UUID, provider string) error {
	key := proxyKeyID.String() + ":" + provider
	if _, ok := m.providerMappings[key]; !ok {
		return errors.New("provider mapping not found")
	}
	delete(m.providerMappings, key)
	return nil
}

// mockSecretStore implements secrets.SecretStore for testing
type mockSecretStore struct {
	prefix string
}

func (m *mockSecretStore) Encrypt(plaintext string) (string, error) {
	return m.prefix + plaintext, nil
}

func (m *mockSecretStore) Decrypt(ciphertext string) (string, error) {
	if len(ciphertext) < len(m.prefix) {
		return "", errors.New("invalid ciphertext")
	}
	return ciphertext[len(m.prefix):], nil
}

func setupTest() (*ProxyResolver, *mockProxyKeyStorage, uuid.UUID, *models.ProxyKey) {
	store := newMockProxyKeyStorage()
	secretStore := &mockSecretStore{prefix: "enc:"}
	resolver := NewProxyResolver(store, secretStore)

	majordomoKeyID := uuid.New()
	proxyKeyID := uuid.New()
	proxyKeyHash := HashAPIKey("mdm_pk_testkey123")

	pk := &models.ProxyKey{
		ID:                proxyKeyID,
		KeyHash:           proxyKeyHash,
		Name:              "Test Proxy Key",
		MajordomoAPIKeyID: majordomoKeyID,
		IsActive:          true,
		CreatedAt:         time.Now(),
	}
	store.proxyKeys[proxyKeyHash] = pk

	// Set up provider mapping
	store.providerMappings[proxyKeyID.String()+":openai"] = &models.ProviderMapping{
		ID:           uuid.New(),
		ProxyKeyID:   proxyKeyID,
		Provider:     "openai",
		EncryptedKey: "enc:sk-real-openai-key",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	return resolver, store, majordomoKeyID, pk
}

func TestResolveProxyKey_HappyPath(t *testing.T) {
	resolver, _, majordomoKeyID, pk := setupTest()
	ctx := context.Background()

	providerKey, proxyKeyID, err := resolver.ResolveProxyKey(ctx, "mdm_pk_testkey123", "openai", majordomoKeyID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if providerKey != "sk-real-openai-key" {
		t.Fatalf("expected 'sk-real-openai-key', got %q", providerKey)
	}
	if proxyKeyID == nil || *proxyKeyID != pk.ID {
		t.Fatalf("expected proxy key ID %s, got %v", pk.ID, proxyKeyID)
	}
}

func TestResolveProxyKey_NotAProxyKey(t *testing.T) {
	resolver, _, majordomoKeyID, _ := setupTest()
	ctx := context.Background()

	providerKey, proxyKeyID, err := resolver.ResolveProxyKey(ctx, "sk-regular-api-key", "openai", majordomoKeyID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if providerKey != "" {
		t.Fatalf("expected empty provider key, got %q", providerKey)
	}
	if proxyKeyID != nil {
		t.Fatalf("expected nil proxy key ID, got %v", proxyKeyID)
	}
}

func TestResolveProxyKey_NotFound(t *testing.T) {
	resolver, _, majordomoKeyID, _ := setupTest()
	ctx := context.Background()

	_, _, err := resolver.ResolveProxyKey(ctx, "mdm_pk_nonexistent", "openai", majordomoKeyID)
	if !errors.Is(err, ErrProxyKeyNotFound) {
		t.Fatalf("expected ErrProxyKeyNotFound, got %v", err)
	}
}

func TestResolveProxyKey_Revoked(t *testing.T) {
	resolver, store, majordomoKeyID, _ := setupTest()
	ctx := context.Background()

	// Revoke the key
	revokedHash := HashAPIKey("mdm_pk_revokedkey")
	now := time.Now()
	store.proxyKeys[revokedHash] = &models.ProxyKey{
		ID:                uuid.New(),
		KeyHash:           revokedHash,
		Name:              "Revoked Key",
		MajordomoAPIKeyID: majordomoKeyID,
		IsActive:          false,
		RevokedAt:         &now,
		CreatedAt:         time.Now(),
	}

	_, _, err := resolver.ResolveProxyKey(ctx, "mdm_pk_revokedkey", "openai", majordomoKeyID)
	if !errors.Is(err, ErrProxyKeyRevoked) {
		t.Fatalf("expected ErrProxyKeyRevoked, got %v", err)
	}
}

func TestResolveProxyKey_WrongOwner(t *testing.T) {
	resolver, _, _, _ := setupTest()
	ctx := context.Background()

	differentKeyID := uuid.New()
	_, _, err := resolver.ResolveProxyKey(ctx, "mdm_pk_testkey123", "openai", differentKeyID)
	if !errors.Is(err, ErrProxyKeyWrongOwner) {
		t.Fatalf("expected ErrProxyKeyWrongOwner, got %v", err)
	}
}

func TestResolveProxyKey_NoProviderMapping(t *testing.T) {
	resolver, _, majordomoKeyID, _ := setupTest()
	ctx := context.Background()

	_, _, err := resolver.ResolveProxyKey(ctx, "mdm_pk_testkey123", "anthropic", majordomoKeyID)
	if !errors.Is(err, ErrNoProviderMapping) {
		t.Fatalf("expected ErrNoProviderMapping, got %v", err)
	}
}

func TestResolveProxyKey_CachingWorks(t *testing.T) {
	resolver, store, majordomoKeyID, _ := setupTest()
	ctx := context.Background()

	// First call - populates cache
	key1, _, err := resolver.ResolveProxyKey(ctx, "mdm_pk_testkey123", "openai", majordomoKeyID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Remove from storage to prove cache is used
	hash := HashAPIKey("mdm_pk_testkey123")
	delete(store.proxyKeys, hash)

	// Second call - should use cache
	key2, _, err := resolver.ResolveProxyKey(ctx, "mdm_pk_testkey123", "openai", majordomoKeyID)
	if err != nil {
		t.Fatalf("unexpected error on cached call: %v", err)
	}

	if key1 != key2 {
		t.Fatalf("expected same key from cache, got %q and %q", key1, key2)
	}
}

func TestResolveProxyKey_InvalidateCache(t *testing.T) {
	resolver, store, majordomoKeyID, _ := setupTest()
	ctx := context.Background()

	// First call - populates cache
	_, _, err := resolver.ResolveProxyKey(ctx, "mdm_pk_testkey123", "openai", majordomoKeyID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Invalidate cache
	hash := HashAPIKey("mdm_pk_testkey123")
	resolver.InvalidateCache(hash)

	// Remove from storage
	delete(store.proxyKeys, hash)

	// Should now fail since cache is cleared and storage has no entry
	_, _, err = resolver.ResolveProxyKey(ctx, "mdm_pk_testkey123", "openai", majordomoKeyID)
	if !errors.Is(err, ErrProxyKeyNotFound) {
		t.Fatalf("expected ErrProxyKeyNotFound after cache invalidation, got %v", err)
	}
}
