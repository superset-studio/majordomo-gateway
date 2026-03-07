package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/httputil"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// MetadataHandler provides REST API endpoints for metadata key management.
type MetadataHandler struct {
	metadata storage.MetadataKeyStorage
	apiKeys  storage.APIKeyStorage
}

// NewMetadataHandler creates a new metadata key management handler.
func NewMetadataHandler(metadata storage.MetadataKeyStorage, apiKeys storage.APIKeyStorage) *MetadataHandler {
	return &MetadataHandler{
		metadata: metadata,
		apiKeys:  apiKeys,
	}
}

// ListMetadataKeys handles GET /api/v1/admin/metadata-keys
func (h *MetadataHandler) ListMetadataKeys(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	keys, err := h.metadata.ListMetadataKeys(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("failed to list metadata keys", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, keys)
}

type updateMetadataKeyRequest struct {
	IsActive    *bool   `json:"is_active"`
	DisplayName *string `json:"display_name"`
}

// UpdateMetadataKey handles PUT /api/v1/admin/api-keys/{id}/metadata-keys/{keyName}
func (h *MetadataHandler) UpdateMetadataKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	apiKeyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid API key ID")
		return
	}

	keyName := chi.URLParam(r, "keyName")
	if keyName == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "missing key name")
		return
	}

	// Verify API key ownership
	key, err := h.apiKeys.GetAPIKeyByID(r.Context(), apiKeyID)
	if err != nil {
		if err == storage.ErrAPIKeyNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "API key not found")
			return
		}
		slog.Error("failed to get API key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if key.UserID == nil || *key.UserID != claims.UserID {
		httputil.WriteJSONError(w, http.StatusNotFound, "API key not found")
		return
	}

	var req updateMetadataKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Handle activation/deactivation
	if req.IsActive != nil {
		if *req.IsActive {
			err = h.metadata.ActivateMetadataKey(r.Context(), apiKeyID, keyName)
		} else {
			err = h.metadata.DeactivateMetadataKey(r.Context(), apiKeyID, keyName)
		}
		if err != nil {
			if err == storage.ErrMetadataKeyNotFound {
				httputil.WriteJSONError(w, http.StatusNotFound, "metadata key not found")
				return
			}
			slog.Error("failed to update metadata key active state", "error", err)
			httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	// Handle display name update
	if req.DisplayName != nil {
		if err := h.metadata.UpdateMetadataKeyDisplayName(r.Context(), apiKeyID, keyName, req.DisplayName); err != nil {
			if err == storage.ErrMetadataKeyNotFound {
				httputil.WriteJSONError(w, http.StatusNotFound, "metadata key not found")
				return
			}
			slog.Error("failed to update metadata key display name", "error", err)
			httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
