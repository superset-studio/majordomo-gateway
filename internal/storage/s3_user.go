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

	gcs "cloud.google.com/go/storage"
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

// GenerateS3Key creates a storage key for request/response bodies.
// Format: {apiKeyID}/{date}/{requestID}.json.gz
func GenerateS3Key(apiKeyID uuid.UUID, requestID uuid.UUID, timestamp time.Time) string {
	date := timestamp.UTC().Format("2006-01-02")
	return fmt.Sprintf("%s/%s/%s.json.gz", apiKeyID.String(), date, requestID.String())
}

// UserBodyStorage manages per-user/org storage clients (S3 or GCS) for uploading
// request/response bodies.
type UserBodyStorage struct {
	clients sync.Map // ownerID (string) → *userStorageClient
}

type userStorageClient struct {
	provider   models.CloudStorageProviderType
	s3Client   *s3.Client
	gcsClient  *gcs.Client
	bucket     string
	configHash string
}

// NewUserBodyStorage creates a new UserBodyStorage.
func NewUserBodyStorage() *UserBodyStorage {
	return &UserBodyStorage{}
}

// Invalidate drops the cached storage client for one owner so the next
// Upload/Download rebuilds it from a fresh config. Safe to call when no
// entry exists. Also closes the underlying GCS client (if any) to release
// the HTTP connection pool.
//
// This is the missing link in the storage cache: getOrCreateClient
// invalidates by configHash only when the *caller* passes a fresh cfg, and
// upstream callers cache the cfg too. So a dashboard config change never
// observes a different hash → never triggers a rebuild → keeps using the
// stale client. The handler now calls Invalidate from the admin write
// paths, closing that gap.
func (u *UserBodyStorage) Invalidate(ownerID uuid.UUID) {
	key := ownerID.String()
	existing, ok := u.clients.LoadAndDelete(key)
	if !ok {
		return
	}
	client, ok := existing.(*userStorageClient)
	if !ok {
		return
	}
	if client.gcsClient != nil {
		// Best-effort close — the GCS client owns an HTTP pool.
		_ = client.gcsClient.Close()
	}
}

// Download fetches and decompresses a body from the owner's bucket (S3 or GCS).
func (u *UserBodyStorage) Download(ctx context.Context, ownerID uuid.UUID, cfg *models.UserCloudStorageConfig, key string) (*S3BodyContent, error) {
	client, err := u.getOrCreateClient(ownerID, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating storage client for %s: %w", ownerID, err)
	}

	switch client.provider {
	case models.CloudStorageProviderGCS:
		return gcsDownloadBody(ctx, client.gcsClient, client.bucket, key)
	default:
		return downloadS3Body(ctx, client.s3Client, client.bucket, key)
	}
}

// Upload uploads a body to the owner's bucket asynchronously (fire-and-forget).
func (u *UserBodyStorage) Upload(ctx context.Context, ownerID uuid.UUID, cfg *models.UserCloudStorageConfig, upload *BodyUpload) {
	go u.doUpload(ownerID, cfg, upload)
}

func (u *UserBodyStorage) doUpload(ownerID uuid.UUID, cfg *models.UserCloudStorageConfig, upload *BodyUpload) {
	client, err := u.getOrCreateClient(ownerID, cfg)
	if err != nil {
		slog.Error("failed to create storage client", "error", err, "owner_id", ownerID, "request_id", upload.RequestID, "provider", cfg.Provider)
		return
	}

	ctx := context.Background()

	switch client.provider {
	case models.CloudStorageProviderGCS:
		if err := gcsUploadBody(ctx, client.gcsClient, client.bucket, upload.Key, upload); err != nil {
			slog.Error("failed to upload to user GCS", "error", err, "request_id", upload.RequestID, "key", upload.Key, "owner_id", ownerID)
			return
		}
		slog.Debug("uploaded body to user GCS", "request_id", upload.RequestID, "key", upload.Key, "owner_id", ownerID)
	default:
		if err := s3UploadBody(ctx, client.s3Client, client.bucket, upload); err != nil {
			slog.Error("failed to upload to user S3", "error", err, "request_id", upload.RequestID, "key", upload.Key, "owner_id", ownerID)
			return
		}
		slog.Debug("uploaded body to user S3", "request_id", upload.RequestID, "key", upload.Key, "owner_id", ownerID)
	}
}

func (u *UserBodyStorage) getOrCreateClient(ownerID uuid.UUID, cfg *models.UserCloudStorageConfig) (*userStorageClient, error) {
	hash := string(cfg.Provider) + "|"
	switch cfg.Provider {
	case models.CloudStorageProviderGCS:
		hash += cfg.GCSBucket + "|" + cfg.GCSProjectID + "|" + cfg.GCSCredentialsJSON[:min(16, len(cfg.GCSCredentialsJSON))]
	default:
		hash += cfg.Bucket + "|" + cfg.Region + "|" + cfg.Endpoint + "|" + cfg.AccessKeyID
	}

	key := ownerID.String()
	if existing, ok := u.clients.Load(key); ok {
		client := existing.(*userStorageClient)
		if client.configHash == hash {
			return client, nil
		}
		// Config changed — close old GCS client if present
		if client.gcsClient != nil {
			client.gcsClient.Close()
		}
	}

	switch cfg.Provider {
	case models.CloudStorageProviderGCS:
		return u.createGCSClient(key, cfg, hash)
	default:
		return u.createS3Client(key, cfg, hash)
	}
}

func (u *UserBodyStorage) createS3Client(key string, cfg *models.UserCloudStorageConfig, hash string) (*userStorageClient, error) {
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
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		})
	}

	s3Client := s3.NewFromConfig(awsCfg, s3Opts...)
	client := &userStorageClient{
		provider:   models.CloudStorageProviderS3,
		s3Client:   s3Client,
		bucket:     cfg.Bucket,
		configHash: hash,
	}

	u.clients.Store(key, client)
	return client, nil
}

func (u *UserBodyStorage) createGCSClient(key string, cfg *models.UserCloudStorageConfig, hash string) (*userStorageClient, error) {
	ctx := context.Background()
	gcsClient, err := newGCSClient(ctx, cfg.GCSCredentialsJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	client := &userStorageClient{
		provider:   models.CloudStorageProviderGCS,
		gcsClient:  gcsClient,
		bucket:     cfg.GCSBucket,
		configHash: hash,
	}

	u.clients.Store(key, client)
	return client, nil
}

// s3UploadBody uploads a gzipped JSON body to an S3 bucket using the per-user client.
func s3UploadBody(ctx context.Context, client *s3.Client, bucket string, upload *BodyUpload) error {
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
		return fmt.Errorf("marshal body content: %w", err)
	}

	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	if _, err := gzWriter.Write(jsonData); err != nil {
		return fmt.Errorf("gzip write: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		return fmt.Errorf("gzip close: %w", err)
	}

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:          aws.String(bucket),
		Key:             aws.String(upload.Key),
		Body:            bytes.NewReader(buf.Bytes()),
		ContentType:     aws.String("application/json"),
		ContentEncoding: aws.String("gzip"),
	})
	return err
}

// UserS3Storage is a type alias for backward compatibility.
type UserS3Storage = UserBodyStorage

// NewUserS3Storage creates a new UserBodyStorage (deprecated: use NewUserBodyStorage).
func NewUserS3Storage() *UserBodyStorage {
	return NewUserBodyStorage()
}
