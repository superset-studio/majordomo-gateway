package storage

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGCSBodyStorage_GenerateKey(t *testing.T) {
	s := &GCSBodyStorage{bucket: "test-bucket"}
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ts := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name      string
		keyPrefix string
		wantKey   string
	}{
		{
			name:      "short prefix used as-is",
			keyPrefix: "abc123",
			wantKey:   "abc123/2024-03-15/11111111-1111-1111-1111-111111111111.json.gz",
		},
		{
			name:      "prefix longer than 16 chars is truncated",
			keyPrefix: "abcdefghijklmnopqrstuvwxyz",
			wantKey:   "abcdefghijklmnop/2024-03-15/11111111-1111-1111-1111-111111111111.json.gz",
		},
		{
			name:      "exactly 16 chars not truncated",
			keyPrefix: "abcdefghijklmnop",
			wantKey:   "abcdefghijklmnop/2024-03-15/11111111-1111-1111-1111-111111111111.json.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.GenerateKey(tt.keyPrefix, requestID, ts)
			if got != tt.wantKey {
				t.Errorf("GenerateKey() = %q, want %q", got, tt.wantKey)
			}
		})
	}
}

func TestGenerateUserGCSClaudeCodeKey(t *testing.T) {
	sessionID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	requestID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	ts := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
	sessionName := "my-session"

	tests := []struct {
		name        string
		apiKeyName  string
		sessionName *string
		wantPrefix  string
		wantSuffix  string
	}{
		{
			name:        "with session name",
			apiKeyName:  "my-key",
			sessionName: &sessionName,
			wantPrefix:  "claude-code/my-key/my-session-22222222-2222-2222-2222-222222222222/2024-03-15/",
			wantSuffix:  ".json.gz",
		},
		{
			name:        "without session name uses session ID only",
			apiKeyName:  "my-key",
			sessionName: nil,
			wantPrefix:  "claude-code/my-key/22222222-2222-2222-2222-222222222222/2024-03-15/",
			wantSuffix:  ".json.gz",
		},
		{
			name:        "empty session name uses session ID only",
			apiKeyName:  "my-key",
			sessionName: func() *string { s := ""; return &s }(),
			wantPrefix:  "claude-code/my-key/22222222-2222-2222-2222-222222222222/2024-03-15/",
			wantSuffix:  ".json.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateUserGCSClaudeCodeKey(tt.apiKeyName, sessionID, tt.sessionName, requestID, ts)
			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("key %q does not start with %q", got, tt.wantPrefix)
			}
			if !strings.HasSuffix(got, tt.wantSuffix) {
				t.Errorf("key %q does not end with %q", got, tt.wantSuffix)
			}
			if !strings.Contains(got, requestID.String()) {
				t.Errorf("key %q does not contain request ID %q", got, requestID.String())
			}
		})
	}
}

func TestGenerateUserGCSRequestKey(t *testing.T) {
	requestID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	ts := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)

	got := GenerateUserGCSRequestKey("my-key", requestID, ts)
	want := "requests/my-key/2024-03-15/44444444-4444-4444-4444-444444444444.json.gz"
	if got != want {
		t.Errorf("GenerateUserGCSRequestKey() = %q, want %q", got, want)
	}
}

// TestGCSKeyFormat_MatchesS3 verifies that GCS key formats are identical to their
// S3 counterparts, ensuring the body_s3_key column stores consistent paths across
// both backends.
func TestGCSKeyFormat_MatchesS3(t *testing.T) {
	sessionID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	requestID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	ts := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
	sessionName := "test-session"
	apiKeyName := "test-key"

	gcsCC := GenerateUserGCSClaudeCodeKey(apiKeyName, sessionID, &sessionName, requestID, ts)
	s3CC := GenerateUserS3ClaudeCodeKey(apiKeyName, sessionID, &sessionName, requestID, ts)
	if gcsCC != s3CC {
		t.Errorf("claude-code key mismatch: GCS=%q S3=%q", gcsCC, s3CC)
	}

	gcsReq := GenerateUserGCSRequestKey(apiKeyName, requestID, ts)
	s3Req := GenerateUserS3RequestKey(apiKeyName, requestID, ts)
	if gcsReq != s3Req {
		t.Errorf("request key mismatch: GCS=%q S3=%q", gcsReq, s3Req)
	}
}
