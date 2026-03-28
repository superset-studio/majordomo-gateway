package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/superset-studio/majordomo-gateway/internal/models"
)

// CreateWaitlistEntry inserts a new waitlist entry. Returns the entry and
// whether it was newly created (true) or already existed (false).
func (s *PostgresStorage) CreateWaitlistEntry(ctx context.Context, email string, source *string) (*models.WaitlistEntry, bool, error) {
	query := `
		INSERT INTO waitlist_entries (email, source)
		VALUES ($1, $2)
		ON CONFLICT (email) DO NOTHING
		RETURNING id, email, source, created_at`

	var entry models.WaitlistEntry
	err := s.db.QueryRowxContext(ctx, query, email, source).StructScan(&entry)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Already exists — fetch and return it
			existing, fetchErr := s.GetWaitlistEntryByEmail(ctx, email)
			if fetchErr != nil {
				return nil, false, fetchErr
			}
			return existing, false, nil
		}
		return nil, false, err
	}
	return &entry, true, nil
}

// GetWaitlistEntryByEmail returns a waitlist entry by email address.
func (s *PostgresStorage) GetWaitlistEntryByEmail(ctx context.Context, email string) (*models.WaitlistEntry, error) {
	query := `SELECT id, email, source, created_at FROM waitlist_entries WHERE email = $1`
	var entry models.WaitlistEntry
	err := s.db.GetContext(ctx, &entry, query, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &entry, nil
}
