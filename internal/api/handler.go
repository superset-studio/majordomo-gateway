package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/httputil"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/secrets"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// Handler provides REST API endpoints for proxy key management.
type Handler struct {
	proxyKeySvc *ProxyKeyService
}

// NewHandler creates a new API handler.
func NewHandler(store storage.ProxyKeyStorage, secretStore secrets.SecretStore) *Handler {
	return &Handler{
		proxyKeySvc: NewProxyKeyService(store, secretStore),
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
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createProxyKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "name is required")
		return
	}

	pk, plaintext, err := h.proxyKeySvc.CreateProxyKey(r.Context(), info.ID, req.Name, req.Description)
	if err != nil {
		slog.Error("failed to create proxy key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, createProxyKeyResponse{ProxyKey: pk, Key: plaintext})
}

// ListProxyKeys handles GET /api/v1/proxy-keys
func (h *Handler) ListProxyKeys(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	keys, err := h.proxyKeySvc.ListProxyKeys(r.Context(), info.ID)
	if err != nil {
		slog.Error("failed to list proxy keys", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, keys)
}

// GetProxyKey handles GET /api/v1/proxy-keys/{id}
func (h *Handler) GetProxyKey(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid proxy key ID")
		return
	}

	pk, err := h.proxyKeySvc.GetProxyKey(r.Context(), id, info.ID)
	if err != nil {
		if err == storage.ErrProxyKeyNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "proxy key not found")
			return
		}
		slog.Error("failed to get proxy key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, pk)
}

// RevokeProxyKey handles DELETE /api/v1/proxy-keys/{id}
func (h *Handler) RevokeProxyKey(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid proxy key ID")
		return
	}

	if err := h.proxyKeySvc.RevokeProxyKey(r.Context(), id, info.ID); err != nil {
		if err == storage.ErrProxyKeyNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "proxy key not found")
			return
		}
		slog.Error("failed to revoke proxy key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// SetProviderMapping handles PUT /api/v1/proxy-keys/{id}/providers/{provider}
func (h *Handler) SetProviderMapping(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid proxy key ID")
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

	if err := h.proxyKeySvc.SetProviderMapping(r.Context(), id, info.ID, providerName, req.APIKey); err != nil {
		if err == storage.ErrProxyKeyNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "proxy key not found")
			return
		}
		slog.Error("failed to set provider mapping", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "provider": providerName})
}

// DeleteProviderMapping handles DELETE /api/v1/proxy-keys/{id}/providers/{provider}
func (h *Handler) DeleteProviderMapping(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid proxy key ID")
		return
	}

	providerName := chi.URLParam(r, "provider")

	if err := h.proxyKeySvc.DeleteProviderMapping(r.Context(), id, info.ID, providerName); err != nil {
		if err == storage.ErrProxyKeyNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "proxy key not found")
			return
		}
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

// ListProviderMappings handles GET /api/v1/proxy-keys/{id}/providers
func (h *Handler) ListProviderMappings(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid proxy key ID")
		return
	}

	resp, err := h.proxyKeySvc.ListProviderMappings(r.Context(), id, info.ID)
	if err != nil {
		if err == storage.ErrProxyKeyNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "proxy key not found")
			return
		}
		slog.Error("failed to list provider mappings", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}
