package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/experiment"
	"github.com/superset-studio/majordomo-gateway/internal/httputil"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// ExperimentHandler handles admin API requests for A/B test experiments.
type ExperimentHandler struct {
	store   experiment.ExperimentStorage
	apiKeys storage.APIKeyStorage
	router  *experiment.Router
}

// NewExperimentHandler creates a new ExperimentHandler.
func NewExperimentHandler(store experiment.ExperimentStorage, apiKeys storage.APIKeyStorage, router *experiment.Router) *ExperimentHandler {
	return &ExperimentHandler{store: store, apiKeys: apiKeys, router: router}
}

// --- Experiments CRUD ---

type createExperimentRequest struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	APIKeyID        string `json:"apiKeyId"`
	Sticky          bool   `json:"sticky"`
	StickyKeyHeader string `json:"stickyKeyHeader"`
}

// Create handles POST /api/v1/admin/experiments
func (h *ExperimentHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createExperimentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "name is required")
		return
	}

	exp := &experiment.Experiment{
		ID:     uuid.New(),
		UserID: &claims.UserID,
		OrgID:  claims.OrgID,
		Name:   req.Name,
		Status: experiment.StatusDraft,
		Sticky: req.Sticky,
	}
	if req.Description != "" {
		exp.Description = &req.Description
	}
	if req.APIKeyID != "" {
		id, err := uuid.Parse(req.APIKeyID)
		if err != nil {
			httputil.WriteJSONError(w, http.StatusBadRequest, "invalid apiKeyId")
			return
		}
		exp.APIKeyID = &id
	}
	if req.StickyKeyHeader != "" {
		exp.StickyKeyHeader = &req.StickyKeyHeader
	}

	if err := h.store.CreateExperiment(r.Context(), exp); err != nil {
		slog.Error("failed to create experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, toExperimentResponse(exp))
}

// List handles GET /api/v1/admin/experiments
func (h *ExperimentHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	limit, offset := experimentParsePagination(r, 20, 100)

	items, total, err := h.store.ListExperiments(r.Context(), claims.UserID, claims.OrgID, limit, offset)
	if err != nil {
		slog.Error("failed to list experiments", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Convert to API response
	var list []models.ExperimentListItem
	for _, item := range items {
		list = append(list, models.ExperimentListItem{
			ID:           item.ID,
			APIKeyID:     item.APIKeyID,
			Name:         item.Name,
			Status:       item.Status,
			Sticky:       item.Sticky,
			VariantCount: item.VariantCount,
		})
	}
	if list == nil {
		list = []models.ExperimentListItem{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"experiments": list,
		"numRecords":  total,
	})
}

// Get handles GET /api/v1/admin/experiments/{id}
func (h *ExperimentHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid experiment ID")
		return
	}

	exp, err := h.store.GetExperiment(r.Context(), id)
	if err != nil {
		if err == storage.ErrExperimentNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
			return
		}
		slog.Error("failed to get experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !verifyExperimentOwnership(claims, exp) {
		httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, toExperimentResponse(exp))
}

type updateExperimentRequest struct {
	Name            *string `json:"name"`
	Description     *string `json:"description"`
	Sticky          *bool   `json:"sticky"`
	StickyKeyHeader *string `json:"stickyKeyHeader"`
}

// Update handles PUT /api/v1/admin/experiments/{id}
func (h *ExperimentHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid experiment ID")
		return
	}

	exp, err := h.store.GetExperiment(r.Context(), id)
	if err != nil {
		if err == storage.ErrExperimentNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
			return
		}
		slog.Error("failed to get experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyExperimentOwnership(claims, exp) {
		httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
		return
	}

	var req updateExperimentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.store.UpdateExperiment(r.Context(), id, &experiment.UpdateExperimentInput{
		Name:            req.Name,
		Description:     req.Description,
		Sticky:          req.Sticky,
		StickyKeyHeader: req.StickyKeyHeader,
	}); err != nil {
		slog.Error("failed to update experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Re-fetch to return updated state
	updated, _ := h.store.GetExperiment(r.Context(), id)
	httputil.WriteJSON(w, http.StatusOK, toExperimentResponse(updated))
}

// Delete handles DELETE /api/v1/admin/experiments/{id}
func (h *ExperimentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid experiment ID")
		return
	}

	exp, err := h.store.GetExperiment(r.Context(), id)
	if err != nil {
		if err == storage.ErrExperimentNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
			return
		}
		slog.Error("failed to get experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyExperimentOwnership(claims, exp) {
		httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
		return
	}

	if exp.Status == experiment.StatusActive {
		httputil.WriteJSONError(w, http.StatusConflict, "cannot delete an active experiment; pause or complete it first")
		return
	}

	if err := h.store.DeleteExperiment(r.Context(), id); err != nil {
		slog.Error("failed to delete experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Variants ---

type addVariantRequest struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Weight    int    `json:"weight"`
	IsControl bool   `json:"isControl"`
}

// AddVariant handles POST /api/v1/admin/experiments/{id}/variants
func (h *ExperimentHandler) AddVariant(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	expID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid experiment ID")
		return
	}

	exp, err := h.store.GetExperiment(r.Context(), expID)
	if err != nil {
		if err == storage.ErrExperimentNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
			return
		}
		slog.Error("failed to get experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyExperimentOwnership(claims, exp) {
		httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
		return
	}

	var req addVariantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Provider == "" || req.Model == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "name, provider, and model are required")
		return
	}
	if req.Weight < 0 {
		httputil.WriteJSONError(w, http.StatusBadRequest, "weight must be non-negative")
		return
	}
	if req.Weight == 0 {
		req.Weight = 1
	}

	v := &experiment.Variant{
		ID:           uuid.New(),
		ExperimentID: expID,
		Name:         req.Name,
		Provider:     req.Provider,
		Model:        req.Model,
		Weight:       req.Weight,
		IsControl:    req.IsControl,
	}

	if err := h.store.CreateVariant(r.Context(), v); err != nil {
		slog.Error("failed to create variant", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, v)
}

type updateVariantRequest struct {
	Name      *string `json:"name"`
	Provider  *string `json:"provider"`
	Model     *string `json:"model"`
	Weight    *int    `json:"weight"`
	IsControl *bool   `json:"isControl"`
}

// UpdateVariant handles PUT /api/v1/admin/experiments/{id}/variants/{variantId}
func (h *ExperimentHandler) UpdateVariant(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	expID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid experiment ID")
		return
	}

	exp, err := h.store.GetExperiment(r.Context(), expID)
	if err != nil {
		if err == storage.ErrExperimentNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
			return
		}
		slog.Error("failed to get experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyExperimentOwnership(claims, exp) {
		httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
		return
	}

	variantID, err := uuid.Parse(chi.URLParam(r, "variantId"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid variant ID")
		return
	}

	var req updateVariantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.store.UpdateVariant(r.Context(), variantID, &experiment.UpdateVariantInput{
		Name:      req.Name,
		Provider:  req.Provider,
		Model:     req.Model,
		Weight:    req.Weight,
		IsControl: req.IsControl,
	}); err != nil {
		if err == storage.ErrVariantNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "variant not found")
			return
		}
		slog.Error("failed to update variant", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteVariant handles DELETE /api/v1/admin/experiments/{id}/variants/{variantId}
func (h *ExperimentHandler) DeleteVariant(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	expID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid experiment ID")
		return
	}

	exp, err := h.store.GetExperiment(r.Context(), expID)
	if err != nil {
		if err == storage.ErrExperimentNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
			return
		}
		slog.Error("failed to get experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyExperimentOwnership(claims, exp) {
		httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
		return
	}

	variantID, err := uuid.Parse(chi.URLParam(r, "variantId"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid variant ID")
		return
	}

	if err := h.store.DeleteVariant(r.Context(), variantID); err != nil {
		if err == storage.ErrVariantNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "variant not found")
			return
		}
		slog.Error("failed to delete variant", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Lifecycle ---

// Activate handles POST /api/v1/admin/experiments/{id}/activate
func (h *ExperimentHandler) Activate(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid experiment ID")
		return
	}

	exp, err := h.store.GetExperiment(r.Context(), id)
	if err != nil {
		if err == storage.ErrExperimentNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
			return
		}
		slog.Error("failed to get experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyExperimentOwnership(claims, exp) {
		httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
		return
	}

	if exp.Status != experiment.StatusDraft && exp.Status != experiment.StatusPaused {
		httputil.WriteJSONError(w, http.StatusConflict, "experiment must be in draft or paused status to activate")
		return
	}

	// Validate: at least 2 variants with total weight > 0
	if len(exp.Variants) < 2 {
		httputil.WriteJSONError(w, http.StatusBadRequest, "experiment must have at least 2 variants")
		return
	}
	totalWeight := 0
	for _, v := range exp.Variants {
		totalWeight += v.Weight
	}
	if totalWeight <= 0 {
		httputil.WriteJSONError(w, http.StatusBadRequest, "total variant weight must be greater than 0")
		return
	}

	// Check for conflicting active experiments
	hasConflict, err := h.store.HasActiveExperiment(r.Context(), exp.APIKeyID, claims.UserID, claims.OrgID, &id)
	if err != nil {
		slog.Error("failed to check experiment conflict", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if hasConflict {
		httputil.WriteJSONError(w, http.StatusConflict, "another experiment is already active for the same scope")
		return
	}

	if err := h.store.UpdateExperimentStatus(r.Context(), id, experiment.StatusActive); err != nil {
		slog.Error("failed to activate experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.router.InvalidateCache()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "active"})
}

// Pause handles POST /api/v1/admin/experiments/{id}/pause
func (h *ExperimentHandler) Pause(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid experiment ID")
		return
	}

	exp, err := h.store.GetExperiment(r.Context(), id)
	if err != nil {
		if err == storage.ErrExperimentNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
			return
		}
		slog.Error("failed to get experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyExperimentOwnership(claims, exp) {
		httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
		return
	}

	if exp.Status != experiment.StatusActive {
		httputil.WriteJSONError(w, http.StatusConflict, "experiment must be active to pause")
		return
	}

	if err := h.store.UpdateExperimentStatus(r.Context(), id, experiment.StatusPaused); err != nil {
		slog.Error("failed to pause experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.router.InvalidateCache()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

// Complete handles POST /api/v1/admin/experiments/{id}/complete
func (h *ExperimentHandler) Complete(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid experiment ID")
		return
	}

	exp, err := h.store.GetExperiment(r.Context(), id)
	if err != nil {
		if err == storage.ErrExperimentNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
			return
		}
		slog.Error("failed to get experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyExperimentOwnership(claims, exp) {
		httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
		return
	}

	if err := h.store.UpdateExperimentStatus(r.Context(), id, experiment.StatusCompleted); err != nil {
		slog.Error("failed to complete experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.router.InvalidateCache()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "completed"})
}

// --- Results ---

// GetResults handles POST /api/v1/admin/experiments/{id}/results
func (h *ExperimentHandler) GetResults(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid experiment ID")
		return
	}

	exp, err := h.store.GetExperiment(r.Context(), id)
	if err != nil {
		if err == storage.ErrExperimentNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
			return
		}
		slog.Error("failed to get experiment", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyExperimentOwnership(claims, exp) {
		httputil.WriteJSONError(w, http.StatusNotFound, "experiment not found")
		return
	}

	// The store needs to implement GetExperimentResults. We use the storage package
	// directly since it has the analytics query.
	resultsStore, ok := h.store.(interface {
		GetExperimentResults(ctx interface{}, experimentID uuid.UUID, userID uuid.UUID, orgID *uuid.UUID) ([]storage.ExperimentVariantResultRow, error)
	})
	if !ok {
		httputil.WriteJSONError(w, http.StatusNotImplemented, "results not available")
		return
	}

	rows, err := resultsStore.GetExperimentResults(r.Context(), id, claims.UserID, claims.OrgID)
	if err != nil {
		slog.Error("failed to get experiment results", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	totalRequests := 0
	var variants []models.ExperimentVariantResult
	for _, row := range rows {
		totalRequests += row.RequestCount
		errorRate := 0.0
		if row.RequestCount > 0 {
			errorRate = float64(row.ErrorCount) / float64(row.RequestCount)
		}
		variants = append(variants, models.ExperimentVariantResult{
			VariantName:     row.VariantName,
			RequestCount:    row.RequestCount,
			AvgLatencyMs:    row.AvgLatencyMs,
			TotalCost:       row.TotalCost,
			AvgInputTokens:  row.AvgInputTokens,
			AvgOutputTokens: row.AvgOutputTokens,
			ErrorCount:      row.ErrorCount,
			ErrorRate:       errorRate,
		})
	}
	if variants == nil {
		variants = []models.ExperimentVariantResult{}
	}

	httputil.WriteJSON(w, http.StatusOK, models.ExperimentResults{
		ExperimentID:   id,
		ExperimentName: exp.Name,
		TotalRequests:  totalRequests,
		Variants:       variants,
	})
}

// --- Helpers ---

func verifyExperimentOwnership(claims *auth.JWTClaims, exp *experiment.Experiment) bool {
	if claims.OrgID != nil && exp.OrgID != nil {
		return *exp.OrgID == *claims.OrgID
	}
	if exp.UserID != nil {
		return *exp.UserID == claims.UserID
	}
	return false
}

func experimentParsePagination(r *http.Request, defaultLimit, maxLimit int) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return limit, offset
}

func toExperimentResponse(exp *experiment.Experiment) models.Experiment {
	resp := models.Experiment{
		ID:              exp.ID,
		UserID:          exp.UserID,
		OrgID:           exp.OrgID,
		APIKeyID:        exp.APIKeyID,
		Name:            exp.Name,
		Description:     exp.Description,
		Status:          exp.Status,
		Sticky:          exp.Sticky,
		StickyKeyHeader: exp.StickyKeyHeader,
	}
	for _, v := range exp.Variants {
		resp.Variants = append(resp.Variants, models.ExperimentVariant{
			ID:           v.ID,
			ExperimentID: v.ExperimentID,
			Name:         v.Name,
			Provider:     v.Provider,
			Model:        v.Model,
			Weight:       v.Weight,
			IsControl:    v.IsControl,
		})
	}
	if resp.Variants == nil {
		resp.Variants = []models.ExperimentVariant{}
	}
	return resp
}
