package api

import (
	"net/http"

	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/secrets"
)

// mergedS3Credentials is the result of mergeS3Credentials — both the
// plaintext form (so the handler can run ValidateS3Config against it) and
// the encrypted form (so the handler can persist without re-encrypting).
type mergedS3Credentials struct {
	accessKeyID              string
	secretAccessKey          string
	encryptedAccessKeyID     string
	encryptedSecretAccessKey string
}

// mergeS3Credentials reconciles a partial S3 credentials update against the
// previously-saved row. The caller supplies whatever the request body has
// (which may be empty strings); this helper fills in missing pieces from
// the existing encrypted columns when the user is editing an already-saved
// S3 config.
//
//   - When both reqAccessKeyID and reqSecretAccessKey are non-empty, they're
//     accepted as-is and encrypted (initial setup OR explicit re-entry of
//     both keys).
//   - When either is empty AND the existing row has both encrypted columns
//     AND the existing provider matches the target ("s3" or ""), the missing
//     value is pulled from the saved encrypted column (decrypted only into
//     the plaintext form, never persisted plaintext).
//   - When neither path can populate both fields, we return a 400 with the
//     same message the old behavior produced.
//
// `targetProvider` is "" for the legacy /me/s3-config endpoint (no provider
// concept) or "s3" / "gcs" for the new endpoint. When the user is switching
// providers we must NOT pull old creds across — the saved keys belong to a
// different provider's bucket.
func mergeS3Credentials(
	reqAccessKeyID, reqSecretAccessKey string,
	existing existingStorageConfig,
	secretStore secrets.SecretStore,
	targetProvider string,
) (*mergedS3Credentials, int, string) {
	if reqAccessKeyID != "" && reqSecretAccessKey != "" {
		// Initial setup or explicit re-entry. Encrypt both and we're done.
		encAccess, err := secretStore.Encrypt(reqAccessKeyID)
		if err != nil {
			return nil, http.StatusInternalServerError, "failed to encrypt accessKeyId"
		}
		encSecret, err := secretStore.Encrypt(reqSecretAccessKey)
		if err != nil {
			return nil, http.StatusInternalServerError, "failed to encrypt secretAccessKey"
		}
		return &mergedS3Credentials{
			accessKeyID:              reqAccessKeyID,
			secretAccessKey:          reqSecretAccessKey,
			encryptedAccessKeyID:     encAccess,
			encryptedSecretAccessKey: encSecret,
		}, 0, ""
	}

	// Partial input → only valid if we can fall back to saved encrypted
	// credentials AND the user isn't switching providers.
	savedProvider := existing.cloudStorageProviderOrEmpty()
	if targetProvider != "" && savedProvider != "" && savedProvider != targetProvider {
		// Switching providers without re-supplying creds — refuse rather
		// than silently mixing.
		return nil, http.StatusBadRequest, "accessKeyId and secretAccessKey are required when switching storage providers"
	}
	if existing.encryptedAccessKeyID() == "" || existing.encryptedSecretAccessKey() == "" {
		return nil, http.StatusBadRequest, "accessKeyId and secretAccessKey are required for S3"
	}

	encAccess := existing.encryptedAccessKeyID()
	encSecret := existing.encryptedSecretAccessKey()

	// Resolve plaintext values for the merged fields. When the request
	// supplies one half (rare but possible — the dashboard might prefill
	// access key but not secret), we still encrypt the new value and reuse
	// the saved one for the omitted half.
	plainAccess := reqAccessKeyID
	if plainAccess == "" {
		decrypted, err := secretStore.Decrypt(encAccess)
		if err != nil {
			return nil, http.StatusInternalServerError, "failed to decrypt saved accessKeyId"
		}
		plainAccess = decrypted
	} else {
		// New access key provided; encrypt it and stop reusing the saved
		// encryption blob.
		newEnc, err := secretStore.Encrypt(reqAccessKeyID)
		if err != nil {
			return nil, http.StatusInternalServerError, "failed to encrypt accessKeyId"
		}
		encAccess = newEnc
	}

	plainSecret := reqSecretAccessKey
	if plainSecret == "" {
		decrypted, err := secretStore.Decrypt(encSecret)
		if err != nil {
			return nil, http.StatusInternalServerError, "failed to decrypt saved secretAccessKey"
		}
		plainSecret = decrypted
	} else {
		newEnc, err := secretStore.Encrypt(reqSecretAccessKey)
		if err != nil {
			return nil, http.StatusInternalServerError, "failed to encrypt secretAccessKey"
		}
		encSecret = newEnc
	}

	return &mergedS3Credentials{
		accessKeyID:              plainAccess,
		secretAccessKey:          plainSecret,
		encryptedAccessKeyID:     encAccess,
		encryptedSecretAccessKey: encSecret,
	}, 0, ""
}

// mergedGCSCredentials is the GCS analog of mergedS3Credentials.
type mergedGCSCredentials struct {
	credentialsJSON          string
	encryptedCredentialsJSON string
}

// mergeGCSCredentials is the GCS counterpart to mergeS3Credentials. When the
// request omits gcsCredentialsJson, we reuse the saved encrypted blob.
func mergeGCSCredentials(
	reqCredentialsJSON string,
	existing existingStorageConfig,
	secretStore secrets.SecretStore,
	targetProvider string,
) (*mergedGCSCredentials, int, string) {
	if reqCredentialsJSON != "" {
		enc, err := secretStore.Encrypt(reqCredentialsJSON)
		if err != nil {
			return nil, http.StatusInternalServerError, "failed to encrypt gcsCredentialsJson"
		}
		return &mergedGCSCredentials{
			credentialsJSON:          reqCredentialsJSON,
			encryptedCredentialsJSON: enc,
		}, 0, ""
	}

	savedProvider := existing.cloudStorageProviderOrEmpty()
	if targetProvider != "" && savedProvider != "" && savedProvider != targetProvider {
		return nil, http.StatusBadRequest, "gcsCredentialsJson is required when switching storage providers"
	}
	if existing.encryptedGCSCredentialsJSON() == "" {
		return nil, http.StatusBadRequest, "gcsCredentialsJson is required for GCS"
	}

	plain, err := secretStore.Decrypt(existing.encryptedGCSCredentialsJSON())
	if err != nil {
		return nil, http.StatusInternalServerError, "failed to decrypt saved gcsCredentialsJson"
	}

	return &mergedGCSCredentials{
		credentialsJSON:          plain,
		encryptedCredentialsJSON: existing.encryptedGCSCredentialsJSON(),
	}, 0, ""
}

// existingStorageConfig is the narrow view of an owner row (user or org)
// that the merge helpers need. Keeping it as an interface lets the user
// path and the org path share one helper without leaking the underlying
// model types beyond their existing consumers.
type existingStorageConfig interface {
	cloudStorageProviderOrEmpty() string
	encryptedAccessKeyID() string
	encryptedSecretAccessKey() string
	encryptedGCSCredentialsJSON() string
}

// userStorageView adapts *models.User to existingStorageConfig.
type userStorageView struct{ u *models.User }

func (v userStorageView) cloudStorageProviderOrEmpty() string {
	if v.u == nil || v.u.CloudStorageProvider == nil {
		return ""
	}
	return *v.u.CloudStorageProvider
}
func (v userStorageView) encryptedAccessKeyID() string {
	if v.u == nil || v.u.S3AccessKeyIDEncrypted == nil {
		return ""
	}
	return *v.u.S3AccessKeyIDEncrypted
}
func (v userStorageView) encryptedSecretAccessKey() string {
	if v.u == nil || v.u.S3SecretAccessKeyEncrypted == nil {
		return ""
	}
	return *v.u.S3SecretAccessKeyEncrypted
}
func (v userStorageView) encryptedGCSCredentialsJSON() string {
	if v.u == nil || v.u.GCSCredentialsJSONEncrypted == nil {
		return ""
	}
	return *v.u.GCSCredentialsJSONEncrypted
}

// orgStorageView adapts *models.Organization to existingStorageConfig.
type orgStorageView struct{ o *models.Organization }

func (v orgStorageView) cloudStorageProviderOrEmpty() string {
	if v.o == nil || v.o.CloudStorageProvider == nil {
		return ""
	}
	return *v.o.CloudStorageProvider
}
func (v orgStorageView) encryptedAccessKeyID() string {
	if v.o == nil || v.o.S3AccessKeyIDEncrypted == nil {
		return ""
	}
	return *v.o.S3AccessKeyIDEncrypted
}
func (v orgStorageView) encryptedSecretAccessKey() string {
	if v.o == nil || v.o.S3SecretAccessKeyEncrypted == nil {
		return ""
	}
	return *v.o.S3SecretAccessKeyEncrypted
}
func (v orgStorageView) encryptedGCSCredentialsJSON() string {
	if v.o == nil || v.o.GCSCredentialsJSONEncrypted == nil {
		return ""
	}
	return *v.o.GCSCredentialsJSONEncrypted
}
