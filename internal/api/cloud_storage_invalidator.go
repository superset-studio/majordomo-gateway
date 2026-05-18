package api

import "github.com/google/uuid"

// CloudStorageInvalidator drops cached cloud-storage state for one owner so a
// dashboard config change (or explicit reload) takes effect immediately
// rather than after the proxy's in-memory TTL expires.
//
// This is satisfied by *proxy.Handler. Keeping it as an interface lets the
// api package call into the proxy without importing it (which would cycle:
// proxy already imports api types indirectly via shared models).
//
// All methods must be safe to call concurrently from any HTTP handler
// goroutine — implementations should not block.
type CloudStorageInvalidator interface {
	// InvalidateUserCloudStorage drops the cached config + client for one
	// user. Called after PUT/DELETE on /me/cloud-storage-config (or the
	// legacy /me/s3-config).
	InvalidateUserCloudStorage(userID uuid.UUID)

	// InvalidateOrgCloudStorage drops the cached config + client for one
	// organization. Called after PUT/DELETE on /orgs/current/cloud-storage-config.
	InvalidateOrgCloudStorage(orgID uuid.UUID)
}
