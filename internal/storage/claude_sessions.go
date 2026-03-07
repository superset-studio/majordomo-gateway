package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

var (
	ErrClaudeSessionNotFound       = errors.New("claude session not found")
	ErrClaudeRequestDetailNotFound = errors.New("claude request detail not found")
)

const claudeSessionColumns = `id, majordomo_api_key_id, session_name, started_at, ended_at, total_requests, total_input_tokens, total_output_tokens, total_cost, created_at`

const claudeRequestDetailColumns = `id, llm_request_id, session_id, message_count, user_message_count, assistant_message_count, tool_names, tool_use_count, has_thinking, is_plan_mode, stop_reason, system_prompt_hash, created_at`

func (s *PostgresStorage) CreateClaudeSession(ctx context.Context, session *models.ClaudeSession) error {
	query := `
		INSERT INTO claude_sessions (id, majordomo_api_key_id, session_name, started_at)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + claudeSessionColumns

	return s.db.QueryRowxContext(ctx, query,
		session.ID, session.MajordomoAPIKeyID, session.SessionName, session.StartedAt,
	).StructScan(session)
}

func (s *PostgresStorage) EndClaudeSession(ctx context.Context, sessionID uuid.UUID) error {
	query := `UPDATE claude_sessions SET ended_at = now() WHERE id = $1 AND ended_at IS NULL`

	result, err := s.db.ExecContext(ctx, query, sessionID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrClaudeSessionNotFound
	}
	return nil
}

func (s *PostgresStorage) GetClaudeSession(ctx context.Context, sessionID uuid.UUID) (*models.ClaudeSession, error) {
	query := `SELECT ` + claudeSessionColumns + ` FROM claude_sessions WHERE id = $1`

	var session models.ClaudeSession
	if err := s.db.GetContext(ctx, &session, query, sessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrClaudeSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (s *PostgresStorage) ListClaudeSessions(ctx context.Context, apiKeyID uuid.UUID, limit, offset int) ([]*models.ClaudeSession, int, error) {
	countQuery := `SELECT COUNT(*) FROM claude_sessions WHERE majordomo_api_key_id = $1`
	var total int
	if err := s.db.GetContext(ctx, &total, countQuery, apiKeyID); err != nil {
		return nil, 0, err
	}

	query := `SELECT ` + claudeSessionColumns + `
		FROM claude_sessions
		WHERE majordomo_api_key_id = $1
		ORDER BY started_at DESC
		LIMIT $2 OFFSET $3`

	var sessions []*models.ClaudeSession
	if err := s.db.SelectContext(ctx, &sessions, query, apiKeyID, limit, offset); err != nil {
		return nil, 0, err
	}
	return sessions, total, nil
}

func (s *PostgresStorage) UpdateClaudeSessionStats(ctx context.Context, sessionID uuid.UUID, inputTokens, outputTokens int, cost float64) error {
	query := `
		UPDATE claude_sessions
		SET total_requests = total_requests + 1,
			total_input_tokens = total_input_tokens + $2,
			total_output_tokens = total_output_tokens + $3,
			total_cost = total_cost + $4
		WHERE id = $1`

	_, err := s.db.ExecContext(ctx, query, sessionID, inputTokens, outputTokens, cost)
	return err
}

func (s *PostgresStorage) CreateClaudeRequestDetail(ctx context.Context, detail *models.ClaudeRequestDetail) error {
	query := `
		INSERT INTO claude_request_details (
			id, llm_request_id, session_id, message_count, user_message_count, assistant_message_count,
			tool_names, tool_use_count, has_thinking, is_plan_mode, stop_reason, system_prompt_hash
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

	_, err := s.db.ExecContext(ctx, query,
		detail.ID, detail.LLMRequestID, detail.SessionID,
		detail.MessageCount, detail.UserMessageCount, detail.AssistantMessageCount,
		pq.Array(detail.ToolNames), detail.ToolUseCount,
		detail.HasThinking, detail.IsPlanMode, detail.StopReason, detail.SystemPromptHash,
	)
	return err
}

func (s *PostgresStorage) ListClaudeSessionRequests(ctx context.Context, sessionID uuid.UUID, limit, offset int) ([]*models.ClaudeRequestDetail, int, error) {
	countQuery := `SELECT COUNT(*) FROM claude_request_details WHERE session_id = $1`
	var total int
	if err := s.db.GetContext(ctx, &total, countQuery, sessionID); err != nil {
		return nil, 0, err
	}

	query := `SELECT ` + claudeRequestDetailColumns + `
		FROM claude_request_details
		WHERE session_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`

	var details []*models.ClaudeRequestDetail
	if err := s.db.SelectContext(ctx, &details, query, sessionID, limit, offset); err != nil {
		return nil, 0, err
	}
	return details, total, nil
}
