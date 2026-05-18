package api

import (
    "context"
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
    "golang.org/x/crypto/bcrypt"
)



// AdminHandler provides REST API endpoints for the admin web UI.
type AdminHandler struct {
    apiKeys      storage.APIKeyStorage
    proxyKeys    storage.ProxyKeyStorage
    users        storage.UserStorage
    orgs         storage.OrganizationStorage
    providerKeys storage.ProviderKeyStorage
    secrets      secrets.SecretStore
    jwt          *auth.JWTService
    proxyKeySvc  *ProxyKeyService
    pwdResets    storage.PasswordResetStorage
    emailVerify  storage.EmailVerificationStorage
    email        EmailSender
    frontendURL  string
}

// NewAdminHandler creates a new admin API handler.
func NewAdminHandler(
    apiKeys storage.APIKeyStorage,
    proxyKeys storage.ProxyKeyStorage,
    users storage.UserStorage,
    orgs storage.OrganizationStorage,
    providerKeys storage.ProviderKeyStorage,
    secretStore secrets.SecretStore,
    jwtSvc *auth.JWTService,
    pwdResets storage.PasswordResetStorage,
    emailVerify storage.EmailVerificationStorage,
    email EmailSender,
    frontendURL string,
) *AdminHandler {
    return &AdminHandler{
        apiKeys:      apiKeys,
        proxyKeys:    proxyKeys,
        users:        users,
        orgs:         orgs,
        providerKeys: providerKeys,
        secrets:      secretStore,
        jwt:          jwtSvc,
        proxyKeySvc:  NewProxyKeyService(proxyKeys, secretStore),
        pwdResets:    pwdResets,
        emailVerify:  emailVerify,
        email:        email,
        frontendURL:  strings.TrimRight(frontendURL, "/"),
    }
}

// --- Password reset ---

type resetRequest struct {
    Username string `json:"username"`
    Email    string `json:"email"`
}

// RequestPasswordReset handles POST /api/v1/admin/password/reset-request
// Always returns 200 to prevent user enumeration.
func (h *AdminHandler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
    if h.pwdResets == nil {
        httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
        return
    }
    var req resetRequest
    _ = json.NewDecoder(r.Body).Decode(&req)

    // Find user by username or email
    var user *models.User
    var err error
    if req.Username != "" {
        user, err = h.users.GetUserByUsername(r.Context(), req.Username)
    } else if req.Email != "" {
        user, err = h.users.GetUserByEmail(r.Context(), req.Email)
    }
    if err != nil {
        slog.Error("password reset lookup failed", "error", err)
        // Return OK regardless
        httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
        return
    }
    if user == nil || user.PasswordHash == nil {
        // OAuth-only or missing user: still pretend success
        httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
        return
    }

    // Create token valid for 1 hour
    token, err := randomHex(24)
    if err != nil {
        slog.Error("failed to generate reset token", "error", err)
        httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
        return
    }
    expires := time.Now().Add(1 * time.Hour)
    if _, err := h.pwdResets.CreatePasswordResetToken(r.Context(), user.ID, token, expires); err != nil {
        slog.Error("failed to create password reset token", "error", err)
        // Still return OK to avoid enumeration
        httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
        return
    }
    resetURL := h.frontendURL + "/reset-password/" + token
    sendTo := req.Email
    if sendTo == "" && user.Email != nil {
        sendTo = *user.Email
    }
    if h.email != nil && sendTo != "" {
        if err := h.email.SendReset(sendTo, resetURL); err != nil {
            slog.Error("failed to send password reset email", "error", err)
        }
    } else {
        slog.Info("password reset token created (no email)", "user", user.Username, "token", token)
    }
    httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type resetPasswordRequest struct {
    Token       string `json:"token"`
    NewPassword string `json:"new_password"`
}

// ResetPassword handles POST /api/v1/admin/password/reset
func (h *AdminHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
    if h.pwdResets == nil {
        httputil.WriteJSONError(w, http.StatusBadRequest, "password reset not supported")
        return
    }
    var req resetPasswordRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" || req.NewPassword == "" {
        httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
        return
    }
    pr, err := h.pwdResets.GetPasswordResetByToken(r.Context(), req.Token)
    if err != nil || pr == nil || pr.UsedAt != nil || time.Now().After(pr.ExpiresAt) {
        httputil.WriteJSONError(w, http.StatusBadRequest, "invalid or expired token")
        return
    }

    newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 12)
    if err != nil {
        slog.Error("failed to hash password", "error", err)
        httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
        return
    }
    if err := h.users.UpdateUserPassword(r.Context(), pr.UserID, string(newHash)); err != nil {
        slog.Error("failed to update password", "error", err)
        httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
        return
    }
    _ = h.pwdResets.MarkPasswordResetUsed(r.Context(), pr.ID)
    httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Login ---

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string              `json:"token"`
	User  *models.User        `json:"user"`
	Org   *models.Organization `json:"org,omitempty"`
}

// Login handles POST /api/v1/admin/login
func (h *AdminHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Username == "" || req.Password == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	// Try lookup by username first (which is the email for new accounts),
	// then fall back to email lookup for backwards compatibility.
	user, err := h.users.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		slog.Error("failed to get user", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if user == nil && strings.Contains(req.Username, "@") {
		user, err = h.users.GetUserByEmail(r.Context(), req.Username)
		if err != nil {
			slog.Error("failed to get user by email", "error", err)
			httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	if user == nil || !user.IsActive {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if user.PasswordHash == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "this account uses OAuth sign-in")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.Password)); err != nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if !user.EmailVerified {
		httputil.WriteJSONError(w, http.StatusForbidden, "please verify your email before signing in")
		return
	}

	// Resolve org membership for JWT
	var orgID *uuid.UUID
	var orgRole *string
	var org *models.Organization
	if h.orgs != nil {
		o, member, err := h.orgs.GetUserOrganization(r.Context(), user.ID)
		if err != nil {
			slog.Error("failed to get user organization", "error", err)
			httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if o != nil {
			orgID = &o.ID
			orgRole = &member.Role
			org = o
		}
	}

	token, err := h.jwt.GenerateToken(user.ID, user.Username, orgID, orgRole)
	if err != nil {
		slog.Error("failed to generate token", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, loginResponse{Token: token, User: user, Org: org})
}

// --- Signup ---

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Signup handles POST /api/v1/admin/signup
func (h *AdminHandler) Signup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || !strings.Contains(req.Email, "@") {
		httputil.WriteJSONError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if req.Password == "" || len(req.Password) < 8 {
		httputil.WriteJSONError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	existing, err := h.users.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		slog.Error("failed to check existing user", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if existing != nil {
		httputil.WriteJSONError(w, http.StatusConflict, "an account with this email already exists")
		return
	}

	user, err := h.users.CreateUser(r.Context(), &models.CreateUserInput{
		Username: req.Email,
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		slog.Error("failed to create user", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	h.sendVerificationEmail(r, user.ID, req.Email)
	httputil.WriteJSON(w, http.StatusCreated, map[string]string{"status": "ok", "message": "verification email sent"})
}

// --- Email Verification ---

type verifyEmailRequest struct {
	Token string `json:"token"`
}

// VerifyEmail handles POST /api/v1/admin/email/verify
func (h *AdminHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	vt, err := h.emailVerify.GetEmailVerificationByToken(r.Context(), req.Token)
	if err != nil || vt == nil || vt.UsedAt != nil || time.Now().After(vt.ExpiresAt) {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid or expired verification link")
		return
	}

	if err := h.users.MarkUserEmailVerified(r.Context(), vt.UserID); err != nil {
		slog.Error("failed to mark email verified", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = h.emailVerify.MarkEmailVerificationUsed(r.Context(), vt.ID)

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type resendVerificationRequest struct {
	Email string `json:"email"`
}

// ResendVerification handles POST /api/v1/admin/email/verify/resend
func (h *AdminHandler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	var req resendVerificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	user, err := h.users.GetUserByEmail(r.Context(), req.Email)
	if err != nil || user == nil || user.EmailVerified {
		// Always return 200 to prevent enumeration
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	h.sendVerificationEmail(r, user.ID, req.Email)
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// sendVerificationEmail generates a verification token and sends the email.
func (h *AdminHandler) sendVerificationEmail(r *http.Request, userID uuid.UUID, email string) {
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

// --- Me ---

// Me handles GET /api/v1/admin/me
func (h *AdminHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.users.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("failed to get user", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, user)
}

// --- Change Password ---

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePassword handles PUT /api/v1/admin/me/password
func (h *AdminHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "current_password and new_password are required")
		return
	}

	user, err := h.users.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("failed to get user", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if user.PasswordHash == nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "cannot change password for OAuth accounts")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "current password is incorrect")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 12)
	if err != nil {
		slog.Error("failed to hash password", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := h.users.UpdateUserPassword(r.Context(), claims.UserID, string(newHash)); err != nil {
		slog.Error("failed to update password", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- S3 Config ---

type updateS3ConfigRequest struct {
	Bucket         string `json:"bucket"`
	Region         string `json:"region"`
	Endpoint       string `json:"endpoint"`
	AccessKeyID    string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
}

type s3ConfigResponse struct {
	Bucket         string `json:"bucket"`
	Region         string `json:"region"`
	Endpoint       string `json:"endpoint"`
	CredentialsSet bool   `json:"credentialsSet"`
}

// GetS3Config handles GET /api/v1/admin/me/s3-config
func (h *AdminHandler) GetS3Config(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.users.GetUserS3Config(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("failed to get user S3 config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := s3ConfigResponse{
		CredentialsSet: user.S3AccessKeyIDEncrypted != nil && *user.S3AccessKeyIDEncrypted != "",
	}
	if user.S3Bucket != nil {
		resp.Bucket = *user.S3Bucket
	}
	if user.S3Region != nil {
		resp.Region = *user.S3Region
	}
	if user.S3Endpoint != nil {
		resp.Endpoint = *user.S3Endpoint
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// UpdateS3Config handles PUT /api/v1/admin/me/s3-config
func (h *AdminHandler) UpdateS3Config(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
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

	if err := h.users.UpdateUserS3Config(r.Context(), claims.UserID, req.Bucket, region, req.Endpoint, encAccessKeyID, encSecretAccessKey); err != nil {
		slog.Error("failed to update user S3 config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := s3ConfigResponse{
		Bucket:         req.Bucket,
		Region:         region,
		Endpoint:       req.Endpoint,
		CredentialsSet: true,
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// DeleteS3Config handles DELETE /api/v1/admin/me/s3-config
func (h *AdminHandler) DeleteS3Config(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.users.ClearUserS3Config(r.Context(), claims.UserID); err != nil {
		slog.Error("failed to clear user S3 config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Cloud Storage Config ---

type updateCloudStorageConfigRequest struct {
	Provider       string `json:"provider"` // "s3" or "gcs"
	// S3 fields
	Bucket         string `json:"bucket,omitempty"`
	Region         string `json:"region,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	AccessKeyID    string `json:"accessKeyId,omitempty"`
	SecretAccessKey string `json:"secretAccessKey,omitempty"`
	// GCS fields
	GCSBucket          string `json:"gcsBucket,omitempty"`
	GCSProjectID       string `json:"gcsProjectId,omitempty"`
	GCSCredentialsJSON string `json:"gcsCredentialsJson,omitempty"`
}

type cloudStorageConfigResponse struct {
	Provider       string `json:"provider"`
	// S3 fields
	Bucket         string `json:"bucket,omitempty"`
	Region         string `json:"region,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	CredentialsSet bool   `json:"credentialsSet"`
	// GCS fields
	GCSBucket    string `json:"gcsBucket,omitempty"`
	GCSProjectID string `json:"gcsProjectId,omitempty"`
}

// GetCloudStorageConfig handles GET /api/v1/admin/me/cloud-storage-config
func (h *AdminHandler) GetCloudStorageConfig(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.users.GetUserCloudStorageConfig(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("failed to get user cloud storage config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := buildCloudStorageResponse(user.CloudStorageProvider, user.S3Bucket, user.S3Region, user.S3Endpoint, user.S3AccessKeyIDEncrypted, user.GCSBucket, user.GCSProjectID)
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// UpdateCloudStorageConfig handles PUT /api/v1/admin/me/cloud-storage-config
func (h *AdminHandler) UpdateCloudStorageConfig(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
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

	if err := h.users.UpdateUserCloudStorageConfig(r.Context(), claims.UserID, req.Provider, s3Bucket, s3Region, s3Endpoint, encS3AccessKeyID, encS3SecretKey, gcsBucket, gcsProjectID, encGCSCredJSON); err != nil {
		slog.Error("failed to update user cloud storage config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
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

// DeleteCloudStorageConfig handles DELETE /api/v1/admin/me/cloud-storage-config
func (h *AdminHandler) DeleteCloudStorageConfig(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.users.ClearUserCloudStorageConfig(r.Context(), claims.UserID); err != nil {
		slog.Error("failed to clear user cloud storage config", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// TestCloudStorageConfig handles POST /api/v1/admin/me/cloud-storage-config/test
//
// When the body is empty, it tests the user's currently-saved config. When
// the body contains a full updateCloudStorageConfigRequest, it tests *that*
// config without persisting — letting the dashboard validate writes before
// the user hits Save. This is the missing companion to the HeadBucket-only
// "S3 Configured" badge (see superset-studio/majordomo-gateway#6).
func (h *AdminHandler) TestCloudStorageConfig(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	cfg, status, errMsg := resolveStorageTestConfig(r, func(ctx context.Context) (*models.UserCloudStorageConfig, error) {
		user, err := h.users.GetUserCloudStorageConfig(ctx, claims.UserID)
		if err != nil {
			return nil, err
		}
		return decryptUserStorageConfig(user, h.secrets)
	})
	if errMsg != "" {
		httputil.WriteJSONError(w, status, errMsg)
		return
	}

	runStorageTest(r.Context(), w, cfg)
}

// buildCloudStorageResponse creates a cloudStorageConfigResponse from model fields.
func buildCloudStorageResponse(provider *string, s3Bucket, s3Region, s3Endpoint, encS3Key *string, gcsBucket, gcsProjectID *string) cloudStorageConfigResponse {
	resp := cloudStorageConfigResponse{}
	if provider != nil {
		resp.Provider = *provider
	}
	switch resp.Provider {
	case "gcs":
		if gcsBucket != nil {
			resp.GCSBucket = *gcsBucket
		}
		if gcsProjectID != nil {
			resp.GCSProjectID = *gcsProjectID
		}
		resp.CredentialsSet = true
	default:
		if s3Bucket != nil {
			resp.Bucket = *s3Bucket
		}
		if s3Region != nil {
			resp.Region = *s3Region
		}
		if s3Endpoint != nil {
			resp.Endpoint = *s3Endpoint
		}
		resp.CredentialsSet = encS3Key != nil && *encS3Key != ""
		if resp.Bucket == "" {
			resp.Provider = ""
			resp.CredentialsSet = false
		}
	}
	return resp
}

// --- API Keys ---

type adminCreateAPIKeyRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type adminCreateAPIKeyResponse struct {
	*models.APIKey
	Key string `json:"key"`
}

// ListAPIKeys handles GET /api/v1/admin/api-keys
func (h *AdminHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var keys []*models.APIKey
	var err error
	if claims.OrgID != nil {
		keys, err = h.apiKeys.ListAPIKeysByOrgID(r.Context(), *claims.OrgID)
	} else {
		keys, err = h.apiKeys.ListAPIKeysByUserID(r.Context(), claims.UserID)
	}
	if err != nil {
		slog.Error("failed to list API keys", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, keys)
}

// CreateAPIKey handles POST /api/v1/admin/api-keys
func (h *AdminHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req adminCreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "name is required")
		return
	}

	plaintext, hash, err := auth.GenerateAPIKey()
	if err != nil {
		slog.Error("failed to generate API key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	userID := claims.UserID
	input := &models.CreateAPIKeyInput{
		Name:        req.Name,
		Description: req.Description,
		UserID:      &userID,
		OrgID:       claims.OrgID,
	}

	key, err := h.apiKeys.CreateAPIKey(r.Context(), hash, input)
	if err != nil {
		slog.Error("failed to create API key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, adminCreateAPIKeyResponse{APIKey: key, Key: plaintext})
}

// GetAPIKey handles GET /api/v1/admin/api-keys/{id}
func (h *AdminHandler) GetAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	key, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	httputil.WriteJSON(w, http.StatusOK, key)
}

// UpdateAPIKey handles PUT /api/v1/admin/api-keys/{id}
func (h *AdminHandler) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	apiKey, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	var req struct {
		Name        *string `json:"name,omitempty"`
		Description *string `json:"description,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	input := &models.UpdateAPIKeyInput{
		Name:        req.Name,
		Description: req.Description,
	}

	updated, err := h.apiKeys.UpdateAPIKey(r.Context(), apiKey.ID, input)
	if err != nil {
		slog.Error("failed to update API key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, updated)
}

// RevokeAPIKey handles DELETE /api/v1/admin/api-keys/{id}
func (h *AdminHandler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	apiKey, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	if err := h.apiKeys.RevokeAPIKey(r.Context(), apiKey.ID); err != nil {
		slog.Error("failed to revoke API key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// --- Proxy Keys (nested under API keys) ---

// ListProxyKeys handles GET /api/v1/admin/api-keys/{id}/proxy-keys
func (h *AdminHandler) ListProxyKeys(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	apiKey, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	keys, err := h.proxyKeySvc.ListProxyKeys(r.Context(), apiKey.ID)
	if err != nil {
		slog.Error("failed to list proxy keys", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, keys)
}

// CreateProxyKey handles POST /api/v1/admin/api-keys/{id}/proxy-keys
func (h *AdminHandler) CreateProxyKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	apiKey, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	var req struct {
		Name        string  `json:"name"`
		Description *string `json:"description,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "name is required")
		return
	}

	pk, plaintext, err := h.proxyKeySvc.CreateProxyKey(r.Context(), apiKey.ID, req.Name, req.Description)
	if err != nil {
		slog.Error("failed to create proxy key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := struct {
		*models.ProxyKey
		Key string `json:"key"`
	}{ProxyKey: pk, Key: plaintext}

	httputil.WriteJSON(w, http.StatusCreated, resp)
}

// GetProxyKey handles GET /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}
func (h *AdminHandler) GetProxyKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	httputil.WriteJSON(w, http.StatusOK, pk)
}

// RevokeProxyKey handles DELETE /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}
func (h *AdminHandler) RevokeProxyKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	if err := h.proxyKeySvc.RevokeProxyKey(r.Context(), pk.ID, pk.MajordomoAPIKeyID); err != nil {
		slog.Error("failed to revoke proxy key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// --- Provider Mappings (nested under proxy keys) ---

// ListProviderMappings handles GET /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}/providers
func (h *AdminHandler) ListProviderMappings(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	resp, err := h.proxyKeySvc.ListProviderMappings(r.Context(), pk.ID, pk.MajordomoAPIKeyID)
	if err != nil {
		slog.Error("failed to list provider mappings", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// SetProviderMapping handles PUT /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}/providers/{provider}
func (h *AdminHandler) SetProviderMapping(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	providerName := chi.URLParam(r, "provider")
	if providerName == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "provider is required")
		return
	}

	var req setProviderMappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.APIKey == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "api_key is required")
		return
	}

	if err := h.proxyKeySvc.SetProviderMapping(r.Context(), pk.ID, pk.MajordomoAPIKeyID, providerName, req.APIKey); err != nil {
		slog.Error("failed to set provider mapping", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "provider": providerName})
}

// DeleteProviderMapping handles DELETE /api/v1/admin/api-keys/{id}/proxy-keys/{pkId}/providers/{provider}
func (h *AdminHandler) DeleteProviderMapping(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, pk, ok := h.verifyProxyKeyOwnership(w, r, claims)
	if !ok {
		return
	}

	providerName := chi.URLParam(r, "provider")
	if providerName == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "provider is required")
		return
	}

	if err := h.proxyKeySvc.DeleteProviderMapping(r.Context(), pk.ID, pk.MajordomoAPIKeyID, providerName); err != nil {
		if err == storage.ErrProviderMappingNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "provider mapping not found")
			return
		}
		slog.Error("failed to delete provider mapping", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Ownership verification helpers ---

// verifyAPIKeyOwnership parses the {id} URL param, fetches the API key, and verifies
// it belongs to the authenticated user. Returns the key and true on success.
func (h *AdminHandler) verifyAPIKeyOwnership(w http.ResponseWriter, r *http.Request, claims *auth.JWTClaims) (*models.APIKey, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid API key ID")
		return nil, false
	}

	key, err := h.apiKeys.GetAPIKeyByID(r.Context(), id)
	if err != nil {
		if err == storage.ErrAPIKeyNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "API key not found")
			return nil, false
		}
		slog.Error("failed to get API key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}

	// Org-scoped: verify key belongs to the same org
	if claims.OrgID != nil {
		if key.OrgID == nil || *key.OrgID != *claims.OrgID {
			httputil.WriteJSONError(w, http.StatusNotFound, "API key not found")
			return nil, false
		}
	} else {
		// Personal: verify key belongs to the user
		if key.UserID == nil || *key.UserID != claims.UserID {
			httputil.WriteJSONError(w, http.StatusNotFound, "API key not found")
			return nil, false
		}
	}

	return key, true
}

// verifyProxyKeyOwnership verifies both the API key and proxy key ownership chain.
func (h *AdminHandler) verifyProxyKeyOwnership(w http.ResponseWriter, r *http.Request, claims *auth.JWTClaims) (*models.APIKey, *models.ProxyKey, bool) {
	apiKey, ok := h.verifyAPIKeyOwnership(w, r, claims)
	if !ok {
		return nil, nil, false
	}

	pkID, err := uuid.Parse(chi.URLParam(r, "pkId"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid proxy key ID")
		return nil, nil, false
	}

	pk, err := h.proxyKeys.GetProxyKeyByID(r.Context(), pkID)
	if err != nil {
		if err == storage.ErrProxyKeyNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "proxy key not found")
			return nil, nil, false
		}
		slog.Error("failed to get proxy key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return nil, nil, false
	}

	if pk.MajordomoAPIKeyID != apiKey.ID {
		httputil.WriteJSONError(w, http.StatusNotFound, "proxy key not found")
		return nil, nil, false
	}

	return apiKey, pk, true
}

// --- Provider API Keys (for Replay) ---

type setProviderKeyRequest struct {
	APIKey string `json:"apiKey"`
}

// ListProviderKeys handles GET /api/v1/admin/me/provider-keys
func (h *AdminHandler) ListProviderKeys(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	keys, err := h.providerKeys.ListProviderKeys(r.Context(), &claims.UserID, claims.OrgID)
	if err != nil {
		slog.Error("failed to list provider keys", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, keys)
}

// SetProviderKey handles PUT /api/v1/admin/me/provider-keys/{provider}
func (h *AdminHandler) SetProviderKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	provider := chi.URLParam(r, "provider")
	if provider == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "provider is required")
		return
	}

	var req setProviderKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.APIKey == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "apiKey is required")
		return
	}

	if h.secrets == nil {
		httputil.WriteJSONError(w, http.StatusInternalServerError, "encryption not configured")
		return
	}

	encKey, err := h.secrets.Encrypt(req.APIKey)
	if err != nil {
		slog.Error("failed to encrypt provider API key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	ownerUserID := &claims.UserID
	ownerOrgID := claims.OrgID
	if ownerOrgID != nil {
		ownerUserID = nil
	}

	if err := h.providerKeys.SetProviderKey(r.Context(), ownerUserID, ownerOrgID, provider, encKey); err != nil {
		slog.Error("failed to set provider key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteProviderKey handles DELETE /api/v1/admin/me/provider-keys/{provider}
func (h *AdminHandler) DeleteProviderKey(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	provider := chi.URLParam(r, "provider")
	if provider == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "provider is required")
		return
	}

	ownerUserID := &claims.UserID
	ownerOrgID := claims.OrgID
	if ownerOrgID != nil {
		ownerUserID = nil
	}

	if err := h.providerKeys.DeleteProviderKey(r.Context(), ownerUserID, ownerOrgID, provider); err != nil {
		slog.Error("failed to delete provider key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
