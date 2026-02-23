package storage

import (
	"time"

	"github.com/google/uuid"
)

// BodyStorage is the interface for storing full request/response bodies
// in an external object store (S3, GCS, etc.).
type BodyStorage interface {
	GenerateKey(keyPrefix string, requestID uuid.UUID, timestamp time.Time) string
	Upload(upload *BodyUpload)
	Close() error
}
