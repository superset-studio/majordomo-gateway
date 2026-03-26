package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrOrgNotFound     = errors.New("organization not found")
	ErrMemberNotFound  = errors.New("member not found")
	ErrInviteNotFound  = errors.New("invite not found")
	ErrInviteExpired   = errors.New("invite has expired")
	ErrAlreadyMember   = errors.New("user is already a member")
	ErrDuplicateInvite = errors.New("invite already exists for this email")
)

const orgColumns = `id, name, slug, s3_bucket, s3_region, s3_endpoint, s3_access_key_id_encrypted, s3_secret_access_key_encrypted, created_at`

const orgMemberColumns = `org_id, user_id, role, created_at`

const orgInviteColumns = `id, org_id, email, role, token, invited_by, expires_at, accepted_at, created_at`

// CreateOrganizationWithUser creates a new user, organization, and admin membership in a single transaction.
func (s *PostgresStorage) CreateOrganizationWithUser(ctx context.Context, orgInput *models.CreateOrganizationInput, userInput *models.CreateUserInput) (*models.User, *models.Organization, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Create user
	hash, err := bcrypt.GenerateFromPassword([]byte(userInput.Password), 12)
	if err != nil {
		return nil, nil, fmt.Errorf("hash password: %w", err)
	}

	var user models.User
	err = tx.QueryRowxContext(ctx, `
		INSERT INTO users (username, email, password_hash)
		VALUES ($1, $2, $3)
		RETURNING `+userColumns, userInput.Username, userInput.Email, string(hash)).StructScan(&user)
	if err != nil {
		return nil, nil, fmt.Errorf("insert user: %w", err)
	}

	// Create organization
	var org models.Organization
	err = tx.QueryRowxContext(ctx, `
		INSERT INTO organizations (name, slug)
		VALUES ($1, $2)
		RETURNING `+orgColumns, orgInput.Name, orgInput.Slug).StructScan(&org)
	if err != nil {
		return nil, nil, fmt.Errorf("insert org: %w", err)
	}

	// Add user as admin member
	_, err = tx.ExecContext(ctx, `
		INSERT INTO organization_members (org_id, user_id, role)
		VALUES ($1, $2, 'admin')`, org.ID, user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("insert member: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit: %w", err)
	}

	return &user, &org, nil
}

// CreateOrganization creates a new organization and adds the creator as an admin member.
func (s *PostgresStorage) CreateOrganization(ctx context.Context, input *models.CreateOrganizationInput, creatorUserID uuid.UUID) (*models.Organization, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var org models.Organization
	err = tx.QueryRowxContext(ctx, `
		INSERT INTO organizations (name, slug)
		VALUES ($1, $2)
		RETURNING `+orgColumns, input.Name, input.Slug).StructScan(&org)
	if err != nil {
		return nil, fmt.Errorf("insert org: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO organization_members (org_id, user_id, role)
		VALUES ($1, $2, 'admin')`, org.ID, creatorUserID)
	if err != nil {
		return nil, fmt.Errorf("insert member: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &org, nil
}

// GetOrganizationByID retrieves an organization by its UUID.
func (s *PostgresStorage) GetOrganizationByID(ctx context.Context, id uuid.UUID) (*models.Organization, error) {
	query := `SELECT ` + orgColumns + ` FROM organizations WHERE id = $1`

	var org models.Organization
	err := s.db.GetContext(ctx, &org, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOrgNotFound
	}
	if err != nil {
		return nil, err
	}

	return &org, nil
}

// GetOrganizationBySlug retrieves an organization by its slug.
func (s *PostgresStorage) GetOrganizationBySlug(ctx context.Context, slug string) (*models.Organization, error) {
	query := `SELECT ` + orgColumns + ` FROM organizations WHERE slug = $1`

	var org models.Organization
	err := s.db.GetContext(ctx, &org, query, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOrgNotFound
	}
	if err != nil {
		return nil, err
	}

	return &org, nil
}

// UpdateOrganization updates an organization's name and/or slug.
func (s *PostgresStorage) UpdateOrganization(ctx context.Context, id uuid.UUID, input *models.UpdateOrganizationInput) (*models.Organization, error) {
	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	if input.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *input.Name)
		argIdx++
	}

	if input.Slug != nil {
		setClauses = append(setClauses, fmt.Sprintf("slug = $%d", argIdx))
		args = append(args, *input.Slug)
		argIdx++
	}

	if len(setClauses) == 0 {
		return s.GetOrganizationByID(ctx, id)
	}

	query := "UPDATE organizations SET "
	for i, clause := range setClauses {
		if i > 0 {
			query += ", "
		}
		query += clause
	}
	query += fmt.Sprintf(" WHERE id = $%d RETURNING ", argIdx) + orgColumns
	args = append(args, id)

	var org models.Organization
	err := s.db.QueryRowxContext(ctx, query, args...).StructScan(&org)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOrgNotFound
	}
	if err != nil {
		return nil, err
	}

	return &org, nil
}

// AddMember adds a user to an organization with the given role.
func (s *PostgresStorage) AddMember(ctx context.Context, orgID, userID uuid.UUID, role string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO organization_members (org_id, user_id, role)
		VALUES ($1, $2, $3)`, orgID, userID, role)
	if err != nil {
		if isDuplicateKeyError(err) {
			return ErrAlreadyMember
		}
		return err
	}
	return nil
}

// RemoveMember removes a user from an organization.
func (s *PostgresStorage) RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM organization_members
		WHERE org_id = $1 AND user_id = $2`, orgID, userID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrMemberNotFound
	}
	return nil
}

// GetMember retrieves a specific member from an organization.
func (s *PostgresStorage) GetMember(ctx context.Context, orgID, userID uuid.UUID) (*models.OrganizationMember, error) {
	query := `
		SELECT m.org_id, m.user_id, m.role, m.created_at, u.username, u.email
		FROM organization_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND m.user_id = $2`

	var member models.OrganizationMember
	err := s.db.GetContext(ctx, &member, query, orgID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMemberNotFound
	}
	if err != nil {
		return nil, err
	}

	return &member, nil
}

// ListMembers retrieves all members of an organization.
func (s *PostgresStorage) ListMembers(ctx context.Context, orgID uuid.UUID) ([]*models.OrganizationMember, error) {
	query := `
		SELECT m.org_id, m.user_id, m.role, m.created_at, u.username, u.email
		FROM organization_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1
		ORDER BY m.created_at`

	var members []*models.OrganizationMember
	err := s.db.SelectContext(ctx, &members, query, orgID)
	if err != nil {
		return nil, err
	}

	return members, nil
}

// GetUserOrganization retrieves the organization a user belongs to (if any).
// Returns nil, nil if the user has no org membership.
func (s *PostgresStorage) GetUserOrganization(ctx context.Context, userID uuid.UUID) (*models.Organization, *models.OrganizationMember, error) {
	// Get membership
	memberQuery := `SELECT ` + orgMemberColumns + ` FROM organization_members WHERE user_id = $1`
	var member models.OrganizationMember
	err := s.db.GetContext(ctx, &member, memberQuery, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	// Get organization
	orgQuery := `SELECT ` + orgColumns + ` FROM organizations WHERE id = $1`
	var org models.Organization
	err = s.db.GetContext(ctx, &org, orgQuery, member.OrgID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	return &org, &member, nil
}

// UpdateMemberRole changes a member's role in the organization.
func (s *PostgresStorage) UpdateMemberRole(ctx context.Context, orgID, userID uuid.UUID, role string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE organization_members
		SET role = $1
		WHERE org_id = $2 AND user_id = $3`, role, orgID, userID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrMemberNotFound
	}
	return nil
}

// UpdateOrgS3Config sets S3 body storage configuration for an organization.
func (s *PostgresStorage) UpdateOrgS3Config(ctx context.Context, orgID uuid.UUID, bucket, region, endpoint, encAccessKeyID, encSecretAccessKey string) error {
	query := `
		UPDATE organizations
		SET s3_bucket = $1, s3_region = $2, s3_endpoint = $3,
			s3_access_key_id_encrypted = $4, s3_secret_access_key_encrypted = $5
		WHERE id = $6`

	result, err := s.db.ExecContext(ctx, query, bucket, region, endpoint, encAccessKeyID, encSecretAccessKey, orgID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrOrgNotFound
	}
	return nil
}

// ClearOrgS3Config removes S3 body storage configuration from an organization.
func (s *PostgresStorage) ClearOrgS3Config(ctx context.Context, orgID uuid.UUID) error {
	query := `
		UPDATE organizations
		SET s3_bucket = NULL, s3_region = 'us-east-1', s3_endpoint = NULL,
			s3_access_key_id_encrypted = NULL, s3_secret_access_key_encrypted = NULL
		WHERE id = $1`

	result, err := s.db.ExecContext(ctx, query, orgID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrOrgNotFound
	}
	return nil
}

// GetOrgS3Config retrieves S3 configuration columns for an organization.
func (s *PostgresStorage) GetOrgS3Config(ctx context.Context, orgID uuid.UUID) (*models.Organization, error) {
	query := `SELECT s3_bucket, s3_region, s3_endpoint, s3_access_key_id_encrypted, s3_secret_access_key_encrypted FROM organizations WHERE id = $1`

	var org models.Organization
	err := s.db.GetContext(ctx, &org, query, orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOrgNotFound
	}
	if err != nil {
		return nil, err
	}
	return &org, nil
}

// UpdateOrgCloudStorageConfig sets cloud storage configuration (S3 or GCS) for an organization.
func (s *PostgresStorage) UpdateOrgCloudStorageConfig(ctx context.Context, orgID uuid.UUID, provider, s3Bucket, s3Region, s3Endpoint, encS3AccessKeyID, encS3SecretKey, gcsBucket, gcsProjectID, encGCSCredJSON string) error {
	query := `
		UPDATE organizations
		SET cloud_storage_provider = $1,
			s3_bucket = $2, s3_region = $3, s3_endpoint = $4,
			s3_access_key_id_encrypted = $5, s3_secret_access_key_encrypted = $6,
			gcs_bucket = $7, gcs_project_id = $8, gcs_credentials_json_encrypted = $9
		WHERE id = $10`

	result, err := s.db.ExecContext(ctx, query, provider, s3Bucket, s3Region, s3Endpoint, encS3AccessKeyID, encS3SecretKey, gcsBucket, gcsProjectID, encGCSCredJSON, orgID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrOrgNotFound
	}
	return nil
}

// ClearOrgCloudStorageConfig removes all cloud storage configuration from an organization.
func (s *PostgresStorage) ClearOrgCloudStorageConfig(ctx context.Context, orgID uuid.UUID) error {
	query := `
		UPDATE organizations
		SET cloud_storage_provider = NULL,
			s3_bucket = NULL, s3_region = 'us-east-1', s3_endpoint = NULL,
			s3_access_key_id_encrypted = NULL, s3_secret_access_key_encrypted = NULL,
			gcs_bucket = NULL, gcs_project_id = NULL, gcs_credentials_json_encrypted = NULL
		WHERE id = $1`

	result, err := s.db.ExecContext(ctx, query, orgID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrOrgNotFound
	}
	return nil
}

// GetOrgCloudStorageConfig retrieves all cloud storage configuration columns for an organization.
func (s *PostgresStorage) GetOrgCloudStorageConfig(ctx context.Context, orgID uuid.UUID) (*models.Organization, error) {
	query := `SELECT cloud_storage_provider,
		s3_bucket, s3_region, s3_endpoint, s3_access_key_id_encrypted, s3_secret_access_key_encrypted,
		gcs_bucket, gcs_project_id, gcs_credentials_json_encrypted
		FROM organizations WHERE id = $1`

	var org models.Organization
	err := s.db.GetContext(ctx, &org, query, orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOrgNotFound
	}
	if err != nil {
		return nil, err
	}
	return &org, nil
}

// CreateInvite creates a new organization invite.
func (s *PostgresStorage) CreateInvite(ctx context.Context, orgID uuid.UUID, input *models.CreateInviteInput, invitedBy uuid.UUID, token string, expiresAt time.Time) (*models.OrganizationInvite, error) {
	query := `
		INSERT INTO organization_invites (org_id, email, role, token, invited_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + orgInviteColumns

	var invite models.OrganizationInvite
	err := s.db.QueryRowxContext(ctx, query, orgID, input.Email, input.Role, token, invitedBy, expiresAt).StructScan(&invite)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrDuplicateInvite
		}
		return nil, err
	}

	return &invite, nil
}

// GetInviteByToken retrieves a pending, non-expired invite by its token.
func (s *PostgresStorage) GetInviteByToken(ctx context.Context, token string) (*models.OrganizationInvite, error) {
	query := `SELECT ` + orgInviteColumns + ` FROM organization_invites WHERE token = $1 AND accepted_at IS NULL AND expires_at > now()`

	var invite models.OrganizationInvite
	err := s.db.GetContext(ctx, &invite, query, token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInviteNotFound
	}
	if err != nil {
		return nil, err
	}

	return &invite, nil
}

// GetInviteByID retrieves an invite by its UUID.
func (s *PostgresStorage) GetInviteByID(ctx context.Context, id uuid.UUID) (*models.OrganizationInvite, error) {
	query := `SELECT ` + orgInviteColumns + ` FROM organization_invites WHERE id = $1`

	var invite models.OrganizationInvite
	err := s.db.GetContext(ctx, &invite, query, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInviteNotFound
	}
	if err != nil {
		return nil, err
	}

	return &invite, nil
}

// ListPendingInvites retrieves all pending invites for an organization.
func (s *PostgresStorage) ListPendingInvites(ctx context.Context, orgID uuid.UUID) ([]*models.OrganizationInvite, error) {
	query := `SELECT ` + orgInviteColumns + ` FROM organization_invites WHERE org_id = $1 AND accepted_at IS NULL ORDER BY created_at DESC`

	var invites []*models.OrganizationInvite
	err := s.db.SelectContext(ctx, &invites, query, orgID)
	if err != nil {
		return nil, err
	}

	return invites, nil
}

// AcceptInvite marks an invite as accepted and adds the user to the organization in a transaction.
func (s *PostgresStorage) AcceptInvite(ctx context.Context, inviteID uuid.UUID) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Get the invite, verify it's still valid
	var invite models.OrganizationInvite
	err = tx.GetContext(ctx, &invite, `SELECT `+orgInviteColumns+` FROM organization_invites WHERE id = $1 FOR UPDATE`, inviteID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInviteNotFound
	}
	if err != nil {
		return err
	}

	if invite.AcceptedAt != nil {
		return ErrInviteNotFound
	}
	if time.Now().After(invite.ExpiresAt) {
		return ErrInviteExpired
	}

	// Mark invite as accepted
	_, err = tx.ExecContext(ctx, `UPDATE organization_invites SET accepted_at = now() WHERE id = $1`, inviteID)
	if err != nil {
		return fmt.Errorf("accept invite: %w", err)
	}

	// Look up user by email to add as member
	var userID uuid.UUID
	err = tx.GetContext(ctx, &userID, `SELECT id FROM users WHERE email = $1`, invite.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("no user with email %s", invite.Email)
	}
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	// Add as member
	_, err = tx.ExecContext(ctx, `
		INSERT INTO organization_members (org_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO NOTHING`, invite.OrgID, userID, invite.Role)
	if err != nil {
		return fmt.Errorf("insert member: %w", err)
	}

	return tx.Commit()
}

// DeleteInvite deletes an invite by its UUID.
func (s *PostgresStorage) DeleteInvite(ctx context.Context, id uuid.UUID) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM organization_invites WHERE id = $1`, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrInviteNotFound
	}
	return nil
}

// ListInvitesByEmail retrieves all pending, non-expired invites for an email address.
func (s *PostgresStorage) ListInvitesByEmail(ctx context.Context, email string) ([]*models.OrganizationInvite, error) {
	query := `SELECT ` + orgInviteColumns + ` FROM organization_invites WHERE email = $1 AND accepted_at IS NULL AND expires_at > now() ORDER BY created_at DESC`

	var invites []*models.OrganizationInvite
	err := s.db.SelectContext(ctx, &invites, query, email)
	if err != nil {
		return nil, err
	}

	return invites, nil
}

// isDuplicateKeyError checks if the error is a unique constraint violation (pq error code 23505).
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint")
}
