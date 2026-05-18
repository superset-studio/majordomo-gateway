package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superset-studio/majordomo-gateway/internal/models"
)

func TestResolveStorageTestConfig_BodyParsed(t *testing.T) {
	t.Parallel()

	body := `{
		"provider": "s3",
		"bucket": "test-bucket",
		"region": "us-west-2",
		"endpoint": "https://example.com",
		"accessKeyId": "AKIA...",
		"secretAccessKey": "secret"
	}`

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	cfg, status, errMsg := resolveStorageTestConfig(req, func(ctx context.Context) (*models.UserCloudStorageConfig, error) {
		t.Fatal("loader should not be invoked when a body is provided")
		return nil, nil
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: status=%d msg=%q", status, errMsg)
	}
	if cfg == nil || cfg.Provider != models.CloudStorageProviderS3 {
		t.Fatalf("expected S3 config, got %+v", cfg)
	}
	if cfg.Bucket != "test-bucket" || cfg.Region != "us-west-2" {
		t.Fatalf("body fields not propagated: %+v", cfg)
	}
	if cfg.AccessKeyID != "AKIA..." || cfg.SecretAccessKey != "secret" {
		t.Fatalf("credentials not propagated: %+v", cfg)
	}
}

func TestResolveStorageTestConfig_EmptyBodyUsesLoader(t *testing.T) {
	t.Parallel()

	saved := &models.UserCloudStorageConfig{
		Provider:        models.CloudStorageProviderS3,
		Bucket:          "saved-bucket",
		Region:          "us-east-1",
		AccessKeyID:     "saved-key",
		SecretAccessKey: "saved-secret",
	}

	for _, body := range []string{"", "  ", "\n\n\t", "  \r\n"} {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		cfg, _, errMsg := resolveStorageTestConfig(req, func(ctx context.Context) (*models.UserCloudStorageConfig, error) {
			return saved, nil
		})
		if errMsg != "" {
			t.Fatalf("body %q: unexpected error: %q", body, errMsg)
		}
		if cfg != saved {
			t.Fatalf("body %q: loader result not returned (got %+v)", body, cfg)
		}
	}
}

func TestResolveStorageTestConfig_EmptyBodyNoSavedConfig(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	_, status, errMsg := resolveStorageTestConfig(req, func(ctx context.Context) (*models.UserCloudStorageConfig, error) {
		return nil, nil
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
	if !strings.Contains(errMsg, "no storage config") {
		t.Fatalf("expected 'no storage config' hint, got %q", errMsg)
	}
}

func TestResolveStorageTestConfig_LoaderError(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	_, status, errMsg := resolveStorageTestConfig(req, func(ctx context.Context) (*models.UserCloudStorageConfig, error) {
		return nil, errors.New("db boom")
	})
	if status != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", status)
	}
	if errMsg == "" {
		t.Fatalf("expected non-empty error message")
	}
}

func TestResolveStorageTestConfig_InvalidJSON(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{not json"))
	_, status, errMsg := resolveStorageTestConfig(req, func(ctx context.Context) (*models.UserCloudStorageConfig, error) {
		t.Fatal("loader should not be invoked for malformed body")
		return nil, nil
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
	if errMsg == "" {
		t.Fatal("expected error message")
	}
}

func TestCloudConfigFromRequest_S3DefaultsRegion(t *testing.T) {
	t.Parallel()

	cfg, _, errMsg := cloudConfigFromRequest(&updateCloudStorageConfigRequest{
		Provider:        "s3",
		Bucket:          "b",
		AccessKeyID:     "k",
		SecretAccessKey: "s",
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %q", errMsg)
	}
	if cfg.Region != "us-east-1" {
		t.Fatalf("expected default region 'us-east-1', got %q", cfg.Region)
	}
}

func TestCloudConfigFromRequest_S3MissingFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  updateCloudStorageConfigRequest
		want string
	}{
		{
			name: "no bucket",
			req:  updateCloudStorageConfigRequest{Provider: "s3", AccessKeyID: "k", SecretAccessKey: "s"},
			want: "bucket is required",
		},
		{
			name: "no credentials",
			req:  updateCloudStorageConfigRequest{Provider: "s3", Bucket: "b"},
			want: "accessKeyId and secretAccessKey",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, status, errMsg := cloudConfigFromRequest(&tc.req)
			if status != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", status)
			}
			if !strings.Contains(errMsg, tc.want) {
				t.Fatalf("expected error to contain %q, got %q", tc.want, errMsg)
			}
		})
	}
}

func TestCloudConfigFromRequest_BadProvider(t *testing.T) {
	t.Parallel()

	_, status, errMsg := cloudConfigFromRequest(&updateCloudStorageConfigRequest{Provider: "azure"})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
	if !strings.Contains(errMsg, "'s3'") && !strings.Contains(errMsg, "'gcs'") {
		t.Fatalf("expected provider hint, got %q", errMsg)
	}
}

func TestReadMaybeEmptyJSON_LargeBodyRejected(t *testing.T) {
	t.Parallel()

	// 2 MiB body — must reject. Reading 1 MiB+1 byte triggers the cap, but
	// we send 2 MiB to make the test resilient to off-by-one tweaks.
	big := bytes.Repeat([]byte("x"), 2<<20)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(big))
	_, err := readMaybeEmptyJSON(req)
	if err == nil {
		t.Fatal("expected error for oversize body")
	}
}
