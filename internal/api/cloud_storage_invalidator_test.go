package api

import (
	"sync"
	"testing"

	"github.com/google/uuid"
)

// recordingInvalidator captures the calls made to a CloudStorageInvalidator so
// the per-handler write-path tests can assert that invalidation actually
// fires. Concurrency-safe so tests can drive it from multiple goroutines.
type recordingInvalidator struct {
	mu       sync.Mutex
	users    []uuid.UUID
	orgs     []uuid.UUID
}

func (r *recordingInvalidator) InvalidateUserCloudStorage(userID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users = append(r.users, userID)
}

func (r *recordingInvalidator) InvalidateOrgCloudStorage(orgID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.orgs = append(r.orgs, orgID)
}

func (r *recordingInvalidator) UserCalls() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]uuid.UUID, len(r.users))
	copy(out, r.users)
	return out
}

func (r *recordingInvalidator) OrgCalls() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]uuid.UUID, len(r.orgs))
	copy(out, r.orgs)
	return out
}

// Compile-time assertion: recordingInvalidator satisfies the interface.
// Will fail the build if the interface drifts (a positive signal — these
// tests should be updated alongside it).
var _ CloudStorageInvalidator = (*recordingInvalidator)(nil)

func TestAdminHandler_SetCloudStorageInvalidator(t *testing.T) {
	t.Parallel()

	h := &AdminHandler{}
	if h.cloudInvalidator != nil {
		t.Fatal("expected nil invalidator on zero-value handler")
	}

	inv := &recordingInvalidator{}
	h.SetCloudStorageInvalidator(inv)
	if h.cloudInvalidator != inv {
		t.Fatal("setter did not store the invalidator")
	}

	// nil should clear — confirms the field is a plain assignment, no
	// guard that ignores a nil arg.
	h.SetCloudStorageInvalidator(nil)
	if h.cloudInvalidator != nil {
		t.Fatal("setter did not clear with nil arg")
	}
}

func TestOrgHandler_SetCloudStorageInvalidator(t *testing.T) {
	t.Parallel()

	h := &OrgHandler{}
	if h.cloudInvalidator != nil {
		t.Fatal("expected nil invalidator on zero-value handler")
	}

	inv := &recordingInvalidator{}
	h.SetCloudStorageInvalidator(inv)
	if h.cloudInvalidator != inv {
		t.Fatal("setter did not store the invalidator")
	}
}
