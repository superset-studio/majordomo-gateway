package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

var (
	ErrReplayRunNotFound   = errors.New("replay run not found")
	ErrNoPendingReplayRun  = errors.New("no pending replay run")
)

const replayRunColumns = `id, user_id, org_id, status, error_message,
	source_api_key_id, source_provider, source_model, source_start, source_end, source_metadata, source_limit,
	target_provider, target_model,
	judge_enabled, judge_provider, judge_model,
	total_requests, exact_matches, judge_equivalent, divergent,
	original_total_cost, replay_total_cost, original_avg_latency_ms, replay_avg_latency_ms,
	started_at, completed_at, created_at`

const replayRunListColumns = `id, status, source_model, target_provider, target_model,
	total_requests, exact_matches, judge_equivalent, divergent,
	original_total_cost, replay_total_cost, created_at`

const replayResultColumns = `id, replay_run_id, source_request_id,
	original_provider, original_model, original_cost, original_latency_ms, original_input_tokens, original_output_tokens,
	replay_response, replay_cost, replay_latency_ms, replay_input_tokens, replay_output_tokens,
	exact_match, judge_equivalent, judge_reason, error_message, created_at`

func (s *PostgresStorage) CreateReplayRun(ctx context.Context, run *models.ReplayRun) error {
	// Convert nil []byte to a *string for JSONB — nil *string → SQL NULL
	var sourceMetadata *string
	if len(run.SourceMetadata) > 0 {
		str := string(run.SourceMetadata)
		sourceMetadata = &str
	}

	query := `
		INSERT INTO replay_runs (
			id, user_id, org_id, status,
			source_api_key_id, source_provider, source_model, source_start, source_end, source_metadata, source_limit,
			target_provider, target_model,
			judge_enabled, judge_provider, judge_model
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8, $9, $10, $11,
			$12, $13,
			$14, $15, $16
		)
		RETURNING id`

	var id uuid.UUID
	err := s.db.QueryRowxContext(ctx, query,
		run.ID, run.UserID, run.OrgID, run.Status,
		run.SourceAPIKeyID, run.SourceProvider, run.SourceModel,
		run.SourceStart, run.SourceEnd, sourceMetadata, run.SourceLimit,
		run.TargetProvider, run.TargetModel,
		run.JudgeEnabled, run.JudgeProvider, run.JudgeModel,
	).Scan(&id)
	return err
}

func (s *PostgresStorage) GetReplayRun(ctx context.Context, id uuid.UUID) (*models.ReplayRun, error) {
	var run models.ReplayRun
	err := s.db.GetContext(ctx, &run, `SELECT `+replayRunColumns+` FROM replay_runs WHERE id = $1`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrReplayRunNotFound
		}
		return nil, err
	}
	parseReplayRunMetadata(&run)
	return &run, nil
}

func (s *PostgresStorage) ListReplayRuns(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, limit, offset int) ([]*models.ReplayRunListItem, int, error) {
	var total int
	var countQuery string
	var countArg interface{}

	if orgID != nil {
		countQuery = `SELECT COUNT(*) FROM replay_runs WHERE org_id = $1`
		countArg = *orgID
	} else {
		countQuery = `SELECT COUNT(*) FROM replay_runs WHERE user_id = $1`
		countArg = userID
	}
	if err := s.db.GetContext(ctx, &total, countQuery, countArg); err != nil {
		return nil, 0, err
	}

	var query string
	var args []interface{}
	if orgID != nil {
		query = `SELECT ` + replayRunListColumns + ` FROM replay_runs WHERE org_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		args = []interface{}{*orgID, limit, offset}
	} else {
		query = `SELECT ` + replayRunListColumns + ` FROM replay_runs WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		args = []interface{}{userID, limit, offset}
	}

	var items []*models.ReplayRunListItem
	if err := s.db.SelectContext(ctx, &items, query, args...); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *PostgresStorage) UpdateReplayRunStatus(ctx context.Context, id uuid.UUID, status string, errorMessage *string) error {
	var query string
	var args []interface{}

	switch status {
	case "running":
		query = `UPDATE replay_runs SET status = $2, started_at = now() WHERE id = $1`
		args = []interface{}{id, status}
	case "completed":
		query = `UPDATE replay_runs SET status = $2, completed_at = now() WHERE id = $1`
		args = []interface{}{id, status}
	case "failed":
		query = `UPDATE replay_runs SET status = $2, error_message = $3, completed_at = now() WHERE id = $1`
		args = []interface{}{id, status, errorMessage}
	default:
		query = `UPDATE replay_runs SET status = $2 WHERE id = $1`
		args = []interface{}{id, status}
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrReplayRunNotFound
	}
	return nil
}

func (s *PostgresStorage) CancelReplayRun(ctx context.Context, id uuid.UUID, userID uuid.UUID, orgID *uuid.UUID) error {
	var query string
	var args []interface{}

	if orgID != nil {
		query = `UPDATE replay_runs SET status = 'cancelled' WHERE id = $1 AND org_id = $2 AND status IN ('pending', 'running')`
		args = []interface{}{id, *orgID}
	} else {
		query = `UPDATE replay_runs SET status = 'cancelled' WHERE id = $1 AND user_id = $2 AND status IN ('pending', 'running')`
		args = []interface{}{id, userID}
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("replay run not found or not cancellable")
	}
	return nil
}

func (s *PostgresStorage) ListReplayResults(ctx context.Context, runID uuid.UUID, limit, offset int) ([]*models.ReplayResult, int, error) {
	var total int
	if err := s.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM replay_results WHERE replay_run_id = $1`, runID); err != nil {
		return nil, 0, err
	}

	var items []*models.ReplayResult
	if err := s.db.SelectContext(ctx, &items,
		`SELECT `+replayResultColumns+` FROM replay_results WHERE replay_run_id = $1 ORDER BY created_at LIMIT $2 OFFSET $3`,
		runID, limit, offset); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *PostgresStorage) GetReplayResult(ctx context.Context, id uuid.UUID) (*models.ReplayResult, error) {
	var result models.ReplayResult
	err := s.db.GetContext(ctx, &result, `SELECT `+replayResultColumns+` FROM replay_results WHERE id = $1`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("replay result not found")
		}
		return nil, err
	}
	return &result, nil
}

func (s *PostgresStorage) ListLLMProviders(ctx context.Context) ([]*models.LLMProvider, error) {
	type row struct {
		ProviderID uuid.UUID `db:"provider_id"`
		Provider   string    `db:"provider"`
		Model      string    `db:"model"`
	}
	var rows []row
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT p.id AS provider_id, p.provider, m.model
		FROM llm_providers p
		JOIN llm_models m ON m.provider_id = p.id
		ORDER BY p.provider, m.model`); err != nil {
		return nil, err
	}

	providerMap := make(map[string]*models.LLMProvider)
	var result []*models.LLMProvider
	for _, r := range rows {
		p, ok := providerMap[r.Provider]
		if !ok {
			p = &models.LLMProvider{
				ID:       r.ProviderID,
				Provider: r.Provider,
			}
			providerMap[r.Provider] = p
			result = append(result, p)
		}
		p.Models = append(p.Models, r.Model)
	}
	return result, nil
}

// parseReplayRunMetadata converts the raw JSONB source_metadata into the parsed map.
func parseReplayRunMetadata(run *models.ReplayRun) {
	if run.SourceMetadata != nil {
		m := make(map[string]string)
		json.Unmarshal(run.SourceMetadata, &m)
		run.SourceMetadataParsed = m
	}
}
