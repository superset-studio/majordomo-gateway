package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

var (
	ErrAPIKeyNotFound = errors.New("API key not found")
)

// CreateAPIKey creates a new API key in the database
func (s *PostgresStorage) CreateAPIKey(ctx context.Context, keyHash string, input *models.CreateAPIKeyInput) (*models.APIKey, error) {
	query := `
		INSERT INTO api_keys (key_hash, name, description)
		VALUES ($1, $2, $3)
		RETURNING id, key_hash, name, description, is_active, created_at, revoked_at, last_used_at, request_count`

	var key models.APIKey
	err := s.db.QueryRowxContext(ctx, query, keyHash, input.Name, input.Description).StructScan(&key)
	if err != nil {
		return nil, err
	}

	return &key, nil
}

// GetAPIKeyByHash retrieves an API key by its hash
func (s *PostgresStorage) GetAPIKeyByHash(ctx context.Context, keyHash string) (*models.APIKey, error) {
	query := `
		SELECT id, key_hash, name, description, is_active, created_at, revoked_at, last_used_at, request_count
		FROM api_keys
		WHERE key_hash = $1`

	var key models.APIKey
	err := s.db.GetContext(ctx, &key, query, keyHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // Key not found
	}
	if err != nil {
		return nil, err
	}

	return &key, nil
}

// GetAPIKeyByID retrieves an API key by its UUID
func (s *PostgresStorage) GetAPIKeyByID(ctx context.Context, id uuid.UUID) (*models.APIKey, error) {
	query := `
		SELECT id, key_hash, name, description, is_active, created_at, revoked_at, last_used_at, request_count
		FROM api_keys
		WHERE id = $1`

	var key models.APIKey
	err := s.db.GetContext(ctx, &key, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}

	return &key, nil
}

// ListAPIKeys retrieves all API keys
func (s *PostgresStorage) ListAPIKeys(ctx context.Context) ([]*models.APIKey, error) {
	query := `
		SELECT id, key_hash, name, description, is_active, created_at, revoked_at, last_used_at, request_count
		FROM api_keys
		ORDER BY created_at DESC`

	var keys []*models.APIKey
	err := s.db.SelectContext(ctx, &keys, query)
	if err != nil {
		return nil, err
	}

	return keys, nil
}

// UpdateAPIKey updates an API key's name and/or description
func (s *PostgresStorage) UpdateAPIKey(ctx context.Context, id uuid.UUID, input *models.UpdateAPIKeyInput) (*models.APIKey, error) {
	// Build dynamic update query
	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	if input.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *input.Name)
		argIdx++
	}

	if input.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, *input.Description)
		argIdx++
	}

	if len(setClauses) == 0 {
		// Nothing to update, just return current state
		return s.GetAPIKeyByID(ctx, id)
	}

	// Build query string manually since we have dynamic columns
	query := "UPDATE api_keys SET "
	for i, clause := range setClauses {
		if i > 0 {
			query += ", "
		}
		query += clause
	}
	query += fmt.Sprintf(" WHERE id = $%d", argIdx)
	query += " RETURNING id, key_hash, name, description, is_active, created_at, revoked_at, last_used_at, request_count"
	args = append(args, id)

	var key models.APIKey
	err := s.db.QueryRowxContext(ctx, query, args...).StructScan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}

	return &key, nil
}

// RevokeAPIKey marks an API key as revoked
func (s *PostgresStorage) RevokeAPIKey(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE api_keys
		SET is_active = false, revoked_at = $1
		WHERE id = $2 AND is_active = true`

	result, err := s.db.ExecContext(ctx, query, time.Now(), id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrAPIKeyNotFound
	}

	return nil
}

// UpdateAPIKeyLastUsed updates the last_used_at timestamp and increments request_count
func (s *PostgresStorage) UpdateAPIKeyLastUsed(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE api_keys
		SET last_used_at = $1, request_count = request_count + 1
		WHERE id = $2`

	_, err := s.db.ExecContext(ctx, query, time.Now(), id)
	return err
}
