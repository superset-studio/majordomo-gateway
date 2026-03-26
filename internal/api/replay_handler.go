package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/httputil"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

type ReplayHandler struct {
	replay  storage.ReplayStorage
	apiKeys storage.APIKeyStorage
}

func NewReplayHandler(replay storage.ReplayStorage, apiKeys storage.APIKeyStorage) *ReplayHandler {
	return &ReplayHandler{replay: replay, apiKeys: apiKeys}
}

// ListProviders handles GET /api/v1/admin/replay/providers
func (h *ReplayHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := h.replay.ListLLMProviders(r.Context())
	if err != nil {
		slog.Error("failed to list LLM providers", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, providers)
}

type createReplayRunRequest struct {
	SourceAPIKeyID string            `json:"sourceApiKeyId"`
	SourceProvider string            `json:"sourceProvider"`
	SourceModel    string            `json:"sourceModel"`
	SourceStart    string            `json:"sourceStart"`
	SourceEnd      string            `json:"sourceEnd"`
	SourceMetadata map[string]string `json:"sourceMetadata"`
	SourceLimit    int               `json:"sourceLimit"`
	TargetProvider string            `json:"targetProvider"`
	TargetModel    string            `json:"targetModel"`
	JudgeEnabled   bool              `json:"judgeEnabled"`
	JudgeProvider  string            `json:"judgeProvider"`
	JudgeModel     string            `json:"judgeModel"`
}

// CreateRun handles POST /api/v1/admin/replay/runs
func (h *ReplayHandler) CreateRun(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createReplayRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.TargetProvider == "" || req.TargetModel == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "targetProvider and targetModel are required")
		return
	}
	if req.JudgeEnabled && (req.JudgeProvider == "" || req.JudgeModel == "") {
		httputil.WriteJSONError(w, http.StatusBadRequest, "judgeProvider and judgeModel are required when judge is enabled")
		return
	}

	run := &models.ReplayRun{
		ID:             uuid.New(),
		UserID:         claims.UserID,
		OrgID:          claims.OrgID,
		Status:         "pending",
		TargetProvider: req.TargetProvider,
		TargetModel:    req.TargetModel,
		JudgeEnabled:   req.JudgeEnabled,
		SourceLimit:    req.SourceLimit,
	}

	if run.SourceLimit <= 0 {
		run.SourceLimit = 50
	}
	if run.SourceLimit > 200 {
		run.SourceLimit = 200
	}

	if req.SourceAPIKeyID != "" {
		id, err := uuid.Parse(req.SourceAPIKeyID)
		if err != nil {
			httputil.WriteJSONError(w, http.StatusBadRequest, "invalid sourceApiKeyId")
			return
		}
		run.SourceAPIKeyID = &id
	}
	if req.SourceProvider != "" {
		run.SourceProvider = &req.SourceProvider
	}
	if req.SourceModel != "" {
		run.SourceModel = &req.SourceModel
	}
	if req.SourceStart != "" {
		t, err := time.Parse(time.RFC3339, req.SourceStart)
		if err != nil {
			t, err = time.Parse("2006-01-02", req.SourceStart)
			if err != nil {
				httputil.WriteJSONError(w, http.StatusBadRequest, "invalid sourceStart format")
				return
			}
		}
		run.SourceStart = &t
	}
	if req.SourceEnd != "" {
		t, err := time.Parse(time.RFC3339, req.SourceEnd)
		if err != nil {
			t, err = time.Parse("2006-01-02", req.SourceEnd)
			if err != nil {
				httputil.WriteJSONError(w, http.StatusBadRequest, "invalid sourceEnd format")
				return
			}
		}
		run.SourceEnd = &t
	}
	if len(req.SourceMetadata) > 0 {
		b, _ := json.Marshal(req.SourceMetadata)
		run.SourceMetadata = b
	}
	if req.JudgeProvider != "" {
		run.JudgeProvider = &req.JudgeProvider
	}
	if req.JudgeModel != "" {
		run.JudgeModel = &req.JudgeModel
	}

	slog.Info("creating replay run",
		"source_provider_nil", run.SourceProvider == nil,
		"source_model_nil", run.SourceModel == nil,
		"judge_provider_nil", run.JudgeProvider == nil,
		"req_source_provider", req.SourceProvider,
		"req_source_model", req.SourceModel,
	)

	if err := h.replay.CreateReplayRun(r.Context(), run); err != nil {
		slog.Error("failed to create replay run", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, run)
}

// ListRuns handles GET /api/v1/admin/replay/runs
func (h *ReplayHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	runs, total, err := h.replay.ListReplayRuns(r.Context(), claims.UserID, claims.OrgID, limit, offset)
	if err != nil {
		slog.Error("failed to list replay runs", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"runs":       runs,
		"numRecords": total,
	})
}

// GetRun handles GET /api/v1/admin/replay/runs/{id}
func (h *ReplayHandler) GetRun(w http.ResponseWriter, r *http.Request) {
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

	run, err := h.replay.GetReplayRun(r.Context(), id)
	if err != nil {
		if err == storage.ErrReplayRunNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "replay run not found")
			return
		}
		slog.Error("failed to get replay run", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Verify ownership
	if claims.OrgID != nil {
		if run.OrgID == nil || *run.OrgID != *claims.OrgID {
			httputil.WriteJSONError(w, http.StatusNotFound, "replay run not found")
			return
		}
	} else if run.UserID != claims.UserID {
		httputil.WriteJSONError(w, http.StatusNotFound, "replay run not found")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, run)
}

// CancelRun handles POST /api/v1/admin/replay/runs/{id}/cancel
func (h *ReplayHandler) CancelRun(w http.ResponseWriter, r *http.Request) {
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

	if err := h.replay.CancelReplayRun(r.Context(), id, claims.UserID, claims.OrgID); err != nil {
		slog.Error("failed to cancel replay run", "error", err)
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// ListResults handles GET /api/v1/admin/replay/runs/{id}/results
func (h *ReplayHandler) ListResults(w http.ResponseWriter, r *http.Request) {
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
	run, err := h.replay.GetReplayRun(r.Context(), runID)
	if err != nil {
		if err == storage.ErrReplayRunNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "replay run not found")
			return
		}
		slog.Error("failed to get replay run", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if claims.OrgID != nil {
		if run.OrgID == nil || *run.OrgID != *claims.OrgID {
			httputil.WriteJSONError(w, http.StatusNotFound, "replay run not found")
			return
		}
	} else if run.UserID != claims.UserID {
		httputil.WriteJSONError(w, http.StatusNotFound, "replay run not found")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	results, total, err := h.replay.ListReplayResults(r.Context(), runID, limit, offset)
	if err != nil {
		slog.Error("failed to list replay results", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"results":    results,
		"numRecords": total,
	})
}

// GetResult handles GET /api/v1/admin/replay/runs/{id}/results/{resultId}
func (h *ReplayHandler) GetResult(w http.ResponseWriter, r *http.Request) {
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

	result, err := h.replay.GetReplayResult(r.Context(), resultID)
	if err != nil {
		slog.Error("failed to get replay result", "error", err)
		httputil.WriteJSONError(w, http.StatusNotFound, "replay result not found")
		return
	}

	// Verify run ownership via the result's run
	run, err := h.replay.GetReplayRun(r.Context(), result.ReplayRunID)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusNotFound, "replay run not found")
		return
	}
	if claims.OrgID != nil {
		if run.OrgID == nil || *run.OrgID != *claims.OrgID {
			httputil.WriteJSONError(w, http.StatusNotFound, "replay result not found")
			return
		}
	} else if run.UserID != claims.UserID {
		httputil.WriteJSONError(w, http.StatusNotFound, "replay result not found")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, result)
}
