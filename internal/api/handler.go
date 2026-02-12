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
)

// Handler provides REST API endpoints for proxy key management.
type Handler struct {
	storage storage.ProxyKeyStorage
	secrets secrets.SecretStore
}

// NewHandler creates a new API handler.
func NewHandler(store storage.ProxyKeyStorage, secretStore secrets.SecretStore) *Handler {
	return &Handler{
		storage: store,
		secrets: secretStore,
	}
}

type createProxyKeyRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type createProxyKeyResponse struct {
	*models.ProxyKey
	Key string `json:"key"` // Plaintext key, shown once
}

// CreateProxyKey handles POST /api/v1/proxy-keys
func (h *Handler) CreateProxyKey(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req createProxyKeyRequest
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

	pk, err := h.storage.CreateProxyKey(r.Context(), hash, info.ID, input)
	if err != nil {
		slog.Error("failed to create proxy key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := createProxyKeyResponse{
		ProxyKey: pk,
		Key:      plaintext,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// ListProxyKeys handles GET /api/v1/proxy-keys
func (h *Handler) ListProxyKeys(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	keys, err := h.storage.ListProxyKeys(r.Context(), info.ID)
	if err != nil {
		slog.Error("failed to list proxy keys", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

// GetProxyKey handles GET /api/v1/proxy-keys/{id}
func (h *Handler) GetProxyKey(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid proxy key ID", http.StatusBadRequest)
		return
	}

	pk, err := h.storage.GetProxyKeyByID(r.Context(), id)
	if err != nil {
		if err == storage.ErrProxyKeyNotFound {
			http.Error(w, "proxy key not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to get proxy key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if pk.MajordomoAPIKeyID != info.ID {
		http.Error(w, "proxy key not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pk)
}

// RevokeProxyKey handles DELETE /api/v1/proxy-keys/{id}
func (h *Handler) RevokeProxyKey(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid proxy key ID", http.StatusBadRequest)
		return
	}

	// Verify ownership
	pk, err := h.storage.GetProxyKeyByID(r.Context(), id)
	if err != nil {
		if err == storage.ErrProxyKeyNotFound {
			http.Error(w, "proxy key not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to get proxy key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if pk.MajordomoAPIKeyID != info.ID {
		http.Error(w, "proxy key not found", http.StatusNotFound)
		return
	}

	if err := h.storage.RevokeProxyKey(r.Context(), id); err != nil {
		slog.Error("failed to revoke proxy key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
}

type setProviderMappingRequest struct {
	APIKey string `json:"api_key"`
}

// SetProviderMapping handles PUT /api/v1/proxy-keys/{id}/providers/{provider}
func (h *Handler) SetProviderMapping(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid proxy key ID", http.StatusBadRequest)
		return
	}

	providerName := chi.URLParam(r, "provider")
	if providerName == "" {
		http.Error(w, "provider is required", http.StatusBadRequest)
		return
	}

	// Verify ownership
	pk, err := h.storage.GetProxyKeyByID(r.Context(), id)
	if err != nil {
		if err == storage.ErrProxyKeyNotFound {
			http.Error(w, "proxy key not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to get proxy key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if pk.MajordomoAPIKeyID != info.ID {
		http.Error(w, "proxy key not found", http.StatusNotFound)
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

	if err := h.storage.SetProviderMapping(r.Context(), id, providerName, encrypted); err != nil {
		slog.Error("failed to set provider mapping", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "provider": providerName})
}

// DeleteProviderMapping handles DELETE /api/v1/proxy-keys/{id}/providers/{provider}
func (h *Handler) DeleteProviderMapping(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid proxy key ID", http.StatusBadRequest)
		return
	}

	providerName := chi.URLParam(r, "provider")

	// Verify ownership
	pk, err := h.storage.GetProxyKeyByID(r.Context(), id)
	if err != nil {
		if err == storage.ErrProxyKeyNotFound {
			http.Error(w, "proxy key not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to get proxy key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if pk.MajordomoAPIKeyID != info.ID {
		http.Error(w, "proxy key not found", http.StatusNotFound)
		return
	}

	if err := h.storage.DeleteProviderMapping(r.Context(), id, providerName); err != nil {
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

type providerMappingResponse struct {
	ID         uuid.UUID `json:"id"`
	ProxyKeyID uuid.UUID `json:"proxy_key_id"`
	Provider   string    `json:"provider"`
	CreatedAt  string    `json:"created_at"`
	UpdatedAt  string    `json:"updated_at"`
}

// ListProviderMappings handles GET /api/v1/proxy-keys/{id}/providers
func (h *Handler) ListProviderMappings(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid proxy key ID", http.StatusBadRequest)
		return
	}

	// Verify ownership
	pk, err := h.storage.GetProxyKeyByID(r.Context(), id)
	if err != nil {
		if err == storage.ErrProxyKeyNotFound {
			http.Error(w, "proxy key not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to get proxy key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if pk.MajordomoAPIKeyID != info.ID {
		http.Error(w, "proxy key not found", http.StatusNotFound)
		return
	}

	mappings, err := h.storage.ListProviderMappings(r.Context(), id)
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
