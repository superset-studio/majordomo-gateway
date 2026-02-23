package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

var ErrRequestNotFound = errors.New("request not found")

const requestListItemColumns = `id, majordomo_api_key_id, provider, model, requested_at, response_time_ms, input_tokens, output_tokens, total_cost, status_code, error_message`

// appendMetadataFilters appends AND indexed_metadata->>$N = $M clauses for each metadata filter.
func appendMetadataFilters(query string, args []interface{}, filters []MetadataFilter) (string, []interface{}) {
	for _, f := range filters {
		query += fmt.Sprintf(` AND indexed_metadata->>$%d = $%d`, len(args)+1, len(args)+2)
		args = append(args, f.Key, f.Value)
	}
	return query, args
}

func (s *PostgresStorage) GetUsageSummary(ctx context.Context, filter *UsageFilter) (*models.UsageSummary, error) {
	query := `
		SELECT
			COUNT(*) AS total_requests,
			COALESCE(SUM(input_tokens), 0) AS total_input_tokens,
			COALESCE(SUM(output_tokens), 0) AS total_output_tokens,
			COALESCE(SUM(total_cost), 0) AS total_cost
		FROM llm_requests
		WHERE user_id = $1 AND requested_at >= $2 AND requested_at < $3`

	args := []interface{}{filter.UserID, filter.Start, filter.End}
	query, args = appendMetadataFilters(query, args, filter.MetadataFilters)

	var summary struct {
		TotalRequests     int64   `db:"total_requests"`
		TotalInputTokens  int64   `db:"total_input_tokens"`
		TotalOutputTokens int64   `db:"total_output_tokens"`
		TotalCost         float64 `db:"total_cost"`
	}
	if err := s.db.GetContext(ctx, &summary, query, args...); err != nil {
		return nil, err
	}

	return &models.UsageSummary{
		TotalRequests:     summary.TotalRequests,
		TotalInputTokens:  summary.TotalInputTokens,
		TotalOutputTokens: summary.TotalOutputTokens,
		TotalCost:         summary.TotalCost,
	}, nil
}

func (s *PostgresStorage) GetDailyUsage(ctx context.Context, filter *UsageFilter) ([]*models.DailyUsage, error) {
	query := `
		SELECT
			TO_CHAR(DATE_TRUNC('day', requested_at), 'YYYY-MM-DD') AS date,
			COUNT(*) AS request_count,
			COALESCE(SUM(input_tokens), 0) AS input_tokens,
			COALESCE(SUM(output_tokens), 0) AS output_tokens,
			COALESCE(SUM(total_cost), 0) AS total_cost
		FROM llm_requests
		WHERE user_id = $1 AND requested_at >= $2 AND requested_at < $3
			AND ($4::uuid IS NULL OR majordomo_api_key_id = $4)`

	args := []interface{}{filter.UserID, filter.Start, filter.End, filter.APIKeyID}
	query, args = appendMetadataFilters(query, args, filter.MetadataFilters)
	query += `
		GROUP BY DATE_TRUNC('day', requested_at)
		ORDER BY date`

	type row struct {
		Date         string  `db:"date"`
		RequestCount int64   `db:"request_count"`
		InputTokens  int64   `db:"input_tokens"`
		OutputTokens int64   `db:"output_tokens"`
		TotalCost    float64 `db:"total_cost"`
	}

	var rows []row
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	result := make([]*models.DailyUsage, len(rows))
	for i, r := range rows {
		result[i] = &models.DailyUsage{
			Date:         r.Date,
			RequestCount: r.RequestCount,
			InputTokens:  r.InputTokens,
			OutputTokens: r.OutputTokens,
			TotalCost:    r.TotalCost,
		}
	}
	return result, nil
}

func (s *PostgresStorage) GetModelBreakdown(ctx context.Context, filter *UsageFilter) ([]*models.ModelUsage, error) {
	query := `
		SELECT
			provider,
			model,
			COUNT(*) AS request_count,
			COALESCE(SUM(input_tokens), 0) AS input_tokens,
			COALESCE(SUM(output_tokens), 0) AS output_tokens,
			COALESCE(SUM(total_cost), 0) AS total_cost
		FROM llm_requests
		WHERE user_id = $1 AND requested_at >= $2 AND requested_at < $3
			AND ($4::uuid IS NULL OR majordomo_api_key_id = $4)`

	args := []interface{}{filter.UserID, filter.Start, filter.End, filter.APIKeyID}
	query, args = appendMetadataFilters(query, args, filter.MetadataFilters)
	query += `
		GROUP BY provider, model
		ORDER BY total_cost DESC`

	type row struct {
		Provider     string  `db:"provider"`
		Model        string  `db:"model"`
		RequestCount int64   `db:"request_count"`
		InputTokens  int64   `db:"input_tokens"`
		OutputTokens int64   `db:"output_tokens"`
		TotalCost    float64 `db:"total_cost"`
	}

	var rows []row
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	result := make([]*models.ModelUsage, len(rows))
	for i, r := range rows {
		result[i] = &models.ModelUsage{
			Provider:     r.Provider,
			Model:        r.Model,
			RequestCount: r.RequestCount,
			InputTokens:  r.InputTokens,
			OutputTokens: r.OutputTokens,
			TotalCost:    r.TotalCost,
		}
	}
	return result, nil
}

func (s *PostgresStorage) GetAPIKeyBreakdown(ctx context.Context, filter *UsageFilter) ([]*models.APIKeyUsage, error) {
	query := `
		SELECT
			lr.majordomo_api_key_id AS api_key_id,
			ak.name AS api_key_name,
			COUNT(*) AS request_count,
			COALESCE(SUM(lr.input_tokens), 0) AS input_tokens,
			COALESCE(SUM(lr.output_tokens), 0) AS output_tokens,
			COALESCE(SUM(lr.total_cost), 0) AS total_cost
		FROM llm_requests lr
		JOIN api_keys ak ON ak.id = lr.majordomo_api_key_id
		WHERE lr.user_id = $1 AND lr.requested_at >= $2 AND lr.requested_at < $3`

	args := []interface{}{filter.UserID, filter.Start, filter.End}
	// Metadata filters use non-aliased column since it's on llm_requests
	for _, f := range filter.MetadataFilters {
		query += fmt.Sprintf(` AND lr.indexed_metadata->>$%d = $%d`, len(args)+1, len(args)+2)
		args = append(args, f.Key, f.Value)
	}
	query += `
		GROUP BY lr.majordomo_api_key_id, ak.name
		ORDER BY total_cost DESC`

	type row struct {
		APIKeyID     uuid.UUID `db:"api_key_id"`
		APIKeyName   string    `db:"api_key_name"`
		RequestCount int64     `db:"request_count"`
		InputTokens  int64     `db:"input_tokens"`
		OutputTokens int64     `db:"output_tokens"`
		TotalCost    float64   `db:"total_cost"`
	}

	var rows []row
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	result := make([]*models.APIKeyUsage, len(rows))
	for i, r := range rows {
		result[i] = &models.APIKeyUsage{
			APIKeyID:     r.APIKeyID,
			APIKeyName:   r.APIKeyName,
			RequestCount: r.RequestCount,
			InputTokens:  r.InputTokens,
			OutputTokens: r.OutputTokens,
			TotalCost:    r.TotalCost,
		}
	}
	return result, nil
}

func (s *PostgresStorage) ListUsageRequests(ctx context.Context, filter *UsageFilter, limit, offset int) ([]*models.RequestListItem, int, error) {
	countQuery := `
		SELECT COUNT(*)
		FROM llm_requests
		WHERE user_id = $1 AND requested_at >= $2 AND requested_at < $3
			AND ($4::uuid IS NULL OR majordomo_api_key_id = $4)`

	countArgs := []interface{}{filter.UserID, filter.Start, filter.End, filter.APIKeyID}
	countQuery, countArgs = appendMetadataFilters(countQuery, countArgs, filter.MetadataFilters)

	var total int
	if err := s.db.GetContext(ctx, &total, countQuery, countArgs...); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT ` + requestListItemColumns + `
		FROM llm_requests
		WHERE user_id = $1 AND requested_at >= $2 AND requested_at < $3
			AND ($4::uuid IS NULL OR majordomo_api_key_id = $4)`

	args := []interface{}{filter.UserID, filter.Start, filter.End, filter.APIKeyID}
	query, args = appendMetadataFilters(query, args, filter.MetadataFilters)
	query += fmt.Sprintf(`
		ORDER BY requested_at DESC
		LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	var items []*models.RequestListItem
	if err := s.db.SelectContext(ctx, &items, query, args...); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *PostgresStorage) GetMetadataBreakdown(ctx context.Context, filter *UsageFilter, keyName string) ([]*models.MetadataBreakdown, error) {
	query := `
		SELECT
			indexed_metadata->>$4 AS value,
			COUNT(*) AS request_count,
			COALESCE(SUM(input_tokens), 0) AS input_tokens,
			COALESCE(SUM(output_tokens), 0) AS output_tokens,
			COALESCE(SUM(total_cost), 0) AS total_cost
		FROM llm_requests
		WHERE user_id = $1 AND requested_at >= $2 AND requested_at < $3
			AND ($5::uuid IS NULL OR majordomo_api_key_id = $5)
			AND indexed_metadata ? $4`

	args := []interface{}{filter.UserID, filter.Start, filter.End, keyName, filter.APIKeyID}
	query, args = appendMetadataFilters(query, args, filter.MetadataFilters)
	query += fmt.Sprintf(`
		GROUP BY indexed_metadata->>$4
		ORDER BY total_cost DESC`)

	type row struct {
		Value        string  `db:"value"`
		RequestCount int64   `db:"request_count"`
		InputTokens  int64   `db:"input_tokens"`
		OutputTokens int64   `db:"output_tokens"`
		TotalCost    float64 `db:"total_cost"`
	}

	var rows []row
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	result := make([]*models.MetadataBreakdown, len(rows))
	for i, r := range rows {
		result[i] = &models.MetadataBreakdown{
			Value:        r.Value,
			RequestCount: r.RequestCount,
			InputTokens:  r.InputTokens,
			OutputTokens: r.OutputTokens,
			TotalCost:    r.TotalCost,
		}
	}
	return result, nil
}

const requestLogColumns = `id, user_id, majordomo_api_key_id, proxy_key_id, provider_api_key_hash, provider_api_key_alias,
	provider, model, request_path, request_method,
	requested_at, responded_at, response_time_ms,
	input_tokens, output_tokens, cached_tokens, cache_creation_tokens,
	input_cost, output_cost, total_cost,
	status_code, error_message, raw_metadata, indexed_metadata,
	request_body, response_body, body_s3_key, model_alias_found, created_at, body_storage_key`

func (s *PostgresStorage) GetRequestDetail(ctx context.Context, requestID uuid.UUID, userID uuid.UUID) (*models.RequestLog, error) {
	query := `SELECT ` + requestLogColumns + ` FROM llm_requests WHERE id = $1 AND user_id = $2`

	sqlRow := s.db.QueryRowxContext(ctx, query, requestID, userID)

	var (
		id                  uuid.UUID
		dbUserID            *uuid.UUID
		majordomoAPIKeyID   *uuid.UUID
		proxyKeyID          *uuid.UUID
		providerAPIKeyHash  *string
		providerAPIKeyAlias *string
		provider            string
		model               string
		requestPath         string
		requestMethod       string
		requestedAt         time.Time
		respondedAt         time.Time
		responseTimeMs      int64
		inputTokens         int
		outputTokens        int
		cachedTokens        int
		cacheCreationTokens int
		inputCost           float64
		outputCost          float64
		totalCost           float64
		statusCode          int
		errorMessage        *string
		rawMetadataJSON     []byte
		indexedMetadataJSON []byte
		requestBody         *string
		responseBody        *string
		bodyS3Key           *string
		modelAliasFound     bool
		createdAt           time.Time
		bodyStorageKey      *string
	)

	err := sqlRow.Scan(
		&id, &dbUserID, &majordomoAPIKeyID, &proxyKeyID, &providerAPIKeyHash, &providerAPIKeyAlias,
		&provider, &model, &requestPath, &requestMethod,
		&requestedAt, &respondedAt, &responseTimeMs,
		&inputTokens, &outputTokens, &cachedTokens, &cacheCreationTokens,
		&inputCost, &outputCost, &totalCost,
		&statusCode, &errorMessage, &rawMetadataJSON, &indexedMetadataJSON,
		&requestBody, &responseBody, &bodyS3Key, &modelAliasFound, &createdAt, &bodyStorageKey,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRequestNotFound
		}
		return nil, err
	}

	result := &models.RequestLog{
		ID:                  id,
		UserID:              dbUserID,
		MajordomoAPIKeyID:   majordomoAPIKeyID,
		ProxyKeyID:          proxyKeyID,
		ProviderAPIKeyHash:  providerAPIKeyHash,
		ProviderAPIKeyAlias: providerAPIKeyAlias,
		Provider:            provider,
		Model:               model,
		RequestPath:         requestPath,
		RequestMethod:       requestMethod,
		RequestedAt:         requestedAt,
		RespondedAt:         respondedAt,
		ResponseTimeMs:      responseTimeMs,
		InputTokens:         inputTokens,
		OutputTokens:        outputTokens,
		CachedTokens:        cachedTokens,
		CacheCreationTokens: cacheCreationTokens,
		InputCost:           inputCost,
		OutputCost:          outputCost,
		TotalCost:           totalCost,
		StatusCode:          statusCode,
		ErrorMessage:        errorMessage,
		RequestBody:         requestBody,
		ResponseBody:        responseBody,
		BodyS3Key:           bodyS3Key,
		BodyStorageKey:      bodyStorageKey,
		ModelAliasFound:     modelAliasFound,
		CreatedAt:           createdAt,
	}

	if rawMetadataJSON != nil {
		result.RawMetadata = make(map[string]string)
		json.Unmarshal(rawMetadataJSON, &result.RawMetadata)
	}
	if indexedMetadataJSON != nil {
		result.IndexedMetadata = make(map[string]string)
		json.Unmarshal(indexedMetadataJSON, &result.IndexedMetadata)
	}

	return result, nil
}
