package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

// adminTestStorage stitches together the minimum a handler test needs.
// Methods unused by the handler under test panic to surface accidental coupling.
type adminUserStore struct {
	usersByID       map[uuid.UUID]*models.User
	usersByUsername map[string]*models.User
	usersByEmail    map[string]*models.User
}

func newAdminUserStore() *adminUserStore {
	return &adminUserStore{
		usersByID:       make(map[uuid.UUID]*models.User),
		usersByUsername: make(map[string]*models.User),
		usersByEmail:    make(map[string]*models.User),
	}
}

func (s *adminUserStore) put(u *models.User) {
	s.usersByID[u.ID] = u
	s.usersByUsername[u.Username] = u
	if u.Email != nil {
		s.usersByEmail[*u.Email] = u
	}
}

func (s *adminUserStore) GetUserByID(_ context.Context, id uuid.UUID) (*models.User, error) {
	if u, ok := s.usersByID[id]; ok {
		return u, nil
	}
	return nil, nil
}
func (s *adminUserStore) GetUserByUsername(_ context.Context, name string) (*models.User, error) {
	if u, ok := s.usersByUsername[name]; ok {
		return u, nil
	}
	return nil, nil
}
func (s *adminUserStore) GetUserByEmail(_ context.Context, email string) (*models.User, error) {
	if u, ok := s.usersByEmail[email]; ok {
		return u, nil
	}
	return nil, nil
}
func (s *adminUserStore) CreateUser(_ context.Context, input *models.CreateUserInput) (*models.User, error) {
	u := &models.User{
		ID:        uuid.New(),
		Username:  input.Username,
		IsActive:  true,
		CreatedAt: time.Now(),
	}
	if input.Email != "" {
		e := input.Email
		u.Email = &e
	}
	if input.Password == "" {
		u.PasswordHash = nil
	} else {
		// Pretend-hash; no real bcrypt in tests.
		hash := "hashed:" + input.Password
		u.PasswordHash = &hash
	}
	s.put(u)
	return u, nil
}

// Unused-by-tests stubs.
func (s *adminUserStore) GetUserByAuthProvider(context.Context, string, string) (*models.User, error) {
	return nil, nil
}
func (s *adminUserStore) CreateOAuthUser(context.Context, *models.CreateOAuthUserInput) (*models.User, error) {
	return nil, nil
}
func (s *adminUserStore) ListUsers(context.Context) ([]*models.User, error) { return nil, nil }
func (s *adminUserStore) UpdateUserPassword(context.Context, uuid.UUID, string) error {
	return nil
}
func (s *adminUserStore) UpdateUserS3Config(context.Context, uuid.UUID, string, string, string, string, string) error {
	return nil
}
func (s *adminUserStore) ClearUserS3Config(context.Context, uuid.UUID) error { return nil }
func (s *adminUserStore) GetUserS3Config(context.Context, uuid.UUID) (*models.User, error) {
	return nil, nil
}
func (s *adminUserStore) UpdateUserCloudStorageConfig(context.Context, uuid.UUID, string, string, string, string, string, string, string, string, string) error {
	return nil
}
func (s *adminUserStore) ClearUserCloudStorageConfig(context.Context, uuid.UUID) error {
	return nil
}
func (s *adminUserStore) GetUserCloudStorageConfig(context.Context, uuid.UUID) (*models.User, error) {
	return nil, nil
}
func (s *adminUserStore) MarkUserEmailVerified(context.Context, uuid.UUID) error { return nil }

// Helper to build a request with JWT claims attached.
func reqWithClaims(method, target string, body []byte, claims *auth.JWTClaims) *http.Request {
	r := httptest.NewRequest(method, target, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if claims != nil {
		ctx := context.WithValue(r.Context(), jwtUserInfoKey, claims)
		r = r.WithContext(ctx)
	}
	return r
}

// --- CreateUserAdmin tests ----------------------------------------------

func TestCreateUserAdmin_HappyPath(t *testing.T) {
	users := newAdminUserStore()
	h := &AdminHandler{users: users}

	body, _ := json.Marshal(map[string]any{"username": "customer-1"})
	r := reqWithClaims(http.MethodPost, "/admin/users", body, &auth.JWTClaims{UserID: uuid.New(), Username: "aiagents"})
	w := httptest.NewRecorder()

	h.CreateUserAdmin(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body=%s)", w.Code, w.Body.String())
	}
	var got models.User
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Username != "customer-1" {
		t.Fatalf("unexpected username %q", got.Username)
	}
	if got.PasswordHash != nil {
		t.Fatalf("expected null password_hash for machine-managed user, got %v", *got.PasswordHash)
	}
}

func TestCreateUserAdmin_RejectsMissingUsername(t *testing.T) {
	h := &AdminHandler{users: newAdminUserStore()}

	body, _ := json.Marshal(map[string]any{})
	r := reqWithClaims(http.MethodPost, "/admin/users", body, &auth.JWTClaims{Username: "aiagents"})
	w := httptest.NewRecorder()

	h.CreateUserAdmin(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateUserAdmin_RejectsDuplicateUsername(t *testing.T) {
	users := newAdminUserStore()
	users.put(&models.User{ID: uuid.New(), Username: "customer-1", IsActive: true})
	h := &AdminHandler{users: users}

	body, _ := json.Marshal(map[string]any{"username": "customer-1"})
	r := reqWithClaims(http.MethodPost, "/admin/users", body, &auth.JWTClaims{Username: "aiagents"})
	w := httptest.NewRecorder()

	h.CreateUserAdmin(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestCreateUserAdmin_RejectsDuplicateEmail(t *testing.T) {
	users := newAdminUserStore()
	existingEmail := "taken@example.com"
	users.put(&models.User{ID: uuid.New(), Username: "u", Email: &existingEmail, IsActive: true})
	h := &AdminHandler{users: users}

	body, _ := json.Marshal(map[string]any{"username": "different", "email": existingEmail})
	r := reqWithClaims(http.MethodPost, "/admin/users", body, &auth.JWTClaims{Username: "aiagents"})
	w := httptest.NewRecorder()

	h.CreateUserAdmin(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestCreateUserAdmin_AllowsNilEmail(t *testing.T) {
	h := &AdminHandler{users: newAdminUserStore()}

	body, _ := json.Marshal(map[string]any{"username": "no-email-user"})
	r := reqWithClaims(http.MethodPost, "/admin/users", body, &auth.JWTClaims{Username: "aiagents"})
	w := httptest.NewRecorder()

	h.CreateUserAdmin(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

// --- AdminOnlyMiddleware tests ------------------------------------------

func TestAdminOnlyMiddleware_AllowsListedUsername(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	h := AdminOnlyMiddleware([]string{"aiagents"})(next)

	r := reqWithClaims(http.MethodPost, "/x", nil, &auth.JWTClaims{Username: "aiagents"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if !called {
		t.Fatalf("expected next handler to be called")
	}
	if w.Code != http.StatusNoContent {
		t.Fatalf("unexpected status %d", w.Code)
	}
}

func TestAdminOnlyMiddleware_RejectsUnlistedUsername(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatalf("next should not be called")
	})
	h := AdminOnlyMiddleware([]string{"aiagents"})(next)

	r := reqWithClaims(http.MethodPost, "/x", nil, &auth.JWTClaims{Username: "random-user"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestAdminOnlyMiddleware_RejectsMissingClaims(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatalf("next should not be called")
	})
	h := AdminOnlyMiddleware([]string{"aiagents"})(next)

	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAdminOnlyMiddleware_DisabledWhenAllowlistEmpty(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatalf("next should not be called when allowlist is empty")
	})
	h := AdminOnlyMiddleware(nil)(next)

	r := reqWithClaims(http.MethodPost, "/x", nil, &auth.JWTClaims{Username: "aiagents"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (disabled), got %d", w.Code)
	}
}

// --- CreateAPIKeyForUser tests ------------------------------------------

// Reuse the auth-package mock api-key storage shape, locally redefined to
// avoid an import cycle (api -> auth would loop).
type adminAPIKeyStore struct {
	keys map[string]*models.APIKey
}

func newAdminAPIKeyStore() *adminAPIKeyStore {
	return &adminAPIKeyStore{keys: make(map[string]*models.APIKey)}
}

func (s *adminAPIKeyStore) CreateAPIKey(_ context.Context, hash string, input *models.CreateAPIKeyInput) (*models.APIKey, error) {
	k := &models.APIKey{
		ID:        uuid.New(),
		KeyHash:   hash,
		Name:      input.Name,
		UserID:    input.UserID,
		OrgID:     input.OrgID,
		IsActive:  true,
		CreatedAt: time.Now(),
	}
	s.keys[hash] = k
	return k, nil
}
func (s *adminAPIKeyStore) GetAPIKeyByHash(_ context.Context, hash string) (*models.APIKey, error) {
	if k, ok := s.keys[hash]; ok {
		return k, nil
	}
	return nil, nil
}
func (s *adminAPIKeyStore) GetAPIKeyByID(context.Context, uuid.UUID) (*models.APIKey, error) {
	return nil, nil
}
func (s *adminAPIKeyStore) ListAPIKeys(context.Context) ([]*models.APIKey, error) {
	return nil, nil
}
func (s *adminAPIKeyStore) UpdateAPIKey(context.Context, uuid.UUID, *models.UpdateAPIKeyInput) (*models.APIKey, error) {
	return nil, nil
}
func (s *adminAPIKeyStore) RevokeAPIKey(context.Context, uuid.UUID) error { return nil }
func (s *adminAPIKeyStore) UpdateAPIKeyLastUsed(context.Context, uuid.UUID) error {
	return nil
}
func (s *adminAPIKeyStore) ListAPIKeysByUserID(context.Context, uuid.UUID) ([]*models.APIKey, error) {
	return nil, nil
}
func (s *adminAPIKeyStore) ListAPIKeysByOrgID(context.Context, uuid.UUID) ([]*models.APIKey, error) {
	return nil, nil
}

// chiContextWithUserID embeds a {userID} URL param so chi.URLParam can read it.
func chiContextWithUserID(r *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("userID", id)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestCreateAPIKeyForUser_HappyPath(t *testing.T) {
	users := newAdminUserStore()
	target := &models.User{ID: uuid.New(), Username: "customer-1", IsActive: true}
	users.put(target)

	apiKeys := newAdminAPIKeyStore()
	h := &AdminHandler{users: users, apiKeys: apiKeys}

	body, _ := json.Marshal(map[string]any{"name": "customer-1-key"})
	r := reqWithClaims(http.MethodPost, "/admin/users/"+target.ID.String()+"/api-keys", body, &auth.JWTClaims{Username: "aiagents"})
	r = chiContextWithUserID(r, target.ID.String())
	w := httptest.NewRecorder()

	h.CreateAPIKeyForUser(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp adminCreateAPIKeyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Key == "" {
		t.Fatalf("expected plaintext key in response")
	}
	if resp.APIKey == nil || resp.APIKey.UserID == nil || *resp.APIKey.UserID != target.ID {
		t.Fatalf("expected key attributed to %s, got %+v", target.ID, resp.APIKey)
	}
}

func TestCreateAPIKeyForUser_InvalidUserID(t *testing.T) {
	h := &AdminHandler{users: newAdminUserStore(), apiKeys: newAdminAPIKeyStore()}

	body, _ := json.Marshal(map[string]any{"name": "k"})
	r := reqWithClaims(http.MethodPost, "/admin/users/not-a-uuid/api-keys", body, &auth.JWTClaims{Username: "aiagents"})
	r = chiContextWithUserID(r, "not-a-uuid")
	w := httptest.NewRecorder()

	h.CreateAPIKeyForUser(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateAPIKeyForUser_UnknownUser(t *testing.T) {
	h := &AdminHandler{users: newAdminUserStore(), apiKeys: newAdminAPIKeyStore()}

	someID := uuid.New().String()
	body, _ := json.Marshal(map[string]any{"name": "k"})
	r := reqWithClaims(http.MethodPost, "/admin/users/"+someID+"/api-keys", body, &auth.JWTClaims{Username: "aiagents"})
	r = chiContextWithUserID(r, someID)
	w := httptest.NewRecorder()

	h.CreateAPIKeyForUser(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCreateAPIKeyForUser_RejectsMissingName(t *testing.T) {
	users := newAdminUserStore()
	target := &models.User{ID: uuid.New(), Username: "customer-1", IsActive: true}
	users.put(target)
	h := &AdminHandler{users: users, apiKeys: newAdminAPIKeyStore()}

	body, _ := json.Marshal(map[string]any{})
	r := reqWithClaims(http.MethodPost, "/admin/users/"+target.ID.String()+"/api-keys", body, &auth.JWTClaims{Username: "aiagents"})
	r = chiContextWithUserID(r, target.ID.String())
	w := httptest.NewRecorder()

	h.CreateAPIKeyForUser(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
