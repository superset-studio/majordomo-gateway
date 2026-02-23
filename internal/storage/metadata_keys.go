package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

var ErrMetadataKeyNotFound = errors.New("metadata key not found")

const metadataKeyColumns = `mk.majordomo_api_key_id, mk.key_name, mk.display_name, mk.key_type, mk.is_required, mk.is_active, mk.activated_at, mk.request_count, mk.last_seen_at, mk.approx_cardinality, mk.created_at`

func (s *PostgresStorage) ListMetadataKeys(ctx context.Context, userID uuid.UUID) ([]*models.MetadataKey, error) {
	query := `
		SELECT ` + metadataKeyColumns + `
		FROM llm_requests_metadata_keys mk
		JOIN api_keys ak ON ak.id = mk.majordomo_api_key_id
		WHERE ak.user_id = $1
		ORDER BY mk.request_count DESC`

	var keys []*models.MetadataKey
	if err := s.db.SelectContext(ctx, &keys, query, userID); err != nil {
		return nil, fmt.Errorf("list metadata keys: %w", err)
	}
	return keys, nil
}

// ListActiveMetadataKeysByAPIKey returns active metadata keys for a specific API key.
func (s *PostgresStorage) ListActiveMetadataKeysByAPIKey(ctx context.Context, apiKeyID uuid.UUID) ([]string, error) {
	query := `SELECT key_name FROM llm_requests_metadata_keys WHERE majordomo_api_key_id = $1 AND is_active = true`

	var names []string
	if err := s.db.SelectContext(ctx, &names, query, apiKeyID); err != nil {
		return nil, fmt.Errorf("list active metadata keys: %w", err)
	}
	return names, nil
}

// ListAllActiveMetadataKeys returns all (api_key_id, key_name) pairs that are active.
func (s *PostgresStorage) ListAllActiveMetadataKeys(ctx context.Context) ([]struct {
	APIKeyID uuid.UUID `db:"majordomo_api_key_id"`
	KeyName  string    `db:"key_name"`
}, error) {
	query := `SELECT majordomo_api_key_id, key_name FROM llm_requests_metadata_keys WHERE is_active = true`

	var pairs []struct {
		APIKeyID uuid.UUID `db:"majordomo_api_key_id"`
		KeyName  string    `db:"key_name"`
	}
	if err := s.db.SelectContext(ctx, &pairs, query); err != nil {
		return nil, fmt.Errorf("list all active metadata keys: %w", err)
	}
	return pairs, nil
}

func (s *PostgresStorage) ActivateMetadataKey(ctx context.Context, apiKeyID uuid.UUID, keyName string) error {
	query := `UPDATE llm_requests_metadata_keys SET is_active = true, activated_at = NOW() WHERE majordomo_api_key_id = $1 AND key_name = $2`

	result, err := s.db.ExecContext(ctx, query, apiKeyID, keyName)
	if err != nil {
		return fmt.Errorf("activate metadata key: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("activate metadata key rows affected: %w", err)
	}
	if rows == 0 {
		return ErrMetadataKeyNotFound
	}

	s.activeKeyCache.InvalidateAPIKey(apiKeyID)
	return nil
}

func (s *PostgresStorage) DeactivateMetadataKey(ctx context.Context, apiKeyID uuid.UUID, keyName string) error {
	query := `UPDATE llm_requests_metadata_keys SET is_active = false WHERE majordomo_api_key_id = $1 AND key_name = $2`

	result, err := s.db.ExecContext(ctx, query, apiKeyID, keyName)
	if err != nil {
		return fmt.Errorf("deactivate metadata key: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("deactivate metadata key rows affected: %w", err)
	}
	if rows == 0 {
		return ErrMetadataKeyNotFound
	}

	s.activeKeyCache.InvalidateAPIKey(apiKeyID)
	return nil
}

// BackfillIndexedMetadata copies values for a single active key from raw_metadata
// into indexed_metadata, processing in batches. Reports progress via the logger.
// Returns the total number of rows updated.
func (s *PostgresStorage) BackfillIndexedMetadata(ctx context.Context, apiKeyID uuid.UUID, keyName string, batchSize int) (int64, error) {
	query := `
		UPDATE llm_requests
		SET indexed_metadata = COALESCE(indexed_metadata, '{}'::jsonb) || jsonb_build_object($2, raw_metadata->>$2)
		WHERE id IN (
			SELECT id FROM llm_requests
			WHERE majordomo_api_key_id = $1
				AND raw_metadata ? $2
				AND (indexed_metadata IS NULL OR NOT indexed_metadata ? $2)
			LIMIT $3
		)`

	total := int64(0)
	for {
		result, err := s.db.ExecContext(ctx, query, apiKeyID, keyName, batchSize)
		if err != nil {
			return total, fmt.Errorf("backfill batch for key %q: %w", keyName, err)
		}
		n, _ := result.RowsAffected()
		total += n
		if n > 0 {
			slog.Info("backfill indexed_metadata progress", "api_key_id", apiKeyID, "key", keyName, "batch", n, "total", total)
		}
		if n < int64(batchSize) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	return total, nil
}

// CleanupIndexedMetadata removes a key from indexed_metadata for all requests
// belonging to an API key, processing in batches. Returns total rows updated.
func (s *PostgresStorage) CleanupIndexedMetadata(ctx context.Context, apiKeyID uuid.UUID, keyName string, batchSize int) (int64, error) {
	query := `
		UPDATE llm_requests
		SET indexed_metadata = indexed_metadata - $2
		WHERE id IN (
			SELECT id FROM llm_requests
			WHERE majordomo_api_key_id = $1
				AND indexed_metadata ? $2
			LIMIT $3
		)`

	total := int64(0)
	for {
		result, err := s.db.ExecContext(ctx, query, apiKeyID, keyName, batchSize)
		if err != nil {
			return total, fmt.Errorf("cleanup batch for key %q: %w", keyName, err)
		}
		n, _ := result.RowsAffected()
		total += n
		if n > 0 {
			slog.Info("cleanup indexed_metadata progress", "api_key_id", apiKeyID, "key", keyName, "batch", n, "total", total)
		}
		if n < int64(batchSize) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	return total, nil
}

func (s *PostgresStorage) UpdateMetadataKeyDisplayName(ctx context.Context, apiKeyID uuid.UUID, keyName string, displayName *string) error {
	query := `UPDATE llm_requests_metadata_keys SET display_name = $3 WHERE majordomo_api_key_id = $1 AND key_name = $2`

	result, err := s.db.ExecContext(ctx, query, apiKeyID, keyName, displayName)
	if err != nil {
		return fmt.Errorf("update metadata key display name: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update metadata key display name rows affected: %w", err)
	}
	if rows == 0 {
		return ErrMetadataKeyNotFound
	}

	return nil
}
