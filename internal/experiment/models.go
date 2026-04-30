package experiment

import (
	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/provider"
)

// Status constants for experiment lifecycle.
const (
	StatusDraft     = "draft"
	StatusActive    = "active"
	StatusPaused    = "paused"
	StatusCompleted = "completed"
)

// Experiment represents an A/B test that routes traffic between model variants.
type Experiment struct {
	ID             uuid.UUID  `db:"id"`
	UserID         *uuid.UUID `db:"user_id"`
	OrgID          *uuid.UUID `db:"org_id"`
	APIKeyID       *uuid.UUID `db:"api_key_id"`
	Name           string     `db:"name"`
	Description    *string    `db:"description"`
	Status         string     `db:"status"`
	Sticky         bool       `db:"sticky"`
	StickyKeyHeader *string   `db:"sticky_key_header"`

	Variants []*Variant `db:"-"` // loaded separately
}

// Variant is a single arm of an experiment targeting a specific provider and model.
type Variant struct {
	ID           uuid.UUID `db:"id"`
	ExperimentID uuid.UUID `db:"experiment_id"`
	Name         string    `db:"name"`
	Provider     string    `db:"provider"`
	Model        string    `db:"model"`
	Weight       int       `db:"weight"`
	IsControl    bool      `db:"is_control"`
}

// Assignment records a sticky variant assignment for a subject.
type Assignment struct {
	ID           uuid.UUID `db:"id"`
	ExperimentID uuid.UUID `db:"experiment_id"`
	VariantID    uuid.UUID `db:"variant_id"`
	SubjectHash  string    `db:"subject_hash"`
}

// RoutingContext holds the information needed to make a routing decision.
type RoutingContext struct {
	APIKeyID uuid.UUID
	UserID   *uuid.UUID
	OrgID    *uuid.UUID
	Provider provider.Provider
	Body     []byte
	Headers  map[string]string
}

// RoutingResult is returned when an experiment matches and the request is rerouted.
type RoutingResult struct {
	RewrittenBody   []byte
	NewProviderInfo provider.ProviderInfo
	ProviderChanged bool

	ExperimentID   uuid.UUID
	ExperimentName string
	VariantName    string
	OriginalModel  string
}

// UpdateExperimentInput holds fields that can be updated on an experiment.
type UpdateExperimentInput struct {
	Name            *string
	Description     *string
	Sticky          *bool
	StickyKeyHeader *string
}

// UpdateVariantInput holds fields that can be updated on a variant.
type UpdateVariantInput struct {
	Name      *string
	Provider  *string
	Model     *string
	Weight    *int
	IsControl *bool
}
