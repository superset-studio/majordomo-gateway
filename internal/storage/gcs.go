package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	gcs "cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

// GCSConfig holds the configuration for connecting to a GCS bucket.
type GCSConfig struct {
	Bucket          string
	ProjectID       string
	CredentialsJSON string // raw JSON service account key
}

// ValidateGCSConfig verifies that the given GCS credentials and bucket are accessible.
func ValidateGCSConfig(ctx context.Context, cfg *GCSConfig) error {
	client, err := newGCSClient(ctx, cfg.CredentialsJSON)
	if err != nil {
		return fmt.Errorf("invalid GCS credentials: %w", err)
	}
	defer client.Close()

	// Write and delete a small test object to verify we have object-level access.
	// This only requires storage.objects.create and storage.objects.delete
	// (covered by roles/storage.objectAdmin), unlike bucket.Attrs() which
	// requires storage.buckets.get (roles/storage.admin).
	testKey := ".majordomo-validation-test"
	obj := client.Bucket(cfg.Bucket).Object(testKey)
	w := obj.NewWriter(ctx)
	w.ContentType = "text/plain"
	if _, err := w.Write([]byte("ok")); err != nil {
		w.Close()
		return fmt.Errorf("cannot write to bucket %q: %w", cfg.Bucket, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("cannot write to bucket %q: %w", cfg.Bucket, err)
	}
	if err := obj.Delete(ctx); err != nil {
		slog.Warn("failed to delete GCS validation test object", "bucket", cfg.Bucket, "key", testKey, "error", err)
	}

	return nil
}

// gcsUploadBody uploads a gzipped JSON body to a GCS bucket.
func gcsUploadBody(ctx context.Context, client *gcs.Client, bucket, key string, upload *BodyUpload) error {
	content := S3BodyContent{
		RequestID: upload.RequestID.String(),
		Timestamp: upload.Timestamp.UTC().Format("2006-01-02T15:04:05Z07:00"),
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

	obj := client.Bucket(bucket).Object(key)
	w := obj.NewWriter(ctx)
	w.ContentType = "application/json"
	w.ContentEncoding = "gzip"

	if _, err := w.Write(buf.Bytes()); err != nil {
		w.Close()
		return fmt.Errorf("gcs write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("gcs close writer: %w", err)
	}

	return nil
}

// gcsDownloadBody downloads and decompresses a body from a GCS bucket.
func gcsDownloadBody(ctx context.Context, client *gcs.Client, bucket, key string) (*S3BodyContent, error) {
	obj := client.Bucket(bucket).Object(key)
	reader, err := obj.NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcs read %s/%s: %w", bucket, key, err)
	}
	defer reader.Close()

	compressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading gcs body: %w", err)
	}

	gz, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	decompressed, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("gzip decompress: %w", err)
	}

	var content S3BodyContent
	if err := json.Unmarshal(decompressed, &content); err != nil {
		return nil, fmt.Errorf("unmarshal body content: %w", err)
	}

	return &content, nil
}

// newGCSClient creates a GCS client from credentials JSON or default credentials.
func newGCSClient(ctx context.Context, credentialsJSON string) (*gcs.Client, error) {
	if credentialsJSON != "" {
		client, err := gcs.NewClient(ctx, option.WithCredentialsJSON([]byte(credentialsJSON)))
		if err != nil {
			return nil, fmt.Errorf("gcs client from credentials: %w", err)
		}
		return client, nil
	}

	client, err := gcs.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcs client from default credentials: %w", err)
	}
	return client, nil
}

// logGCSUploadError logs a GCS upload failure without returning.
func logGCSUploadError(err error, requestID, key string, ownerID string) {
	slog.Error("failed to upload to GCS", "error", err, "request_id", requestID, "key", key, "owner_id", ownerID)
}
