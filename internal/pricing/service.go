package pricing

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/superset-studio/majordomo-gateway/internal/models"
)

type ModelPricing struct {
	InputPricePerMillion  float64
	OutputPricePerMillion float64
	CachedPricePerMillion float64
}

type remotePricingResponse struct {
	UpdatedAt string             `json:"updated_at"`
	Prices    []remotePriceEntry `json:"prices"`
}

type remotePriceEntry struct {
	ID          string   `json:"id"`
	Vendor      string   `json:"vendor"`
	Name        string   `json:"name"`
	Input       float64  `json:"input"`
	Output      float64  `json:"output"`
	InputCached *float64 `json:"input_cached"`
}

type fallbackPriceEntry struct {
	InputPricePerMillion  float64 `json:"input_price_per_million"`
	OutputPricePerMillion float64 `json:"output_price_per_million"`
	CachedPricePerMillion float64 `json:"cached_price_per_million"`
}

type Service struct {
	remoteURL       string
	fallbackFile    string
	aliasesFile     string
	refreshInterval time.Duration

	mu      sync.RWMutex
	pricing map[string]ModelPricing
	aliases map[string]string // maps API model name -> pricing model name

	httpClient *http.Client
	done       chan struct{}
}

func NewService(remoteURL, fallbackFile, aliasesFile string, refreshInterval time.Duration) *Service {
	s := &Service{
		remoteURL:       remoteURL,
		fallbackFile:    fallbackFile,
		aliasesFile:     aliasesFile,
		refreshInterval: refreshInterval,
		pricing:         make(map[string]ModelPricing),
		aliases:         make(map[string]string),
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		done:            make(chan struct{}),
	}

	s.loadAliases()
	s.loadPricing()
	go s.refreshLoop()

	return s
}

func (s *Service) loadAliases() {
	if s.aliasesFile == "" {
		return
	}

	data, err := os.ReadFile(s.aliasesFile)
	if err != nil {
		slog.Warn("failed to load model aliases", "error", err, "file", s.aliasesFile)
		return
	}

	var aliases map[string]string
	if err := json.Unmarshal(data, &aliases); err != nil {
		slog.Error("failed to parse model aliases", "error", err)
		return
	}

	s.mu.Lock()
	s.aliases = aliases
	s.mu.Unlock()

	slog.Info("loaded model aliases", "count", len(aliases))
}

func (s *Service) loadPricing() {
	if s.remoteURL != "" {
		if err := s.fetchRemote(); err != nil {
			slog.Warn("failed to fetch remote pricing, using fallback", "error", err)
			s.loadFallback()
		}
	} else {
		s.loadFallback()
	}
}

func (s *Service) fetchRemote() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.remoteURL, nil)
	if err != nil {
		return err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var remoteResp remotePricingResponse
	if err := json.NewDecoder(resp.Body).Decode(&remoteResp); err != nil {
		return err
	}

	pricing := make(map[string]ModelPricing)
	for _, entry := range remoteResp.Prices {
		cachedPrice := 0.0
		if entry.InputCached != nil {
			cachedPrice = *entry.InputCached
		}
		pricing[entry.ID] = ModelPricing{
			InputPricePerMillion:  entry.Input,
			OutputPricePerMillion: entry.Output,
			CachedPricePerMillion: cachedPrice,
		}
	}

	s.mu.Lock()
	s.pricing = pricing
	s.mu.Unlock()

	slog.Info("loaded pricing data from remote", "models", len(pricing), "updated_at", remoteResp.UpdatedAt)
	return nil
}

func (s *Service) loadFallback() {
	data, err := os.ReadFile(s.fallbackFile)
	if err != nil {
		slog.Error("failed to load fallback pricing", "error", err)
		return
	}

	var fallbackData map[string]fallbackPriceEntry
	if err := json.Unmarshal(data, &fallbackData); err != nil {
		slog.Error("failed to parse fallback pricing", "error", err)
		return
	}

	pricing := make(map[string]ModelPricing)
	for model, entry := range fallbackData {
		pricing[model] = ModelPricing{
			InputPricePerMillion:  entry.InputPricePerMillion,
			OutputPricePerMillion: entry.OutputPricePerMillion,
			CachedPricePerMillion: entry.CachedPricePerMillion,
		}
	}

	s.mu.Lock()
	s.pricing = pricing
	s.mu.Unlock()

	slog.Info("loaded pricing data from fallback", "models", len(pricing))
}

func (s *Service) refreshLoop() {
	ticker := time.NewTicker(s.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := s.fetchRemote(); err != nil {
				slog.Warn("failed to refresh pricing", "error", err)
			}
		case <-s.done:
			return
		}
	}
}

func (s *Service) Calculate(metrics *models.UsageMetrics) models.Cost {
	s.mu.RLock()
	pricing, ok := s.pricing[metrics.Model]
	if !ok {
		// Try alias lookup
		if aliasedModel, hasAlias := s.aliases[metrics.Model]; hasAlias {
			pricing, ok = s.pricing[aliasedModel]
		}
	}
	s.mu.RUnlock()

	if !ok {
		slog.Warn("no pricing found for model", "model", metrics.Model)
		return models.Cost{ModelAliasFound: false}
	}

	inputCost := float64(metrics.InputTokens-metrics.CachedTokens) * pricing.InputPricePerMillion / 1_000_000
	cachedCost := float64(metrics.CachedTokens) * pricing.CachedPricePerMillion / 1_000_000
	outputCost := float64(metrics.OutputTokens) * pricing.OutputPricePerMillion / 1_000_000

	return models.Cost{
		InputCost:       inputCost + cachedCost,
		OutputCost:      outputCost,
		TotalCost:       inputCost + cachedCost + outputCost,
		ModelAliasFound: true,
	}
}

func (s *Service) Close() {
	close(s.done)
}
