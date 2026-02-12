package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

var (
	ErrProxyKeyNotFound      = errors.New("proxy key not found")
	ErrProviderMappingNotFound = errors.New("provider mapping not found")
)

// CreateProxyKey creates a new proxy key in the database
func (s *PostgresStorage) CreateProxyKey(ctx context.Context, keyHash string, majordomoKeyID uuid.UUID, input *models.CreateProxyKeyInput) (*models.ProxyKey, error) {
	query := `
		INSERT INTO proxy_keys (key_hash, name, description, majordomo_api_key_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, key_hash, name, description, majordomo_api_key_id, is_active, created_at, revoked_at, last_used_at, request_count`

	var key models.ProxyKey
	err := s.db.QueryRowxContext(ctx, query, keyHash, input.Name, input.Description, majordomoKeyID).StructScan(&key)
	if err != nil {
		return nil, err
	}

	return &key, nil
}

// GetProxyKeyByHash retrieves a proxy key by its hash
func (s *PostgresStorage) GetProxyKeyByHash(ctx context.Context, keyHash string) (*models.ProxyKey, error) {
	query := `
		SELECT id, key_hash, name, description, majordomo_api_key_id, is_active, created_at, revoked_at, last_used_at, request_count
		FROM proxy_keys
		WHERE key_hash = $1`

	var key models.ProxyKey
	err := s.db.GetContext(ctx, &key, query, keyHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &key, nil
}

// GetProxyKeyByID retrieves a proxy key by its UUID
func (s *PostgresStorage) GetProxyKeyByID(ctx context.Context, id uuid.UUID) (*models.ProxyKey, error) {
	query := `
		SELECT id, key_hash, name, description, majordomo_api_key_id, is_active, created_at, revoked_at, last_used_at, request_count
		FROM proxy_keys
		WHERE id = $1`

	var key models.ProxyKey
	err := s.db.GetContext(ctx, &key, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProxyKeyNotFound
	}
	if err != nil {
		return nil, err
	}

	return &key, nil
}

// ListProxyKeys retrieves all proxy keys for a given Majordomo API key
func (s *PostgresStorage) ListProxyKeys(ctx context.Context, majordomoKeyID uuid.UUID) ([]*models.ProxyKey, error) {
	query := `
		SELECT id, key_hash, name, description, majordomo_api_key_id, is_active, created_at, revoked_at, last_used_at, request_count
		FROM proxy_keys
		WHERE majordomo_api_key_id = $1
		ORDER BY created_at DESC`

	var keys []*models.ProxyKey
	err := s.db.SelectContext(ctx, &keys, query, majordomoKeyID)
	if err != nil {
		return nil, err
	}

	return keys, nil
}

// RevokeProxyKey marks a proxy key as revoked
func (s *PostgresStorage) RevokeProxyKey(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE proxy_keys
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
		return ErrProxyKeyNotFound
	}

	return nil
}

// UpdateProxyKeyLastUsed updates the last_used_at timestamp and increments request_count
func (s *PostgresStorage) UpdateProxyKeyLastUsed(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE proxy_keys
		SET last_used_at = $1, request_count = request_count + 1
		WHERE id = $2`

	_, err := s.db.ExecContext(ctx, query, time.Now(), id)
	return err
}

// SetProviderMapping creates or updates a provider mapping for a proxy key
func (s *PostgresStorage) SetProviderMapping(ctx context.Context, proxyKeyID uuid.UUID, provider string, encryptedKey string) error {
	query := `
		INSERT INTO proxy_key_provider_mappings (proxy_key_id, provider, encrypted_key)
		VALUES ($1, $2, $3)
		ON CONFLICT (proxy_key_id, provider) DO UPDATE
		SET encrypted_key = EXCLUDED.encrypted_key, updated_at = now()`

	_, err := s.db.ExecContext(ctx, query, proxyKeyID, provider, encryptedKey)
	return err
}

// GetProviderMapping retrieves a provider mapping for a proxy key and provider
func (s *PostgresStorage) GetProviderMapping(ctx context.Context, proxyKeyID uuid.UUID, provider string) (*models.ProviderMapping, error) {
	query := `
		SELECT id, proxy_key_id, provider, encrypted_key, created_at, updated_at
		FROM proxy_key_provider_mappings
		WHERE proxy_key_id = $1 AND provider = $2`

	var mapping models.ProviderMapping
	err := s.db.GetContext(ctx, &mapping, query, proxyKeyID, provider)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &mapping, nil
}

// ListProviderMappings retrieves all provider mappings for a proxy key
func (s *PostgresStorage) ListProviderMappings(ctx context.Context, proxyKeyID uuid.UUID) ([]*models.ProviderMapping, error) {
	query := `
		SELECT id, proxy_key_id, provider, encrypted_key, created_at, updated_at
		FROM proxy_key_provider_mappings
		WHERE proxy_key_id = $1
		ORDER BY provider`

	var mappings []*models.ProviderMapping
	err := s.db.SelectContext(ctx, &mappings, query, proxyKeyID)
	if err != nil {
		return nil, err
	}

	return mappings, nil
}

// DeleteProviderMapping removes a provider mapping for a proxy key
func (s *PostgresStorage) DeleteProviderMapping(ctx context.Context, proxyKeyID uuid.UUID, provider string) error {
	query := `
		DELETE FROM proxy_key_provider_mappings
		WHERE proxy_key_id = $1 AND provider = $2`

	result, err := s.db.ExecContext(ctx, query, proxyKeyID, provider)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrProviderMappingNotFound
	}

	return nil
}
