package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/httputil"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

type EvalHandler struct {
	eval    storage.EvalStorage
	apiKeys storage.APIKeyStorage
}

func NewEvalHandler(eval storage.EvalStorage, apiKeys storage.APIKeyStorage) *EvalHandler {
	return &EvalHandler{eval: eval, apiKeys: apiKeys}
}

// --- Eval Sets ---

type createEvalSetRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CreateEvalSet handles POST /api/v1/admin/eval/sets
func (h *EvalHandler) CreateEvalSet(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createEvalSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "name is required")
		return
	}

	set := &models.EvalSet{
		ID:     uuid.New(),
		UserID: claims.UserID,
		OrgID:  claims.OrgID,
		Name:   req.Name,
	}
	if req.Description != "" {
		set.Description = &req.Description
	}

	if err := h.eval.CreateEvalSet(r.Context(), set); err != nil {
		slog.Error("failed to create eval set", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, set)
}

// ListEvalSets handles GET /api/v1/admin/eval/sets
func (h *EvalHandler) ListEvalSets(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	limit, offset := evalParsePagination(r, 20, 100)

	sets, total, err := h.eval.ListEvalSets(r.Context(), claims.UserID, claims.OrgID, limit, offset)
	if err != nil {
		slog.Error("failed to list eval sets", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"sets":       sets,
		"numRecords": total,
	})
}

// GetEvalSet handles GET /api/v1/admin/eval/sets/{id}
func (h *EvalHandler) GetEvalSet(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid eval set ID")
		return
	}

	set, err := h.eval.GetEvalSet(r.Context(), id)
	if err != nil {
		if err == storage.ErrEvalSetNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
			return
		}
		slog.Error("failed to get eval set", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !verifyEvalSetOwnership(claims, set) {
		httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, set)
}

type updateEvalSetRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// UpdateEvalSet handles PUT /api/v1/admin/eval/sets/{id}
func (h *EvalHandler) UpdateEvalSet(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid eval set ID")
		return
	}

	// Verify ownership
	set, err := h.eval.GetEvalSet(r.Context(), id)
	if err != nil {
		if err == storage.ErrEvalSetNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
			return
		}
		slog.Error("failed to get eval set", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyEvalSetOwnership(claims, set) {
		httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
		return
	}

	var req updateEvalSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "name is required")
		return
	}

	var desc *string
	if req.Description != "" {
		desc = &req.Description
	}

	updated, err := h.eval.UpdateEvalSet(r.Context(), id, req.Name, desc)
	if err != nil {
		if err == storage.ErrEvalSetNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
			return
		}
		slog.Error("failed to update eval set", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, updated)
}

// DeleteEvalSet handles DELETE /api/v1/admin/eval/sets/{id}
func (h *EvalHandler) DeleteEvalSet(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid eval set ID")
		return
	}

	// Verify ownership
	set, err := h.eval.GetEvalSet(r.Context(), id)
	if err != nil {
		if err == storage.ErrEvalSetNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
			return
		}
		slog.Error("failed to get eval set", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyEvalSetOwnership(claims, set) {
		httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
		return
	}

	if err := h.eval.DeleteEvalSet(r.Context(), id); err != nil {
		slog.Error("failed to delete eval set", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Eval Set Items ---

type addItemsRequest struct {
	RequestIDs []string `json:"requestIds"`
}

// AddItems handles POST /api/v1/admin/eval/sets/{id}/items
func (h *EvalHandler) AddItems(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	evalSetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid eval set ID")
		return
	}

	// Verify ownership
	set, err := h.eval.GetEvalSet(r.Context(), evalSetID)
	if err != nil {
		if err == storage.ErrEvalSetNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
			return
		}
		slog.Error("failed to get eval set", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyEvalSetOwnership(claims, set) {
		httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
		return
	}

	var req addItemsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.RequestIDs) == 0 {
		httputil.WriteJSONError(w, http.StatusBadRequest, "requestIds is required")
		return
	}

	var ids []uuid.UUID
	for _, s := range req.RequestIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request ID: "+s)
			return
		}
		ids = append(ids, id)
	}

	inserted, err := h.eval.AddEvalSetItems(r.Context(), evalSetID, ids)
	if err != nil {
		slog.Error("failed to add eval set items", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"added": inserted,
	})
}

type addItemsFromFiltersRequest struct {
	SourceAPIKeyID string            `json:"sourceApiKeyId"`
	SourceProvider string            `json:"sourceProvider"`
	SourceModel    string            `json:"sourceModel"`
	SourceStart    string            `json:"sourceStart"`
	SourceEnd      string            `json:"sourceEnd"`
	SourceMetadata map[string]string `json:"sourceMetadata"`
	Limit          int               `json:"limit"`
}

// AddItemsFromFilters handles POST /api/v1/admin/eval/sets/{id}/items/from-filters
func (h *EvalHandler) AddItemsFromFilters(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	evalSetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid eval set ID")
		return
	}

	// Verify ownership
	set, err := h.eval.GetEvalSet(r.Context(), evalSetID)
	if err != nil {
		if err == storage.ErrEvalSetNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
			return
		}
		slog.Error("failed to get eval set", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyEvalSetOwnership(claims, set) {
		httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
		return
	}

	var req addItemsFromFiltersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	filters := &storage.EvalSetSourceFilters{
		Metadata: req.SourceMetadata,
		Limit:    req.Limit,
	}

	if req.SourceAPIKeyID != "" {
		id, err := uuid.Parse(req.SourceAPIKeyID)
		if err != nil {
			httputil.WriteJSONError(w, http.StatusBadRequest, "invalid sourceApiKeyId")
			return
		}
		filters.APIKeyID = &id
	}
	if req.SourceProvider != "" {
		filters.Provider = &req.SourceProvider
	}
	if req.SourceModel != "" {
		filters.Model = &req.SourceModel
	}
	if req.SourceStart != "" {
		t, err := parseFlexTime(req.SourceStart)
		if err != nil {
			httputil.WriteJSONError(w, http.StatusBadRequest, "invalid sourceStart format")
			return
		}
		filters.Start = &t
	}
	if req.SourceEnd != "" {
		t, err := parseFlexTime(req.SourceEnd)
		if err != nil {
			httputil.WriteJSONError(w, http.StatusBadRequest, "invalid sourceEnd format")
			return
		}
		filters.End = &t
	}

	inserted, err := h.eval.AddEvalSetItemsFromFilters(r.Context(), evalSetID, claims.UserID, claims.OrgID, filters)
	if err != nil {
		slog.Error("failed to add eval set items from filters", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"added": inserted,
	})
}

// RemoveItem handles DELETE /api/v1/admin/eval/sets/{id}/items/{requestId}
func (h *EvalHandler) RemoveItem(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	evalSetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid eval set ID")
		return
	}

	// Verify ownership
	set, err := h.eval.GetEvalSet(r.Context(), evalSetID)
	if err != nil {
		if err == storage.ErrEvalSetNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
			return
		}
		slog.Error("failed to get eval set", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyEvalSetOwnership(claims, set) {
		httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
		return
	}

	requestID, err := uuid.Parse(chi.URLParam(r, "requestId"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request ID")
		return
	}

	if err := h.eval.RemoveEvalSetItem(r.Context(), evalSetID, requestID); err != nil {
		slog.Error("failed to remove eval set item", "error", err)
		httputil.WriteJSONError(w, http.StatusNotFound, "eval set item not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListItems handles GET /api/v1/admin/eval/sets/{id}/items
func (h *EvalHandler) ListItems(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	evalSetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid eval set ID")
		return
	}

	// Verify ownership
	set, err := h.eval.GetEvalSet(r.Context(), evalSetID)
	if err != nil {
		if err == storage.ErrEvalSetNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
			return
		}
		slog.Error("failed to get eval set", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyEvalSetOwnership(claims, set) {
		httputil.WriteJSONError(w, http.StatusNotFound, "eval set not found")
		return
	}

	limit, offset := evalParsePagination(r, 50, 200)

	items, total, err := h.eval.ListEvalSetItems(r.Context(), evalSetID, limit, offset)
	if err != nil {
		slog.Error("failed to list eval set items", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items":      items,
		"numRecords": total,
	})
}

// --- Eval Runs ---

type createEvalRunRequest struct {
	EvalSetID      string `json:"evalSetId"`
	TargetProvider string `json:"targetProvider"`
	TargetModel    string `json:"targetModel"`
	Evaluators     []any  `json:"evaluators"`
}

// CreateRun handles POST /api/v1/admin/eval/runs
func (h *EvalHandler) CreateRun(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createEvalRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.EvalSetID == "" || req.TargetProvider == "" || req.TargetModel == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "evalSetId, targetProvider, and targetModel are required")
		return
	}

	evalSetID, err := uuid.Parse(req.EvalSetID)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid evalSetId")
		return
	}

	// Verify eval set exists and belongs to user
	set, err := h.eval.GetEvalSet(r.Context(), evalSetID)
	if err != nil {
		if err == storage.ErrEvalSetNotFound {
			httputil.WriteJSONError(w, http.StatusBadRequest, "eval set not found")
			return
		}
		slog.Error("failed to get eval set", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyEvalSetOwnership(claims, set) {
		httputil.WriteJSONError(w, http.StatusBadRequest, "eval set not found")
		return
	}

	run := &models.EvalRun{
		ID:             uuid.New(),
		UserID:         claims.UserID,
		OrgID:          claims.OrgID,
		EvalSetID:      evalSetID,
		Status:         "pending",
		TargetProvider: req.TargetProvider,
		TargetModel:    req.TargetModel,
	}

	if len(req.Evaluators) > 0 {
		b, err := json.Marshal(req.Evaluators)
		if err != nil {
			httputil.WriteJSONError(w, http.StatusBadRequest, "invalid evaluators")
			return
		}
		run.Evaluators = b
	}

	if err := h.eval.CreateEvalRun(r.Context(), run); err != nil {
		slog.Error("failed to create eval run", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	run.ParseJSONFields()
	httputil.WriteJSON(w, http.StatusCreated, run)
}

// ListRuns handles GET /api/v1/admin/eval/runs
func (h *EvalHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	limit, offset := evalParsePagination(r, 20, 100)

	runs, total, err := h.eval.ListEvalRuns(r.Context(), claims.UserID, claims.OrgID, limit, offset)
	if err != nil {
		slog.Error("failed to list eval runs", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"runs":       runs,
		"numRecords": total,
	})
}

// GetRun handles GET /api/v1/admin/eval/runs/{id}
func (h *EvalHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid run ID")
		return
	}

	run, err := h.eval.GetEvalRun(r.Context(), id)
	if err != nil {
		if err == storage.ErrEvalRunNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "eval run not found")
			return
		}
		slog.Error("failed to get eval run", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !verifyEvalRunOwnership(claims, run) {
		httputil.WriteJSONError(w, http.StatusNotFound, "eval run not found")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, run)
}

// CancelRun handles POST /api/v1/admin/eval/runs/{id}/cancel
func (h *EvalHandler) CancelRun(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid run ID")
		return
	}

	if err := h.eval.CancelEvalRun(r.Context(), id, claims.UserID, claims.OrgID); err != nil {
		slog.Error("failed to cancel eval run", "error", err)
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// ListResults handles GET /api/v1/admin/eval/runs/{id}/results
func (h *EvalHandler) ListResults(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	runID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid run ID")
		return
	}

	// Verify run ownership
	run, err := h.eval.GetEvalRun(r.Context(), runID)
	if err != nil {
		if err == storage.ErrEvalRunNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "eval run not found")
			return
		}
		slog.Error("failed to get eval run", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !verifyEvalRunOwnership(claims, run) {
		httputil.WriteJSONError(w, http.StatusNotFound, "eval run not found")
		return
	}

	limit, offset := evalParsePagination(r, 50, 200)

	results, total, err := h.eval.ListEvalResults(r.Context(), runID, limit, offset)
	if err != nil {
		slog.Error("failed to list eval results", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"results":    results,
		"numRecords": total,
	})
}

// GetResult handles GET /api/v1/admin/eval/runs/{id}/results/{resultId}
func (h *EvalHandler) GetResult(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	resultID, err := uuid.Parse(chi.URLParam(r, "resultId"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid result ID")
		return
	}

	result, err := h.eval.GetEvalResult(r.Context(), resultID)
	if err != nil {
		slog.Error("failed to get eval result", "error", err)
		httputil.WriteJSONError(w, http.StatusNotFound, "eval result not found")
		return
	}

	// Verify run ownership via the result's run
	run, err := h.eval.GetEvalRun(r.Context(), result.EvalRunID)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusNotFound, "eval run not found")
		return
	}
	if !verifyEvalRunOwnership(claims, run) {
		httputil.WriteJSONError(w, http.StatusNotFound, "eval result not found")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, result)
}

// --- Helpers ---

func verifyEvalSetOwnership(claims *auth.JWTClaims, set *models.EvalSet) bool {
	if claims.OrgID != nil {
		return set.OrgID != nil && *set.OrgID == *claims.OrgID
	}
	return set.UserID == claims.UserID
}

func verifyEvalRunOwnership(claims *auth.JWTClaims, run *models.EvalRun) bool {
	if claims.OrgID != nil {
		return run.OrgID != nil && *run.OrgID == *claims.OrgID
	}
	return run.UserID == claims.UserID
}

func evalParsePagination(r *http.Request, defaultLimit, maxLimit int) (int, int) {
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

func parseFlexTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02", s)
	}
	return t, err
}
