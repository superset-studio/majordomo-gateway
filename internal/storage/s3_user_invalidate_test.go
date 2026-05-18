package storage

import (
	"testing"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

// TestUserBodyStorage_Invalidate_DropsCachedClient verifies that Invalidate
// removes the entry from the sync.Map so the next call to getOrCreateClient
// builds a fresh client. This is the core fix for issue #7.
func TestUserBodyStorage_Invalidate_DropsCachedClient(t *testing.T) {
	t.Parallel()

	u := NewUserBodyStorage()
	ownerID := uuid.New()

	// Prime the cache by triggering a client build. Using the S3 path with
	// minimal config — the actual AWS calls don't happen until Upload, so
	// this exercises the cache plumbing without network IO.
	cfg := &models.UserCloudStorageConfig{
		Provider:        models.CloudStorageProviderS3,
		Bucket:          "test-bucket",
		Region:          "us-east-1",
		AccessKeyID:     "AKIA...",
		SecretAccessKey: "secret",
	}

	client1, err := u.getOrCreateClient(ownerID, cfg)
	if err != nil {
		t.Fatalf("first getOrCreateClient: %v", err)
	}

	// Same owner + same hash → cached pointer reused.
	client2, err := u.getOrCreateClient(ownerID, cfg)
	if err != nil {
		t.Fatalf("second getOrCreateClient: %v", err)
	}
	if client1 != client2 {
		t.Fatal("expected cached pointer to be reused for identical config")
	}

	// Invalidate evicts the entry.
	u.Invalidate(ownerID)
	if _, ok := u.clients.Load(ownerID.String()); ok {
		t.Fatal("Invalidate did not remove the cache entry")
	}

	// Next call builds a fresh client — different pointer.
	client3, err := u.getOrCreateClient(ownerID, cfg)
	if err != nil {
		t.Fatalf("third getOrCreateClient: %v", err)
	}
	if client3 == client1 {
		t.Fatal("expected a freshly-built client after Invalidate, got the old pointer")
	}
}

func TestUserBodyStorage_Invalidate_NoEntryIsSafe(t *testing.T) {
	t.Parallel()

	u := NewUserBodyStorage()
	// Should not panic when nothing is cached.
	u.Invalidate(uuid.New())
}
