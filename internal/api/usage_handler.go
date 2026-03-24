package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/httputil"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/secrets"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// UsageHandler provides REST API endpoints for usage reporting.
type UsageHandler struct {
	usage        storage.UsageStorage
	apiKeys      storage.APIKeyStorage
	userStore    storage.UserStorage
	orgStore     storage.OrganizationStorage
	secretStore  secrets.SecretStore
	s3Storage    *storage.S3BodyStorage
	userS3       *storage.UserS3Storage
}

// NewUsageHandler creates a new usage reporting handler.
func NewUsageHandler(
	usage storage.UsageStorage,
	apiKeys storage.APIKeyStorage,
	userStore storage.UserStorage,
	orgStore storage.OrganizationStorage,
	secretStore secrets.SecretStore,
	s3Storage *storage.S3BodyStorage,
	userS3 *storage.UserS3Storage,
) *UsageHandler {
	return &UsageHandler{
		usage:       usage,
		apiKeys:     apiKeys,
		userStore:   userStore,
		orgStore:    orgStore,
		secretStore: secretStore,
		s3Storage:   s3Storage,
		userS3:      userS3,
	}
}

// GetSummary handles POST /api/v1/admin/usage/summary
func (h *UsageHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	setFilterScope(filter, claims)

	if filter.APIKeyID != nil {
		if !h.verifyAPIKeyOwnership(w, r, *filter.APIKeyID, claims) {
			return
		}
	}

	summary, err := h.usage.GetUsageSummary(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get usage summary", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, summary)
}

// GetDailyUsage handles POST /api/v1/admin/usage/daily
func (h *UsageHandler) GetDailyUsage(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	setFilterScope(filter, claims)

	if filter.APIKeyID != nil {
		if !h.verifyAPIKeyOwnership(w, r, *filter.APIKeyID, claims) {
			return
		}
	}

	daily, err := h.usage.GetDailyUsage(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get daily usage", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, daily)
}

// GetModelBreakdown handles POST /api/v1/admin/usage/models
func (h *UsageHandler) GetModelBreakdown(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	setFilterScope(filter, claims)

	if filter.APIKeyID != nil {
		if !h.verifyAPIKeyOwnership(w, r, *filter.APIKeyID, claims) {
			return
		}
	}

	models, err := h.usage.GetModelBreakdown(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get model breakdown", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, models)
}

// GetAPIKeyBreakdown handles POST /api/v1/admin/usage/api-keys
func (h *UsageHandler) GetAPIKeyBreakdown(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	setFilterScope(filter, claims)

	breakdown, err := h.usage.GetAPIKeyBreakdown(r.Context(), filter)
	if err != nil {
		slog.Error("failed to get API key breakdown", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, breakdown)
}

// ListRequests handles POST /api/v1/admin/usage/requests
func (h *UsageHandler) ListRequests(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	filter, req, err := decodeUsageRequest(r)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	setFilterScope(filter, claims)

	if filter.APIKeyID != nil {
		if !h.verifyAPIKeyOwnership(w, r, *filter.APIKeyID, claims) {
			return
		}
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	requests, total, err := h.usage.ListUsageRequests(r.Context(), filter, limit, offset)
	if err != nil {
		slog.Error("failed to list usage requests", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"requests":   requests,
		"numRecords": total,
	})
}

// GetRequestDetail handles GET /api/v1/admin/usage/requests/{id}
func (h *UsageHandler) GetRequestDetail(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request ID")
		return
	}

	detail, err := h.usage.GetRequestDetail(r.Context(), id, claims.UserID, claims.OrgID)
	if err != nil {
		if err == storage.ErrRequestNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "request not found")
			return
		}
		slog.Error("failed to get request detail", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, detail)
}

// GetMetadataBreakdown handles POST /api/v1/admin/usage/metadata/{keyName}
func (h *UsageHandler) GetMetadataBreakdown(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	keyName := chi.URLParam(r, "keyName")
	if keyName == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "missing key name")
		return
	}

	filter, _, err := decodeUsageRequest(r)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	setFilterScope(filter, claims)

	if filter.APIKeyID != nil {
		if !h.verifyAPIKeyOwnership(w, r, *filter.APIKeyID, claims) {
			return
		}
	}

	breakdown, err := h.usage.GetMetadataBreakdown(r.Context(), filter, keyName)
	if err != nil {
		slog.Error("failed to get metadata breakdown", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, breakdown)
}

// GetRequestBody handles GET /api/v1/admin/usage/requests/{id}/body
func (h *UsageHandler) GetRequestBody(w http.ResponseWriter, r *http.Request) {
	claims := GetUserInfo(r.Context())
	if claims == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request ID")
		return
	}

	detail, err := h.usage.GetRequestDetail(r.Context(), id, claims.UserID, claims.OrgID)
	if err != nil {
		if err == storage.ErrRequestNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "request not found")
			return
		}
		slog.Error("failed to get request detail", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if detail.BodyS3Key == nil {
		httputil.WriteJSONError(w, http.StatusNotFound, "no body stored for this request")
		return
	}

	content, err := h.downloadBody(r.Context(), detail)
	if err != nil {
		slog.Error("failed to download request body from S3", "error", err, "request_id", id, "s3_key", *detail.BodyS3Key)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "failed to retrieve request body")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, content)
}

// downloadBody resolves the S3 bucket (user → org → global) and downloads the body.
func (h *UsageHandler) downloadBody(ctx context.Context, detail *models.RequestLog) (*storage.S3BodyContent, error) {
	key := *detail.BodyS3Key

	// Try user S3.
	if detail.UserID != nil && h.userS3 != nil && h.secretStore != nil && h.userStore != nil {
		if cfg := h.resolveUserS3Config(ctx, *detail.UserID); cfg != nil {
			return h.userS3.Download(ctx, *detail.UserID, cfg, key)
		}
	}

	// Try org S3.
	if detail.OrgID != nil && h.userS3 != nil && h.secretStore != nil && h.orgStore != nil {
		if cfg := h.resolveOrgS3Config(ctx, *detail.OrgID); cfg != nil {
			return h.userS3.Download(ctx, *detail.OrgID, cfg, key)
		}
	}

	// Fall back to global S3.
	if h.s3Storage != nil {
		return h.s3Storage.Download(ctx, key)
	}

	return nil, fmt.Errorf("no S3 configuration available for request %s", detail.ID)
}

func (h *UsageHandler) resolveUserS3Config(ctx context.Context, userID uuid.UUID) *models.UserS3Config {
	user, err := h.userStore.GetUserS3Config(ctx, userID)
	if err != nil {
		return nil
	}
	if user.S3Bucket == nil || *user.S3Bucket == "" || user.S3AccessKeyIDEncrypted == nil || user.S3SecretAccessKeyEncrypted == nil {
		return nil
	}
	accessKeyID, err := h.secretStore.Decrypt(*user.S3AccessKeyIDEncrypted)
	if err != nil {
		slog.Error("failed to decrypt user S3 access key", "error", err, "user_id", userID)
		return nil
	}
	secretKey, err := h.secretStore.Decrypt(*user.S3SecretAccessKeyEncrypted)
	if err != nil {
		slog.Error("failed to decrypt user S3 secret key", "error", err, "user_id", userID)
		return nil
	}
	cfg := &models.UserS3Config{
		Bucket:          *user.S3Bucket,
		AccessKeyID:     accessKeyID,
		SecretAccessKey:  secretKey,
	}
	if user.S3Region != nil {
		cfg.Region = *user.S3Region
	}
	if user.S3Endpoint != nil {
		cfg.Endpoint = *user.S3Endpoint
	}
	return cfg
}

func (h *UsageHandler) resolveOrgS3Config(ctx context.Context, orgID uuid.UUID) *models.UserS3Config {
	org, err := h.orgStore.GetOrgS3Config(ctx, orgID)
	if err != nil {
		return nil
	}
	if org.S3Bucket == nil || *org.S3Bucket == "" || org.S3AccessKeyIDEncrypted == nil || org.S3SecretAccessKeyEncrypted == nil {
		return nil
	}
	accessKeyID, err := h.secretStore.Decrypt(*org.S3AccessKeyIDEncrypted)
	if err != nil {
		slog.Error("failed to decrypt org S3 access key", "error", err, "org_id", orgID)
		return nil
	}
	secretKey, err := h.secretStore.Decrypt(*org.S3SecretAccessKeyEncrypted)
	if err != nil {
		slog.Error("failed to decrypt org S3 secret key", "error", err, "org_id", orgID)
		return nil
	}
	cfg := &models.UserS3Config{
		Bucket:          *org.S3Bucket,
		AccessKeyID:     accessKeyID,
		SecretAccessKey:  secretKey,
	}
	if org.S3Region != nil {
		cfg.Region = *org.S3Region
	}
	if org.S3Endpoint != nil {
		cfg.Endpoint = *org.S3Endpoint
	}
	return cfg
}

// verifyAPIKeyOwnership checks that the given API key belongs to the user or their org.
// Returns false and writes an error response if ownership cannot be verified.
func (h *UsageHandler) verifyAPIKeyOwnership(w http.ResponseWriter, r *http.Request, apiKeyID uuid.UUID, claims *auth.JWTClaims) bool {
	key, err := h.apiKeys.GetAPIKeyByID(r.Context(), apiKeyID)
	if err != nil {
		if err == storage.ErrAPIKeyNotFound {
			httputil.WriteJSONError(w, http.StatusNotFound, "API key not found")
			return false
		}
		slog.Error("failed to get API key", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return false
	}

	if claims.OrgID != nil {
		if key.OrgID == nil || *key.OrgID != *claims.OrgID {
			httputil.WriteJSONError(w, http.StatusNotFound, "API key not found")
			return false
		}
	} else {
		if key.UserID == nil || *key.UserID != claims.UserID {
			httputil.WriteJSONError(w, http.StatusNotFound, "API key not found")
			return false
		}
	}

	return true
}
