package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/claudecode"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// ClaudeSessionHandler provides REST API endpoints for Claude Code session management.
type ClaudeSessionHandler struct {
	sessionMgr *claudecode.SessionManager
	storage    storage.ClaudeSessionStorage
}

// NewClaudeSessionHandler creates a new handler for Claude Code session endpoints.
func NewClaudeSessionHandler(mgr *claudecode.SessionManager, store storage.ClaudeSessionStorage) *ClaudeSessionHandler {
	return &ClaudeSessionHandler{
		sessionMgr: mgr,
		storage:    store,
	}
}

type startSessionRequest struct {
	Name string `json:"name"`
}

// StartSession handles POST /api/v1/claude-sessions
func (h *ClaudeSessionHandler) StartSession(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var sessionName *string
	var req startSessionRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.Name != "" {
			sessionName = &req.Name
		}
	}

	session, err := h.sessionMgr.StartSession(r.Context(), info.ID, sessionName)
	if err != nil {
		slog.Error("failed to start claude session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(session)
}

// EndSession handles POST /api/v1/claude-sessions/{id}/end
func (h *ClaudeSessionHandler) EndSession(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid session ID", http.StatusBadRequest)
		return
	}

	// Verify ownership
	existing, err := h.storage.GetClaudeSession(r.Context(), id)
	if err != nil {
		if err == storage.ErrClaudeSessionNotFound {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to get claude session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if existing.MajordomoAPIKeyID != info.ID {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	session, err := h.sessionMgr.EndSession(r.Context(), id)
	if err != nil {
		if err == storage.ErrClaudeSessionNotFound {
			http.Error(w, "session not found or already ended", http.StatusNotFound)
			return
		}
		slog.Error("failed to end claude session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

// ListSessions handles GET /api/v1/claude-sessions
func (h *ClaudeSessionHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	limit, offset := parsePagination(r)

	sessions, total, err := h.storage.ListClaudeSessions(r.Context(), info.ID, limit, offset)
	if err != nil {
		slog.Error("failed to list claude sessions", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions":   sessions,
		"numRecords": total,
	})
}

// GetSession handles GET /api/v1/claude-sessions/{id}
func (h *ClaudeSessionHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid session ID", http.StatusBadRequest)
		return
	}

	session, err := h.storage.GetClaudeSession(r.Context(), id)
	if err != nil {
		if err == storage.ErrClaudeSessionNotFound {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to get claude session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if session.MajordomoAPIKeyID != info.ID {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

// ListSessionRequests handles GET /api/v1/claude-sessions/{id}/requests
func (h *ClaudeSessionHandler) ListSessionRequests(w http.ResponseWriter, r *http.Request) {
	info := GetAPIKeyInfo(r.Context())
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid session ID", http.StatusBadRequest)
		return
	}

	// Verify ownership
	session, err := h.storage.GetClaudeSession(r.Context(), id)
	if err != nil {
		if err == storage.ErrClaudeSessionNotFound {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to get claude session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if session.MajordomoAPIKeyID != info.ID {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	limit, offset := parsePagination(r)

	details, total, err := h.storage.ListClaudeSessionRequests(r.Context(), id, limit, offset)
	if err != nil {
		slog.Error("failed to list claude session requests", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"requests":   details,
		"numRecords": total,
	})
}

// parsePagination extracts limit and offset from query parameters.
// Defaults: limit=50 (max 200), offset=0.
func parsePagination(r *http.Request) (int, int) {
	limit := 50
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 200 {
		limit = 200
	}

	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	return limit, offset
}
