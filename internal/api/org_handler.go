package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/httputil"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/secrets"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// OrgHandler provides REST API endpoints for organization management.
type OrgHandler struct {
	orgs             storage.OrganizationStorage
	users            storage.UserStorage
	secrets          secrets.SecretStore
	jwt              *auth.JWTService
	emailVerify      storage.EmailVerificationStorage
	email            EmailSender
	frontendURL      string
	cloudInvalidator CloudStorageInvalidator
}

// SetCloudStorageInvalidator wires a CloudStorageInvalidator that the handler
// uses to evict cached cloud-storage state after org-level writes/deletes.
// Nil means "no invalidator". See AdminHandler.SetCloudStorageInvalidator for
// the rationale.
func (h *OrgHandler) SetCloudStorageInvalidator(inv CloudStorageInvalidator) {
	h.cloudInvalidator = inv
}

// NewOrgHandler creates a new organization handler.
func NewOrgHandler(
	orgs storage.OrganizationStorage,
	users storage.UserStorage,
	secretStore secrets.SecretStore,
	jwtSvc *auth.JWTService,
	emailVerify storage.EmailVerificationStorage,
	email EmailSender,
	frontendURL string,
) *OrgHandler {
	return &OrgHandler{
		orgs:        orgs,
		users:       users,
		secrets:     secretStore,
		jwt:         jwtSvc,
		emailVerify: emailVerify,
		email:       email,
		frontendURL: strings.TrimRight(frontendURL, "/"),
	}
}

// --- Org Signup ---

type orgSignupRequest struct {
	OrgName  string `json:"orgName"`
	OrgSlug  string `json:"orgSlug"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// OrgSignup handles POST /api/v1/admin/orgs/signup
func (h *OrgHandler) OrgSignup(w http.ResponseWriter, r *http.Request) {
	var req orgSignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.OrgName == "" || req.OrgSlug == "" || req.Email == "" || req.Password == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "orgName, orgSlug, email, and password are required")
		return
	}

	if !strings.Contains(req.Email, "@") {
		httputil.WriteJSONError(w, http.StatusBadRequest, "a valid email is required")
		return
	}

	if len(req.Password) < 8 {
		httputil.WriteJSONError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	user, _, err := h.orgs.CreateOrganizationWithUser(
		r.Context(),
		&models.CreateOrganizationInput{Name: req.OrgName, Slug: req.OrgSlug},
		&models.CreateUserInput{Username: req.Email, Email: req.Email, Password: req.Password},
	)
	if err != nil {
		slog.Error("failed to create org with user", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "failed to create organization")
		return
	}

	// Send verification email
	h.sendVerificationEmail(r, user.ID, req.Email)

	httputil.WriteJSON(w, http.StatusCreated, map[string]string{"status": "ok", "message": "verification email sent"})
}

// sendVerificationEmail generates a verification token and sends the email.
func (h *OrgHandler) sendVerificationEmail(r *http.Request, userID uuid.UUID, email string) {
	token, err := randomHex(24)
	if err != nil {
		slog.Error("failed to generate verification token", "error", err)
		return
	}
	expires := time.Now().Add(24 * time.Hour)
	if _, err := h.emailVerify.CreateEmailVerificationToken(r.Context(), userID, token, expires); err != nil {
		slog.Error("failed to create email verification token", "error", err)
		return
	}
	verifyURL := h.frontendURL + "/verify-email/" + token
	if h.email != nil {
		if err := h.email.SendVerification(email, verifyURL); err != nil {
			slog.Error("failed to send verification email", "error", err)
		}
	} else {
		slog.Info("email verification token created (no email sender)", "email", email, "token", token)
	}
}

// --- Org CRUD ---

// GetCurrentOrg handles GET /api/v1/admin/orgs/current
func (h *OrgHandler) GetCurrentOrg(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if claims.OrgID == nil {
		httputil.WriteJSONError(w, http.StatusNotFound, "no organization associated with this account")
		return
	}

	org, err := h.orgs.GetOrganizationByID(r.Context(), *claims.OrgID)
	if err != nil {
		slog.Error("failed to get organization", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, org)
}

type updateOrgRequest struct {
	Name *string `json:"name,omitempty"`
	Slug *string `json:"slug,omitempty"`
}

// UpdateOrg handles PUT /api/v1/admin/orgs/current
func (h *OrgHandler) UpdateOrg(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}

	var req updateOrgRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	org, err := h.orgs.UpdateOrganization(r.Context(), *claims.OrgID, &models.UpdateOrganizationInput{
		Name: req.Name,
		Slug: req.Slug,
	})
	if err != nil {
		slog.Error("failed to update organization", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, org)
}

// --- Members ---

// ListMembers handles GET /api/v1/admin/orgs/current/members
func (h *OrgHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}

	members, err := h.orgs.ListMembers(r.Context(), *claims.OrgID)
	if err != nil {
		slog.Error("failed to list members", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, members)
}

type updateMemberRoleRequest struct {
	Role string `json:"role"`
}

// UpdateMemberRole handles PUT /api/v1/admin/orgs/current/members/{userId}/role
func (h *OrgHandler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}

	userID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid user ID")
		return
	}

	var req updateMemberRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Role == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "role is required")
		return
	}

	if err := h.orgs.UpdateMemberRole(r.Context(), *claims.OrgID, userID, req.Role); err != nil {
		if err == storage.ErrMemberNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "member not found")
			return
		}
		slog.Error("failed to update member role", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RemoveMember handles DELETE /api/v1/admin/orgs/current/members/{userId}
func (h *OrgHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}

	userID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid user ID")
		return
	}

	// Prevent removing self if last admin
	if userID == claims.UserID {
		members, err := h.orgs.ListMembers(r.Context(), *claims.OrgID)
		if err != nil {
			slog.Error("failed to list members", "error", err)
			httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
		adminCount := 0
		for _, m := range members {
			if m.Role == "admin" {
				adminCount++
			}
		}
		if adminCount <= 1 {
			httputil.WriteJSONError(w, http.StatusBadRequest, "cannot remove the last admin")
			return
		}
	}

	if err := h.orgs.RemoveMember(r.Context(), *claims.OrgID, userID); err != nil {
		if err == storage.ErrMemberNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "member not found")
			return
		}
		slog.Error("failed to remove member", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// --- Invites ---

type createInviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type createInviteResponse struct {
	*models.OrganizationInvite
	Token string `json:"token"`
}

// CreateInvite handles POST /api/v1/admin/orgs/current/invites
func (h *OrgHandler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}

	var req createInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "email is required")
		return
	}

	role := req.Role
	if role == "" {
		role = "member"
	}

	token, err := generateInviteToken()
	if err != nil {
		slog.Error("failed to generate invite token", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	expiresAt := time.Now().Add(7 * 24 * time.Hour) // 7 days

	invite, err := h.orgs.CreateInvite(r.Context(), *claims.OrgID, &models.CreateInviteInput{
		Email: req.Email,
		Role:  role,
	}, claims.UserID, token, expiresAt)
	if err != nil {
		if err == storage.ErrDuplicateInvite {
			httputil.WriteJSONError(w, http.StatusConflict, "an invite already exists for this email")
			return
		}
		slog.Error("failed to create invite", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, createInviteResponse{OrganizationInvite: invite, Token: token})
}

// ListPendingInvites handles GET /api/v1/admin/orgs/current/invites
func (h *OrgHandler) ListPendingInvites(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}

	invites, err := h.orgs.ListPendingInvites(r.Context(), *claims.OrgID)
	if err != nil {
		slog.Error("failed to list invites", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, invites)
}

// RevokeInvite handles DELETE /api/v1/admin/orgs/current/invites/{id}
func (h *OrgHandler) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid invite ID")
		return
	}

	// Verify invite belongs to this org
	invite, err := h.orgs.GetInviteByID(r.Context(), id)
	if err != nil {
		if err == storage.ErrInviteNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "invite not found")
			return
		}
		slog.Error("failed to get invite", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if invite.OrgID != *claims.OrgID {
		httputil.WriteJSONError(w, http.StatusNotFound, "invite not found")
		return
	}

	if err := h.orgs.DeleteInvite(r.Context(), id); err != nil {
		slog.Error("failed to delete invite", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// AcceptInvite handles POST /api/v1/admin/invites/{token}/accept
func (h *OrgHandler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "token is required")
		return
	}

	invite, err := h.orgs.GetInviteByToken(r.Context(), token)
	if err != nil {
		if err == storage.ErrInviteNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "invite not found or expired")
			return
		}
		slog.Error("failed to get invite by token", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Add user as member directly (since they're already authenticated)
	if err := h.orgs.AddMember(r.Context(), invite.OrgID, claims.UserID, invite.Role); err != nil {
		if err == storage.ErrAlreadyMember {
			httputil.WriteJSONError(w, http.StatusConflict, "already a member of this organization")
			return
		}
		slog.Error("failed to add member", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Mark invite as accepted (best-effort; membership is already created)
	_ = h.orgs.DeleteInvite(r.Context(), invite.ID)

	// Generate new JWT with org context
	orgRole := invite.Role
	jwtToken, err := h.jwt.GenerateToken(claims.UserID, claims.Username, &invite.OrgID, &orgRole)
	if err != nil {
		slog.Error("failed to generate token", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	org, _ := h.orgs.GetOrganizationByID(r.Context(), invite.OrgID)

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"token": jwtToken,
		"org":   org,
	})
}

// --- Org S3 Config ---

// GetOrgS3Config handles GET /api/v1/admin/orgs/current/s3-config
func (h *OrgHandler) GetOrgS3Config(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}

	org, err := h.orgs.GetOrgS3Config(r.Context(), *claims.OrgID)
	if err != nil {
		slog.Error("failed to get org S3 config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := s3ConfigResponse{
		CredentialsSet: org.S3AccessKeyIDEncrypted != nil && *org.S3AccessKeyIDEncrypted != "",
	}
	if org.S3Bucket != nil {
		resp.Bucket = *org.S3Bucket
	}
	if org.S3Region != nil {
		resp.Region = *org.S3Region
	}
	if org.S3Endpoint != nil {
		resp.Endpoint = *org.S3Endpoint
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// UpdateOrgS3Config handles PUT /api/v1/admin/orgs/current/s3-config
func (h *OrgHandler) UpdateOrgS3Config(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}

	var req updateS3ConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Bucket == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "bucket is required")
		return
	}
	if req.AccessKeyID == "" || req.SecretAccessKey == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "accessKeyId and secretAccessKey are required")
		return
	}

	if h.secrets == nil {
		httputil.WriteJSONError(w, http.StatusInternalServerError, "encryption not configured")
		return
	}

	region := req.Region
	if region == "" {
		region = "us-east-1"
	}

	encAccessKeyID, err := h.secrets.Encrypt(req.AccessKeyID)
	if err != nil {
		slog.Error("failed to encrypt S3 access key ID", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	encSecretAccessKey, err := h.secrets.Encrypt(req.SecretAccessKey)
	if err != nil {
		slog.Error("failed to encrypt S3 secret access key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Validate S3 connectivity before saving
	if err := storage.ValidateS3Config(r.Context(), &models.UserS3Config{
		Bucket:         req.Bucket,
		Region:         region,
		Endpoint:       req.Endpoint,
		AccessKeyID:    req.AccessKeyID,
		SecretAccessKey: req.SecretAccessKey,
	}); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "S3 validation failed: "+err.Error())
		return
	}

	if err := h.orgs.UpdateOrgS3Config(r.Context(), *claims.OrgID, req.Bucket, region, req.Endpoint, encAccessKeyID, encSecretAccessKey); err != nil {
		slog.Error("failed to update org S3 config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if h.cloudInvalidator != nil {
		h.cloudInvalidator.InvalidateOrgCloudStorage(*claims.OrgID)
	}

	httputil.WriteJSON(w, http.StatusOK, s3ConfigResponse{
		Bucket:         req.Bucket,
		Region:         region,
		Endpoint:       req.Endpoint,
		CredentialsSet: true,
	})
}

// ClearOrgS3Config handles DELETE /api/v1/admin/orgs/current/s3-config
func (h *OrgHandler) ClearOrgS3Config(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}

	if err := h.orgs.ClearOrgS3Config(r.Context(), *claims.OrgID); err != nil {
		slog.Error("failed to clear org S3 config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if h.cloudInvalidator != nil {
		h.cloudInvalidator.InvalidateOrgCloudStorage(*claims.OrgID)
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Cloud Storage Config ---

// GetOrgCloudStorageConfig handles GET /api/v1/admin/orgs/current/cloud-storage-config
func (h *OrgHandler) GetOrgCloudStorageConfig(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}

	org, err := h.orgs.GetOrgCloudStorageConfig(r.Context(), *claims.OrgID)
	if err != nil {
		slog.Error("failed to get org cloud storage config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := buildCloudStorageResponse(org.CloudStorageProvider, org.S3Bucket, org.S3Region, org.S3Endpoint, org.S3AccessKeyIDEncrypted, org.GCSBucket, org.GCSProjectID)
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// UpdateOrgCloudStorageConfig handles PUT /api/v1/admin/orgs/current/cloud-storage-config
func (h *OrgHandler) UpdateOrgCloudStorageConfig(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}

	var req updateCloudStorageConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Provider != "s3" && req.Provider != "gcs" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "provider must be 's3' or 'gcs'")
		return
	}

	if h.secrets == nil {
		httputil.WriteJSONError(w, http.StatusInternalServerError, "encryption not configured")
		return
	}

	var (
		s3Bucket, s3Region, s3Endpoint, encS3AccessKeyID, encS3SecretKey string
		gcsBucket, gcsProjectID, encGCSCredJSON                          string
	)

	switch req.Provider {
	case "s3":
		if req.Bucket == "" {
			httputil.WriteJSONError(w, http.StatusBadRequest, "bucket is required for S3")
			return
		}
		if req.AccessKeyID == "" || req.SecretAccessKey == "" {
			httputil.WriteJSONError(w, http.StatusBadRequest, "accessKeyId and secretAccessKey are required for S3")
			return
		}
		s3Region = req.Region
		if s3Region == "" {
			s3Region = "us-east-1"
		}
		var err error
		encS3AccessKeyID, err = h.secrets.Encrypt(req.AccessKeyID)
		if err != nil {
			slog.Error("failed to encrypt S3 access key ID", "error", err)
			httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
		encS3SecretKey, err = h.secrets.Encrypt(req.SecretAccessKey)
		if err != nil {
			slog.Error("failed to encrypt S3 secret access key", "error", err)
			httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if err := storage.ValidateS3Config(r.Context(), &models.UserS3Config{
			Bucket: req.Bucket, Region: s3Region, Endpoint: req.Endpoint,
			AccessKeyID: req.AccessKeyID, SecretAccessKey: req.SecretAccessKey,
		}); err != nil {
			httputil.WriteJSONError(w, http.StatusBadRequest, "S3 validation failed: "+err.Error())
			return
		}
		s3Bucket = req.Bucket
		s3Endpoint = req.Endpoint

	case "gcs":
		if req.GCSBucket == "" {
			httputil.WriteJSONError(w, http.StatusBadRequest, "gcsBucket is required for GCS")
			return
		}
		if req.GCSCredentialsJSON == "" {
			httputil.WriteJSONError(w, http.StatusBadRequest, "gcsCredentialsJson is required for GCS")
			return
		}
		var err error
		encGCSCredJSON, err = h.secrets.Encrypt(req.GCSCredentialsJSON)
		if err != nil {
			slog.Error("failed to encrypt GCS credentials JSON", "error", err)
			httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if err := storage.ValidateGCSConfig(r.Context(), &storage.GCSConfig{
			Bucket: req.GCSBucket, ProjectID: req.GCSProjectID, CredentialsJSON: req.GCSCredentialsJSON,
		}); err != nil {
			httputil.WriteJSONError(w, http.StatusBadRequest, "GCS validation failed: "+err.Error())
			return
		}
		gcsBucket = req.GCSBucket
		gcsProjectID = req.GCSProjectID
	}

	if err := h.orgs.UpdateOrgCloudStorageConfig(r.Context(), *claims.OrgID, req.Provider, s3Bucket, s3Region, s3Endpoint, encS3AccessKeyID, encS3SecretKey, gcsBucket, gcsProjectID, encGCSCredJSON); err != nil {
		slog.Error("failed to update org cloud storage config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if h.cloudInvalidator != nil {
		h.cloudInvalidator.InvalidateOrgCloudStorage(*claims.OrgID)
	}

	resp := cloudStorageConfigResponse{Provider: req.Provider, CredentialsSet: true}
	switch req.Provider {
	case "s3":
		resp.Bucket = s3Bucket
		resp.Region = s3Region
		resp.Endpoint = s3Endpoint
	case "gcs":
		resp.GCSBucket = gcsBucket
		resp.GCSProjectID = gcsProjectID
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// ClearOrgCloudStorageConfig handles DELETE /api/v1/admin/orgs/current/cloud-storage-config
func (h *OrgHandler) ClearOrgCloudStorageConfig(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}

	if err := h.orgs.ClearOrgCloudStorageConfig(r.Context(), *claims.OrgID); err != nil {
		slog.Error("failed to clear org cloud storage config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if h.cloudInvalidator != nil {
		h.cloudInvalidator.InvalidateOrgCloudStorage(*claims.OrgID)
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ReloadOrgCloudStorageConfig handles POST /api/v1/admin/orgs/current/cloud-storage-config/reload
//
// Org analog of AdminHandler.ReloadCloudStorageConfig. Drops the proxy's
// cached org config + cached storage client without modifying the persisted
// row. See that docstring for the use case.
func (h *OrgHandler) ReloadOrgCloudStorageConfig(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.requireOrgAdmin(w, r)
	if !ok {
		return
	}
	if h.cloudInvalidator != nil {
		h.cloudInvalidator.InvalidateOrgCloudStorage(*claims.OrgID)
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
}

// --- Helpers ---

// requireOrgAdmin extracts JWT claims and verifies the user is an org admin.
// Returns the claims and true if authorized, or writes an error response and returns false.
func (h *OrgHandler) requireOrgAdmin(w http.ResponseWriter, r *http.Request) (*auth.JWTClaims, bool) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}

	if claims.OrgID == nil {
		httputil.WriteJSONError(w, http.StatusForbidden, "no organization associated with this account")
		return nil, false
	}

	if claims.OrgRole == nil || *claims.OrgRole != "admin" {
		httputil.WriteJSONError(w, http.StatusForbidden, "admin role required")
		return nil, false
	}

	return claims, true
}

// generateInviteToken creates a cryptographically random URL-safe token.
func generateInviteToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
