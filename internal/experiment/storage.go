package experiment

import (
	"context"

	"github.com/google/uuid"
)

// ExperimentStorage defines the interface for experiment persistence.
type ExperimentStorage interface {
	// GetActiveExperiments returns active experiments matching this API key, user, or org.
	GetActiveExperiments(ctx context.Context, apiKeyID uuid.UUID, userID, orgID *uuid.UUID) ([]*Experiment, error)

	// CRUD
	CreateExperiment(ctx context.Context, exp *Experiment) error
	GetExperiment(ctx context.Context, id uuid.UUID) (*Experiment, error)
	ListExperiments(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, limit, offset int) ([]*ExperimentListItem, int, error)
	UpdateExperiment(ctx context.Context, id uuid.UUID, input *UpdateExperimentInput) error
	DeleteExperiment(ctx context.Context, id uuid.UUID) error
	UpdateExperimentStatus(ctx context.Context, id uuid.UUID, status string) error

	// Variants
	CreateVariant(ctx context.Context, v *Variant) error
	UpdateVariant(ctx context.Context, id uuid.UUID, input *UpdateVariantInput) error
	DeleteVariant(ctx context.Context, id uuid.UUID) error
	ListVariants(ctx context.Context, experimentID uuid.UUID) ([]*Variant, error)

	// Sticky assignments
	GetAssignment(ctx context.Context, experimentID uuid.UUID, subjectHash string) (*Assignment, error)
	CreateAssignment(ctx context.Context, a *Assignment) error

	// Conflict check: only one active experiment per API key scope
	HasActiveExperiment(ctx context.Context, apiKeyID *uuid.UUID, userID uuid.UUID, orgID *uuid.UUID, excludeID *uuid.UUID) (bool, error)
}

// ExperimentListItem is a lightweight view for listing experiments.
type ExperimentListItem struct {
	Experiment
	VariantCount int `db:"variant_count"`
}
