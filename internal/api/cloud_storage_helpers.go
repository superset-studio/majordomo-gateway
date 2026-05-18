package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superset-studio/majordomo-gateway/internal/httputil"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/secrets"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// loadSavedConfig fetches the currently-saved cloud-storage config and
// decrypts it. Returns (nil, nil) when nothing is configured — callers
// should treat that as "no config to test."
type loadSavedConfig func(ctx context.Context) (*models.UserCloudStorageConfig, error)

// resolveStorageTestConfig builds the *UserCloudStorageConfig to test.
//
// When the request body is empty, it pulls and decrypts the owner's saved
// config via `load`. When the body is a populated updateCloudStorageConfigRequest,
// it tests *that* unsaved config — letting the dashboard validate writes
// before the user hits Save.
//
// Returns (cfg, statusCode, errMsg). When errMsg is non-empty the caller
// must write the JSON error and bail.
func resolveStorageTestConfig(
	r *http.Request,
	load loadSavedConfig,
) (*models.UserCloudStorageConfig, int, string) {
	body, err := readMaybeEmptyJSON(r)
	if err != nil {
		return nil, http.StatusBadRequest, "invalid request body"
	}

	// Empty body → test the currently-saved config.
	if body == nil {
		cfg, err := load(r.Context())
		if err != nil {
			return nil, http.StatusInternalServerError, "failed to load saved storage config"
		}
		if cfg == nil {
			return nil, http.StatusBadRequest, "no storage config saved; pass one in the body to test before saving"
		}
		return cfg, 0, ""
	}

	// Body present → use it directly. Don't persist; don't even encrypt the
	// secrets. We're just constructing an in-memory config for the probe.
	var req updateCloudStorageConfigRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, http.StatusBadRequest, "invalid request body"
	}

	cfg, status, errMsg := cloudConfigFromRequest(&req)
	if errMsg != "" {
		return nil, status, errMsg
	}
	return cfg, 0, ""
}

// runStorageTest executes the storage round-trip and writes the JSON result.
// The probe itself returns a structured result for both success and "the
// storage rejected us" failures; we only emit a 5xx for "the test couldn't
// run" errors (e.g. malformed credentials that wouldn't parse).
func runStorageTest(ctx context.Context, w http.ResponseWriter, cfg *models.UserCloudStorageConfig) {
	result, err := storage.TestCloudStorageConfig(ctx, cfg)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusInternalServerError, "storage test failed to run: "+err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, result)
}

// readMaybeEmptyJSON returns the body bytes, or (nil, nil) if the body is
// empty / whitespace. We don't want callers having to send `{}` to mean
// "test what's saved."
func readMaybeEmptyJSON(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()

	// Cap the read at 1 MiB — well above any legitimate config size, well
	// below anything that could OOM the gateway.
	const maxBody = 1 << 20
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for {
		n, err := r.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if len(buf) > maxBody {
				return nil, errors.New("body too large")
			}
		}
		if err != nil {
			break
		}
	}

	// Trim leading whitespace — `{}` and `   ` and `\n\n` should all behave.
	trimmed := buf
	for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\t' || trimmed[0] == '\r' || trimmed[0] == '\n') {
		trimmed = trimmed[1:]
	}
	if len(trimmed) == 0 {
		return nil, nil
	}
	return trimmed, nil
}

// cloudConfigFromRequest validates an updateCloudStorageConfigRequest and
// converts it into the in-memory UserCloudStorageConfig the storage tester
// expects. Returns (nil, status, errMsg) on validation failure.
func cloudConfigFromRequest(req *updateCloudStorageConfigRequest) (*models.UserCloudStorageConfig, int, string) {
	switch req.Provider {
	case "s3":
		if req.Bucket == "" {
			return nil, http.StatusBadRequest, "bucket is required for S3"
		}
		if req.AccessKeyID == "" || req.SecretAccessKey == "" {
			return nil, http.StatusBadRequest, "accessKeyId and secretAccessKey are required for S3"
		}
		region := req.Region
		if region == "" {
			region = "us-east-1"
		}
		return &models.UserCloudStorageConfig{
			Provider:        models.CloudStorageProviderS3,
			Bucket:          req.Bucket,
			Region:          region,
			Endpoint:        req.Endpoint,
			AccessKeyID:     req.AccessKeyID,
			SecretAccessKey: req.SecretAccessKey,
		}, 0, ""

	case "gcs":
		if req.GCSBucket == "" {
			return nil, http.StatusBadRequest, "gcsBucket is required for GCS"
		}
		if req.GCSCredentialsJSON == "" {
			return nil, http.StatusBadRequest, "gcsCredentialsJson is required for GCS"
		}
		return &models.UserCloudStorageConfig{
			Provider:           models.CloudStorageProviderGCS,
			GCSBucket:          req.GCSBucket,
			GCSProjectID:       req.GCSProjectID,
			GCSCredentialsJSON: req.GCSCredentialsJSON,
		}, 0, ""

	default:
		return nil, http.StatusBadRequest, "provider must be 's3' or 'gcs'"
	}
}

// decryptUserStorageConfig pulls the decrypted UserCloudStorageConfig from a
// User row. Mirrors proxy.Handler.resolveUserCloudConfig but lives in the api
// package so the storage-test endpoint doesn't need to depend on proxy.
func decryptUserStorageConfig(user *models.User, secretStore secrets.SecretStore) (*models.UserCloudStorageConfig, error) {
	if user == nil {
		return nil, nil
	}
	provider := ""
	if user.CloudStorageProvider != nil {
		provider = *user.CloudStorageProvider
	}

	switch models.CloudStorageProviderType(provider) {
	case models.CloudStorageProviderGCS:
		if user.GCSBucket == nil || *user.GCSBucket == "" || user.GCSCredentialsJSONEncrypted == nil {
			return nil, nil
		}
		credJSON, err := secretStore.Decrypt(*user.GCSCredentialsJSONEncrypted)
		if err != nil {
			return nil, err
		}
		projectID := ""
		if user.GCSProjectID != nil {
			projectID = *user.GCSProjectID
		}
		return &models.UserCloudStorageConfig{
			Provider:           models.CloudStorageProviderGCS,
			GCSBucket:          *user.GCSBucket,
			GCSProjectID:       projectID,
			GCSCredentialsJSON: credJSON,
		}, nil

	default:
		if user.S3Bucket == nil || *user.S3Bucket == "" || user.S3AccessKeyIDEncrypted == nil || user.S3SecretAccessKeyEncrypted == nil {
			return nil, nil
		}
		accessKeyID, err := secretStore.Decrypt(*user.S3AccessKeyIDEncrypted)
		if err != nil {
			return nil, err
		}
		secretAccessKey, err := secretStore.Decrypt(*user.S3SecretAccessKeyEncrypted)
		if err != nil {
			return nil, err
		}
		region := "us-east-1"
		if user.S3Region != nil {
			region = *user.S3Region
		}
		endpoint := ""
		if user.S3Endpoint != nil {
			endpoint = *user.S3Endpoint
		}
		return &models.UserCloudStorageConfig{
			Provider:        models.CloudStorageProviderS3,
			Bucket:          *user.S3Bucket,
			Region:          region,
			Endpoint:        endpoint,
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
		}, nil
	}
}

// decryptOrgStorageConfig is the Organization analog of
// decryptUserStorageConfig. Kept separate because the row types differ
// even though the storage column shapes are identical.
func decryptOrgStorageConfig(org *models.Organization, secretStore secrets.SecretStore) (*models.UserCloudStorageConfig, error) {
	if org == nil {
		return nil, nil
	}
	provider := ""
	if org.CloudStorageProvider != nil {
		provider = *org.CloudStorageProvider
	}

	switch models.CloudStorageProviderType(provider) {
	case models.CloudStorageProviderGCS:
		if org.GCSBucket == nil || *org.GCSBucket == "" || org.GCSCredentialsJSONEncrypted == nil {
			return nil, nil
		}
		credJSON, err := secretStore.Decrypt(*org.GCSCredentialsJSONEncrypted)
		if err != nil {
			return nil, err
		}
		projectID := ""
		if org.GCSProjectID != nil {
			projectID = *org.GCSProjectID
		}
		return &models.UserCloudStorageConfig{
			Provider:           models.CloudStorageProviderGCS,
			GCSBucket:          *org.GCSBucket,
			GCSProjectID:       projectID,
			GCSCredentialsJSON: credJSON,
		}, nil

	default:
		if org.S3Bucket == nil || *org.S3Bucket == "" || org.S3AccessKeyIDEncrypted == nil || org.S3SecretAccessKeyEncrypted == nil {
			return nil, nil
		}
		accessKeyID, err := secretStore.Decrypt(*org.S3AccessKeyIDEncrypted)
		if err != nil {
			return nil, err
		}
		secretAccessKey, err := secretStore.Decrypt(*org.S3SecretAccessKeyEncrypted)
		if err != nil {
			return nil, err
		}
		region := "us-east-1"
		if org.S3Region != nil {
			region = *org.S3Region
		}
		endpoint := ""
		if org.S3Endpoint != nil {
			endpoint = *org.S3Endpoint
		}
		return &models.UserCloudStorageConfig{
			Provider:        models.CloudStorageProviderS3,
			Bucket:          *org.S3Bucket,
			Region:          region,
			Endpoint:        endpoint,
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
		}, nil
	}
}
