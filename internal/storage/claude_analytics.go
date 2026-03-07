package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

// claudeBaseWhere builds the common WHERE clause and args for Claude analytics queries.
func claudeBaseWhere(filter *UsageFilter) (string, []interface{}) {
	args := []interface{}{filter.UserID, filter.Start, filter.End}
	where := `ak.user_id = $1 AND cs.started_at >= $2 AND cs.started_at < $3`
	if filter.APIKeyID != nil {
		args = append(args, *filter.APIKeyID)
		where += fmt.Sprintf(` AND cs.majordomo_api_key_id = $%d`, len(args))
	}
	return where, args
}

// appendClaudeMetadataExists adds an EXISTS subquery for session-level queries
// that don't join llm_requests directly.
func appendClaudeMetadataExists(where string, args []interface{}, filters []MetadataFilter) (string, []interface{}) {
	if len(filters) == 0 {
		return where, args
	}
	sub := `SELECT 1 FROM claude_request_details crd2 JOIN llm_requests lr2 ON lr2.id = crd2.llm_request_id WHERE crd2.session_id = cs.id`
	for _, f := range filters {
		sub += fmt.Sprintf(` AND lr2.indexed_metadata->>$%d = $%d`, len(args)+1, len(args)+2)
		args = append(args, f.Key, f.Value)
	}
	where += ` AND EXISTS (` + sub + `)`
	return where, args
}

// appendClaudeMetadataFilters adds direct metadata filter clauses for queries that join llm_requests as lr.
func appendClaudeMetadataFilters(where string, args []interface{}, filters []MetadataFilter) (string, []interface{}) {
	for _, f := range filters {
		where += fmt.Sprintf(` AND lr.indexed_metadata->>$%d = $%d`, len(args)+1, len(args)+2)
		args = append(args, f.Key, f.Value)
	}
	return where, args
}

func (s *PostgresStorage) GetClaudeSummary(ctx context.Context, filter *UsageFilter) (*models.ClaudeSummary, error) {
	// Session-level aggregates
	sessionWhere, sessionArgs := claudeBaseWhere(filter)
	sessionWhere, sessionArgs = appendClaudeMetadataExists(sessionWhere, sessionArgs, filter.MetadataFilters)

	sessionQuery := `
		SELECT
			COUNT(*) AS total_sessions,
			COALESCE(SUM(cs.total_cost), 0) AS total_cost,
			COALESCE(AVG(EXTRACT(EPOCH FROM (cs.ended_at - cs.started_at)) / 60.0), 0) AS avg_duration_minutes,
			CASE WHEN COUNT(*) > 0 THEN COALESCE(SUM(cs.total_cost), 0) / COUNT(*) ELSE 0 END AS avg_cost_per_session,
			COALESCE(AVG(cs.total_requests), 0) AS avg_requests_per_session
		FROM claude_sessions cs
		JOIN api_keys ak ON ak.id = cs.majordomo_api_key_id
		WHERE ` + sessionWhere

	var summary models.ClaudeSummary
	err := s.db.QueryRowxContext(ctx, sessionQuery, sessionArgs...).Scan(
		&summary.TotalSessions,
		&summary.TotalCost,
		&summary.AvgDurationMinutes,
		&summary.AvgCostPerSession,
		&summary.AvgRequestsPerSession,
	)
	if err != nil {
		return nil, err
	}

	// Request-level rates (cache hit, thinking, plan mode) + cache token totals
	rateWhere, rateArgs := claudeBaseWhere(filter)
	rateWhere, rateArgs = appendClaudeMetadataFilters(rateWhere, rateArgs, filter.MetadataFilters)

	rateQuery := `
		SELECT
			CASE WHEN COALESCE(SUM(lr.input_tokens), 0) > 0
				THEN COALESCE(SUM(lr.cached_tokens), 0)::float / SUM(lr.input_tokens)
				ELSE 0
			END AS cache_hit_rate,
			CASE WHEN COUNT(*) > 0
				THEN COUNT(*) FILTER (WHERE crd.has_thinking)::float / COUNT(*)
				ELSE 0
			END AS thinking_rate,
			CASE WHEN COUNT(*) > 0
				THEN COUNT(*) FILTER (WHERE crd.is_plan_mode)::float / COUNT(*)
				ELSE 0
			END AS plan_mode_rate,
			COALESCE(SUM(lr.cache_creation_tokens), 0) AS total_cache_creation_tokens,
			COALESCE(SUM(lr.cached_tokens), 0) AS total_cached_tokens,
			COALESCE(SUM(lr.input_tokens), 0) AS total_input_tokens
		FROM claude_request_details crd
		JOIN llm_requests lr ON lr.id = crd.llm_request_id
		JOIN claude_sessions cs ON cs.id = crd.session_id
		JOIN api_keys ak ON ak.id = cs.majordomo_api_key_id
		WHERE ` + rateWhere

	err = s.db.QueryRowxContext(ctx, rateQuery, rateArgs...).Scan(
		&summary.CacheHitRate,
		&summary.ThinkingRate,
		&summary.PlanModeRate,
		&summary.TotalCacheCreationTokens,
		&summary.TotalCachedTokens,
		&summary.TotalInputTokens,
	)
	if err != nil {
		return nil, err
	}

	return &summary, nil
}

func (s *PostgresStorage) GetClaudeDailyStats(ctx context.Context, filter *UsageFilter) ([]*models.ClaudeDailyStats, error) {
	where, args := claudeBaseWhere(filter)
	where, args = appendClaudeMetadataExists(where, args, filter.MetadataFilters)

	query := `
		SELECT
			TO_CHAR(DATE_TRUNC('day', cs.started_at), 'YYYY-MM-DD') AS date,
			COUNT(*) AS session_count,
			COALESCE(SUM(cs.total_cost), 0) AS total_cost,
			COALESCE(AVG(EXTRACT(EPOCH FROM (cs.ended_at - cs.started_at)) / 60.0), 0) AS avg_duration_minutes,
			COALESCE(AVG(cs.total_requests), 0) AS avg_requests_per_session
		FROM claude_sessions cs
		JOIN api_keys ak ON ak.id = cs.majordomo_api_key_id
		WHERE ` + where + `
		GROUP BY DATE_TRUNC('day', cs.started_at)
		ORDER BY date`

	var stats []*models.ClaudeDailyStats
	if err := s.db.SelectContext(ctx, &stats, query, args...); err != nil {
		return nil, err
	}
	return stats, nil
}

func (s *PostgresStorage) ListClaudeSessionsAdmin(ctx context.Context, filter *UsageFilter, limit, offset int) ([]*models.ClaudeSessionListItem, int, error) {
	where, args := claudeBaseWhere(filter)
	where, args = appendClaudeMetadataExists(where, args, filter.MetadataFilters)

	countQuery := `
		SELECT COUNT(*)
		FROM claude_sessions cs
		JOIN api_keys ak ON ak.id = cs.majordomo_api_key_id
		WHERE ` + where

	var total int
	if err := s.db.GetContext(ctx, &total, countQuery, args...); err != nil {
		return nil, 0, err
	}

	// Rebuild args for data query (same base where + limit/offset)
	dataWhere, dataArgs := claudeBaseWhere(filter)
	dataWhere, dataArgs = appendClaudeMetadataExists(dataWhere, dataArgs, filter.MetadataFilters)
	dataArgs = append(dataArgs, limit, offset)

	dataQuery := fmt.Sprintf(`
		SELECT
			cs.id,
			cs.majordomo_api_key_id,
			ak.name AS api_key_name,
			cs.session_name,
			cs.started_at,
			cs.ended_at,
			EXTRACT(EPOCH FROM (cs.ended_at - cs.started_at)) / 60.0 AS duration_minutes,
			cs.total_requests,
			cs.total_input_tokens,
			cs.total_output_tokens,
			cs.total_cost,
			COALESCE(rd.tool_count, 0) AS tool_count,
			COALESCE(rd.thinking_count, 0) AS thinking_count,
			COALESCE(rd.plan_mode_count, 0) AS plan_mode_count,
			cs.created_at
		FROM claude_sessions cs
		JOIN api_keys ak ON ak.id = cs.majordomo_api_key_id
		LEFT JOIN LATERAL (
			SELECT
				COALESCE(SUM(crd.tool_use_count), 0) AS tool_count,
				COUNT(*) FILTER (WHERE crd.has_thinking) AS thinking_count,
				COUNT(*) FILTER (WHERE crd.is_plan_mode) AS plan_mode_count
			FROM claude_request_details crd
			WHERE crd.session_id = cs.id
		) rd ON true
		WHERE %s
		ORDER BY cs.started_at DESC
		LIMIT $%d OFFSET $%d`, dataWhere, len(dataArgs)-1, len(dataArgs))

	var sessions []*models.ClaudeSessionListItem
	if err := s.db.SelectContext(ctx, &sessions, dataQuery, dataArgs...); err != nil {
		return nil, 0, err
	}
	return sessions, total, nil
}

func (s *PostgresStorage) GetClaudeToolUsage(ctx context.Context, filter *UsageFilter, topN int) ([]*models.ClaudeToolUsage, error) {
	where, args := claudeBaseWhere(filter)
	where, args = appendClaudeMetadataExists(where, args, filter.MetadataFilters)
	args = append(args, topN)

	query := fmt.Sprintf(`
		SELECT
			tool_name,
			COUNT(*) AS use_count,
			COUNT(*)::float / NULLIF(SUM(COUNT(*)) OVER(), 0) * 100 AS percentage
		FROM claude_request_details crd
		JOIN claude_sessions cs ON cs.id = crd.session_id
		JOIN api_keys ak ON ak.id = cs.majordomo_api_key_id,
		LATERAL unnest(crd.tool_names) AS tool_name
		WHERE %s
		GROUP BY tool_name
		ORDER BY use_count DESC
		LIMIT $%d`, where, len(args))

	var tools []*models.ClaudeToolUsage
	if err := s.db.SelectContext(ctx, &tools, query, args...); err != nil {
		return nil, err
	}
	return tools, nil
}

func (s *PostgresStorage) GetClaudePerformance(ctx context.Context, filter *UsageFilter) (*models.ClaudePerformance, error) {
	where, args := claudeBaseWhere(filter)
	where, args = appendClaudeMetadataFilters(where, args, filter.MetadataFilters)

	query := `
		SELECT
			COALESCE(PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY lr.response_time_ms), 0) AS p50_ms,
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY lr.response_time_ms), 0) AS p95_ms,
			COALESCE(PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY lr.response_time_ms), 0) AS p99_ms,
			COUNT(*) AS total_requests,
			COUNT(*) FILTER (WHERE lr.status_code >= 400) AS error_count,
			CASE WHEN COUNT(*) > 0
				THEN COUNT(*) FILTER (WHERE lr.status_code >= 400)::float / COUNT(*)
				ELSE 0
			END AS error_rate
		FROM claude_request_details crd
		JOIN llm_requests lr ON lr.id = crd.llm_request_id
		JOIN claude_sessions cs ON cs.id = crd.session_id
		JOIN api_keys ak ON ak.id = cs.majordomo_api_key_id
		WHERE ` + where

	var perf models.ClaudePerformance
	if err := s.db.GetContext(ctx, &perf, query, args...); err != nil {
		return nil, err
	}
	return &perf, nil
}

func (s *PostgresStorage) GetClaudeSessionDetail(ctx context.Context, sessionID uuid.UUID, userID uuid.UUID) (*models.ClaudeSessionDetail, error) {
	// Fetch session with ownership check
	sessionQuery := `
		SELECT cs.id, cs.majordomo_api_key_id, cs.started_at, cs.ended_at, cs.total_requests, cs.total_input_tokens, cs.total_output_tokens, cs.total_cost, cs.created_at
		FROM claude_sessions cs
		JOIN api_keys ak ON ak.id = cs.majordomo_api_key_id
		WHERE cs.id = $1 AND ak.user_id = $2`

	var session models.ClaudeSession
	if err := s.db.GetContext(ctx, &session, sessionQuery, sessionID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrClaudeSessionNotFound
		}
		return nil, err
	}

	// Aggregated rows: unnest tools (with fallback for empty arrays),
	// divide per-request metrics by tool count so summing across tools = original total.
	rowsQuery := `
		WITH expanded AS (
			SELECT
				lr.model,
				tool_name,
				crd.has_thinking,
				crd.is_plan_mode,
				lr.input_tokens,
				lr.output_tokens,
				lr.cached_tokens,
				lr.total_cost,
				lr.response_time_ms,
				GREATEST(array_length(crd.tool_names, 1), 1) AS num_tools
			FROM claude_request_details crd
			JOIN llm_requests lr ON lr.id = crd.llm_request_id,
			LATERAL unnest(COALESCE(NULLIF(crd.tool_names, '{}'), ARRAY['(no tool)'])) AS tool_name
			WHERE crd.session_id = $1
		)
		SELECT model, tool_name, has_thinking, is_plan_mode,
		       COUNT(*) AS use_count,
		       COALESCE(SUM(input_tokens / num_tools), 0) AS input_tokens,
		       COALESCE(SUM(output_tokens / num_tools), 0) AS output_tokens,
		       COALESCE(SUM(cached_tokens / num_tools), 0) AS cached_tokens,
		       COALESCE(SUM(total_cost / num_tools), 0) AS total_cost,
		       COALESCE(AVG(response_time_ms), 0) AS avg_response_time_ms
		FROM expanded
		GROUP BY model, tool_name, has_thinking, is_plan_mode
		ORDER BY total_cost DESC`

	var detailRows []*models.ClaudeSessionDetailRow
	if err := s.db.SelectContext(ctx, &detailRows, rowsQuery, sessionID); err != nil {
		return nil, err
	}

	return &models.ClaudeSessionDetail{
		Session: &session,
		Rows:    detailRows,
	}, nil
}

func (s *PostgresStorage) GetClaudeModelUsage(ctx context.Context, filter *UsageFilter) ([]*models.ClaudeModelUsage, error) {
	where, args := claudeBaseWhere(filter)
	where, args = appendClaudeMetadataFilters(where, args, filter.MetadataFilters)

	query := `
		SELECT
			lr.model,
			COUNT(*) AS request_count,
			COALESCE(SUM(lr.input_tokens), 0) AS input_tokens,
			COALESCE(SUM(lr.output_tokens), 0) AS output_tokens,
			COALESCE(SUM(lr.total_cost), 0) AS total_cost
		FROM claude_request_details crd
		JOIN llm_requests lr ON lr.id = crd.llm_request_id
		JOIN claude_sessions cs ON cs.id = crd.session_id
		JOIN api_keys ak ON ak.id = cs.majordomo_api_key_id
		WHERE ` + where + `
		GROUP BY lr.model
		ORDER BY total_cost DESC`

	var result []*models.ClaudeModelUsage
	if err := s.db.SelectContext(ctx, &result, query, args...); err != nil {
		return nil, err
	}
	return result, nil
}

