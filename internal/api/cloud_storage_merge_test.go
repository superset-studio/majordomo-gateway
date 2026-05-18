package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/superset-studio/majordomo-gateway/internal/secrets"
)

// fakeSecretStore is a deterministic SecretStore for unit tests. The
// encryption is just a prefix swap so we can assert on call ordering and
// verify that encrypt(decrypt(x)) is identity.
type fakeSecretStore struct {
	encryptErr error
	decryptErr error
}

func (f *fakeSecretStore) Encrypt(plaintext string) (string, error) {
	if f.encryptErr != nil {
		return "", f.encryptErr
	}
	return "enc:" + plaintext, nil
}

func (f *fakeSecretStore) Decrypt(ciphertext string) (string, error) {
	if f.decryptErr != nil {
		return "", f.decryptErr
	}
	if !strings.HasPrefix(ciphertext, "enc:") {
		return "", errors.New("not encrypted")
	}
	return strings.TrimPrefix(ciphertext, "enc:"), nil
}

// Compile-time assertion that the fake satisfies the SecretStore interface.
var _ secrets.SecretStore = (*fakeSecretStore)(nil)

// stubExisting is a hand-rolled existingStorageConfig — keeps the tests
// independent of the real *models.User / *models.Organization types.
type stubExisting struct {
	provider       string
	encAccess      string
	encSecret      string
	encGCSCredJSON string
}

func (s stubExisting) cloudStorageProviderOrEmpty() string       { return s.provider }
func (s stubExisting) encryptedAccessKeyID() string              { return s.encAccess }
func (s stubExisting) encryptedSecretAccessKey() string          { return s.encSecret }
func (s stubExisting) encryptedGCSCredentialsJSON() string       { return s.encGCSCredJSON }

func TestMergeS3Credentials_BothProvidedEncrypts(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{}
	merged, _, errMsg := mergeS3Credentials("AKIA-new", "secret-new", stubExisting{}, store, "")
	if errMsg != "" {
		t.Fatalf("unexpected error: %q", errMsg)
	}
	if merged.accessKeyID != "AKIA-new" || merged.secretAccessKey != "secret-new" {
		t.Fatalf("plaintext not preserved: %+v", merged)
	}
	if merged.encryptedAccessKeyID != "enc:AKIA-new" || merged.encryptedSecretAccessKey != "enc:secret-new" {
		t.Fatalf("encryption not applied: %+v", merged)
	}
}

func TestMergeS3Credentials_BlankBothPullsFromSaved(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{}
	saved := stubExisting{
		provider:  "s3",
		encAccess: "enc:AKIA-saved",
		encSecret: "enc:secret-saved",
	}
	merged, _, errMsg := mergeS3Credentials("", "", saved, store, "s3")
	if errMsg != "" {
		t.Fatalf("unexpected error: %q", errMsg)
	}
	if merged.accessKeyID != "AKIA-saved" || merged.secretAccessKey != "secret-saved" {
		t.Fatalf("plaintext not decrypted from saved blobs: %+v", merged)
	}
	// Encrypted values should be the SAME pointers/strings as saved — we
	// don't re-encrypt when reusing.
	if merged.encryptedAccessKeyID != "enc:AKIA-saved" || merged.encryptedSecretAccessKey != "enc:secret-saved" {
		t.Fatalf("expected reused encrypted blobs, got %+v", merged)
	}
}

func TestMergeS3Credentials_OneBlankReusesThatOneOnly(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{}
	saved := stubExisting{
		provider:  "s3",
		encAccess: "enc:AKIA-saved",
		encSecret: "enc:secret-saved",
	}
	// User re-entered just the secret key, not the access key.
	merged, _, errMsg := mergeS3Credentials("", "secret-new", saved, store, "s3")
	if errMsg != "" {
		t.Fatalf("unexpected error: %q", errMsg)
	}
	if merged.accessKeyID != "AKIA-saved" {
		t.Fatalf("expected saved access key, got %q", merged.accessKeyID)
	}
	if merged.secretAccessKey != "secret-new" {
		t.Fatalf("expected new secret, got %q", merged.secretAccessKey)
	}
	if merged.encryptedAccessKeyID != "enc:AKIA-saved" {
		t.Fatalf("expected saved encrypted access blob, got %q", merged.encryptedAccessKeyID)
	}
	if merged.encryptedSecretAccessKey != "enc:secret-new" {
		t.Fatalf("expected new encrypted secret blob, got %q", merged.encryptedSecretAccessKey)
	}
}

func TestMergeS3Credentials_NoSavedAndBlankRejects(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{}
	_, status, errMsg := mergeS3Credentials("", "", stubExisting{}, store, "")
	if status != 400 {
		t.Fatalf("expected 400, got %d", status)
	}
	if !strings.Contains(errMsg, "accessKeyId") || !strings.Contains(errMsg, "secretAccessKey") {
		t.Fatalf("expected the legacy error string, got %q", errMsg)
	}
}

func TestMergeS3Credentials_ProviderSwitchRequiresFreshKeys(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{}
	saved := stubExisting{
		provider:       "gcs",
		encGCSCredJSON: "enc:{}",
	}
	// User is switching from GCS to S3 but didn't supply S3 creds — must
	// fail rather than silently reusing whatever S3 creds may or may not
	// be lingering on the row.
	_, status, errMsg := mergeS3Credentials("", "", saved, store, "s3")
	if status != 400 {
		t.Fatalf("expected 400 on provider switch, got %d", status)
	}
	if !strings.Contains(errMsg, "switching") {
		t.Fatalf("expected switching-provider hint, got %q", errMsg)
	}
}

func TestMergeS3Credentials_LegacyEndpointHasNoProviderConcept(t *testing.T) {
	t.Parallel()

	// The legacy /me/s3-config endpoint passes targetProvider="" because
	// it predates the multi-provider concept. Even when the saved row has
	// a different provider, we should still attempt the merge (the legacy
	// endpoint only ever wrote S3 credentials, so any saved encrypted-S3
	// blobs are by definition S3).
	store := &fakeSecretStore{}
	saved := stubExisting{
		provider:  "gcs", // hypothetical edge case
		encAccess: "enc:AKIA-saved",
		encSecret: "enc:secret-saved",
	}
	merged, _, errMsg := mergeS3Credentials("", "", saved, store, "")
	if errMsg != "" {
		t.Fatalf("expected legacy merge to succeed regardless of saved provider, got %q", errMsg)
	}
	if merged.accessKeyID != "AKIA-saved" {
		t.Fatalf("expected saved key reused, got %q", merged.accessKeyID)
	}
}

func TestMergeGCSCredentials_BlankPullsFromSaved(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{}
	saved := stubExisting{provider: "gcs", encGCSCredJSON: `enc:{"type":"sa"}`}
	merged, _, errMsg := mergeGCSCredentials("", saved, store, "gcs")
	if errMsg != "" {
		t.Fatalf("unexpected error: %q", errMsg)
	}
	if merged.credentialsJSON != `{"type":"sa"}` {
		t.Fatalf("expected decrypted saved blob, got %q", merged.credentialsJSON)
	}
}

func TestMergeGCSCredentials_NewProvidedEncrypts(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{}
	merged, _, errMsg := mergeGCSCredentials(`{"new":"sa"}`, stubExisting{}, store, "gcs")
	if errMsg != "" {
		t.Fatalf("unexpected error: %q", errMsg)
	}
	if merged.encryptedCredentialsJSON != `enc:{"new":"sa"}` {
		t.Fatalf("expected fresh encryption, got %q", merged.encryptedCredentialsJSON)
	}
}

func TestMergeGCSCredentials_ProviderSwitchRequiresFreshJSON(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{}
	saved := stubExisting{
		provider:  "s3",
		encAccess: "enc:AKIA",
		encSecret: "enc:s",
	}
	_, status, errMsg := mergeGCSCredentials("", saved, store, "gcs")
	if status != 400 {
		t.Fatalf("expected 400, got %d", status)
	}
	if !strings.Contains(errMsg, "switching") {
		t.Fatalf("expected switching-provider hint, got %q", errMsg)
	}
}

func TestMergeS3Credentials_EncryptFailurePropagates(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{encryptErr: errors.New("kms down")}
	_, status, errMsg := mergeS3Credentials("AKIA", "s", stubExisting{}, store, "")
	if status != 500 {
		t.Fatalf("expected 500 on encrypt failure, got %d", status)
	}
	if !strings.Contains(errMsg, "encrypt") {
		t.Fatalf("expected encrypt hint, got %q", errMsg)
	}
}

func TestMergeS3Credentials_DecryptFailurePropagates(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{decryptErr: errors.New("kms down")}
	saved := stubExisting{
		provider:  "s3",
		encAccess: "enc:AKIA-saved",
		encSecret: "enc:secret-saved",
	}
	_, status, errMsg := mergeS3Credentials("", "", saved, store, "s3")
	if status != 500 {
		t.Fatalf("expected 500 on decrypt failure, got %d", status)
	}
	if !strings.Contains(errMsg, "decrypt") {
		t.Fatalf("expected decrypt hint, got %q", errMsg)
	}
}
