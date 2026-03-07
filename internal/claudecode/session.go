package claudecode

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// SessionManager manages Claude Code session lifecycle.
type SessionManager struct {
	store storage.ClaudeSessionStorage
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager(store storage.ClaudeSessionStorage) *SessionManager {
	return &SessionManager{store: store}
}

// StartSession creates a new Claude Code session with an optional name.
func (m *SessionManager) StartSession(ctx context.Context, apiKeyID uuid.UUID, sessionName *string) (*models.ClaudeSession, error) {
	session := &models.ClaudeSession{
		ID:                uuid.New(),
		MajordomoAPIKeyID: apiKeyID,
		SessionName:       sessionName,
		StartedAt:         time.Now(),
	}

	if err := m.store.CreateClaudeSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

// EndSession marks a session as ended and returns the updated session.
func (m *SessionManager) EndSession(ctx context.Context, sessionID uuid.UUID) (*models.ClaudeSession, error) {
	if err := m.store.EndClaudeSession(ctx, sessionID); err != nil {
		return nil, err
	}
	return m.store.GetClaudeSession(ctx, sessionID)
}
