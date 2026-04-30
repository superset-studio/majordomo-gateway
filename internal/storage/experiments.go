package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/experiment"
)

var (
	ErrExperimentNotFound = errors.New("experiment not found")
	ErrVariantNotFound    = errors.New("variant not found")
)

const experimentColumns = `id, user_id, org_id, api_key_id, name, description, status, sticky, sticky_key_header, created_at, updated_at`

const variantColumns = `id, experiment_id, name, provider, model, weight, is_control, created_at`

// --- Experiments ---

func (s *PostgresStorage) CreateExperiment(ctx context.Context, exp *experiment.Experiment) error {
	query := `
		INSERT INTO experiments (id, user_id, org_id, api_key_id, name, description, status, sticky, sticky_key_header)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err := s.db.ExecContext(ctx, query,
		exp.ID, exp.UserID, exp.OrgID, exp.APIKeyID, exp.Name, exp.Description,
		exp.Status, exp.Sticky, exp.StickyKeyHeader,
	)
	return err
}

func (s *PostgresStorage) GetExperiment(ctx context.Context, id uuid.UUID) (*experiment.Experiment, error) {
	var exp experiment.Experiment
	err := s.db.GetContext(ctx, &exp,
		`SELECT `+experimentColumns+` FROM experiments WHERE id = $1`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrExperimentNotFound
		}
		return nil, err
	}

	variants, err := s.ListVariants(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load variants: %w", err)
	}
	exp.Variants = variants
	return &exp, nil
}

func (s *PostgresStorage) ListExperiments(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, limit, offset int) ([]*experiment.ExperimentListItem, int, error) {
	var total int
	var countQuery string
	var countArg interface{}

	if orgID != nil {
		countQuery = `SELECT COUNT(*) FROM experiments WHERE org_id = $1`
		countArg = *orgID
	} else {
		countQuery = `SELECT COUNT(*) FROM experiments WHERE user_id = $1`
		countArg = userID
	}
	if err := s.db.GetContext(ctx, &total, countQuery, countArg); err != nil {
		return nil, 0, err
	}

	var query string
	var args []interface{}
	selectCols := experimentColumns + `, (SELECT COUNT(*) FROM experiment_variants WHERE experiment_id = experiments.id) AS variant_count`
	if orgID != nil {
		query = `SELECT ` + selectCols + ` FROM experiments WHERE org_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		args = []interface{}{*orgID, limit, offset}
	} else {
		query = `SELECT ` + selectCols + ` FROM experiments WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		args = []interface{}{userID, limit, offset}
	}

	var items []*experiment.ExperimentListItem
	if err := s.db.SelectContext(ctx, &items, query, args...); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *PostgresStorage) UpdateExperiment(ctx context.Context, id uuid.UUID, input *experiment.UpdateExperimentInput) error {
	query := `
		UPDATE experiments SET
			name = COALESCE($2, name),
			description = COALESCE($3, description),
			sticky = COALESCE($4, sticky),
			sticky_key_header = COALESCE($5, sticky_key_header),
			updated_at = now()
		WHERE id = $1`

	result, err := s.db.ExecContext(ctx, query, id, input.Name, input.Description, input.Sticky, input.StickyKeyHeader)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrExperimentNotFound
	}
	return nil
}

func (s *PostgresStorage) DeleteExperiment(ctx context.Context, id uuid.UUID) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM experiments WHERE id = $1`, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrExperimentNotFound
	}
	return nil
}

func (s *PostgresStorage) UpdateExperimentStatus(ctx context.Context, id uuid.UUID, status string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE experiments SET status = $2, updated_at = now() WHERE id = $1`,
		id, status)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrExperimentNotFound
	}
	return nil
}

// --- Variants ---

func (s *PostgresStorage) CreateVariant(ctx context.Context, v *experiment.Variant) error {
	query := `
		INSERT INTO experiment_variants (id, experiment_id, name, provider, model, weight, is_control)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`

	_, err := s.db.ExecContext(ctx, query,
		v.ID, v.ExperimentID, v.Name, v.Provider, v.Model, v.Weight, v.IsControl,
	)
	return err
}

func (s *PostgresStorage) UpdateVariant(ctx context.Context, id uuid.UUID, input *experiment.UpdateVariantInput) error {
	query := `
		UPDATE experiment_variants SET
			name = COALESCE($2, name),
			provider = COALESCE($3, provider),
			model = COALESCE($4, model),
			weight = COALESCE($5, weight),
			is_control = COALESCE($6, is_control)
		WHERE id = $1`

	result, err := s.db.ExecContext(ctx, query, id, input.Name, input.Provider, input.Model, input.Weight, input.IsControl)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrVariantNotFound
	}
	return nil
}

func (s *PostgresStorage) DeleteVariant(ctx context.Context, id uuid.UUID) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM experiment_variants WHERE id = $1`, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrVariantNotFound
	}
	return nil
}

func (s *PostgresStorage) ListVariants(ctx context.Context, experimentID uuid.UUID) ([]*experiment.Variant, error) {
	var variants []*experiment.Variant
	err := s.db.SelectContext(ctx, &variants,
		`SELECT `+variantColumns+` FROM experiment_variants WHERE experiment_id = $1 ORDER BY created_at`, experimentID)
	if err != nil {
		return nil, err
	}
	return variants, nil
}

// --- Sticky Assignments ---

func (s *PostgresStorage) GetAssignment(ctx context.Context, experimentID uuid.UUID, subjectHash string) (*experiment.Assignment, error) {
	var a experiment.Assignment
	err := s.db.GetContext(ctx, &a,
		`SELECT id, experiment_id, variant_id, subject_hash FROM experiment_assignments
		 WHERE experiment_id = $1 AND subject_hash = $2`,
		experimentID, subjectHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (s *PostgresStorage) CreateAssignment(ctx context.Context, a *experiment.Assignment) error {
	// ON CONFLICT DO NOTHING handles concurrent inserts for the same subject
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO experiment_assignments (id, experiment_id, variant_id, subject_hash)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (experiment_id, subject_hash) DO NOTHING`,
		a.ID, a.ExperimentID, a.VariantID, a.SubjectHash)
	return err
}

// --- Active Experiments Query ---

func (s *PostgresStorage) GetActiveExperiments(ctx context.Context, apiKeyID uuid.UUID, userID, orgID *uuid.UUID) ([]*experiment.Experiment, error) {
	// Find active experiments that match:
	// 1. Targeted at this specific API key, OR
	// 2. Targeted at all keys for this user/org (api_key_id IS NULL)
	query := `
		SELECT ` + experimentColumns + `
		FROM experiments
		WHERE status = 'active'
		  AND (
		    api_key_id = $1
		    OR (api_key_id IS NULL AND user_id = $2)
		    OR (api_key_id IS NULL AND org_id = $3)
		  )
		ORDER BY created_at
		LIMIT 1`

	var experiments []*experiment.Experiment
	err := s.db.SelectContext(ctx, &experiments, query, apiKeyID, userID, orgID)
	if err != nil {
		return nil, err
	}

	// Load variants for each experiment
	for _, exp := range experiments {
		variants, err := s.ListVariants(ctx, exp.ID)
		if err != nil {
			return nil, fmt.Errorf("load variants for experiment %s: %w", exp.ID, err)
		}
		exp.Variants = variants
	}

	return experiments, nil
}

// --- Conflict Check ---

func (s *PostgresStorage) HasActiveExperiment(ctx context.Context, apiKeyID *uuid.UUID, userID uuid.UUID, orgID *uuid.UUID, excludeID *uuid.UUID) (bool, error) {
	// Check if there's already an active experiment that would conflict.
	// Conflict means: same API key, or same user/org wildcard scope.
	query := `
		SELECT EXISTS(
			SELECT 1 FROM experiments
			WHERE status = 'active'
			  AND ($1::uuid IS NULL OR api_key_id = $1 OR (api_key_id IS NULL AND (user_id = $2 OR org_id = $3)))
			  AND ($4::uuid IS NULL OR id != $4)
		)`

	var exists bool
	err := s.db.GetContext(ctx, &exists, query, apiKeyID, userID, orgID, excludeID)
	return exists, err
}

// --- Results Analytics ---

func (s *PostgresStorage) GetExperimentResults(ctx context.Context, experimentID uuid.UUID, userID uuid.UUID, orgID *uuid.UUID) ([]ExperimentVariantResultRow, error) {
	// Aggregate metrics from llm_requests grouped by experiment variant.
	// Experiment metadata is stored in indexed_metadata via the _experiment-id and _experiment-variant keys.
	query := `
		SELECT
			indexed_metadata->>'_experiment-variant' AS variant_name,
			COUNT(*) AS request_count,
			COALESCE(AVG(response_time_ms), 0) AS avg_latency_ms,
			COALESCE(SUM(total_cost), 0) AS total_cost,
			COALESCE(AVG(input_tokens), 0) AS avg_input_tokens,
			COALESCE(AVG(output_tokens), 0) AS avg_output_tokens,
			COUNT(*) FILTER (WHERE status_code >= 400) AS error_count
		FROM llm_requests
		WHERE indexed_metadata->>'_experiment-id' = $1
		  AND (user_id = $2 OR org_id = $3)
		GROUP BY indexed_metadata->>'_experiment-variant'
		ORDER BY request_count DESC`

	var rows []ExperimentVariantResultRow
	err := s.db.SelectContext(ctx, &rows, query, experimentID.String(), userID, orgID)
	return rows, err
}

// ExperimentVariantResultRow is the raw DB result for variant analytics.
type ExperimentVariantResultRow struct {
	VariantName     string  `db:"variant_name"`
	RequestCount    int     `db:"request_count"`
	AvgLatencyMs    float64 `db:"avg_latency_ms"`
	TotalCost       float64 `db:"total_cost"`
	AvgInputTokens  float64 `db:"avg_input_tokens"`
	AvgOutputTokens float64 `db:"avg_output_tokens"`
	ErrorCount      int     `db:"error_count"`
}
