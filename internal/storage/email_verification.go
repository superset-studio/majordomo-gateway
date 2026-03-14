package storage

import (
    "context"
    "database/sql"
    "errors"
    "time"

    "github.com/google/uuid"
    "github.com/superset-studio/majordomo-gateway/internal/models"
)

// CreateEmailVerificationToken inserts a new email verification token for a user.
func (s *PostgresStorage) CreateEmailVerificationToken(ctx context.Context, userID uuid.UUID, token string, expiresAt time.Time) (*models.EmailVerificationToken, error) {
    query := `
        INSERT INTO email_verification_tokens (user_id, token, expires_at)
        VALUES ($1, $2, $3)
        RETURNING id, user_id, token, expires_at, used_at, created_at`

    var vt models.EmailVerificationToken
    if err := s.db.QueryRowxContext(ctx, query, userID, token, expiresAt).StructScan(&vt); err != nil {
        return nil, err
    }
    return &vt, nil
}

// GetEmailVerificationByToken returns a verification token row by token string.
func (s *PostgresStorage) GetEmailVerificationByToken(ctx context.Context, token string) (*models.EmailVerificationToken, error) {
    query := `SELECT id, user_id, token, expires_at, used_at, created_at FROM email_verification_tokens WHERE token = $1`
    var vt models.EmailVerificationToken
    err := s.db.GetContext(ctx, &vt, query, token)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }
    return &vt, nil
}

// MarkEmailVerificationUsed marks a verification token as used.
func (s *PostgresStorage) MarkEmailVerificationUsed(ctx context.Context, id uuid.UUID) error {
    query := `UPDATE email_verification_tokens SET used_at = now() WHERE id = $1`
    _, err := s.db.ExecContext(ctx, query, id)
    return err
}
