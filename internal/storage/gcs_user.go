package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	gcs "cloud.google.com/go/storage"
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"google.golang.org/api/option"
)

// ValidateGCSConfig verifies that the given GCS credentials and bucket are accessible
// by performing a bucket Attrs call.
func ValidateGCSConfig(ctx context.Context, cfg *models.UserGCSConfig) error {
	var opts []option.ClientOption
	if len(cfg.CredentialsJSON) > 0 {
		opts = append(opts, option.WithCredentialsJSON(cfg.CredentialsJSON))
	}

	client, err := gcs.NewClient(ctx, opts...)
	if err != nil {
		return fmt.Errorf("invalid GCS credentials: %w", err)
	}
	defer client.Close()

	_, err = client.Bucket(cfg.Bucket).Attrs(ctx)
	if err != nil {
		return fmt.Errorf("cannot access bucket %q: %w", cfg.Bucket, err)
	}

	return nil
}

// UserGCSStorage manages per-user GCS clients for uploading request/response bodies.
type UserGCSStorage struct {
	clients sync.Map // userID (string) → *userGCSClient
}

type userGCSClient struct {
	client     *gcs.Client
	bucket     string
	configHash string
}

// NewUserGCSStorage creates a new UserGCSStorage.
func NewUserGCSStorage() *UserGCSStorage {
	return &UserGCSStorage{}
}

// GenerateUserGCSClaudeCodeKey creates a GCS key for Claude Code request/response bodies.
// Format: claude-code/{api-key-name}/{session-dir}/{date}/{requestID}.json.gz
func GenerateUserGCSClaudeCodeKey(apiKeyName string, sessionID uuid.UUID, sessionName *string, requestID uuid.UUID, timestamp time.Time) string {
	date := timestamp.UTC().Format("2006-01-02")

	sessionDir := sessionID.String()
	if sessionName != nil && *sessionName != "" {
		sessionDir = *sessionName + "-" + sessionID.String()
	}

	return fmt.Sprintf("claude-code/%s/%s/%s/%s.json.gz", apiKeyName, sessionDir, date, requestID.String())
}

// GenerateUserGCSRequestKey creates a GCS key for general LLM request/response bodies.
// Format: requests/{api-key-name}/{date}/{requestID}.json.gz
func GenerateUserGCSRequestKey(apiKeyName string, requestID uuid.UUID, timestamp time.Time) string {
	date := timestamp.UTC().Format("2006-01-02")
	return fmt.Sprintf("requests/%s/%s/%s.json.gz", apiKeyName, date, requestID.String())
}

// Upload uploads a body to the user's GCS bucket asynchronously (fire-and-forget).
// ctx is intentionally not forwarded to the goroutine — the upload must complete
// even after the request context is cancelled.
func (u *UserGCSStorage) Upload(ctx context.Context, userID uuid.UUID, cfg *models.UserGCSConfig, upload *BodyUpload) {
	go u.doUpload(userID, cfg, upload)
}

func (u *UserGCSStorage) doUpload(userID uuid.UUID, cfg *models.UserGCSConfig, upload *BodyUpload) {
	client, err := u.getOrCreateClient(userID, cfg)
	if err != nil {
		slog.Error("failed to create user GCS client", "error", err, "user_id", userID, "request_id", upload.RequestID)
		return
	}

	content := S3BodyContent{
		RequestID: upload.RequestID.String(),
		Timestamp: upload.Timestamp.UTC().Format(time.RFC3339),
		Request: S3RequestContent{
			Method:  upload.RequestMethod,
			Path:    upload.RequestPath,
			Headers: upload.RequestHeaders,
			Body:    toJSONRawMessage(upload.RequestBody),
		},
		Response: S3ResponseContent{
			StatusCode: upload.ResponseStatus,
			Headers:    upload.ResponseHeaders,
			Body:       toJSONRawMessage(upload.ResponseBody),
		},
	}

	jsonData, err := json.Marshal(content)
	if err != nil {
		slog.Error("failed to marshal user GCS body content", "error", err, "request_id", upload.RequestID)
		return
	}

	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	if _, err := gzWriter.Write(jsonData); err != nil {
		slog.Error("failed to gzip user GCS body content", "error", err, "request_id", upload.RequestID)
		return
	}
	if err := gzWriter.Close(); err != nil {
		slog.Error("failed to close gzip writer", "error", err, "request_id", upload.RequestID)
		return
	}

	ctx := context.Background()
	wc := client.client.Bucket(client.bucket).Object(upload.Key).NewWriter(ctx)
	wc.ContentType = "application/json"
	wc.ContentEncoding = "gzip"

	if _, err := wc.Write(buf.Bytes()); err != nil {
		slog.Error("failed to write to user GCS", "error", err, "request_id", upload.RequestID, "key", upload.Key, "user_id", userID)
		_ = wc.Close()
		return
	}

	if err := wc.Close(); err != nil {
		slog.Error("failed to close user GCS writer", "error", err, "request_id", upload.RequestID, "key", upload.Key, "user_id", userID)
		return
	}

	slog.Debug("uploaded body to user GCS", "request_id", upload.RequestID, "key", upload.Key, "user_id", userID)
}

func (u *UserGCSStorage) getOrCreateClient(userID uuid.UUID, cfg *models.UserGCSConfig) (*userGCSClient, error) {
	hash := fmt.Sprintf("%x", sha256.Sum256(append([]byte(cfg.Bucket+"|"), cfg.CredentialsJSON...)))

	key := userID.String()
	if existing, ok := u.clients.Load(key); ok {
		client := existing.(*userGCSClient)
		if client.configHash == hash {
			return client, nil
		}
		// Config changed — close the stale client before replacing it.
		_ = client.client.Close()
	}

	var opts []option.ClientOption
	if len(cfg.CredentialsJSON) > 0 {
		opts = append(opts, option.WithCredentialsJSON(cfg.CredentialsJSON))
	}

	gcsClient, err := gcs.NewClient(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client for user %s: %w", userID, err)
	}

	client := &userGCSClient{
		client:     gcsClient,
		bucket:     cfg.Bucket,
		configHash: hash,
	}

	u.clients.Store(key, client)
	return client, nil
}
