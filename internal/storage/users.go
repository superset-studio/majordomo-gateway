package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserNotFound = errors.New("user not found")
)

const userColumns = `id, username, password_hash, email, auth_provider, auth_provider_id, is_active, created_at, s3_bucket, s3_region, s3_endpoint, s3_access_key_id_encrypted, s3_secret_access_key_encrypted`

// CreateUser creates a new user with a bcrypt-hashed password
func (s *PostgresStorage) CreateUser(ctx context.Context, input *models.CreateUserInput) (*models.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), 12)
	if err != nil {
		return nil, err
	}

	query := `
		INSERT INTO users (username, password_hash)
		VALUES ($1, $2)
		RETURNING ` + userColumns

	var user models.User
	err = s.db.QueryRowxContext(ctx, query, input.Username, string(hash)).StructScan(&user)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetUserByID retrieves a user by their UUID
func (s *PostgresStorage) GetUserByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	query := `SELECT ` + userColumns + ` FROM users WHERE id = $1`

	var user models.User
	err := s.db.GetContext(ctx, &user, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetUserByUsername retrieves a user by their username
func (s *PostgresStorage) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	query := `SELECT ` + userColumns + ` FROM users WHERE username = $1`

	var user models.User
	err := s.db.GetContext(ctx, &user, query, username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetUserByAuthProvider retrieves a user by their OAuth provider and provider ID
func (s *PostgresStorage) GetUserByAuthProvider(ctx context.Context, provider, providerID string) (*models.User, error) {
	query := `SELECT ` + userColumns + ` FROM users WHERE auth_provider = $1 AND auth_provider_id = $2`

	var user models.User
	err := s.db.GetContext(ctx, &user, query, provider, providerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// CreateOAuthUser creates a new user from an OAuth provider (no password)
func (s *PostgresStorage) CreateOAuthUser(ctx context.Context, input *models.CreateOAuthUserInput) (*models.User, error) {
	query := `
		INSERT INTO users (username, email, auth_provider, auth_provider_id)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + userColumns

	var user models.User
	err := s.db.QueryRowxContext(ctx, query, input.Username, input.Email, input.AuthProvider, input.AuthProviderID).StructScan(&user)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// ListUsers retrieves all users
func (s *PostgresStorage) ListUsers(ctx context.Context) ([]*models.User, error) {
	query := `SELECT ` + userColumns + ` FROM users ORDER BY created_at DESC`

	var users []*models.User
	err := s.db.SelectContext(ctx, &users, query)
	if err != nil {
		return nil, err
	}

	return users, nil
}

// UpdateUserS3Config sets S3 body storage configuration for a user.
func (s *PostgresStorage) UpdateUserS3Config(ctx context.Context, userID uuid.UUID, bucket, region, endpoint, encAccessKeyID, encSecretAccessKey string) error {
	query := `
		UPDATE users
		SET s3_bucket = $1, s3_region = $2, s3_endpoint = $3,
			s3_access_key_id_encrypted = $4, s3_secret_access_key_encrypted = $5
		WHERE id = $6`

	result, err := s.db.ExecContext(ctx, query, bucket, region, endpoint, encAccessKeyID, encSecretAccessKey, userID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ClearUserS3Config removes S3 body storage configuration for a user.
func (s *PostgresStorage) ClearUserS3Config(ctx context.Context, userID uuid.UUID) error {
	query := `
		UPDATE users
		SET s3_bucket = NULL, s3_region = 'us-east-1', s3_endpoint = NULL,
			s3_access_key_id_encrypted = NULL, s3_secret_access_key_encrypted = NULL
		WHERE id = $1`

	result, err := s.db.ExecContext(ctx, query, userID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// GetUserS3Config retrieves S3 configuration columns for a user.
func (s *PostgresStorage) GetUserS3Config(ctx context.Context, userID uuid.UUID) (*models.User, error) {
	query := `SELECT s3_bucket, s3_region, s3_endpoint, s3_access_key_id_encrypted, s3_secret_access_key_encrypted FROM users WHERE id = $1`

	var user models.User
	err := s.db.GetContext(ctx, &user, query, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// UpdateUserPassword updates a user's password hash
func (s *PostgresStorage) UpdateUserPassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	query := `
		UPDATE users
		SET password_hash = $1
		WHERE id = $2`

	result, err := s.db.ExecContext(ctx, query, passwordHash, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrUserNotFound
	}

	return nil
}
