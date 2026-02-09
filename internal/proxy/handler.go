package proxy

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/config"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/pricing"
	"github.com/superset-studio/majordomo-gateway/internal/provider"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

type Handler struct {
	upstream  *UpstreamClient
	storage   storage.Storage
	s3Storage *storage.S3BodyStorage
	pricing   *pricing.Service
	resolver  *auth.Resolver
	config    *config.Config
	providers map[provider.Provider]string
}

// ProviderKeyInfo contains hashed provider API key information
type ProviderKeyInfo struct {
	Hash  *string
	Alias *string
}

func NewHandler(
	storage storage.Storage,
	s3Storage *storage.S3BodyStorage,
	pricingSvc *pricing.Service,
	resolver *auth.Resolver,
	cfg *config.Config,
) *Handler {
	providers := map[provider.Provider]string{
		provider.ProviderOpenAI:    cfg.Providers.OpenAI.BaseURL,
		provider.ProviderAnthropic: cfg.Providers.Anthropic.BaseURL,
		provider.ProviderGemini:    cfg.Providers.Gemini.BaseURL,
	}

	return &Handler{
		upstream:  NewUpstreamClient(),
		storage:   storage,
		s3Storage: s3Storage,
		pricing:   pricingSvc,
		resolver:  resolver,
		config:    cfg,
		providers: providers,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestedAt := time.Now()
	requestID := uuid.New()

	// Validate Majordomo API key
	apiKey := r.Header.Get("X-Majordomo-Key")
	apiKeyInfo, err := h.resolver.ResolveAPIKey(ctx, apiKey)
	if err != nil {
		slog.Debug("API key validation failed", "error", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract provider API key info (for tracking, not validation)
	providerKeyInfo := extractProviderKeyInfo(r)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	headers := extractHeaders(r.Header)
	providerInfo := provider.Detect(r.URL.Path, headers)

	baseURL := h.providers[providerInfo.Provider]
	if baseURL == "" {
		baseURL = providerInfo.BaseURL
	}

	// Translate request if needed (e.g., OpenAI format → Anthropic format)
	upstreamBody := body
	if provider.IsTranslationRequired(providerInfo.Provider) {
		translated, newPath, err := provider.TranslateOpenAIToAnthropic(body)
		if err != nil {
			slog.Warn("request translation failed, forwarding as-is", "error", err, "request_id", requestID)
		} else {
			upstreamBody = translated
			r.URL.Path = newPath
		}

		// Convert Authorization: Bearer <key> → x-api-key: <key> for Anthropic
		if authHeader := r.Header.Get("Authorization"); authHeader != "" {
			apiKey := strings.TrimPrefix(authHeader, "Bearer ")
			r.Header.Set("X-Api-Key", apiKey)
			r.Header.Del("Authorization")
			r.Header.Set("Anthropic-Version", "2023-06-01")
		}
	}

	resp, err := h.upstream.Forward(ctx, baseURL, r, upstreamBody)
	if err != nil {
		slog.Error("upstream request failed", "error", err, "request_id", requestID)
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}

	// Translate response back if needed (e.g., Anthropic format → OpenAI format)
	if provider.IsTranslationRequired(providerInfo.Provider) && resp.StatusCode < 400 {
		translated, err := provider.TranslateAnthropicToOpenAI(resp.Body, "")
		if err != nil {
			slog.Warn("response translation failed, returning as-is", "error", err, "request_id", requestID)
		} else {
			resp.Body = translated
		}
	}

	respondedAt := time.Now()

	// Copy response headers, filtering out hop-by-hop and Content-Encoding
	copyResponseHeaders(resp.Headers, w.Header())

	// Check if we should compress the response for the client
	acceptEncoding := r.Header.Get("Accept-Encoding")
	contentType := resp.Headers.Get("Content-Type")
	responseBody := resp.Body

	if ShouldCompress(acceptEncoding, contentType, len(resp.Body)) {
		compressed, err := GzipCompress(resp.Body)
		if err != nil {
			slog.Warn("failed to compress response, sending uncompressed", "error", err, "request_id", requestID)
		} else {
			responseBody = compressed
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Vary", "Accept-Encoding")
		}
	}

	w.WriteHeader(resp.StatusCode)
	w.Write(responseBody)

	go h.logRequest(ctx, requestID, apiKeyInfo, providerKeyInfo, providerInfo, r, body, resp, requestedAt, respondedAt, headers)
}

func (h *Handler) logRequest(
	ctx context.Context,
	requestID uuid.UUID,
	apiKeyInfo *models.APIKeyInfo,
	providerKeyInfo *ProviderKeyInfo,
	providerInfo provider.ProviderInfo,
	req *http.Request,
	reqBody []byte,
	resp *UpstreamResponse,
	requestedAt, respondedAt time.Time,
	customHeaders map[string]string,
) {
	parser := provider.GetParser(providerInfo.Provider)
	metrics, err := parser.ParseResponse(resp.Body)
	if err != nil {
		slog.Warn("failed to parse response", "error", err, "request_id", requestID)
		metrics = &models.UsageMetrics{
			Provider: string(providerInfo.Provider),
			Model:    parser.ExtractModel(reqBody),
		}
	}

	// Fall back to request model if response doesn't include it
	if metrics.Model == "" {
		metrics.Model = parser.ExtractModel(reqBody)
	}

	metrics.ResponseTime = resp.ResponseTime

	cost := h.pricing.Calculate(metrics)

	var errMsg *string
	if resp.StatusCode >= 400 {
		msg := string(resp.Body)
		if len(msg) > 500 {
			msg = msg[:500]
		}
		errMsg = &msg
	}

	log := &models.RequestLog{
		ID: requestID,

		// Majordomo API key (validated)
		MajordomoAPIKeyID: &apiKeyInfo.ID,

		// Provider API key (for usage tracking)
		ProviderAPIKeyHash:  providerKeyInfo.Hash,
		ProviderAPIKeyAlias: providerKeyInfo.Alias,

		Provider:      metrics.Provider,
		Model:         metrics.Model,
		RequestPath:   req.URL.Path,
		RequestMethod: req.Method,

		RequestedAt:    requestedAt,
		RespondedAt:    respondedAt,
		ResponseTimeMs: resp.ResponseTime.Milliseconds(),

		InputTokens:         metrics.InputTokens,
		OutputTokens:        metrics.OutputTokens,
		CachedTokens:        metrics.CachedTokens,
		CacheCreationTokens: metrics.CacheCreationTokens,

		InputCost:  cost.InputCost,
		OutputCost: cost.OutputCost,
		TotalCost:  cost.TotalCost,

		StatusCode:   resp.StatusCode,
		ErrorMessage: errMsg,

		RawMetadata:     extractCustomMetadata(customHeaders),
		ModelAliasFound: cost.ModelAliasFound,
	}

	switch h.config.Logging.BodyStorage {
	case "s3":
		if h.s3Storage != nil {
			s3Key := h.s3Storage.GenerateKey(apiKeyInfo.ID.String(), requestID, requestedAt)
			log.BodyS3Key = &s3Key

			h.s3Storage.Upload(&storage.BodyUpload{
				Key:             s3Key,
				RequestID:       requestID,
				Timestamp:       requestedAt,
				RequestMethod:   req.Method,
				RequestPath:     req.URL.Path,
				RequestHeaders:  customHeaders,
				RequestBody:     reqBody,
				ResponseStatus:  resp.StatusCode,
				ResponseHeaders: storage.ExtractResponseHeaders(resp.Headers),
				ResponseBody:    resp.Body,
			})
		}
	case "postgres":
		if h.config.Logging.StoreRequestBody {
			body := truncateBody(string(reqBody), h.config.Logging.MaxBodySize)
			log.RequestBody = &body
		}
		if h.config.Logging.StoreResponseBody {
			body := truncateBody(string(resp.Body), h.config.Logging.MaxBodySize)
			log.ResponseBody = &body
		}
	}

	h.storage.WriteRequestLog(ctx, log)
}

// extractProviderKeyInfo extracts and hashes the provider API key from the Authorization header
func extractProviderKeyInfo(r *http.Request) *ProviderKeyInfo {
	info := &ProviderKeyInfo{}

	// Hash the Authorization header if present
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		hash := auth.HashAPIKey(authHeader)
		info.Hash = &hash
	}

	// Get optional provider alias header
	if alias := r.Header.Get("X-Majordomo-Provider-Alias"); alias != "" {
		info.Alias = &alias
	}

	return info
}

func extractHeaders(h http.Header) map[string]string {
	result := make(map[string]string)
	for key, values := range h {
		lowerKey := strings.ToLower(key)
		if strings.HasPrefix(lowerKey, "x-majordomo") {
			result[lowerKey] = values[0]
		}
	}
	return result
}

func extractCustomMetadata(headers map[string]string) map[string]string {
	metadata := make(map[string]string)
	for key, value := range headers {
		// Exclude reserved headers
		if key != "x-majordomo-key" && key != "x-majordomo-provider" && key != "x-majordomo-provider-alias" {
			cleanKey := strings.TrimPrefix(key, "x-majordomo-")
			metadata[cleanKey] = value
		}
	}
	return metadata
}

func truncateBody(body string, maxSize int) string {
	if len(body) <= maxSize {
		return body
	}
	return body[:maxSize]
}
