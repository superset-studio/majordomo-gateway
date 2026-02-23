package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	gcs "cloud.google.com/go/storage"
	"github.com/google/uuid"
	"google.golang.org/api/option"
)

type GCSConfig struct {
	Bucket          string
	CredentialsFile string
	Endpoint        string
}

type GCSBodyStorage struct {
	client     *gcs.Client
	bucket     string
	uploadChan chan *BodyUpload
	done       chan struct{}
	drained    chan struct{}
}

func NewGCSBodyStorage(ctx context.Context, cfg GCSConfig) (*GCSBodyStorage, error) {
	var opts []option.ClientOption

	if cfg.CredentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile))
	}
	if cfg.Endpoint != "" {
		opts = append(opts, option.WithEndpoint(cfg.Endpoint))
	}

	client, err := gcs.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	g := &GCSBodyStorage{
		client:     client,
		bucket:     cfg.Bucket,
		uploadChan: make(chan *BodyUpload, 1000),
		done:       make(chan struct{}),
		drained:    make(chan struct{}),
	}

	go g.uploadLoop()

	return g, nil
}

func (g *GCSBodyStorage) uploadLoop() {
	defer close(g.drained)
	for {
		select {
		case upload := <-g.uploadChan:
			g.doUpload(upload)
		case <-g.done:
			for len(g.uploadChan) > 0 {
				g.doUpload(<-g.uploadChan)
			}
			return
		}
	}
}

func (g *GCSBodyStorage) doUpload(upload *BodyUpload) {
	ctx := context.Background()

	content := BodyContent{
		RequestID: upload.RequestID.String(),
		Timestamp: upload.Timestamp.UTC().Format(time.RFC3339),
		Request: RequestContent{
			Method:  upload.RequestMethod,
			Path:    upload.RequestPath,
			Headers: upload.RequestHeaders,
			Body:    toJSONRawMessage(upload.RequestBody),
		},
		Response: ResponseContent{
			StatusCode: upload.ResponseStatus,
			Headers:    upload.ResponseHeaders,
			Body:       toJSONRawMessage(upload.ResponseBody),
		},
	}

	jsonData, err := json.Marshal(content)
	if err != nil {
		slog.Error("failed to marshal body content", "error", err, "request_id", upload.RequestID)
		return
	}

	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	if _, err := gzWriter.Write(jsonData); err != nil {
		slog.Error("failed to gzip body content", "error", err, "request_id", upload.RequestID)
		return
	}
	if err := gzWriter.Close(); err != nil {
		slog.Error("failed to close gzip writer", "error", err, "request_id", upload.RequestID)
		return
	}

	obj := g.client.Bucket(g.bucket).Object(upload.Key)
	w := obj.NewWriter(ctx)
	w.ContentType = "application/json"
	w.ContentEncoding = "gzip"

	if _, err := w.Write(buf.Bytes()); err != nil {
		slog.Error("failed to write to GCS", "error", err, "request_id", upload.RequestID, "key", upload.Key)
		w.Close()
		return
	}
	if err := w.Close(); err != nil {
		slog.Error("failed to close GCS writer", "error", err, "request_id", upload.RequestID, "key", upload.Key)
		return
	}

	slog.Debug("uploaded body to GCS", "request_id", upload.RequestID, "key", upload.Key)
}

func (g *GCSBodyStorage) Upload(upload *BodyUpload) {
	select {
	case g.uploadChan <- upload:
	default:
		slog.Warn("GCS upload channel full, dropping upload", "request_id", upload.RequestID)
	}
}

// GenerateKey creates a GCS object key for storing request/response bodies.
// The keyPrefix is typically the Majordomo API key ID (first 16 chars used).
func (g *GCSBodyStorage) GenerateKey(keyPrefix string, requestID uuid.UUID, timestamp time.Time) string {
	date := timestamp.UTC().Format("2006-01-02")
	prefix := keyPrefix
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	return fmt.Sprintf("%s/%s/%s.json.gz", prefix, date, requestID.String())
}

func (g *GCSBodyStorage) Close() error {
	close(g.done)
	<-g.drained
	return g.client.Close()
}
