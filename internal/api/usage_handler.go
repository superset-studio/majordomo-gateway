package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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

type usageRequest struct {
	Preset          string           `json:"preset"`
	Start           string           `json:"start"`
	End             string           `json:"end"`
	APIKeyID        string           `json:"api_key_id"`
	MetadataFilters []metadataFilter `json:"metadata_filters"`
	Limit           int              `json:"limit"`
	Offset          int              `json:"offset"`
}

type metadataFilter struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// decodeUsageRequest decodes a JSON body into a UsageFilter.
func decodeUsageRequest(r *http.Request) (*storage.UsageFilter, *usageRequest, error) {
	var req usageRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, nil, fmt.Errorf("invalid JSON body: %w", err)
		}
	}

	if len(req.MetadataFilters) > 2 {
		return nil, nil, fmt.Errorf("at most 2 metadata filters allowed")
	}

	filter := &storage.UsageFilter{}

	// Parse API key filter
	if req.APIKeyID != "" {
		parsed, err := uuid.Parse(req.APIKeyID)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid api_key_id: %w", err)
		}
		filter.APIKeyID = &parsed
	}

	// Parse metadata filters
	for _, mf := range req.MetadataFilters {
		if mf.Key == "" || mf.Value == "" {
			return nil, nil, fmt.Errorf("metadata filter key and value must be non-empty")
		}
		filter.MetadataFilters = append(filter.MetadataFilters, storage.MetadataFilter{
			Key:   mf.Key,
			Value: mf.Value,
		})
	}

	// Parse date range
	if req.Start != "" {
		start, err := parseDate(req.Start)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid start date: %w", err)
		}
		filter.Start = start

		if req.End != "" {
			end, err := parseDate(req.End)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid end date: %w", err)
			}
			filter.End = end
		} else {
			filter.End = time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
		}
	} else {
		// Use preset (default: 30d)
		preset := req.Preset
		if preset == "" {
			preset = "30d"
		}

		now := time.Now().UTC()
		filter.End = now.Truncate(24 * time.Hour).Add(24 * time.Hour)

		switch preset {
		case "7d":
			filter.Start = filter.End.AddDate(0, 0, -7)
		case "90d":
			filter.Start = filter.End.AddDate(0, 0, -90)
		default:
			filter.Start = filter.End.AddDate(0, 0, -30)
		}
	}

	return filter, &req, nil
}

// parseDate parses YYYY-MM-DD or RFC3339.
func parseDate(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

// GetSummary handles POST /api/v1/admin/usage/summary
func (h *UsageHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter.UserID = claims.UserID

	summary, err := h.usage.GetUsageSummary(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get usage summary", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// GetDailyUsage handles POST /api/v1/admin/usage/daily
func (h *UsageHandler) GetDailyUsage(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter.UserID = claims.UserID

	if filter.APIKeyID != nil {
		if !h.verifyAPIKeyBelongsToUser(w, r, *filter.APIKeyID, claims.UserID) {
			return
		}
	}

	daily, err := h.usage.GetDailyUsage(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get daily usage", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(daily)
}

// GetModelBreakdown handles POST /api/v1/admin/usage/models
func (h *UsageHandler) GetModelBreakdown(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter.UserID = claims.UserID

	if filter.APIKeyID != nil {
		if !h.verifyAPIKeyBelongsToUser(w, r, *filter.APIKeyID, claims.UserID) {
			return
		}
	}

	models, err := h.usage.GetModelBreakdown(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get model breakdown", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models)
}

// GetAPIKeyBreakdown handles POST /api/v1/admin/usage/api-keys
func (h *UsageHandler) GetAPIKeyBreakdown(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter.UserID = claims.UserID

	breakdown, err := h.usage.GetAPIKeyBreakdown(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get API key breakdown", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(breakdown)
}

// ListRequests handles POST /api/v1/admin/usage/requests
func (h *UsageHandler) ListRequests(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	filter, req, err := decodeUsageRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter.UserID = claims.UserID

	if filter.APIKeyID != nil {
		if !h.verifyAPIKeyBelongsToUser(w, r, *filter.APIKeyID, claims.UserID) {
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
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"requests":   requests,
		"numRecords": total,
	})
}

// GetRequestDetail handles GET /api/v1/admin/usage/requests/{id}
func (h *UsageHandler) GetRequestDetail(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid request ID", http.StatusBadRequest)
		return
	}

	detail, err := h.usage.GetRequestDetail(r.Context(), id, claims.UserID)
	if err != nil {
		if err == storage.ErrRequestNotFound {
			http.Error(w, "request not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to get request detail", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(detail)
}

// GetMetadataBreakdown handles POST /api/v1/admin/usage/metadata/{keyName}
func (h *UsageHandler) GetMetadataBreakdown(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	keyName := chi.URLParam(r, "keyName")
	if keyName == "" {
		http.Error(w, "missing key name", http.StatusBadRequest)
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter.UserID = claims.UserID

	if filter.APIKeyID != nil {
		if !h.verifyAPIKeyBelongsToUser(w, r, *filter.APIKeyID, claims.UserID) {
			return
		}
	}

	breakdown, err := h.usage.GetMetadataBreakdown(r.Context(), filter, keyName)
	if err != nil {
		slog.Error("failed to get metadata breakdown", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(breakdown)
}

// verifyAPIKeyBelongsToUser checks that the given API key belongs to the user.
// Returns false and writes an error response if ownership cannot be verified.
func (h *UsageHandler) verifyAPIKeyBelongsToUser(w http.ResponseWriter, r *http.Request, apiKeyID uuid.UUID, userID uuid.UUID) bool {
	key, err := h.apiKeys.GetAPIKeyByID(r.Context(), apiKeyID)
	if err != nil {
		if err == storage.ErrAPIKeyNotFound {
			http.Error(w, "API key not found", http.StatusNotFound)
			return false
		}
		slog.Error("failed to get API key", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return false
	}

	if key.UserID == nil || *key.UserID != userID {
		http.Error(w, "API key not found", http.StatusNotFound)
		return false
	}

	return true
}
