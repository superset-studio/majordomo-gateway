package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

var ErrProviderKeyNotFound = errors.New("provider API key not found")

// SetProviderKey upserts a provider API key for a user or org.
func (s *PostgresStorage) SetProviderKey(ctx context.Context, userID *uuid.UUID, orgID *uuid.UUID, provider string, encryptedKey string) error {
	if userID != nil {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO provider_api_keys (user_id, provider, encrypted_key, updated_at)
			VALUES ($1, $2, $3, now())
			ON CONFLICT (user_id, provider) WHERE user_id IS NOT NULL
			DO UPDATE SET encrypted_key = $3, updated_at = now()`,
			*userID, provider, encryptedKey)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_api_keys (org_id, provider, encrypted_key, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (org_id, provider) WHERE org_id IS NOT NULL
		DO UPDATE SET encrypted_key = $3, updated_at = now()`,
		*orgID, provider, encryptedKey)
	return err
}

// ListProviderKeys returns the configured providers (without keys) for a user or org.
func (s *PostgresStorage) ListProviderKeys(ctx context.Context, userID *uuid.UUID, orgID *uuid.UUID) ([]*models.ProviderKeyInfo, error) {
	var query string
	var arg interface{}
	if orgID != nil {
		query = `SELECT provider, created_at FROM provider_api_keys WHERE org_id = $1 ORDER BY provider`
		arg = *orgID
	} else {
		query = `SELECT provider, created_at FROM provider_api_keys WHERE user_id = $1 ORDER BY provider`
		arg = *userID
	}

	var items []*models.ProviderKeyInfo
	if err := s.db.SelectContext(ctx, &items, query, arg); err != nil {
		return nil, err
	}
	return items, nil
}

// GetProviderKey returns the full provider key record (including encrypted key).
func (s *PostgresStorage) GetProviderKey(ctx context.Context, userID *uuid.UUID, orgID *uuid.UUID, provider string) (*models.ProviderAPIKey, error) {
	var query string
	var args []interface{}
	if orgID != nil {
		query = `SELECT id, user_id, org_id, provider, encrypted_key, created_at, updated_at
			FROM provider_api_keys WHERE org_id = $1 AND provider = $2`
		args = []interface{}{*orgID, provider}
	} else {
		query = `SELECT id, user_id, org_id, provider, encrypted_key, created_at, updated_at
			FROM provider_api_keys WHERE user_id = $1 AND provider = $2`
		args = []interface{}{*userID, provider}
	}

	var key models.ProviderAPIKey
	if err := s.db.GetContext(ctx, &key, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrProviderKeyNotFound
		}
		return nil, err
	}
	return &key, nil
}

// DeleteProviderKey removes a provider API key for a user or org.
func (s *PostgresStorage) DeleteProviderKey(ctx context.Context, userID *uuid.UUID, orgID *uuid.UUID, provider string) error {
	var query string
	var args []interface{}
	if orgID != nil {
		query = `DELETE FROM provider_api_keys WHERE org_id = $1 AND provider = $2`
		args = []interface{}{*orgID, provider}
	} else {
		query = `DELETE FROM provider_api_keys WHERE user_id = $1 AND provider = $2`
		args = []interface{}{*userID, provider}
	}
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}
