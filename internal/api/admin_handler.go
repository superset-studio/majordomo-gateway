package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/httputil"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/secrets"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

// AdminHandler provides REST API endpoints for the admin web UI.
type AdminHandler struct {
	apiKeys     storage.APIKeyStorage
	proxyKeys   storage.ProxyKeyStorage
	users       storage.UserStorage
	orgs        storage.OrganizationStorage
	secrets     secrets.SecretStore
	jwt         *auth.JWTService
	proxyKeySvc *ProxyKeyService
}

// NewAdminHandler creates a new admin API handler.
func NewAdminHandler(
	apiKeys storage.APIKeyStorage,
	proxyKeys storage.ProxyKeyStorage,
	users storage.UserStorage,
	orgs storage.OrganizationStorage,
	secretStore secrets.SecretStore,
	jwtSvc *auth.JWTService,
) *AdminHandler {
	return &AdminHandler{
		apiKeys:     apiKeys,
		proxyKeys:   proxyKeys,
		users:       users,
		orgs:        orgs,
		secrets:     secretStore,
		jwt:         jwtSvc,
		proxyKeySvc: NewProxyKeyService(proxyKeys, secretStore),
	}
}

// --- Login ---

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string              `json:"token"`
	User  *models.User        `json:"user"`
	Org   *models.Organization `json:"org,omitempty"`
}

// Login handles POST /api/v1/admin/login
func (h *AdminHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Username == "" || req.Password == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	user, err := h.users.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		slog.Error("failed to get user", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if user == nil || !user.IsActive {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if user.PasswordHash == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "this account uses OAuth sign-in")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.Password)); err != nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Resolve org membership for JWT
	var orgID *uuid.UUID
	var orgRole *string
	var org *models.Organization
	if h.orgs != nil {
		o, member, err := h.orgs.GetUserOrganization(r.Context(), user.ID)
		if err != nil {
			slog.Error("failed to get user organization", "error", err)
			httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if o != nil {
			orgID = &o.ID
			orgRole = &member.Role
			org = o
		}
	}

	token, err := h.jwt.GenerateToken(user.ID, user.Username, orgID, orgRole)
	if err != nil {
		slog.Error("failed to generate token", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, loginResponse{Token: token, User: user, Org: org})
}

// --- Me ---

// Me handles GET /api/v1/admin/me
func (h *AdminHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.users.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("failed to get user", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, user)
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
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "current_password and new_password are required")
		return
	}

	user, err := h.users.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("failed to get user", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if user.PasswordHash == nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "cannot change password for OAuth accounts")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "current password is incorrect")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 12)
	if err != nil {
		slog.Error("failed to hash password", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := h.users.UpdateUserPassword(r.Context(), claims.UserID, string(newHash)); err != nil {
		slog.Error("failed to update password", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- S3 Config ---

type updateS3ConfigRequest struct {
	Bucket         string `json:"bucket"`
	Region         string `json:"region"`
	Endpoint       string `json:"endpoint"`
	AccessKeyID    string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
}

type s3ConfigResponse struct {
	Bucket         string `json:"bucket"`
	Region         string `json:"region"`
	Endpoint       string `json:"endpoint"`
	CredentialsSet bool   `json:"credentialsSet"`
}

// GetS3Config handles GET /api/v1/admin/me/s3-config
func (h *AdminHandler) GetS3Config(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.users.GetUserS3Config(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("failed to get user S3 config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := s3ConfigResponse{
		CredentialsSet: user.S3AccessKeyIDEncrypted != nil && *user.S3AccessKeyIDEncrypted != "",
	}
	if user.S3Bucket != nil {
		resp.Bucket = *user.S3Bucket
	}
	if user.S3Region != nil {
		resp.Region = *user.S3Region
	}
	if user.S3Endpoint != nil {
		resp.Endpoint = *user.S3Endpoint
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// UpdateS3Config handles PUT /api/v1/admin/me/s3-config
func (h *AdminHandler) UpdateS3Config(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req updateS3ConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Bucket == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "bucket is required")
		return
	}
	if req.AccessKeyID == "" || req.SecretAccessKey == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "accessKeyId and secretAccessKey are required")
		return
	}

	if h.secrets == nil {
		httputil.WriteJSONError(w, http.StatusInternalServerError, "encryption not configured")
		return
	}

	region := req.Region
	if region == "" {
		region = "us-east-1"
	}

	encAccessKeyID, err := h.secrets.Encrypt(req.AccessKeyID)
	if err != nil {
		slog.Error("failed to encrypt S3 access key ID", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	encSecretAccessKey, err := h.secrets.Encrypt(req.SecretAccessKey)
	if err != nil {
		slog.Error("failed to encrypt S3 secret access key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Validate S3 connectivity before saving
	if err := storage.ValidateS3Config(r.Context(), &models.UserS3Config{
		Bucket:         req.Bucket,
		Region:         region,
		Endpoint:       req.Endpoint,
		AccessKeyID:    req.AccessKeyID,
		SecretAccessKey: req.SecretAccessKey,
	}); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "S3 validation failed: "+err.Error())
		return
	}

	if err := h.users.UpdateUserS3Config(r.Context(), claims.UserID, req.Bucket, region, req.Endpoint, encAccessKeyID, encSecretAccessKey); err != nil {
		slog.Error("failed to update user S3 config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := s3ConfigResponse{
		Bucket:         req.Bucket,
		Region:         region,
		Endpoint:       req.Endpoint,
		CredentialsSet: true,
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// DeleteS3Config handles DELETE /api/v1/admin/me/s3-config
func (h *AdminHandler) DeleteS3Config(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.users.ClearUserS3Config(r.Context(), claims.UserID); err != nil {
		slog.Error("failed to clear user S3 config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- GCS Config ---

type updateGCSConfigRequest struct {
	Bucket          string `json:"bucket"`
	CredentialsJSON string `json:"credentialsJSON"` // service account JSON; empty = ADC
}

type gcsConfigResponse struct {
	Bucket         string `json:"bucket"`
	CredentialsSet bool   `json:"credentialsSet"`
}

// GetGCSConfig handles GET /api/v1/admin/me/gcs-config
func (h *AdminHandler) GetGCSConfig(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.users.GetUserGCSConfig(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("failed to get user GCS config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := gcsConfigResponse{
		CredentialsSet: user.GCSCredentialsJSONEncrypted != nil && *user.GCSCredentialsJSONEncrypted != "",
	}
	if user.GCSBucket != nil {
		resp.Bucket = *user.GCSBucket
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// UpdateGCSConfig handles PUT /api/v1/admin/me/gcs-config
func (h *AdminHandler) UpdateGCSConfig(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req updateGCSConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Bucket == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "bucket is required")
		return
	}

	// Per-user GCS requires explicit credentials — ADC is not supported here
	// because it would validate against the server's identity, not the user's.
	if req.CredentialsJSON == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "credentialsJSON is required")
		return
	}

	if h.secrets == nil {
		httputil.WriteJSONError(w, http.StatusInternalServerError, "encryption not configured")
		return
	}

	// Validate that credentialsJSON is valid JSON
	if !json.Valid([]byte(req.CredentialsJSON)) {
		httputil.WriteJSONError(w, http.StatusBadRequest, "credentialsJSON must be valid JSON")
		return
	}

	// Validate GCS connectivity before saving
	gcsCfg := &models.UserGCSConfig{Bucket: req.Bucket}
	if req.CredentialsJSON != "" {
		gcsCfg.CredentialsJSON = []byte(req.CredentialsJSON)
	}
	if err := storage.ValidateGCSConfig(r.Context(), gcsCfg); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "GCS validation failed: "+err.Error())
		return
	}

	encCredentialsJSON := ""
	if req.CredentialsJSON != "" {
		var err error
		encCredentialsJSON, err = h.secrets.Encrypt(req.CredentialsJSON)
		if err != nil {
			slog.Error("failed to encrypt GCS credentials JSON", "error", err)
			httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	if err := h.users.UpdateUserGCSConfig(r.Context(), claims.UserID, req.Bucket, encCredentialsJSON); err != nil {
		slog.Error("failed to update user GCS config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := gcsConfigResponse{
		Bucket:         req.Bucket,
		CredentialsSet: req.CredentialsJSON != "",
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// DeleteGCSConfig handles DELETE /api/v1/admin/me/gcs-config
func (h *AdminHandler) DeleteGCSConfig(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.users.ClearUserGCSConfig(r.Context(), claims.UserID); err != nil {
		slog.Error("failed to clear user GCS config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var keys []*models.APIKey
	var err error
	if claims.OrgID != nil {
		keys, err = h.apiKeys.ListAPIKeysByOrgID(r.Context(), *claims.OrgID)
	} else {
		keys, err = h.apiKeys.ListAPIKeysByUserID(r.Context(), claims.UserID)
	}
	if err != nil {
		slog.Error("failed to list API keys", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, keys)
}

// CreateAPIKey handles POST /api/v1/admin/api-keys
func (h *AdminHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req adminCreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "name is required")
		return
	}

	plaintext, hash, err := auth.GenerateAPIKey()
	if err != nil {
		slog.Error("failed to generate API key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	userID := claims.UserID
	input := &models.CreateAPIKeyInput{
		Name:        req.Name,
		Description: req.Description,
		UserID:      &userID,
		OrgID:       claims.OrgID,
	}

	key, err := h.apiKeys.CreateAPIKey(r.Context(), hash, input)
	if err != nil {
		slog.Error("failed to create API key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, adminCreateAPIKeyResponse{APIKey: key, Key: plaintext})
}

// GetAPIKey handles GET /api/v1/admin/api-keys/{id}
func (h *AdminHandler) GetAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	key, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	httputil.WriteJSON(w, http.StatusOK, key)
}

// UpdateAPIKey handles PUT /api/v1/admin/api-keys/{id}
func (h *AdminHandler) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
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
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	input := &models.UpdateAPIKeyInput{
		Name:        req.Name,
		Description: req.Description,
	}

	updated, err := h.apiKeys.UpdateAPIKey(r.Context(), apiKey.ID, input)
	if err != nil {
		slog.Error("failed to update API key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, updated)
}

// RevokeAPIKey handles DELETE /api/v1/admin/api-keys/{id}
func (h *AdminHandler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	apiKey, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	if err := h.apiKeys.RevokeAPIKey(r.Context(), apiKey.ID); err != nil {
		slog.Error("failed to revoke API key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// --- Proxy Keys (nested under API keys) ---

// ListProxyKeys handles GET /api/v1/admin/api-keys/{id}/proxy-keys
func (h *AdminHandler) ListProxyKeys(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	apiKey, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	keys, err := h.proxyKeySvc.ListProxyKeys(r.Context(), apiKey.ID)
	if err != nil {
		slog.Error("failed to list proxy keys", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, keys)
}

// CreateProxyKey handles POST /api/v1/admin/api-keys/{id}/proxy-keys
func (h *AdminHandler) CreateProxyKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
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
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "name is required")
		return
	}

	pk, plaintext, err := h.proxyKeySvc.CreateProxyKey(r.Context(), apiKey.ID, req.Name, req.Description)
	if err != nil {
		slog.Error("failed to create proxy key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := struct {
		*models.ProxyKey
		Key string `json:"key"`
	}{ProxyKey: pk, Key: plaintext}

	httputil.WriteJSON(w, http.StatusCreated, resp)
}

// GetProxyKey handles GET /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}
func (h *AdminHandler) GetProxyKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	httputil.WriteJSON(w, http.StatusOK, pk)
}

// RevokeProxyKey handles DELETE /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}
func (h *AdminHandler) RevokeProxyKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	if err := h.proxyKeySvc.RevokeProxyKey(r.Context(), pk.ID, pk.MajordomoAPIKeyID); err != nil {
		slog.Error("failed to revoke proxy key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// --- Provider Mappings (nested under proxy keys) ---

// ListProviderMappings handles GET /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}/providers
func (h *AdminHandler) ListProviderMappings(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	resp, err := h.proxyKeySvc.ListProviderMappings(r.Context(), pk.ID, pk.MajordomoAPIKeyID)
	if err != nil {
		slog.Error("failed to list provider mappings", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// SetProviderMapping handles PUT /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}/providers/{provider}
func (h *AdminHandler) SetProviderMapping(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	providerName := chi.URLParam(r, "provider")
	if providerName == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "provider is required")
		return
	}

	var req setProviderMappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.APIKey == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "api_key is required")
		return
	}

	if err := h.proxyKeySvc.SetProviderMapping(r.Context(), pk.ID, pk.MajordomoAPIKeyID, providerName, req.APIKey); err != nil {
		slog.Error("failed to set provider mapping", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "provider": providerName})
}

// DeleteProviderMapping handles DELETE /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}/providers/{provider}
func (h *AdminHandler) DeleteProviderMapping(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	providerName := chi.URLParam(r, "provider")
	if providerName == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "provider is required")
		return
	}

	if err := h.proxyKeySvc.DeleteProviderMapping(r.Context(), pk.ID, pk.MajordomoAPIKeyID, providerName); err != nil {
		if err == storage.ErrProviderMappingNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "provider mapping not found")
			return
		}
		slog.Error("failed to delete provider mapping", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Ownership verification helpers ---

// verifyAPIKeyOwnership parses the {id} URL param, fetches the API key, and verifies
// it belongs to the authenticated user. Returns the key and true on success.
func (h *AdminHandler) verifyAPIKeyOwnership(w http.ResponseWriter, r *http.Request, claims *auth.JWTClaims) (*models.APIKey, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid API key ID")
		return nil, false
	}

	key, err := h.apiKeys.GetAPIKeyByID(r.Context(), id)
	if err != nil {
		if err == storage.ErrAPIKeyNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "API key not found")
			return nil, false
		}
		slog.Error("failed to get API key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}

	// Org-scoped: verify key belongs to the same org
	if claims.OrgID != nil {
		if key.OrgID == nil || *key.OrgID != *claims.OrgID {
			httputil.WriteJSONError(w, http.StatusNotFound, "API key not found")
			return nil, false
		}
	} else {
		// Personal: verify key belongs to the user
		if key.UserID == nil || *key.UserID != claims.UserID {
			httputil.WriteJSONError(w, http.StatusNotFound, "API key not found")
			return nil, false
		}
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
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid proxy key ID")
		return nil, nil, false
	}

	pk, err := h.proxyKeys.GetProxyKeyByID(r.Context(), pkID)
	if err != nil {
		if err == storage.ErrProxyKeyNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "proxy key not found")
			return nil, nil, false
		}
		slog.Error("failed to get proxy key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return nil, nil, false
	}

	if pk.MajordomoAPIKeyID != apiKey.ID {
		httputil.WriteJSONError(w, http.StatusNotFound, "proxy key not found")
		return nil, nil, false
	}

	return apiKey, pk, true
}
