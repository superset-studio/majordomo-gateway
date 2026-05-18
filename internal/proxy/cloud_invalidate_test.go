package proxy

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// TestInvalidateUserCloudStorage_DropsProxyCache asserts that
// InvalidateUserCloudStorage clears the proxy's per-user config cache.
// The body-storage side of the invalidation is exercised in the storage
// package's own TestUserBodyStorage_Invalidate_DropsCachedClient.
func TestInvalidateUserCloudStorage_DropsProxyCache(t *testing.T) {
	t.Parallel()

	h := &Handler{
		userBodyStorage: storage.NewUserBodyStorage(),
		cloudCacheTTL:   5 * time.Minute,
	}
	userID := uuid.New()

	h.userCloudCache.Store(userID.String(), &cachedCloudStorageConfig{
		config: &models.UserCloudStorageConfig{
			Provider: models.CloudStorageProviderS3,
			Bucket:   "stale",
			Region:   "us-east-1",
		},
		fetchedAt: time.Now(),
	})

	h.InvalidateUserCloudStorage(userID)

	if _, ok := h.userCloudCache.Load(userID.String()); ok {
		t.Fatal("proxy userCloudCache still contains entry after invalidate")
	}
}

// TestInvalidateOrgCloudStorage_DropsProxyCache is the org analog.
func TestInvalidateOrgCloudStorage_DropsProxyCache(t *testing.T) {
	t.Parallel()

	h := &Handler{
		userBodyStorage: storage.NewUserBodyStorage(),
		cloudCacheTTL:   5 * time.Minute,
	}
	orgID := uuid.New()

	h.orgCloudCache.Store(orgID.String(), &cachedCloudStorageConfig{
		config: &models.UserCloudStorageConfig{
			Provider: models.CloudStorageProviderS3,
			Bucket:   "stale-org",
			Region:   "us-east-1",
		},
		fetchedAt: time.Now(),
	})

	h.InvalidateOrgCloudStorage(orgID)

	if _, ok := h.orgCloudCache.Load(orgID.String()); ok {
		t.Fatal("proxy orgCloudCache still contains entry after invalidate")
	}
}

func TestInvalidate_NilReceiverIsSafe(t *testing.T) {
	t.Parallel()

	// Should not panic — useful when partial wiring (e.g. tests, or a
	// future bootstrap that constructs handlers before the proxy) calls
	// the invalidator with a nil receiver.
	var h *Handler
	h.InvalidateUserCloudStorage(uuid.New())
	h.InvalidateOrgCloudStorage(uuid.New())
}

func TestInvalidate_NoBodyStorageIsSafe(t *testing.T) {
	t.Parallel()

	// userBodyStorage is nil — Invalidate must still clear the proxy
	// cache and not panic trying to forward to the missing dependency.
	h := &Handler{cloudCacheTTL: 5 * time.Minute}
	id := uuid.New()
	h.userCloudCache.Store(id.String(), &cachedCloudStorageConfig{
		config:    &models.UserCloudStorageConfig{Provider: models.CloudStorageProviderS3},
		fetchedAt: time.Now(),
	})

	h.InvalidateUserCloudStorage(id)
	if _, ok := h.userCloudCache.Load(id.String()); ok {
		t.Fatal("proxy cache not cleared when userBodyStorage is nil")
	}
}

// TestInvalidateUserCloudStorage_Idempotent confirms a second invalidate
// for the same owner is a no-op (no panic, no error).
func TestInvalidateUserCloudStorage_Idempotent(t *testing.T) {
	t.Parallel()

	h := &Handler{
		userBodyStorage: storage.NewUserBodyStorage(),
		cloudCacheTTL:   5 * time.Minute,
	}
	id := uuid.New()
	h.InvalidateUserCloudStorage(id)
	h.InvalidateUserCloudStorage(id) // second call must be safe
}
