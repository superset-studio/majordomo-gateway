package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

// ValidateS3Config verifies that the given S3 credentials and bucket are accessible
// by performing a HeadBucket call.
func ValidateS3Config(ctx context.Context, cfg *models.UserS3Config) error {
	var opts []func(*awsconfig.LoadOptions) error
	opts = append(opts, awsconfig.WithRegion(cfg.Region))

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return fmt.Errorf("invalid AWS credentials: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)

	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(cfg.Bucket),
	})
	if err != nil {
		return fmt.Errorf("cannot access bucket %q: %w", cfg.Bucket, err)
	}

	return nil
}

// UserS3Storage manages per-user S3 clients for uploading Claude Code request/response bodies.
type UserS3Storage struct {
	clients sync.Map // userID (string) → *userS3Client
}

type userS3Client struct {
	client *s3.Client
	bucket string
	// configHash is used to detect when credentials change
	configHash string
}

// NewUserS3Storage creates a new UserS3Storage.
func NewUserS3Storage() *UserS3Storage {
	return &UserS3Storage{}
}

// GenerateUserS3ClaudeCodeKey creates an S3 key for Claude Code request/response bodies.
// Format: claude-code/{api-key-name}/{session-dir}/{date}/{requestID}.json.gz
// If sessionName is provided, session-dir is "{sessionName}-{sessionID}"; otherwise just "{sessionID}".
func GenerateUserS3ClaudeCodeKey(apiKeyName string, sessionID uuid.UUID, sessionName *string, requestID uuid.UUID, timestamp time.Time) string {
	date := timestamp.UTC().Format("2006-01-02")

	sessionDir := sessionID.String()
	if sessionName != nil && *sessionName != "" {
		sessionDir = *sessionName + "-" + sessionID.String()
	}

	return fmt.Sprintf("claude-code/%s/%s/%s/%s.json.gz", apiKeyName, sessionDir, date, requestID.String())
}

// GenerateUserS3RequestKey creates an S3 key for general LLM request/response bodies.
// Format: requests/{api-key-name}/{date}/{requestID}.json.gz
func GenerateUserS3RequestKey(apiKeyName string, requestID uuid.UUID, timestamp time.Time) string {
	date := timestamp.UTC().Format("2006-01-02")
	return fmt.Sprintf("requests/%s/%s/%s.json.gz", apiKeyName, date, requestID.String())
}

// Upload uploads a body to the user's S3 bucket asynchronously (fire-and-forget).
func (u *UserS3Storage) Upload(ctx context.Context, userID uuid.UUID, cfg *models.UserS3Config, upload *BodyUpload) {
	go u.doUpload(userID, cfg, upload)
}

func (u *UserS3Storage) doUpload(userID uuid.UUID, cfg *models.UserS3Config, upload *BodyUpload) {
	client, err := u.getOrCreateClient(userID, cfg)
	if err != nil {
		slog.Error("failed to create user S3 client", "error", err, "user_id", userID, "request_id", upload.RequestID)
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
		slog.Error("failed to marshal user S3 body content", "error", err, "request_id", upload.RequestID)
		return
	}

	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	if _, err := gzWriter.Write(jsonData); err != nil {
		slog.Error("failed to gzip user S3 body content", "error", err, "request_id", upload.RequestID)
		return
	}
	if err := gzWriter.Close(); err != nil {
		slog.Error("failed to close gzip writer", "error", err, "request_id", upload.RequestID)
		return
	}

	_, err = client.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:          aws.String(client.bucket),
		Key:             aws.String(upload.Key),
		Body:            bytes.NewReader(buf.Bytes()),
		ContentType:     aws.String("application/json"),
		ContentEncoding: aws.String("gzip"),
	})
	if err != nil {
		slog.Error("failed to upload to user S3", "error", err, "request_id", upload.RequestID, "key", upload.Key, "user_id", userID)
		return
	}

	slog.Debug("uploaded body to user S3", "request_id", upload.RequestID, "key", upload.Key, "user_id", userID)
}

func (u *UserS3Storage) getOrCreateClient(userID uuid.UUID, cfg *models.UserS3Config) (*userS3Client, error) {
	hash := cfg.Bucket + "|" + cfg.Region + "|" + cfg.Endpoint + "|" + cfg.AccessKeyID

	key := userID.String()
	if existing, ok := u.clients.Load(key); ok {
		client := existing.(*userS3Client)
		if client.configHash == hash {
			return client, nil
		}
	}

	ctx := context.Background()
	var opts []func(*awsconfig.LoadOptions) error
	opts = append(opts, awsconfig.WithRegion(cfg.Region))

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config for user %s: %w", userID, err)
	}

	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		})
	}

	s3Client := s3.NewFromConfig(awsCfg, s3Opts...)
	client := &userS3Client{
		client:     s3Client,
		bucket:     cfg.Bucket,
		configHash: hash,
	}

	u.clients.Store(key, client)
	return client, nil
}
