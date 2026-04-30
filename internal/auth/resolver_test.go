package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

// --- mock APIKeyStorage --------------------------------------------------

type mockAPIKeyStorage struct {
	keys          map[string]*models.APIKey // key_hash -> APIKey
	lastUsedCalls []uuid.UUID
}

func newMockAPIKeyStorage() *mockAPIKeyStorage {
	return &mockAPIKeyStorage{keys: make(map[string]*models.APIKey)}
}

func (m *mockAPIKeyStorage) put(hash string, key *models.APIKey) {
	m.keys[hash] = key
}

func (m *mockAPIKeyStorage) CreateAPIKey(_ context.Context, hash string, input *models.CreateAPIKeyInput) (*models.APIKey, error) {
	k := &models.APIKey{
		ID:        uuid.New(),
		KeyHash:   hash,
		Name:      input.Name,
		UserID:    input.UserID,
		OrgID:     input.OrgID,
		IsActive:  true,
		CreatedAt: time.Now(),
	}
	m.keys[hash] = k
	return k, nil
}

func (m *mockAPIKeyStorage) GetAPIKeyByHash(_ context.Context, hash string) (*models.APIKey, error) {
	if k, ok := m.keys[hash]; ok {
		return k, nil
	}
	return nil, nil
}

func (m *mockAPIKeyStorage) GetAPIKeyByID(_ context.Context, id uuid.UUID) (*models.APIKey, error) {
	for _, k := range m.keys {
		if k.ID == id {
			return k, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockAPIKeyStorage) ListAPIKeys(context.Context) ([]*models.APIKey, error) { return nil, nil }
func (m *mockAPIKeyStorage) UpdateAPIKey(context.Context, uuid.UUID, *models.UpdateAPIKeyInput) (*models.APIKey, error) {
	return nil, nil
}
func (m *mockAPIKeyStorage) RevokeAPIKey(context.Context, uuid.UUID) error { return nil }
func (m *mockAPIKeyStorage) UpdateAPIKeyLastUsed(_ context.Context, id uuid.UUID) error {
	m.lastUsedCalls = append(m.lastUsedCalls, id)
	return nil
}
func (m *mockAPIKeyStorage) ListAPIKeysByUserID(context.Context, uuid.UUID) ([]*models.APIKey, error) {
	return nil, nil
}
func (m *mockAPIKeyStorage) ListAPIKeysByOrgID(context.Context, uuid.UUID) ([]*models.APIKey, error) {
	return nil, nil
}

// --- mock UserStorage ----------------------------------------------------

type mockUserStorage struct {
	users map[uuid.UUID]*models.User
}

func newMockUserStorage() *mockUserStorage {
	return &mockUserStorage{users: make(map[uuid.UUID]*models.User)}
}

func (m *mockUserStorage) put(u *models.User) { m.users[u.ID] = u }

func (m *mockUserStorage) GetUserByID(_ context.Context, id uuid.UUID) (*models.User, error) {
	if u, ok := m.users[id]; ok {
		return u, nil
	}
	return nil, nil
}

// Unused-by-resolver methods — return zero values.
func (m *mockUserStorage) CreateUser(context.Context, *models.CreateUserInput) (*models.User, error) {
	return nil, nil
}
func (m *mockUserStorage) GetUserByUsername(context.Context, string) (*models.User, error) {
	return nil, nil
}
func (m *mockUserStorage) GetUserByEmail(context.Context, string) (*models.User, error) {
	return nil, nil
}
func (m *mockUserStorage) GetUserByAuthProvider(context.Context, string, string) (*models.User, error) {
	return nil, nil
}
func (m *mockUserStorage) CreateOAuthUser(context.Context, *models.CreateOAuthUserInput) (*models.User, error) {
	return nil, nil
}
func (m *mockUserStorage) ListUsers(context.Context) ([]*models.User, error) { return nil, nil }
func (m *mockUserStorage) UpdateUserPassword(context.Context, uuid.UUID, string) error {
	return nil
}
func (m *mockUserStorage) UpdateUserS3Config(context.Context, uuid.UUID, string, string, string, string, string) error {
	return nil
}
func (m *mockUserStorage) ClearUserS3Config(context.Context, uuid.UUID) error { return nil }
func (m *mockUserStorage) GetUserS3Config(context.Context, uuid.UUID) (*models.User, error) {
	return nil, nil
}
func (m *mockUserStorage) UpdateUserCloudStorageConfig(context.Context, uuid.UUID, string, string, string, string, string, string, string, string, string) error {
	return nil
}
func (m *mockUserStorage) ClearUserCloudStorageConfig(context.Context, uuid.UUID) error {
	return nil
}
func (m *mockUserStorage) GetUserCloudStorageConfig(context.Context, uuid.UUID) (*models.User, error) {
	return nil, nil
}
func (m *mockUserStorage) MarkUserEmailVerified(context.Context, uuid.UUID) error { return nil }

// --- tests ---------------------------------------------------------------

// activeKey/inactiveUser test fixtures keep cases readable.
func makeKey(t *testing.T, store *mockAPIKeyStorage, plaintext string, userID *uuid.UUID, active bool) *models.APIKey {
	t.Helper()
	hash := HashAPIKey(plaintext)
	k := &models.APIKey{
		ID:        uuid.New(),
		KeyHash:   hash,
		Name:      "test-key",
		UserID:    userID,
		IsActive:  active,
		CreatedAt: time.Now(),
	}
	store.put(hash, k)
	return k
}

func TestResolveAPIKey_HappyPath(t *testing.T) {
	apiStore := newMockAPIKeyStorage()
	userStore := newMockUserStorage()

	user := &models.User{ID: uuid.New(), Username: "u1", IsActive: true}
	userStore.put(user)
	makeKey(t, apiStore, "mdm_sk_alive", &user.ID, true)

	r := NewResolver(apiStore, userStore)
	info, err := r.ResolveAPIKey(context.Background(), "mdm_sk_alive")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if info == nil || info.UserID == nil || *info.UserID != user.ID {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestResolveAPIKey_RejectsInactiveKey(t *testing.T) {
	apiStore := newMockAPIKeyStorage()
	userStore := newMockUserStorage()

	user := &models.User{ID: uuid.New(), Username: "u1", IsActive: true}
	userStore.put(user)
	makeKey(t, apiStore, "mdm_sk_revoked", &user.ID, false)

	r := NewResolver(apiStore, userStore)
	_, err := r.ResolveAPIKey(context.Background(), "mdm_sk_revoked")
	if !errors.Is(err, ErrAPIKeyInactive) {
		t.Fatalf("expected ErrAPIKeyInactive, got %v", err)
	}
}

func TestResolveAPIKey_RejectsKeyOwnedByInactiveUser(t *testing.T) {
	apiStore := newMockAPIKeyStorage()
	userStore := newMockUserStorage()

	user := &models.User{ID: uuid.New(), Username: "u1", IsActive: false}
	userStore.put(user)
	makeKey(t, apiStore, "mdm_sk_orphan", &user.ID, true)

	r := NewResolver(apiStore, userStore)
	_, err := r.ResolveAPIKey(context.Background(), "mdm_sk_orphan")
	if !errors.Is(err, ErrUserInactive) {
		t.Fatalf("expected ErrUserInactive, got %v", err)
	}
}

func TestResolveAPIKey_AllowsKeyWithNullUser(t *testing.T) {
	apiStore := newMockAPIKeyStorage()
	userStore := newMockUserStorage()

	makeKey(t, apiStore, "mdm_sk_legacy", nil, true)

	r := NewResolver(apiStore, userStore)
	info, err := r.ResolveAPIKey(context.Background(), "mdm_sk_legacy")
	if err != nil {
		t.Fatalf("expected success for null-user key, got %v", err)
	}
	if info == nil || info.UserID != nil {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestResolveAPIKey_NilUserStoreSkipsCheck(t *testing.T) {
	// Backward-compat: passing nil for users should preserve legacy behavior
	// where resolver does not know about user.is_active at all.
	apiStore := newMockAPIKeyStorage()

	user := &models.User{ID: uuid.New(), Username: "u1", IsActive: false}
	makeKey(t, apiStore, "mdm_sk_skipcheck", &user.ID, true)

	r := NewResolver(apiStore, nil)
	if _, err := r.ResolveAPIKey(context.Background(), "mdm_sk_skipcheck"); err != nil {
		t.Fatalf("expected success when users storage is nil, got %v", err)
	}
}

func TestResolveAPIKey_UnknownKeyReturnsErrInvalid(t *testing.T) {
	r := NewResolver(newMockAPIKeyStorage(), newMockUserStorage())
	_, err := r.ResolveAPIKey(context.Background(), "mdm_sk_unknown")
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestResolveAPIKey_EmptyKeyReturnsErrInvalid(t *testing.T) {
	r := NewResolver(newMockAPIKeyStorage(), newMockUserStorage())
	_, err := r.ResolveAPIKey(context.Background(), "")
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestResolveAPIKey_CachesValid(t *testing.T) {
	apiStore := newMockAPIKeyStorage()
	userStore := newMockUserStorage()
	user := &models.User{ID: uuid.New(), Username: "u1", IsActive: true}
	userStore.put(user)
	k := makeKey(t, apiStore, "mdm_sk_warm", &user.ID, true)

	r := NewResolver(apiStore, userStore)
	if _, err := r.ResolveAPIKey(context.Background(), "mdm_sk_warm"); err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Mutate underlying store; cache should still serve the original.
	delete(apiStore.keys, k.KeyHash)
	info, err := r.ResolveAPIKey(context.Background(), "mdm_sk_warm")
	if err != nil {
		t.Fatalf("expected cache hit, got %v", err)
	}
	if info == nil || info.ID != k.ID {
		t.Fatalf("unexpected cached info: %+v", info)
	}
}
