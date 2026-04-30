package experiment

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/provider"
)

// --- Mock Storage ---

type mockStorage struct {
	experiments []*Experiment
	assignments map[string]*Assignment // key: experimentID:subjectHash
}

func newMockStorage(experiments ...*Experiment) *mockStorage {
	return &mockStorage{
		experiments: experiments,
		assignments: make(map[string]*Assignment),
	}
}

func (m *mockStorage) GetActiveExperiments(_ context.Context, _ uuid.UUID, _, _ *uuid.UUID) ([]*Experiment, error) {
	return m.experiments, nil
}

func (m *mockStorage) GetAssignment(_ context.Context, experimentID uuid.UUID, subjectHash string) (*Assignment, error) {
	key := experimentID.String() + ":" + subjectHash
	if a, ok := m.assignments[key]; ok {
		return a, nil
	}
	return nil, nil
}

func (m *mockStorage) CreateAssignment(_ context.Context, a *Assignment) error {
	key := a.ExperimentID.String() + ":" + a.SubjectHash
	if _, ok := m.assignments[key]; ok {
		return nil // ON CONFLICT DO NOTHING
	}
	m.assignments[key] = a
	return nil
}

// Unused CRUD stubs to satisfy the interface
func (m *mockStorage) CreateExperiment(context.Context, *Experiment) error                             { return nil }
func (m *mockStorage) GetExperiment(context.Context, uuid.UUID) (*Experiment, error)                   { return nil, nil }
func (m *mockStorage) ListExperiments(context.Context, uuid.UUID, *uuid.UUID, int, int) ([]*ExperimentListItem, int, error) { return nil, 0, nil }
func (m *mockStorage) UpdateExperiment(context.Context, uuid.UUID, *UpdateExperimentInput) error       { return nil }
func (m *mockStorage) DeleteExperiment(context.Context, uuid.UUID) error                               { return nil }
func (m *mockStorage) UpdateExperimentStatus(context.Context, uuid.UUID, string) error                 { return nil }
func (m *mockStorage) CreateVariant(context.Context, *Variant) error                                   { return nil }
func (m *mockStorage) UpdateVariant(context.Context, uuid.UUID, *UpdateVariantInput) error             { return nil }
func (m *mockStorage) DeleteVariant(context.Context, uuid.UUID) error                                  { return nil }
func (m *mockStorage) ListVariants(context.Context, uuid.UUID) ([]*Variant, error)                     { return nil, nil }
func (m *mockStorage) HasActiveExperiment(context.Context, *uuid.UUID, uuid.UUID, *uuid.UUID, *uuid.UUID) (bool, error) { return false, nil }

// --- Helpers ---

func makeExperiment(sticky bool, variants ...*Variant) *Experiment {
	return &Experiment{
		ID:       uuid.New(),
		Name:     "test-experiment",
		Status:   StatusActive,
		Sticky:   sticky,
		Variants: variants,
	}
}

func makeVariant(name, prov, model string, weight int) *Variant {
	return &Variant{
		ID:       uuid.New(),
		Name:     name,
		Provider: prov,
		Model:    model,
		Weight:   weight,
	}
}

func makeBody(model string) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	return b
}

func routingContext(body []byte) RoutingContext {
	userID := uuid.New()
	return RoutingContext{
		APIKeyID: uuid.New(),
		UserID:   &userID,
		Provider: provider.ProviderOpenAI,
		Body:     body,
		Headers:  map[string]string{},
	}
}

// --- Tests ---

func TestRoute_NoExperiment(t *testing.T) {
	store := newMockStorage()
	router := NewRouter(store, 5*time.Minute)

	body := makeBody("gpt-4o")
	result := router.Route(context.Background(), routingContext(body))
	if result != nil {
		t.Fatal("expected nil result when no experiments exist")
	}
}

func TestRoute_SingleVariant(t *testing.T) {
	// Experiments with fewer than 2 variants should be skipped
	exp := makeExperiment(false, makeVariant("control", "openai", "gpt-4o", 1))
	store := newMockStorage(exp)
	router := NewRouter(store, 5*time.Minute)

	body := makeBody("gpt-4o")
	result := router.Route(context.Background(), routingContext(body))
	if result != nil {
		t.Fatal("expected nil result when experiment has only 1 variant")
	}
}

func TestRoute_RewritesModel(t *testing.T) {
	v1 := makeVariant("control", "openai", "gpt-4o", 0)
	v2 := makeVariant("challenger", "openai", "gpt-4o-mini", 1)
	exp := makeExperiment(false, v1, v2)
	store := newMockStorage(exp)
	router := NewRouter(store, 5*time.Minute)

	body := makeBody("gpt-4o")
	result := router.Route(context.Background(), routingContext(body))
	if result == nil {
		t.Fatal("expected routing result")
	}

	// With weight=0 on control and weight=1 on challenger, all traffic goes to challenger
	if result.VariantName != "challenger" {
		t.Fatalf("expected variant 'challenger', got %q", result.VariantName)
	}

	// Check body was rewritten
	var parsed map[string]interface{}
	json.Unmarshal(result.RewrittenBody, &parsed)
	if parsed["model"] != "gpt-4o-mini" {
		t.Fatalf("expected model 'gpt-4o-mini' in rewritten body, got %v", parsed["model"])
	}

	if result.OriginalModel != "gpt-4o" {
		t.Fatalf("expected original model 'gpt-4o', got %q", result.OriginalModel)
	}

	if result.ExperimentID != exp.ID {
		t.Fatal("experiment ID mismatch")
	}
}

func TestRoute_PreservesBodyFields(t *testing.T) {
	v1 := makeVariant("a", "openai", "gpt-4o", 0)
	v2 := makeVariant("b", "openai", "gpt-4o-mini", 1)
	exp := makeExperiment(false, v1, v2)
	store := newMockStorage(exp)
	router := NewRouter(store, 5*time.Minute)

	body, _ := json.Marshal(map[string]interface{}{
		"model":       "gpt-4o",
		"temperature": 0.7,
		"max_tokens":  2048,
		"messages":    []map[string]string{{"role": "user", "content": "test"}},
	})

	result := router.Route(context.Background(), routingContext(body))
	if result == nil {
		t.Fatal("expected routing result")
	}

	var parsed map[string]interface{}
	json.Unmarshal(result.RewrittenBody, &parsed)

	if parsed["temperature"] != 0.7 {
		t.Fatalf("temperature should be preserved, got %v", parsed["temperature"])
	}
	if parsed["max_tokens"] != float64(2048) {
		t.Fatalf("max_tokens should be preserved, got %v", parsed["max_tokens"])
	}
}

func TestRoute_SameModelNoRewrite(t *testing.T) {
	v1 := makeVariant("control", "openai", "gpt-4o", 0)
	v2 := makeVariant("challenger", "openai", "gpt-4o", 1)
	exp := makeExperiment(false, v1, v2)
	store := newMockStorage(exp)
	router := NewRouter(store, 5*time.Minute)

	body := makeBody("gpt-4o")
	rc := routingContext(body)
	result := router.Route(context.Background(), rc)
	if result == nil {
		t.Fatal("expected routing result even for same model (for tracking)")
	}

	// Body should be unchanged (same pointer)
	if &result.RewrittenBody[0] != &rc.Body[0] {
		t.Fatal("expected body to not be rewritten when model matches")
	}
	if result.ProviderChanged {
		t.Fatal("provider should not change")
	}
}

func TestWeightedRandomSelect_Distribution(t *testing.T) {
	v1 := makeVariant("a", "openai", "gpt-4o", 70)
	v2 := makeVariant("b", "openai", "gpt-4o-mini", 30)

	counts := map[string]int{}
	iterations := 10000
	for i := 0; i < iterations; i++ {
		v := weightedRandomSelect([]*Variant{v1, v2})
		counts[v.Name]++
	}

	// Check roughly 70/30 distribution (with generous tolerance for randomness)
	ratioA := float64(counts["a"]) / float64(iterations)
	if math.Abs(ratioA-0.70) > 0.05 {
		t.Fatalf("expected ~70%% for variant a, got %.2f%% (counts: a=%d, b=%d)", ratioA*100, counts["a"], counts["b"])
	}
}

func TestWeightedRandomSelect_ZeroWeight(t *testing.T) {
	v1 := makeVariant("a", "openai", "gpt-4o", 0)
	v2 := makeVariant("b", "openai", "gpt-4o-mini", 1)

	for i := 0; i < 100; i++ {
		v := weightedRandomSelect([]*Variant{v1, v2})
		if v.Name != "b" {
			t.Fatalf("variant with weight=0 should never be selected, got %q", v.Name)
		}
	}
}

func TestStickySelect_ConsistentAssignment(t *testing.T) {
	v1 := makeVariant("control", "openai", "gpt-4o", 50)
	v2 := makeVariant("challenger", "openai", "gpt-4o-mini", 50)
	exp := makeExperiment(true, v1, v2)
	store := newMockStorage(exp)
	router := NewRouter(store, 5*time.Minute)

	body := makeBody("gpt-4o")
	rc := routingContext(body)

	// First call assigns a variant
	result1 := router.Route(context.Background(), rc)
	if result1 == nil {
		t.Fatal("expected routing result")
	}

	// Subsequent calls with same API key should get the same variant
	for i := 0; i < 10; i++ {
		result := router.Route(context.Background(), rc)
		if result.VariantName != result1.VariantName {
			t.Fatalf("sticky assignment changed on iteration %d: %q != %q", i, result.VariantName, result1.VariantName)
		}
	}
}

func TestRewriteModelInBody(t *testing.T) {
	body := makeBody("gpt-4o")
	rewritten, err := rewriteModelInBody(body, "claude-opus-4-6")
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(rewritten, &parsed)
	if parsed["model"] != "claude-opus-4-6" {
		t.Fatalf("expected model claude-opus-4-6, got %v", parsed["model"])
	}

	// Messages should be preserved
	msgs, ok := parsed["messages"]
	if !ok || msgs == nil {
		t.Fatal("messages field should be preserved")
	}
}

func TestRewriteModelInBody_InvalidJSON(t *testing.T) {
	_, err := rewriteModelInBody([]byte("not json"), "gpt-4o")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestExtractModelFromBody(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		expected string
	}{
		{"valid", makeBody("gpt-4o"), "gpt-4o"},
		{"empty model", makeBody(""), ""},
		{"invalid json", []byte("not json"), ""},
		{"no model field", []byte(`{"messages":[]}`), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractModelFromBody(tt.body)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestResolveProviderChange(t *testing.T) {
	tests := []struct {
		name        string
		from, to    provider.Provider
		expectNil   bool
		expectProv  provider.Provider
	}{
		{"same provider", provider.ProviderOpenAI, provider.ProviderOpenAI, false, provider.ProviderOpenAI},
		{"openai to anthropic", provider.ProviderOpenAI, provider.ProviderAnthropic, false, provider.ProviderAnthropicOpenAI},
		{"anthropic to openai (unsupported)", provider.ProviderAnthropic, provider.ProviderOpenAI, true, ""},
		{"gemini to anthropic (unsupported)", provider.ProviderGemini, provider.ProviderOpenAI, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := resolveProviderChange(tt.from, tt.to)
			if tt.expectNil {
				if info != nil {
					t.Fatalf("expected nil, got %+v", info)
				}
				return
			}
			if info == nil {
				t.Fatal("expected non-nil provider info")
			}
			if info.Provider != tt.expectProv {
				t.Fatalf("expected provider %s, got %s", tt.expectProv, info.Provider)
			}
		})
	}
}

func TestCacheTTL(t *testing.T) {
	cache := newExperimentCache(50 * time.Millisecond)

	apiKeyID := uuid.New()
	ownerID := uuid.New()

	exps := []*Experiment{{ID: uuid.New(), Name: "test"}}
	cache.set(apiKeyID, ownerID, exps)

	// Should be cached
	got, ok := cache.get(apiKeyID, ownerID)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got) != 1 {
		t.Fatal("expected 1 experiment")
	}

	// Wait for TTL expiry
	time.Sleep(60 * time.Millisecond)

	_, ok = cache.get(apiKeyID, ownerID)
	if ok {
		t.Fatal("expected cache miss after TTL")
	}
}

func TestCacheInvalidateAll(t *testing.T) {
	cache := newExperimentCache(5 * time.Minute)

	k1 := uuid.New()
	k2 := uuid.New()
	owner := uuid.New()

	cache.set(k1, owner, []*Experiment{{ID: uuid.New()}})
	cache.set(k2, owner, []*Experiment{{ID: uuid.New()}})

	cache.invalidateAll()

	_, ok1 := cache.get(k1, owner)
	_, ok2 := cache.get(k2, owner)
	if ok1 || ok2 {
		t.Fatal("expected all entries cleared")
	}
}
