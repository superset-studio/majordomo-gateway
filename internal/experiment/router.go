package experiment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/provider"
)

// Router selects experiment variants for incoming requests.
type Router struct {
	store    ExperimentStorage
	cache    *experimentCache
	cacheTTL time.Duration
}

// NewRouter creates a Router with the given storage and cache TTL.
func NewRouter(store ExperimentStorage, cacheTTL time.Duration) *Router {
	return &Router{
		store:    store,
		cache:    newExperimentCache(cacheTTL),
		cacheTTL: cacheTTL,
	}
}

// Route checks for an active experiment and returns a RoutingResult if the request
// should be rerouted. Returns nil if no experiment matches.
func (r *Router) Route(ctx context.Context, rc RoutingContext) *RoutingResult {
	exp := r.findExperiment(ctx, rc)
	if exp == nil || len(exp.Variants) < 2 {
		return nil
	}

	variant := r.selectVariant(ctx, exp, rc)
	if variant == nil {
		return nil
	}

	originalModel := extractModelFromBody(rc.Body)

	// If the selected variant matches what the client already sent, still record
	// the experiment metadata but skip body rewriting.
	if variant.Model == originalModel && provider.Provider(variant.Provider) == rc.Provider {
		return &RoutingResult{
			RewrittenBody:  rc.Body,
			ProviderChanged: false,
			ExperimentID:   exp.ID,
			ExperimentName: exp.Name,
			VariantName:    variant.Name,
			OriginalModel:  originalModel,
		}
	}

	rewritten, err := rewriteModelInBody(rc.Body, variant.Model)
	if err != nil {
		slog.Warn("experiment: failed to rewrite model in body", "error", err, "experiment", exp.ID)
		return nil
	}

	result := &RoutingResult{
		RewrittenBody:  rewritten,
		ProviderChanged: false,
		ExperimentID:   exp.ID,
		ExperimentName: exp.Name,
		VariantName:    variant.Name,
		OriginalModel:  originalModel,
	}

	// If the variant targets a different provider, update provider info.
	variantProvider := provider.Provider(variant.Provider)
	if variantProvider != rc.Provider {
		newInfo := resolveProviderChange(rc.Provider, variantProvider)
		if newInfo != nil {
			result.NewProviderInfo = *newInfo
			result.ProviderChanged = true
		} else {
			// Unsupported cross-provider translation — skip routing.
			slog.Warn("experiment: unsupported cross-provider routing",
				"from", rc.Provider, "to", variant.Provider, "experiment", exp.ID)
			return nil
		}
	}

	return result
}

// InvalidateCache clears the experiment cache. Call after status changes.
func (r *Router) InvalidateCache() {
	r.cache.invalidateAll()
}

// findExperiment returns the first active experiment matching this request.
func (r *Router) findExperiment(ctx context.Context, rc RoutingContext) *Experiment {
	ownerID := ownerIDFromContext(rc)

	experiments, ok := r.cache.get(rc.APIKeyID, ownerID)
	if !ok {
		var err error
		experiments, err = r.store.GetActiveExperiments(ctx, rc.APIKeyID, rc.UserID, rc.OrgID)
		if err != nil {
			slog.Warn("experiment: failed to fetch active experiments", "error", err)
			return nil
		}
		r.cache.set(rc.APIKeyID, ownerID, experiments)
	}

	if len(experiments) == 0 {
		return nil
	}
	return experiments[0]
}

// selectVariant picks a variant using weighted random selection or sticky assignment.
func (r *Router) selectVariant(ctx context.Context, exp *Experiment, rc RoutingContext) *Variant {
	if exp.Sticky {
		return r.stickySelect(ctx, exp, rc)
	}
	return weightedRandomSelect(exp.Variants)
}

// stickySelect ensures the same subject always gets the same variant.
func (r *Router) stickySelect(ctx context.Context, exp *Experiment, rc RoutingContext) *Variant {
	subjectHash := computeSubjectHash(exp, rc)

	// Check existing assignment
	assignment, err := r.store.GetAssignment(ctx, exp.ID, subjectHash)
	if err != nil {
		slog.Warn("experiment: failed to get assignment", "error", err)
		return weightedRandomSelect(exp.Variants)
	}
	if assignment != nil {
		for _, v := range exp.Variants {
			if v.ID == assignment.VariantID {
				return v
			}
		}
	}

	// New subject — assign via weighted random
	variant := weightedRandomSelect(exp.Variants)
	if variant == nil {
		return nil
	}

	err = r.store.CreateAssignment(ctx, &Assignment{
		ID:           uuid.New(),
		ExperimentID: exp.ID,
		VariantID:    variant.ID,
		SubjectHash:  subjectHash,
	})
	if err != nil {
		slog.Warn("experiment: failed to create assignment", "error", err)
	}

	// Re-read in case of race (another goroutine may have won the insert)
	assignment, err = r.store.GetAssignment(ctx, exp.ID, subjectHash)
	if err == nil && assignment != nil {
		for _, v := range exp.Variants {
			if v.ID == assignment.VariantID {
				return v
			}
		}
	}

	return variant
}

// weightedRandomSelect picks a variant using crypto/rand weighted selection.
func weightedRandomSelect(variants []*Variant) *Variant {
	if len(variants) == 0 {
		return nil
	}

	totalWeight := 0
	for _, v := range variants {
		totalWeight += v.Weight
	}
	if totalWeight <= 0 {
		return variants[0]
	}

	n, err := rand.Int(rand.Reader, big.NewInt(int64(totalWeight)))
	if err != nil {
		// Fallback to first variant on RNG failure (extremely unlikely)
		return variants[0]
	}
	r := int(n.Int64())

	cumulative := 0
	for _, v := range variants {
		cumulative += v.Weight
		if r < cumulative {
			return v
		}
	}
	return variants[len(variants)-1]
}

// computeSubjectHash creates a SHA256 hash for sticky assignment identity.
func computeSubjectHash(exp *Experiment, rc RoutingContext) string {
	identity := rc.APIKeyID.String()

	// If experiment specifies a custom sticky key header, include its value
	if exp.StickyKeyHeader != nil && *exp.StickyKeyHeader != "" {
		headerKey := "x-majordomo-" + *exp.StickyKeyHeader
		if val, ok := rc.Headers[headerKey]; ok {
			identity += ":" + val
		}
	}

	h := sha256.Sum256([]byte(identity))
	return fmt.Sprintf("%x", h)
}

// extractModelFromBody reads the "model" field from a JSON request body.
func extractModelFromBody(body []byte) string {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	return req.Model
}

// rewriteModelInBody replaces the "model" field in the JSON body.
func rewriteModelInBody(body []byte, newModel string) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal body: %w", err)
	}

	modelBytes, err := json.Marshal(newModel)
	if err != nil {
		return nil, fmt.Errorf("marshal model: %w", err)
	}
	raw["model"] = modelBytes

	return json.Marshal(raw)
}

// resolveProviderChange determines if a cross-provider route is supported.
// Returns the new ProviderInfo or nil if the translation is not supported.
func resolveProviderChange(from, to provider.Provider) *provider.ProviderInfo {
	// Currently only OpenAI-format → Anthropic is supported via the
	// existing ProviderAnthropicOpenAI translation path.
	if isOpenAIFormat(from) && to == provider.ProviderAnthropic {
		return &provider.ProviderInfo{
			Provider: provider.ProviderAnthropicOpenAI,
			BaseURL:  "https://api.anthropic.com",
		}
	}
	// Same-provider routing (e.g., different models within OpenAI) is always fine.
	if from == to {
		return &provider.ProviderInfo{Provider: to}
	}
	return nil
}

func isOpenAIFormat(p provider.Provider) bool {
	return p == provider.ProviderOpenAI || p == provider.ProviderAzure || p == provider.ProviderGeminiOpenAI
}

func ownerIDFromContext(rc RoutingContext) uuid.UUID {
	if rc.UserID != nil {
		return *rc.UserID
	}
	if rc.OrgID != nil {
		return *rc.OrgID
	}
	return rc.APIKeyID
}
