package storage

import (
    "context"
    "database/sql"
    "errors"
    "time"

    "github.com/google/uuid"
    "github.com/superset-studio/majordomo-gateway/internal/models"
)

// CreatePasswordResetToken inserts a new reset token for a user.
func (s *PostgresStorage) CreatePasswordResetToken(ctx context.Context, userID uuid.UUID, token string, expiresAt time.Time) (*models.PasswordResetToken, error) {
    query := `
        INSERT INTO password_reset_tokens (user_id, token, expires_at)
        VALUES ($1, $2, $3)
        RETURNING id, user_id, token, expires_at, used_at, created_at`

    var pr models.PasswordResetToken
    if err := s.db.QueryRowxContext(ctx, query, userID, token, expiresAt).StructScan(&pr); err != nil {
        return nil, err
    }
    return &pr, nil
}

// GetPasswordResetByToken returns a token row by token string.
func (s *PostgresStorage) GetPasswordResetByToken(ctx context.Context, token string) (*models.PasswordResetToken, error) {
    query := `SELECT id, user_id, token, expires_at, used_at, created_at FROM password_reset_tokens WHERE token = $1`
    var pr models.PasswordResetToken
    err := s.db.GetContext(ctx, &pr, query, token)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }
    return &pr, nil
}

// MarkPasswordResetUsed marks a token as used.
func (s *PostgresStorage) MarkPasswordResetUsed(ctx context.Context, id uuid.UUID) error {
    query := `UPDATE password_reset_tokens SET used_at = now() WHERE id = $1`
    _, err := s.db.ExecContext(ctx, query, id)
    return err
}

