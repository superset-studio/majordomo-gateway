package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/secrets"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

// AdminHandler provides REST API endpoints for the admin web UI.
type AdminHandler struct {
	apiKeys   storage.APIKeyStorage
	proxyKeys storage.ProxyKeyStorage
	users     storage.UserStorage
	secrets   secrets.SecretStore
	jwt       *auth.JWTService
}

// NewAdminHandler creates a new admin API handler.
func NewAdminHandler(
	apiKeys storage.APIKeyStorage,
	proxyKeys storage.ProxyKeyStorage,
	users storage.UserStorage,
	secretStore secrets.SecretStore,
	jwtSvc *auth.JWTService,
) *AdminHandler {
	return &AdminHandler{
		apiKeys:   apiKeys,
		proxyKeys: proxyKeys,
		users:     users,
		secrets:   secretStore,
		jwt:       jwtSvc,
	}
}

// --- Login ---

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string       `json:"token"`
	User  *models.User `json:"user"`
}

// Login handles POST /api/v1/admin/login
func (h *AdminHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" {
		http.Error(w, "username and password are required", http.StatusBadRequest)
		return
	}

	user, err := h.users.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		slog.Error("failed to get user", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if user == nil || !user.IsActive {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := h.jwt.GenerateToken(user.ID, user.Username)
	if err != nil {
		slog.Error("failed to generate token", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(loginResponse{Token: token, User: user})
}

// --- Me ---

// Me handles GET /api/v1/admin/me
func (h *AdminHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := h.users.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("failed to get user", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

// --- Change Password ---

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePassword handles PUT /api/v1/admin/me/password
func (h *AdminHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		http.Error(w, "current_password and new_password are required", http.StatusBadRequest)
		return
	}

	user, err := h.users.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("failed to get user", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		http.Error(w, "current password is incorrect", http.StatusBadRequest)
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 12)
	if err != nil {
		slog.Error("failed to hash password", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := h.users.UpdateUserPassword(r.Context(), claims.UserID, string(newHash)); err != nil {
		slog.Error("failed to update password", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// --- API Keys ---

type adminCreateAPIKeyRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type adminCreateAPIKeyResponse struct {
	*models.APIKey
	Key string `json:"key"`
}

// ListAPIKeys handles GET /api/v1/admin/api-keys
func (h *AdminHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	keys, err := h.apiKeys.ListAPIKeysByUserID(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("failed to list API keys", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

// CreateAPIKey handles POST /api/v1/admin/api-keys
func (h *AdminHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req adminCreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	plaintext, hash, err := auth.GenerateAPIKey()
	if err != nil {
		slog.Error("failed to generate API key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	userID := claims.UserID
	input := &models.CreateAPIKeyInput{
		Name:        req.Name,
		Description: req.Description,
		UserID:      &userID,
	}

	key, err := h.apiKeys.CreateAPIKey(r.Context(), hash, input)
	if err != nil {
		slog.Error("failed to create API key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(adminCreateAPIKeyResponse{APIKey: key, Key: plaintext})
}

// GetAPIKey handles GET /api/v1/admin/api-keys/{id}
func (h *AdminHandler) GetAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	key, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(key)
}

// UpdateAPIKey handles PUT /api/v1/admin/api-keys/{id}
func (h *AdminHandler) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	apiKey, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	var req struct {
		Name        *string `json:"name,omitempty"`
		Description *string `json:"description,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	input := &models.UpdateAPIKeyInput{
		Name:        req.Name,
		Description: req.Description,
	}

	updated, err := h.apiKeys.UpdateAPIKey(r.Context(), apiKey.ID, input)
	if err != nil {
		slog.Error("failed to update API key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

// RevokeAPIKey handles DELETE /api/v1/admin/api-keys/{id}
func (h *AdminHandler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	apiKey, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	if err := h.apiKeys.RevokeAPIKey(r.Context(), apiKey.ID); err != nil {
		slog.Error("failed to revoke API key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
}

// --- Proxy Keys (nested under API keys) ---

// ListProxyKeys handles GET /api/v1/admin/api-keys/{id}/proxy-keys
func (h *AdminHandler) ListProxyKeys(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	apiKey, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	keys, err := h.proxyKeys.ListProxyKeys(r.Context(), apiKey.ID)
	if err != nil {
		slog.Error("failed to list proxy keys", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

// CreateProxyKey handles POST /api/v1/admin/api-keys/{id}/proxy-keys
func (h *AdminHandler) CreateProxyKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	apiKey, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	var req struct {
		Name        string  `json:"name"`
		Description *string `json:"description,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	plaintext, hash, err := auth.GenerateProxyKey()
	if err != nil {
		slog.Error("failed to generate proxy key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	input := &models.CreateProxyKeyInput{
		Name:        req.Name,
		Description: req.Description,
	}

	pk, err := h.proxyKeys.CreateProxyKey(r.Context(), hash, apiKey.ID, input)
	if err != nil {
		slog.Error("failed to create proxy key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := struct {
		*models.ProxyKey
		Key string `json:"key"`
	}{ProxyKey: pk, Key: plaintext}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// GetProxyKey handles GET /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}
func (h *AdminHandler) GetProxyKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pk)
}

// RevokeProxyKey handles DELETE /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}
func (h *AdminHandler) RevokeProxyKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	if err := h.proxyKeys.RevokeProxyKey(r.Context(), pk.ID); err != nil {
		slog.Error("failed to revoke proxy key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
}

// --- Provider Mappings (nested under proxy keys) ---

// ListProviderMappings handles GET /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}/providers
func (h *AdminHandler) ListProviderMappings(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	mappings, err := h.proxyKeys.ListProviderMappings(r.Context(), pk.ID)
	if err != nil {
		slog.Error("failed to list provider mappings", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Never return encrypted keys
	var resp []providerMappingResponse
	for _, m := range mappings {
		resp = append(resp, providerMappingResponse{
			ID:         m.ID,
			ProxyKeyID: m.ProxyKeyID,
			Provider:   m.Provider,
			CreatedAt:  m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt:  m.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// SetProviderMapping handles PUT /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}/providers/{provider}
func (h *AdminHandler) SetProviderMapping(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	providerName := chi.URLParam(r, "provider")
	if providerName == "" {
		http.Error(w, "provider is required", http.StatusBadRequest)
		return
	}

	var req setProviderMappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.APIKey == "" {
		http.Error(w, "api_key is required", http.StatusBadRequest)
		return
	}

	encrypted, err := h.secrets.Encrypt(req.APIKey)
	if err != nil {
		slog.Error("failed to encrypt provider key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := h.proxyKeys.SetProviderMapping(r.Context(), pk.ID, providerName, encrypted); err != nil {
		slog.Error("failed to set provider mapping", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "provider": providerName})
}

// DeleteProviderMapping handles DELETE /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}/providers/{provider}
func (h *AdminHandler) DeleteProviderMapping(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	providerName := chi.URLParam(r, "provider")
	if providerName == "" {
		http.Error(w, "provider is required", http.StatusBadRequest)
		return
	}

	if err := h.proxyKeys.DeleteProviderMapping(r.Context(), pk.ID, providerName); err != nil {
		if err == storage.ErrProviderMappingNotFound {
			http.Error(w, "provider mapping not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to delete provider mapping", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// --- Ownership verification helpers ---

// verifyAPIKeyOwnership parses the {id} URL param, fetches the API key, and verifies
// it belongs to the authenticated user. Returns the key and true on success.
func (h *AdminHandler) verifyAPIKeyOwnership(w http.ResponseWriter, r *http.Request, claims *auth.JWTClaims) (*models.APIKey, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid API key ID", http.StatusBadRequest)
		return nil, false
	}

	key, err := h.apiKeys.GetAPIKeyByID(r.Context(), id)
	if err != nil {
		if err == storage.ErrAPIKeyNotFound {
			http.Error(w, "API key not found", http.StatusNotFound)
			return nil, false
		}
		slog.Error("failed to get API key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}

	if key.UserID == nil || *key.UserID != claims.UserID {
		http.Error(w, "API key not found", http.StatusNotFound)
		return nil, false
	}

	return key, true
}

// verifyProxyKeyOwnership verifies both the API key and proxy key ownership chain.
func (h *AdminHandler) verifyProxyKeyOwnership(w http.ResponseWriter, r *http.Request, claims *auth.JWTClaims) (*models.APIKey, *models.ProxyKey, bool) {
	apiKey, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return nil, nil, false
	}

	pkID, err := uuid.Parse(chi.URLParam(r, "pkId"))
	if err != nil {
		http.Error(w, "invalid proxy key ID", http.StatusBadRequest)
		return nil, nil, false
	}

	pk, err := h.proxyKeys.GetProxyKeyByID(r.Context(), pkID)
	if err != nil {
		if err == storage.ErrProxyKeyNotFound {
			http.Error(w, "proxy key not found", http.StatusNotFound)
			return nil, nil, false
		}
		slog.Error("failed to get proxy key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, nil, false
	}

	if pk.MajordomoAPIKeyID != apiKey.ID {
		http.Error(w, "proxy key not found", http.StatusNotFound)
		return nil, nil, false
	}

	return apiKey, pk, true
}
