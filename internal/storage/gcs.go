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
	"github.com/google/uuid"
	"google.golang.org/api/option"
)

type GCSBodyStorageConfig struct {
	Bucket          string
	CredentialsFile string
	CredentialsJSON string
}

type GCSBodyStorage struct {
	client     *gcs.Client
	bucket     string
	uploadChan chan *BodyUpload
	done       chan struct{}
	wg         sync.WaitGroup
}

func NewGCSBodyStorage(ctx context.Context, cfg GCSBodyStorageConfig) (*GCSBodyStorage, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("GCS bucket name is required")
	}

	var opts []option.ClientOption

	if cfg.CredentialsJSON != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(cfg.CredentialsJSON)))
	} else if cfg.CredentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile))
	}
	// both empty → Application Default Credentials (ADC)

	client, err := gcs.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	s := &GCSBodyStorage{
		client:     client,
		bucket:     cfg.Bucket,
		uploadChan: make(chan *BodyUpload, 1000),
		done:       make(chan struct{}),
	}

	s.wg.Add(1)
	go s.uploadLoop()

	return s, nil
}

func (s *GCSBodyStorage) uploadLoop() {
	defer s.wg.Done()
	for {
		select {
		case upload := <-s.uploadChan:
			s.doUpload(upload)
		case <-s.done:
			for len(s.uploadChan) > 0 {
				s.doUpload(<-s.uploadChan)
			}
			return
		}
	}
}

func (s *GCSBodyStorage) doUpload(upload *BodyUpload) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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
		slog.Error("failed to marshal GCS body content", "error", err, "request_id", upload.RequestID)
		return
	}

	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	if _, err := gzWriter.Write(jsonData); err != nil {
		slog.Error("failed to gzip GCS body content", "error", err, "request_id", upload.RequestID)
		return
	}
	if err := gzWriter.Close(); err != nil {
		slog.Error("failed to close gzip writer", "error", err, "request_id", upload.RequestID)
		return
	}

	wc := s.client.Bucket(s.bucket).Object(upload.Key).NewWriter(ctx)
	wc.ContentType = "application/json"
	wc.ContentEncoding = "gzip"

	if _, err := wc.Write(buf.Bytes()); err != nil {
		slog.Error("failed to write to GCS", "error", err, "request_id", upload.RequestID, "key", upload.Key)
		_ = wc.Close()
		return
	}

	if err := wc.Close(); err != nil {
		slog.Error("failed to close GCS writer", "error", err, "request_id", upload.RequestID, "key", upload.Key)
		return
	}

	slog.Debug("uploaded body to GCS", "request_id", upload.RequestID, "key", upload.Key)
}

func (s *GCSBodyStorage) Upload(upload *BodyUpload) {
	select {
	case s.uploadChan <- upload:
	default:
		slog.Warn("GCS upload channel full, dropping upload", "request_id", upload.RequestID)
	}
}

// GenerateKey creates a GCS key for storing request/response bodies.
// The keyPrefix is typically the Majordomo API key ID (first 16 chars used).
func (s *GCSBodyStorage) GenerateKey(keyPrefix string, requestID uuid.UUID, timestamp time.Time) string {
	date := timestamp.UTC().Format("2006-01-02")
	prefix := keyPrefix
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	return fmt.Sprintf("%s/%s/%s.json.gz", prefix, date, requestID.String())
}

func (s *GCSBodyStorage) Close() error {
	close(s.done)
	s.wg.Wait()
	return s.client.Close()
}
