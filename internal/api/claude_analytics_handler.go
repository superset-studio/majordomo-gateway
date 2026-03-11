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

// ClaudeAnalyticsHandler provides REST API endpoints for Claude Code analytics.
type ClaudeAnalyticsHandler struct {
	analytics storage.ClaudeAnalyticsStorage
	apiKeys   storage.APIKeyStorage
}

// NewClaudeAnalyticsHandler creates a new Claude analytics handler.
func NewClaudeAnalyticsHandler(analytics storage.ClaudeAnalyticsStorage, apiKeys storage.APIKeyStorage) *ClaudeAnalyticsHandler {
	return &ClaudeAnalyticsHandler{
		analytics: analytics,
		apiKeys:   apiKeys,
	}
}

func (h *ClaudeAnalyticsHandler) verifyAPIKeyOwnership(w http.ResponseWriter, r *http.Request, apiKeyID uuid.UUID, claims *auth.JWTClaims) bool {
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

// GetSummary handles POST /api/v1/admin/claude/summary
func (h *ClaudeAnalyticsHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
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

	summary, err := h.analytics.GetClaudeSummary(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get claude summary", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, summary)
}

// GetDailyStats handles POST /api/v1/admin/claude/daily
func (h *ClaudeAnalyticsHandler) GetDailyStats(w http.ResponseWriter, r *http.Request) {
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

	daily, err := h.analytics.GetClaudeDailyStats(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get claude daily stats", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, daily)
}

// ListSessions handles POST /api/v1/admin/claude/sessions
func (h *ClaudeAnalyticsHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
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

	sessions, total, err := h.analytics.ListClaudeSessionsAdmin(r.Context(), filter, limit, offset)
	if err != nil {
		slog.Error("failed to list claude sessions", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"sessions":   sessions,
		"numRecords": total,
	})
}

// GetToolUsage handles POST /api/v1/admin/claude/tools
func (h *ClaudeAnalyticsHandler) GetToolUsage(w http.ResponseWriter, r *http.Request) {
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

	tools, err := h.analytics.GetClaudeToolUsage(r.Context(), filter, 20)
	if err != nil {
		slog.Error("failed to get claude tool usage", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, tools)
}

// GetPerformance handles POST /api/v1/admin/claude/performance
func (h *ClaudeAnalyticsHandler) GetPerformance(w http.ResponseWriter, r *http.Request) {
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

	perf, err := h.analytics.GetClaudePerformance(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get claude performance", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, perf)
}

// GetModelUsage handles POST /api/v1/admin/claude/models
func (h *ClaudeAnalyticsHandler) GetModelUsage(w http.ResponseWriter, r *http.Request) {
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

	models, err := h.analytics.GetClaudeModelUsage(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get claude model usage", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, models)
}

// GetAPIKeyBreakdown handles POST /api/v1/admin/claude/api-keys
func (h *ClaudeAnalyticsHandler) GetAPIKeyBreakdown(w http.ResponseWriter, r *http.Request) {
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

	breakdown, err := h.analytics.GetClaudeAPIKeyBreakdown(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get claude api key breakdown", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, breakdown)
}

// GetSessionDetail handles GET /api/v1/admin/claude/sessions/{id}
func (h *ClaudeAnalyticsHandler) GetSessionDetail(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid session ID")
		return
	}

	detail, err := h.analytics.GetClaudeSessionDetail(r.Context(), id, claims.UserID, claims.OrgID)
	if err != nil {
		if err == storage.ErrClaudeSessionNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		slog.Error("failed to get claude session detail", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, detail)
}
