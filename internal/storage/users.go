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

// CreateUser creates a new user with a bcrypt-hashed password
func (s *PostgresStorage) CreateUser(ctx context.Context, input *models.CreateUserInput) (*models.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), 12)
	if err != nil {
		return nil, err
	}

	query := `
		INSERT INTO users (username, password_hash)
		VALUES ($1, $2)
		RETURNING id, username, password_hash, is_active, created_at`

	var user models.User
	err = s.db.QueryRowxContext(ctx, query, input.Username, string(hash)).StructScan(&user)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetUserByID retrieves a user by their UUID
func (s *PostgresStorage) GetUserByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	query := `
		SELECT id, username, password_hash, is_active, created_at
		FROM users
		WHERE id = $1`

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
	query := `
		SELECT id, username, password_hash, is_active, created_at
		FROM users
		WHERE username = $1`

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

// ListUsers retrieves all users
func (s *PostgresStorage) ListUsers(ctx context.Context) ([]*models.User, error) {
	query := `
		SELECT id, username, password_hash, is_active, created_at
		FROM users
		ORDER BY created_at DESC`

	var users []*models.User
	err := s.db.SelectContext(ctx, &users, query)
	if err != nil {
		return nil, err
	}

	return users, nil
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
