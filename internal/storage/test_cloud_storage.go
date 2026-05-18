package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gcs "cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithy "github.com/aws/smithy-go"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

// StorageTestStage names the operation that succeeded or failed during a
// round-trip test of a cloud-storage configuration.
type StorageTestStage string

const (
	StorageTestStageHead   StorageTestStage = "head"
	StorageTestStagePut    StorageTestStage = "put"
	StorageTestStageDelete StorageTestStage = "delete"
)

// StorageTestResult is the structured outcome of TestCloudStorageConfig.
//
// On success, Ok is true and all three durations are populated. On failure,
// Ok is false, Stage names the operation that failed, ErrorCode is the
// provider-specific error code when one was returned, and ErrorMessage is
// the raw provider message (passed through unchanged so operators see the
// real error without us paraphrasing it).
type StorageTestResult struct {
	Ok           bool             `json:"ok"`
	Stage        StorageTestStage `json:"stage,omitempty"`
	ErrorCode    string           `json:"errorCode,omitempty"`
	ErrorMessage string           `json:"errorMessage,omitempty"`
	HeadMs       int64            `json:"headMs,omitempty"`
	PutMs        int64            `json:"putMs,omitempty"`
	DeleteMs     int64            `json:"deleteMs,omitempty"`
}

// testObjectKey is the well-known key the test probe writes and deletes.
// Using a fixed key (rather than a UUID) keeps the bucket clean if a Delete
// stage fails — the next run overwrites the same object instead of leaking
// one .majordomo-test-* per attempt.
const testObjectKey = ".majordomo-storage-test"

// TestCloudStorageConfig performs a Head → Put → Delete round-trip against
// the given cloud-storage configuration and returns a structured result.
//
// Callers should treat a non-nil error as a 5xx (the test itself failed to
// run, e.g. credentials wouldn't parse) and a result with Ok=false as a 4xx
// (the test ran but the storage rejected something). The handler distinguishes
// these to give the dashboard a clean error UI.
func TestCloudStorageConfig(ctx context.Context, cfg *models.UserCloudStorageConfig) (*StorageTestResult, error) {
	if cfg == nil {
		return nil, errors.New("nil cloud storage config")
	}

	switch cfg.Provider {
	case models.CloudStorageProviderGCS:
		return testGCSConfig(ctx, cfg)
	default:
		return testS3Config(ctx, cfg)
	}
}

func testS3Config(ctx context.Context, cfg *models.UserCloudStorageConfig) (*StorageTestResult, error) {
	var opts []func(*awsconfig.LoadOptions) error
	opts = append(opts, awsconfig.WithRegion(cfg.Region))

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)
	bucket := aws.String(cfg.Bucket)

	result := &StorageTestResult{}

	headStart := time.Now()
	if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: bucket}); err != nil {
		populateS3Error(result, StorageTestStageHead, err)
		return result, nil
	}
	result.HeadMs = elapsedMs(headStart)

	putStart := time.Now()
	body := bytes.NewReader([]byte("majordomo storage test"))
	if _, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      bucket,
		Key:         aws.String(testObjectKey),
		Body:        body,
		ContentType: aws.String("text/plain"),
	}); err != nil {
		populateS3Error(result, StorageTestStagePut, err)
		return result, nil
	}
	result.PutMs = elapsedMs(putStart)

	deleteStart := time.Now()
	if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: bucket,
		Key:    aws.String(testObjectKey),
	}); err != nil {
		populateS3Error(result, StorageTestStageDelete, err)
		// Head + Put succeeded — surface the partial timings so the dashboard
		// can show "writes work, cleanup blocked" instead of an opaque red X.
		return result, nil
	}
	result.DeleteMs = elapsedMs(deleteStart)

	result.Ok = true
	return result, nil
}

func testGCSConfig(ctx context.Context, cfg *models.UserCloudStorageConfig) (*StorageTestResult, error) {
	if cfg.GCSBucket == "" {
		return nil, errors.New("gcs bucket is empty")
	}

	client, err := newGCSClient(ctx, cfg.GCSCredentialsJSON)
	if err != nil {
		return nil, fmt.Errorf("gcs client: %w", err)
	}
	defer client.Close()

	bucket := client.Bucket(cfg.GCSBucket)
	result := &StorageTestResult{}

	headStart := time.Now()
	if _, err := bucket.Attrs(ctx); err != nil {
		// Bucket.Attrs needs storage.buckets.get. If the service-account is
		// scoped to roles/storage.objectAdmin (object-level), Attrs returns
		// a permission error even though the upload would succeed — so we
		// only treat ErrBucketNotExist as a fatal head failure here, and
		// otherwise let the probe continue to the Put stage. The Put stage
		// catches genuine "no access" cases.
		if errors.Is(err, gcs.ErrBucketNotExist) {
			populateGCSError(result, StorageTestStageHead, err)
			return result, nil
		}
	} else {
		result.HeadMs = elapsedMs(headStart)
	}

	putStart := time.Now()
	obj := bucket.Object(testObjectKey)
	w := obj.NewWriter(ctx)
	w.ContentType = "text/plain"
	if _, err := w.Write([]byte("majordomo storage test")); err != nil {
		_ = w.Close()
		populateGCSError(result, StorageTestStagePut, err)
		return result, nil
	}
	if err := w.Close(); err != nil {
		populateGCSError(result, StorageTestStagePut, err)
		return result, nil
	}
	result.PutMs = elapsedMs(putStart)

	deleteStart := time.Now()
	if err := obj.Delete(ctx); err != nil {
		populateGCSError(result, StorageTestStageDelete, err)
		return result, nil
	}
	result.DeleteMs = elapsedMs(deleteStart)

	result.Ok = true
	return result, nil
}

// populateS3Error unwraps a smithy.APIError (the shape all aws-sdk-go-v2
// service errors implement) into the result struct. Non-APIError errors
// (e.g. context timeouts, DNS) get their string form stored verbatim so the
// dashboard surfaces something useful instead of "internal error".
func populateS3Error(r *StorageTestResult, stage StorageTestStage, err error) {
	r.Ok = false
	r.Stage = stage
	var ae smithy.APIError
	if errors.As(err, &ae) {
		r.ErrorCode = ae.ErrorCode()
		r.ErrorMessage = ae.ErrorMessage()
		return
	}
	r.ErrorMessage = trimErr(err.Error())
}

func populateGCSError(r *StorageTestResult, stage StorageTestStage, err error) {
	r.Ok = false
	r.Stage = stage
	r.ErrorMessage = trimErr(err.Error())
}

// trimErr caps an error string at 512 chars so a verbose provider response
// can't blow up the JSON payload in surprising ways.
func trimErr(s string) string {
	s = strings.TrimSpace(s)
	const max = 512
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func elapsedMs(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}
