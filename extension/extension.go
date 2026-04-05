// Package extension defines the public hook interfaces for the majordomo gateway.
// These interfaces are implemented by the enterprise binary to add policy enforcement,
// tool call logging, agent traces, and other enterprise-only behaviors.
//
// This package has no dependencies on internal gateway packages, making it safe to
// import from any external module.
package extension

import (
	"context"

	"github.com/google/uuid"
)

// PolicyViolation describes why a request was blocked by a PolicyEnforcer.
// The enforcer sets HTTPStatus and Message; the handler writes the error response.
type PolicyViolation struct {
	HTTPStatus int    // e.g. 403, 429
	Message    string // returned to the caller as the error body
}

// PolicyContext holds everything known about a request at the pre-proxy enforcement
// point: after API key validation and provider detection, before forwarding upstream.
type PolicyContext struct {
	RequestID uuid.UUID
	APIKeyID  uuid.UUID
	UserID    *uuid.UUID
	OrgID     *uuid.UUID

	// Provider is the detected LLM provider (e.g. "openai", "anthropic", "gemini").
	Provider string

	RequestBody   []byte
	CustomHeaders map[string]string // X-Majordomo-* headers, prefix already stripped
}

// PolicyEnforcer is an optional synchronous hook called before each request is
// forwarded upstream. A non-nil return blocks the request with the specified
// HTTP status and message. Implementations must be safe for concurrent use.
type PolicyEnforcer interface {
	Enforce(ctx context.Context, pc PolicyContext) *PolicyViolation
}

// EnrichmentEvent holds the post-response data passed to a RequestEnricher.
// It is constructed after WriteRequestLog has been called, so the request ID
// is stable and the DB row is queued for insertion.
type EnrichmentEvent struct {
	RequestID uuid.UUID
	APIKeyID  uuid.UUID
	UserID    *uuid.UUID
	OrgID     *uuid.UUID

	// Provider is the detected LLM provider (e.g. "openai", "anthropic", "gemini").
	Provider string

	RequestBody   []byte
	ResponseBody  []byte
	CustomHeaders map[string]string
	StatusCode    int
}

// RequestEnricher is an optional async hook called after WriteRequestLog.
// It runs on the same background goroutine as logRequest — no additional goroutine
// is spawned by the handler. Implementations are responsible for their own
// concurrency, error handling, and database connections.
// Implementations must be safe for concurrent use.
type RequestEnricher interface {
	Enrich(ctx context.Context, event EnrichmentEvent)
}

// Closer is an optional interface that RequestEnricher implementations may
// implement to flush their internal work queues on graceful shutdown.
// The gateway's ShutdownWithTimeout calls Shutdown if the enricher implements this.
type Closer interface {
	Shutdown(ctx context.Context) error
}
