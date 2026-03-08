package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/httputil"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// UsageHandler provides REST API endpoints for usage reporting.
type UsageHandler struct {
	usage   storage.UsageStorage
	apiKeys storage.APIKeyStorage
}

// NewUsageHandler creates a new usage reporting handler.
func NewUsageHandler(usage storage.UsageStorage, apiKeys storage.APIKeyStorage) *UsageHandler {
	return &UsageHandler{
		usage:   usage,
		apiKeys: apiKeys,
	}
}

// GetSummary handles POST /api/v1/admin/usage/summary
func (h *UsageHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	setFilterScope(filter, claims)

	summary, err := h.usage.GetUsageSummary(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get usage summary", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, summary)
}

// GetDailyUsage handles POST /api/v1/admin/usage/daily
func (h *UsageHandler) GetDailyUsage(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	setFilterScope(filter, claims)

	if filter.APIKeyID != nil {
		if !h.verifyAPIKeyOwnership(w, r, *filter.APIKeyID, claims) {
			return
		}
	}

	daily, err := h.usage.GetDailyUsage(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get daily usage", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, daily)
}

// GetModelBreakdown handles POST /api/v1/admin/usage/models
func (h *UsageHandler) GetModelBreakdown(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	setFilterScope(filter, claims)

	if filter.APIKeyID != nil {
		if !h.verifyAPIKeyOwnership(w, r, *filter.APIKeyID, claims) {
			return
		}
	}

	models, err := h.usage.GetModelBreakdown(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get model breakdown", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, models)
}

// GetAPIKeyBreakdown handles POST /api/v1/admin/usage/api-keys
func (h *UsageHandler) GetAPIKeyBreakdown(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	setFilterScope(filter, claims)

	breakdown, err := h.usage.GetAPIKeyBreakdown(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get API key breakdown", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, breakdown)
}

// ListRequests handles POST /api/v1/admin/usage/requests
func (h *UsageHandler) ListRequests(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	filter, req, err := decodeUsageRequest(r)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	setFilterScope(filter, claims)

	if filter.APIKeyID != nil {
		if !h.verifyAPIKeyOwnership(w, r, *filter.APIKeyID, claims) {
			return
		}
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	requests, total, err := h.usage.ListUsageRequests(r.Context(), filter, limit, offset)
	if err != nil {
		slog.Error("failed to list usage requests", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"requests":   requests,
		"numRecords": total,
	})
}

// GetRequestDetail handles GET /api/v1/admin/usage/requests/{id}
func (h *UsageHandler) GetRequestDetail(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request ID")
		return
	}

	detail, err := h.usage.GetRequestDetail(r.Context(), id, claims.UserID, claims.OrgID)
	if err != nil {
		if err == storage.ErrRequestNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "request not found")
			return
		}
		slog.Error("failed to get request detail", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, detail)
}

// GetMetadataBreakdown handles POST /api/v1/admin/usage/metadata/{keyName}
func (h *UsageHandler) GetMetadataBreakdown(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	keyName := chi.URLParam(r, "keyName")
	if keyName == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "missing key name")
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	setFilterScope(filter, claims)

	if filter.APIKeyID != nil {
		if !h.verifyAPIKeyOwnership(w, r, *filter.APIKeyID, claims) {
			return
		}
	}

	breakdown, err := h.usage.GetMetadataBreakdown(r.Context(), filter, keyName)
	if err != nil {
		slog.Error("failed to get metadata breakdown", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, breakdown)
}

// verifyAPIKeyOwnership checks that the given API key belongs to the user or their org.
// Returns false and writes an error response if ownership cannot be verified.
func (h *UsageHandler) verifyAPIKeyOwnership(w http.ResponseWriter, r *http.Request, apiKeyID uuid.UUID, claims *auth.JWTClaims) bool {
	key, err := h.apiKeys.GetAPIKeyByID(r.Context(), apiKeyID)
	if err != nil {
		if err == storage.ErrAPIKeyNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "API key not found")
			return false
		}
		slog.Error("failed to get API key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return false
	}

	if claims.OrgID != nil {
		if key.OrgID == nil || *key.OrgID != *claims.OrgID {
			httputil.WriteJSONError(w, http.StatusNotFound, "API key not found")
			return false
		}
	} else {
		if key.UserID == nil || *key.UserID != claims.UserID {
			httputil.WriteJSONError(w, http.StatusNotFound, "API key not found")
			return false
		}
	}

	return true
}
