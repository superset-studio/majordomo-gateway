package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

type S3BodyStorage struct {
	client     *s3.Client
	bucket     string
	uploadChan chan *BodyUpload
	done       chan struct{}
}

type BodyUpload struct {
	Key             string
	APIKeyHash      string
	RequestID       uuid.UUID
	Timestamp       time.Time
	RequestMethod   string
	RequestPath     string
	RequestHeaders  map[string]string
	RequestBody     []byte
	ResponseStatus  int
	ResponseHeaders map[string]string
	ResponseBody    []byte
}

type S3BodyContent struct {
	RequestID string            `json:"request_id"`
	Timestamp string            `json:"timestamp"`
	Request   S3RequestContent  `json:"request"`
	Response  S3ResponseContent `json:"response"`
}

type S3RequestContent struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

type S3ResponseContent struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       json.RawMessage   `json:"body,omitempty"`
}

type S3Config struct {
	Bucket          string
	Region          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
}

func NewS3BodyStorage(ctx context.Context, cfg S3Config) (*S3BodyStorage, error) {
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

	client := s3.NewFromConfig(awsCfg, s3Opts...)

	s := &S3BodyStorage{
		client:     client,
		bucket:     cfg.Bucket,
		uploadChan: make(chan *BodyUpload, 1000),
		done:       make(chan struct{}),
	}

	go s.uploadLoop()

	return s, nil
}

func (s *S3BodyStorage) uploadLoop() {
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

func (s *S3BodyStorage) doUpload(upload *BodyUpload) {
	ctx := context.Background()

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
		slog.Error("failed to marshal S3 body content", "error", err, "request_id", upload.RequestID)
		return
	}

	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	if _, err := gzWriter.Write(jsonData); err != nil {
		slog.Error("failed to gzip S3 body content", "error", err, "request_id", upload.RequestID)
		return
	}
	if err := gzWriter.Close(); err != nil {
		slog.Error("failed to close gzip writer", "error", err, "request_id", upload.RequestID)
		return
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:          aws.String(s.bucket),
		Key:             aws.String(upload.Key),
		Body:            bytes.NewReader(buf.Bytes()),
		ContentType:     aws.String("application/json"),
		ContentEncoding: aws.String("gzip"),
	})
	if err != nil {
		slog.Error("failed to upload to S3", "error", err, "request_id", upload.RequestID, "key", upload.Key)
		return
	}

	slog.Debug("uploaded body to S3", "request_id", upload.RequestID, "key", upload.Key)
}

func toJSONRawMessage(data []byte) json.RawMessage {
	if len(data) == 0 {
		return nil
	}
	if json.Valid(data) {
		return json.RawMessage(data)
	}
	escaped, _ := json.Marshal(string(data))
	return json.RawMessage(escaped)
}

func (s *S3BodyStorage) Upload(upload *BodyUpload) {
	select {
	case s.uploadChan <- upload:
	default:
		slog.Warn("S3 upload channel full, dropping upload", "request_id", upload.RequestID)
	}
}

func (s *S3BodyStorage) GenerateKey(apiKeyHash string, requestID uuid.UUID, timestamp time.Time) string {
	date := timestamp.UTC().Format("2006-01-02")
	// Use first 16 characters of API key hash as prefix
	prefix := apiKeyHash
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	return fmt.Sprintf("%s/%s/%s.json.gz", prefix, date, requestID.String())
}

func (s *S3BodyStorage) Close() error {
	close(s.done)
	return nil
}

func ExtractResponseHeaders(h http.Header) map[string]string {
	result := make(map[string]string)
	for key, values := range h {
		if len(values) > 0 {
			result[key] = values[0]
		}
	}
	return result
}
