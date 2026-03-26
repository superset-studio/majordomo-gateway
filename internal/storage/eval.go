package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

var (
	ErrEvalSetNotFound = errors.New("eval set not found")
	ErrEvalRunNotFound = errors.New("eval run not found")
)

const evalSetColumns = `id, user_id, org_id, name, description, created_at, updated_at`

const evalSetItemColumns = `id, eval_set_id, request_id, created_at`

const evalRunColumns = `id, user_id, org_id, eval_set_id, status, error_message,
	target_provider, target_model, evaluators, evaluator_summary,
	total_requests, successful_requests, failed_requests,
	original_total_cost, replay_total_cost, judge_total_cost,
	original_avg_latency_ms, replay_avg_latency_ms,
	started_at, completed_at, created_at`

const evalRunListColumns = `r.id, r.eval_set_id, r.status, r.target_provider, r.target_model,
	r.total_requests, r.successful_requests, r.failed_requests,
	r.original_total_cost, r.replay_total_cost, r.judge_total_cost,
	r.created_at, s.name AS eval_set_name`

const evalResultColumns = `id, eval_run_id, source_request_id,
	original_provider, original_model, original_cost, original_latency_ms,
	original_input_tokens, original_output_tokens,
	replay_response, replay_cost, replay_latency_ms,
	replay_input_tokens, replay_output_tokens,
	error_message, created_at`

const evalResultScoreColumns = `id, eval_result_id, evaluator_name, score, reason, created_at`

// --- Eval Sets ---

func (s *PostgresStorage) CreateEvalSet(ctx context.Context, set *models.EvalSet) error {
	query := `
		INSERT INTO eval_sets (id, user_id, org_id, name, description)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`

	var id uuid.UUID
	err := s.db.QueryRowxContext(ctx, query,
		set.ID, set.UserID, set.OrgID, set.Name, set.Description,
	).Scan(&id)
	return err
}

func (s *PostgresStorage) GetEvalSet(ctx context.Context, id uuid.UUID) (*models.EvalSet, error) {
	var set models.EvalSet
	err := s.db.GetContext(ctx, &set,
		`SELECT `+evalSetColumns+`,
		(SELECT COUNT(*) FROM eval_set_items WHERE eval_set_id = eval_sets.id) AS item_count
		FROM eval_sets WHERE id = $1`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEvalSetNotFound
		}
		return nil, err
	}
	return &set, nil
}

func (s *PostgresStorage) ListEvalSets(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, limit, offset int) ([]*models.EvalSet, int, error) {
	var total int
	var countQuery string
	var countArg interface{}

	if orgID != nil {
		countQuery = `SELECT COUNT(*) FROM eval_sets WHERE org_id = $1`
		countArg = *orgID
	} else {
		countQuery = `SELECT COUNT(*) FROM eval_sets WHERE user_id = $1`
		countArg = userID
	}
	if err := s.db.GetContext(ctx, &total, countQuery, countArg); err != nil {
		return nil, 0, err
	}

	var query string
	var args []interface{}
	selectCols := evalSetColumns + `, (SELECT COUNT(*) FROM eval_set_items WHERE eval_set_id = eval_sets.id) AS item_count`
	if orgID != nil {
		query = `SELECT ` + selectCols + ` FROM eval_sets WHERE org_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		args = []interface{}{*orgID, limit, offset}
	} else {
		query = `SELECT ` + selectCols + ` FROM eval_sets WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		args = []interface{}{userID, limit, offset}
	}

	var items []*models.EvalSet
	if err := s.db.SelectContext(ctx, &items, query, args...); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *PostgresStorage) UpdateEvalSet(ctx context.Context, id uuid.UUID, name string, description *string) (*models.EvalSet, error) {
	query := `
		UPDATE eval_sets SET name = $2, description = $3, updated_at = now()
		WHERE id = $1
		RETURNING ` + evalSetColumns

	var set models.EvalSet
	err := s.db.GetContext(ctx, &set, query, id, name, description)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEvalSetNotFound
		}
		return nil, err
	}
	return &set, nil
}

func (s *PostgresStorage) DeleteEvalSet(ctx context.Context, id uuid.UUID) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM eval_sets WHERE id = $1`, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrEvalSetNotFound
	}
	return nil
}

// --- Eval Set Items ---

func (s *PostgresStorage) AddEvalSetItems(ctx context.Context, evalSetID uuid.UUID, requestIDs []uuid.UUID) (int, error) {
	if len(requestIDs) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	inserted := 0
	for _, reqID := range requestIDs {
		result, err := tx.ExecContext(ctx,
			`INSERT INTO eval_set_items (eval_set_id, request_id) VALUES ($1, $2) ON CONFLICT (eval_set_id, request_id) DO NOTHING`,
			evalSetID, reqID)
		if err != nil {
			return 0, err
		}
		rows, _ := result.RowsAffected()
		inserted += int(rows)
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

func (s *PostgresStorage) AddEvalSetItemsFromFilters(ctx context.Context, evalSetID uuid.UUID, userID uuid.UUID, orgID *uuid.UUID, filters *EvalSetSourceFilters) (int, error) {
	conditions := []string{"(request_body IS NOT NULL OR body_s3_key IS NOT NULL)"}
	args := []interface{}{evalSetID}
	idx := 2

	if orgID != nil {
		conditions = append(conditions, fmt.Sprintf("org_id = $%d", idx))
		args = append(args, *orgID)
		idx++
	} else {
		conditions = append(conditions, fmt.Sprintf("user_id = $%d", idx))
		args = append(args, userID)
		idx++
	}

	if filters.APIKeyID != nil {
		conditions = append(conditions, fmt.Sprintf("majordomo_api_key_id = $%d", idx))
		args = append(args, *filters.APIKeyID)
		idx++
	}
	if filters.Provider != nil {
		conditions = append(conditions, fmt.Sprintf("provider = $%d", idx))
		args = append(args, *filters.Provider)
		idx++
	}
	if filters.Model != nil {
		conditions = append(conditions, fmt.Sprintf("model = $%d", idx))
		args = append(args, *filters.Model)
		idx++
	}
	if filters.Start != nil {
		conditions = append(conditions, fmt.Sprintf("requested_at >= $%d", idx))
		args = append(args, *filters.Start)
		idx++
	}
	if filters.End != nil {
		conditions = append(conditions, fmt.Sprintf("requested_at < $%d", idx))
		args = append(args, *filters.End)
		idx++
	}
	for key, value := range filters.Metadata {
		conditions = append(conditions, fmt.Sprintf("raw_metadata->>'%s' = $%d", key, idx))
		args = append(args, value)
		idx++
	}

	limit := filters.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	whereClause := ""
	for i, c := range conditions {
		if i > 0 {
			whereClause += " AND "
		}
		whereClause += c
	}

	query := fmt.Sprintf(`
		INSERT INTO eval_set_items (eval_set_id, request_id)
		SELECT $1, id FROM llm_requests
		WHERE %s
		ORDER BY requested_at DESC
		LIMIT $%d
		ON CONFLICT (eval_set_id, request_id) DO NOTHING`,
		whereClause, idx)
	args = append(args, limit)

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	rows, _ := result.RowsAffected()
	return int(rows), nil
}

func (s *PostgresStorage) RemoveEvalSetItem(ctx context.Context, evalSetID uuid.UUID, requestID uuid.UUID) error {
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM eval_set_items WHERE eval_set_id = $1 AND request_id = $2`,
		evalSetID, requestID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("eval set item not found")
	}
	return nil
}

func (s *PostgresStorage) ListEvalSetItems(ctx context.Context, evalSetID uuid.UUID, limit, offset int) ([]*models.EvalSetItem, int, error) {
	var total int
	if err := s.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM eval_set_items WHERE eval_set_id = $1`, evalSetID); err != nil {
		return nil, 0, err
	}

	var items []*models.EvalSetItem
	if err := s.db.SelectContext(ctx, &items,
		`SELECT `+evalSetItemColumns+` FROM eval_set_items WHERE eval_set_id = $1 ORDER BY created_at LIMIT $2 OFFSET $3`,
		evalSetID, limit, offset); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// --- Eval Runs ---

func (s *PostgresStorage) CreateEvalRun(ctx context.Context, run *models.EvalRun) error {
	var evaluators *string
	if len(run.Evaluators) > 0 {
		str := string(run.Evaluators)
		evaluators = &str
	}

	query := `
		INSERT INTO eval_runs (
			id, user_id, org_id, eval_set_id, status,
			target_provider, target_model, evaluators
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, COALESCE($8::jsonb, '[]'::jsonb)
		)
		RETURNING id`

	var id uuid.UUID
	err := s.db.QueryRowxContext(ctx, query,
		run.ID, run.UserID, run.OrgID, run.EvalSetID, run.Status,
		run.TargetProvider, run.TargetModel, evaluators,
	).Scan(&id)
	return err
}

func (s *PostgresStorage) GetEvalRun(ctx context.Context, id uuid.UUID) (*models.EvalRun, error) {
	var run models.EvalRun
	err := s.db.GetContext(ctx, &run, `SELECT `+evalRunColumns+` FROM eval_runs WHERE id = $1`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEvalRunNotFound
		}
		return nil, err
	}
	run.ParseJSONFields()
	return &run, nil
}

func (s *PostgresStorage) ListEvalRuns(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, limit, offset int) ([]*models.EvalRunListItem, int, error) {
	var total int
	var countQuery string
	var countArg interface{}

	if orgID != nil {
		countQuery = `SELECT COUNT(*) FROM eval_runs WHERE org_id = $1`
		countArg = *orgID
	} else {
		countQuery = `SELECT COUNT(*) FROM eval_runs WHERE user_id = $1`
		countArg = userID
	}
	if err := s.db.GetContext(ctx, &total, countQuery, countArg); err != nil {
		return nil, 0, err
	}

	var query string
	var args []interface{}
	if orgID != nil {
		query = `SELECT ` + evalRunListColumns + ` FROM eval_runs r JOIN eval_sets s ON s.id = r.eval_set_id WHERE r.org_id = $1 ORDER BY r.created_at DESC LIMIT $2 OFFSET $3`
		args = []interface{}{*orgID, limit, offset}
	} else {
		query = `SELECT ` + evalRunListColumns + ` FROM eval_runs r JOIN eval_sets s ON s.id = r.eval_set_id WHERE r.user_id = $1 ORDER BY r.created_at DESC LIMIT $2 OFFSET $3`
		args = []interface{}{userID, limit, offset}
	}

	var items []*models.EvalRunListItem
	if err := s.db.SelectContext(ctx, &items, query, args...); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *PostgresStorage) CancelEvalRun(ctx context.Context, id uuid.UUID, userID uuid.UUID, orgID *uuid.UUID) error {
	var query string
	var args []interface{}

	if orgID != nil {
		query = `UPDATE eval_runs SET status = 'cancelled' WHERE id = $1 AND org_id = $2 AND status IN ('pending', 'running')`
		args = []interface{}{id, *orgID}
	} else {
		query = `UPDATE eval_runs SET status = 'cancelled' WHERE id = $1 AND user_id = $2 AND status IN ('pending', 'running')`
		args = []interface{}{id, userID}
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("eval run not found or not cancellable")
	}
	return nil
}

// --- Eval Results ---

func (s *PostgresStorage) ListEvalResults(ctx context.Context, runID uuid.UUID, limit, offset int) ([]*models.EvalResult, int, error) {
	var total int
	if err := s.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM eval_results WHERE eval_run_id = $1`, runID); err != nil {
		return nil, 0, err
	}

	var items []*models.EvalResult
	if err := s.db.SelectContext(ctx, &items,
		`SELECT `+evalResultColumns+` FROM eval_results WHERE eval_run_id = $1 ORDER BY created_at LIMIT $2 OFFSET $3`,
		runID, limit, offset); err != nil {
		return nil, 0, err
	}

	// Fetch scores for each result
	for _, item := range items {
		var scores []models.EvalResultScore
		if err := s.db.SelectContext(ctx, &scores,
			`SELECT `+evalResultScoreColumns+` FROM eval_result_scores WHERE eval_result_id = $1 ORDER BY evaluator_name`,
			item.ID); err != nil {
			return nil, 0, err
		}
		item.Scores = scores
	}

	return items, total, nil
}

func (s *PostgresStorage) GetEvalResult(ctx context.Context, id uuid.UUID) (*models.EvalResult, error) {
	var result models.EvalResult
	err := s.db.GetContext(ctx, &result, `SELECT `+evalResultColumns+` FROM eval_results WHERE id = $1`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("eval result not found")
		}
		return nil, err
	}

	var scores []models.EvalResultScore
	if err := s.db.SelectContext(ctx, &scores,
		`SELECT `+evalResultScoreColumns+` FROM eval_result_scores WHERE eval_result_id = $1 ORDER BY evaluator_name`,
		result.ID); err != nil {
		return nil, err
	}
	result.Scores = scores

	return &result, nil
}
