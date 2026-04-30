package proxy

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/claudecode"
	"github.com/superset-studio/majordomo-gateway/internal/config"
	"github.com/superset-studio/majordomo-gateway/internal/experiment"
	"github.com/superset-studio/majordomo-gateway/internal/httputil"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/pricing"
	"github.com/superset-studio/majordomo-gateway/internal/provider"
	"github.com/superset-studio/majordomo-gateway/internal/secrets"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

type Handler struct {
	upstream         *UpstreamClient
	storage          storage.Storage
	s3Storage        *storage.S3BodyStorage
	userBodyStorage  *storage.UserBodyStorage
	userStore        storage.UserStorage
	orgStore         storage.OrganizationStorage
	secretStore      secrets.SecretStore
	pricing          *pricing.Service
	resolver         *auth.Resolver
	proxyResolver    *auth.ProxyResolver
	sessionMgr       *claudecode.SessionManager
	config           *config.Config
	providers        map[provider.Provider]string
	userCloudCache   sync.Map // userID (string) → *cachedCloudStorageConfig
	orgCloudCache    sync.Map // orgID (string) → *cachedCloudStorageConfig
	cloudCacheTTL    time.Duration

	// Optional extension points — nil in the OSS binary.
	policyEnforcer   PolicyEnforcer
	requestEnricher  RequestEnricher
	experimentRouter *experiment.Router
}

type cachedCloudStorageConfig struct {
	config    *models.UserCloudStorageConfig
	fetchedAt time.Time
}

// ProviderKeyInfo contains hashed provider API key information
type ProviderKeyInfo struct {
	Hash  *string
	Alias *string
}

func NewHandler(
	store storage.Storage,
	s3Storage *storage.S3BodyStorage,
	userBodyStorage *storage.UserBodyStorage,
	userStore storage.UserStorage,
	orgStore storage.OrganizationStorage,
	secretStore secrets.SecretStore,
	pricingSvc *pricing.Service,
	resolver *auth.Resolver,
	proxyResolver *auth.ProxyResolver,
	sessionMgr *claudecode.SessionManager,
	cfg *config.Config,
	opts ...HandlerOption,
) *Handler {
	providers := map[provider.Provider]string{
		provider.ProviderOpenAI:    cfg.Providers.OpenAI.BaseURL,
		provider.ProviderAnthropic: cfg.Providers.Anthropic.BaseURL,
		provider.ProviderGemini:    cfg.Providers.Gemini.BaseURL,
	}

	h := &Handler{
		upstream:        NewUpstreamClient(cfg.Server.UpstreamTimeout, cfg.Server.StreamHeaderTimeout),
		storage:         store,
		s3Storage:       s3Storage,
		userBodyStorage: userBodyStorage,
		userStore:       userStore,
		orgStore:        orgStore,
		secretStore:     secretStore,
		pricing:         pricingSvc,
		resolver:        resolver,
		proxyResolver:   proxyResolver,
		sessionMgr:      sessionMgr,
		config:          cfg,
		providers:       providers,
		cloudCacheTTL:   5 * time.Minute,
	}

	for _, opt := range opts {
		opt(h)
	}

	return h
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
		httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Extract provider API key info (for tracking, not validation)
	providerKeyInfo := extractProviderKeyInfo(r)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	headers := extractHeaders(r.Header)
	providerInfo := provider.Detect(r.URL.Path, headers)

	if providerInfo.Provider == provider.ProviderUnknown {
		httputil.WriteJSONError(w, http.StatusBadRequest, "unrecognized request path; supported paths: /v1/chat/completions, /v1/completions, /v1/embeddings, /v1/responses (OpenAI), /v1/messages (Anthropic), /<model>:generateContent (Gemini). Alternatively, set X-Majordomo-Provider header.")
		return
	}

	// Experiment routing (A/B testing): select a variant and rewrite the model.
	var experimentResult *experiment.RoutingResult
	if h.experimentRouter != nil {
		experimentResult = h.experimentRouter.Route(ctx, experiment.RoutingContext{
			APIKeyID: apiKeyInfo.ID,
			UserID:   apiKeyInfo.UserID,
			OrgID:    apiKeyInfo.OrgID,
			Provider: providerInfo.Provider,
			Body:     body,
			Headers:  headers,
		})
		if experimentResult != nil {
			body = experimentResult.RewrittenBody
			if experimentResult.ProviderChanged {
				providerInfo = experimentResult.NewProviderInfo
			}
		}
	}

	// Policy enforcement (synchronous, pre-proxy).
	if h.policyEnforcer != nil {
		if violation := h.policyEnforcer.Enforce(ctx, PolicyContext{
			RequestID:     requestID,
			APIKeyID:      apiKeyInfo.ID,
			UserID:        apiKeyInfo.UserID,
			OrgID:         apiKeyInfo.OrgID,
			Provider:      string(providerInfo.Provider),
			RequestBody:   body,
			CustomHeaders: headers,
		}); violation != nil {
			slog.Debug("request blocked by policy", "request_id", requestID, "status", violation.HTTPStatus)
			httputil.WriteJSONError(w, violation.HTTPStatus, violation.Message)
			return
		}
	}

	// Check if Authorization header contains a proxy key
	var proxyKeyID *uuid.UUID
	if h.proxyResolver != nil {
		authHeader := r.Header.Get("Authorization")
		authKey := strings.TrimPrefix(authHeader, "Bearer ")
		providerKey, pkID, proxyErr := h.proxyResolver.ResolveProxyKey(ctx, authKey, string(providerInfo.Provider), apiKeyInfo.ID)
		if proxyErr != nil {
			slog.Debug("proxy key validation failed", "error", proxyErr)
			httputil.WriteJSONError(w, http.StatusUnauthorized, proxyErr.Error())
			return
		}
		if providerKey != "" {
			r.Header.Set("Authorization", "Bearer "+providerKey)
			proxyKeyID = pkID
		}
	}

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

	// Decide whether to use the streaming path.
	// Translation requires the full body up-front, so always buffer those.
	useStreaming := !provider.IsTranslationRequired(providerInfo.Provider)

	var resp *UpstreamResponse
	if useStreaming {
		streamResp, err := h.upstream.ForwardStream(ctx, baseURL, r, upstreamBody)
		if err != nil {
			slog.Error("upstream request failed", "error", err, "request_id", requestID)
			httputil.WriteJSONError(w, http.StatusBadGateway, "upstream request failed")
			return
		}

		isSSE := strings.Contains(streamResp.Headers.Get("Content-Type"), "text/event-stream")

		if isSSE {
			// --- Streaming SSE path ---

			// Disable the server's write deadline for this connection so
			// long-running streams are not killed.
			rc := http.NewResponseController(w)
			if err := rc.SetWriteDeadline(time.Time{}); err != nil {
				slog.Debug("failed to clear write deadline", "error", err)
			}

			// Copy response headers (skip hop-by-hop / Content-Encoding).
			copyResponseHeaders(streamResp.Headers, w.Header())
			w.WriteHeader(streamResp.StatusCode)

			// Flush headers immediately so the client sees them.
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			// Tee the stream: relay to client while capturing for logging.
			var buf bytes.Buffer
			tee := io.TeeReader(streamResp.Body, &buf)

			// Stream chunks to client, flushing after each io.Copy chunk.
			flushWriter := newFlushWriter(w)
			_, copyErr := io.Copy(flushWriter, tee)
			streamResp.Body.Close()

			if copyErr != nil {
				slog.Warn("error streaming response to client", "error", copyErr, "request_id", requestID)
			}

			respondedAt := time.Now()

			// Build an UpstreamResponse from the buffered data for logging.
			resp = &UpstreamResponse{
				StatusCode:   streamResp.StatusCode,
				Headers:      streamResp.Headers,
				Body:         buf.Bytes(),
				ResponseTime: streamResp.ResponseTime,
			}

			h.logAndFinish(r, requestID, apiKeyInfo, providerKeyInfo, proxyKeyID, providerInfo, body, resp, requestedAt, respondedAt, headers, experimentResult)
			return
		}

		// Non-SSE response received via streaming client — buffer the rest.
		respBody, err := io.ReadAll(streamResp.Body)
		streamResp.Body.Close()
		if err != nil {
			slog.Error("failed to read upstream response", "error", err, "request_id", requestID)
			httputil.WriteJSONError(w, http.StatusBadGateway, "upstream request failed")
			return
		}

		resp = &UpstreamResponse{
			StatusCode:   streamResp.StatusCode,
			Headers:      streamResp.Headers,
			Body:         respBody,
			ResponseTime: streamResp.ResponseTime,
		}
	} else {
		// Buffered path (translation required).
		var err error
		resp, err = h.upstream.Forward(ctx, baseURL, r, upstreamBody)
		if err != nil {
			slog.Error("upstream request failed", "error", err, "request_id", requestID)
			httputil.WriteJSONError(w, http.StatusBadGateway, "upstream request failed")
			return
		}

		// Translate response back (e.g., Anthropic format → OpenAI format)
		if resp.StatusCode < 400 {
			translated, err := provider.TranslateAnthropicToOpenAI(resp.Body, "")
			if err != nil {
				slog.Warn("response translation failed, returning as-is", "error", err, "request_id", requestID)
			} else {
				resp.Body = translated
			}
		}
	}

	respondedAt := time.Now()

	// Copy response headers, filtering out hop-by-hop and Content-Encoding
	copyResponseHeaders(resp.Headers, w.Header())

	// Check if we should compress the response for the client.
	// Skip compression for SSE — it defeats streaming (already handled above).
	acceptEncoding := r.Header.Get("Accept-Encoding")
	contentType := resp.Headers.Get("Content-Type")
	responseBody := resp.Body

	if !strings.Contains(contentType, "text/event-stream") && ShouldCompress(acceptEncoding, contentType, len(resp.Body)) {
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

	h.logAndFinish(r, requestID, apiKeyInfo, providerKeyInfo, proxyKeyID, providerInfo, body, resp, requestedAt, respondedAt, headers, experimentResult)
}

// logAndFinish extracts session metadata from request headers and dispatches
// the async log. Shared by both the buffered and streaming paths.
func (h *Handler) logAndFinish(
	r *http.Request,
	requestID uuid.UUID,
	apiKeyInfo *models.APIKeyInfo,
	providerKeyInfo *ProviderKeyInfo,
	proxyKeyID *uuid.UUID,
	providerInfo provider.ProviderInfo,
	reqBody []byte,
	resp *UpstreamResponse,
	requestedAt, respondedAt time.Time,
	headers map[string]string,
	experimentResult *experiment.RoutingResult,
) {
	// Inject experiment metadata into headers so it flows into raw_metadata / indexed_metadata.
	if experimentResult != nil {
		headers["x-majordomo-_experiment-id"] = experimentResult.ExperimentID.String()
		headers["x-majordomo-_experiment-variant"] = experimentResult.VariantName
		headers["x-majordomo-_experiment-original-model"] = experimentResult.OriginalModel
	}
	// Extract Claude Code session ID if present
	var sessionID *uuid.UUID
	if sid := r.Header.Get("X-Majordomo-ClaudeCode-Session-Id"); sid != "" {
		if parsed, parseErr := uuid.Parse(sid); parseErr == nil {
			sessionID = &parsed
		}
	}

	// Extract Claude Code session name if present
	var sessionName *string
	if sn := r.Header.Get("X-Majordomo-ClaudeCode-Session-Name"); sn != "" {
		sessionName = &sn
	}

	// Determine if this is a Claude Code request
	isClaudeCode := r.Header.Get("X-Majordomo-Client") == "claude-code" || sessionID != nil

	go h.logRequest(context.Background(), requestID, apiKeyInfo, providerKeyInfo, proxyKeyID, sessionID, sessionName, isClaudeCode, providerInfo, r, reqBody, resp, requestedAt, respondedAt, headers)
}

func (h *Handler) logRequest(
	ctx context.Context,
	requestID uuid.UUID,
	apiKeyInfo *models.APIKeyInfo,
	providerKeyInfo *ProviderKeyInfo,
	proxyKeyID *uuid.UUID,
	sessionID *uuid.UUID,
	sessionName *string,
	isClaudeCode bool,
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

		// User who owns the API key
		UserID: apiKeyInfo.UserID,

		// Organization that owns the API key
		OrgID: apiKeyInfo.OrgID,

		// Proxy key (if request used one)
		ProxyKeyID: proxyKeyID,

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

	// Body storage: try user/org S3 first, fall back to global storage.
	// Claude Code request bodies are never stored in global storage for privacy;
	// they are only stored when the user or org has configured their own S3.
	uploaded := h.storeBodyToUserOrOrgCloud(ctx, log, apiKeyInfo, requestID, requestedAt, req, customHeaders, reqBody, resp)
	if !uploaded && !isClaudeCode {
		h.storeBodyToGlobalStorage(log, apiKeyInfo, requestID, requestedAt, req, customHeaders, reqBody, resp)
	}

	// Attach Claude Code metadata so it's written after the llm_requests INSERT.
	// Only parse when the request is identified as Claude Code (via X-Majordomo-Client
	// header or X-Majordomo-ClaudeCode-Session-Id presence).
	if isClaudeCode &&
		providerInfo.Provider == provider.ProviderAnthropic &&
		req.URL.Path == "/v1/messages" &&
		resp.StatusCode < 400 &&
		h.sessionMgr != nil {
		meta, parseErr := claudecode.ParseRequestResponse(reqBody, resp.Body)
		if parseErr != nil {
			slog.Debug("failed to parse claude code metadata", "error", parseErr)
		} else {
			log.ClaudeMetadata = &models.ClaudeRequestMetadata{
				SessionID:             sessionID,
				MessageCount:          meta.MessageCount,
				UserMessageCount:      meta.UserMessageCount,
				AssistantMessageCount: meta.AssistantMessageCount,
				ToolNames:             meta.ToolNames,
				ToolUseCount:          meta.ToolUseCount,
				HasThinking:           meta.HasThinking,
				IsPlanMode:            meta.IsPlanMode,
				StopReason:            meta.StopReason,
				SystemPromptHash:      meta.SystemPromptHash,
			}
		}
	}

	h.storage.WriteRequestLog(ctx, log)

	// Post-response enrichment (async extension point).
	// logRequest already runs in a background goroutine — no additional go needed.
	if h.requestEnricher != nil {
		h.requestEnricher.Enrich(ctx, EnrichmentEvent{
			RequestID:     log.ID,
			APIKeyID:      apiKeyInfo.ID,
			UserID:        apiKeyInfo.UserID,
			OrgID:         apiKeyInfo.OrgID,
			Provider:      string(providerInfo.Provider),
			RequestBody:   reqBody,
			ResponseBody:  resp.Body,
			CustomHeaders: customHeaders,
			StatusCode:    resp.StatusCode,
		})
	}
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
		if key != "x-majordomo-key" && key != "x-majordomo-provider" && key != "x-majordomo-provider-alias" && key != "x-majordomo-client" && key != "x-majordomo-claudecode-session-id" && key != "x-majordomo-claudecode-session-name" {
			cleanKey := strings.TrimPrefix(key, "x-majordomo-")
			metadata[cleanKey] = value
		}
	}
	return metadata
}

// getUserCloudStorageConfig retrieves and caches the decrypted cloud storage config for a user.
// Returns nil if the user has no cloud storage config or if decryption fails.
func (h *Handler) getUserCloudStorageConfig(ctx context.Context, userID uuid.UUID) *models.UserCloudStorageConfig {
	key := userID.String()

	if cached, ok := h.userCloudCache.Load(key); ok {
		entry := cached.(*cachedCloudStorageConfig)
		if time.Since(entry.fetchedAt) < h.cloudCacheTTL {
			return entry.config
		}
	}

	if h.userStore == nil || h.secretStore == nil {
		slog.Debug("skipping user cloud storage config: missing dependency", "user_id", userID)
		return nil
	}

	user, err := h.userStore.GetUserCloudStorageConfig(ctx, userID)
	if err != nil {
		slog.Debug("failed to get user cloud storage config", "error", err, "user_id", userID)
		h.userCloudCache.Store(key, &cachedCloudStorageConfig{config: nil, fetchedAt: time.Now()})
		return nil
	}

	cfg := h.resolveUserCloudConfig(user, userID)
	h.userCloudCache.Store(key, &cachedCloudStorageConfig{config: cfg, fetchedAt: time.Now()})
	return cfg
}

// resolveUserCloudConfig decrypts and builds a UserCloudStorageConfig from a User model.
func (h *Handler) resolveUserCloudConfig(user *models.User, ownerID uuid.UUID) *models.UserCloudStorageConfig {
	provider := ""
	if user.CloudStorageProvider != nil {
		provider = *user.CloudStorageProvider
	}

	switch models.CloudStorageProviderType(provider) {
	case models.CloudStorageProviderGCS:
		if user.GCSBucket == nil || *user.GCSBucket == "" || user.GCSCredentialsJSONEncrypted == nil {
			return nil
		}
		credJSON, err := h.secretStore.Decrypt(*user.GCSCredentialsJSONEncrypted)
		if err != nil {
			slog.Error("failed to decrypt GCS credentials JSON", "error", err, "owner_id", ownerID)
			return nil
		}
		projectID := ""
		if user.GCSProjectID != nil {
			projectID = *user.GCSProjectID
		}
		return &models.UserCloudStorageConfig{
			Provider:           models.CloudStorageProviderGCS,
			GCSBucket:          *user.GCSBucket,
			GCSProjectID:       projectID,
			GCSCredentialsJSON: credJSON,
		}
	default:
		// S3 or legacy (no provider set but S3 columns populated)
		if user.S3Bucket == nil || *user.S3Bucket == "" || user.S3AccessKeyIDEncrypted == nil || user.S3SecretAccessKeyEncrypted == nil {
			return nil
		}
		accessKeyID, err := h.secretStore.Decrypt(*user.S3AccessKeyIDEncrypted)
		if err != nil {
			slog.Error("failed to decrypt S3 access key ID", "error", err, "owner_id", ownerID)
			return nil
		}
		secretAccessKey, err := h.secretStore.Decrypt(*user.S3SecretAccessKeyEncrypted)
		if err != nil {
			slog.Error("failed to decrypt S3 secret access key", "error", err, "owner_id", ownerID)
			return nil
		}
		region := "us-east-1"
		if user.S3Region != nil {
			region = *user.S3Region
		}
		endpoint := ""
		if user.S3Endpoint != nil {
			endpoint = *user.S3Endpoint
		}
		return &models.UserCloudStorageConfig{
			Provider:       models.CloudStorageProviderS3,
			Bucket:         *user.S3Bucket,
			Region:         region,
			Endpoint:       endpoint,
			AccessKeyID:    accessKeyID,
			SecretAccessKey: secretAccessKey,
		}
	}
}

// getOrgCloudStorageConfig retrieves and caches the decrypted cloud storage config for an organization.
// Returns nil if the org has no cloud storage config or if decryption fails.
func (h *Handler) getOrgCloudStorageConfig(ctx context.Context, orgID uuid.UUID) *models.UserCloudStorageConfig {
	key := orgID.String()

	if cached, ok := h.orgCloudCache.Load(key); ok {
		entry := cached.(*cachedCloudStorageConfig)
		if time.Since(entry.fetchedAt) < h.cloudCacheTTL {
			return entry.config
		}
	}

	if h.orgStore == nil || h.secretStore == nil {
		return nil
	}

	org, err := h.orgStore.GetOrgCloudStorageConfig(ctx, orgID)
	if err != nil {
		slog.Debug("failed to get org cloud storage config", "error", err, "org_id", orgID)
		h.orgCloudCache.Store(key, &cachedCloudStorageConfig{config: nil, fetchedAt: time.Now()})
		return nil
	}

	cfg := h.resolveOrgCloudConfig(org, orgID)
	h.orgCloudCache.Store(key, &cachedCloudStorageConfig{config: cfg, fetchedAt: time.Now()})
	return cfg
}

// resolveOrgCloudConfig decrypts and builds a UserCloudStorageConfig from an Organization model.
func (h *Handler) resolveOrgCloudConfig(org *models.Organization, orgID uuid.UUID) *models.UserCloudStorageConfig {
	provider := ""
	if org.CloudStorageProvider != nil {
		provider = *org.CloudStorageProvider
	}

	switch models.CloudStorageProviderType(provider) {
	case models.CloudStorageProviderGCS:
		if org.GCSBucket == nil || *org.GCSBucket == "" || org.GCSCredentialsJSONEncrypted == nil {
			return nil
		}
		credJSON, err := h.secretStore.Decrypt(*org.GCSCredentialsJSONEncrypted)
		if err != nil {
			slog.Error("failed to decrypt org GCS credentials JSON", "error", err, "org_id", orgID)
			return nil
		}
		projectID := ""
		if org.GCSProjectID != nil {
			projectID = *org.GCSProjectID
		}
		return &models.UserCloudStorageConfig{
			Provider:           models.CloudStorageProviderGCS,
			GCSBucket:          *org.GCSBucket,
			GCSProjectID:       projectID,
			GCSCredentialsJSON: credJSON,
		}
	default:
		if org.S3Bucket == nil || *org.S3Bucket == "" || org.S3AccessKeyIDEncrypted == nil || org.S3SecretAccessKeyEncrypted == nil {
			return nil
		}
		accessKeyID, err := h.secretStore.Decrypt(*org.S3AccessKeyIDEncrypted)
		if err != nil {
			slog.Error("failed to decrypt org S3 access key ID", "error", err, "org_id", orgID)
			return nil
		}
		secretAccessKey, err := h.secretStore.Decrypt(*org.S3SecretAccessKeyEncrypted)
		if err != nil {
			slog.Error("failed to decrypt org S3 secret access key", "error", err, "org_id", orgID)
			return nil
		}
		region := "us-east-1"
		if org.S3Region != nil {
			region = *org.S3Region
		}
		endpoint := ""
		if org.S3Endpoint != nil {
			endpoint = *org.S3Endpoint
		}
		return &models.UserCloudStorageConfig{
			Provider:       models.CloudStorageProviderS3,
			Bucket:         *org.S3Bucket,
			Region:         region,
			Endpoint:       endpoint,
			AccessKeyID:    accessKeyID,
			SecretAccessKey: secretAccessKey,
		}
	}
}

// buildBodyUpload creates a BodyUpload from the common request/response parameters.
func buildBodyUpload(key string, requestID uuid.UUID, requestedAt time.Time, req *http.Request, customHeaders map[string]string, reqBody []byte, resp *UpstreamResponse) *storage.BodyUpload {
	return &storage.BodyUpload{
		Key:             key,
		RequestID:       requestID,
		Timestamp:       requestedAt,
		RequestMethod:   req.Method,
		RequestPath:     req.URL.Path,
		RequestHeaders:  customHeaders,
		RequestBody:     reqBody,
		ResponseStatus:  resp.StatusCode,
		ResponseHeaders: storage.ExtractResponseHeaders(resp.Headers),
		ResponseBody:    resp.Body,
	}
}

// storeBodyToUserOrOrgCloud attempts to upload the request/response body to a
// user-specific or org-specific cloud storage bucket (S3 or GCS). Returns true if an upload was fired.
func (h *Handler) storeBodyToUserOrOrgCloud(
	ctx context.Context,
	log *models.RequestLog,
	apiKeyInfo *models.APIKeyInfo,
	requestID uuid.UUID,
	requestedAt time.Time,
	req *http.Request,
	customHeaders map[string]string,
	reqBody []byte,
	resp *UpstreamResponse,
) bool {
	if h.userBodyStorage == nil {
		return false
	}

	// Try user cloud storage first, then fall back to org cloud storage.
	type cloudTarget struct {
		ownerID uuid.UUID
		cfg     *models.UserCloudStorageConfig
	}
	var target *cloudTarget

	if apiKeyInfo.UserID != nil {
		if cfg := h.getUserCloudStorageConfig(ctx, *apiKeyInfo.UserID); cfg != nil {
			target = &cloudTarget{ownerID: *apiKeyInfo.UserID, cfg: cfg}
		}
	}
	if target == nil && apiKeyInfo.OrgID != nil {
		if cfg := h.getOrgCloudStorageConfig(ctx, *apiKeyInfo.OrgID); cfg != nil {
			target = &cloudTarget{ownerID: *apiKeyInfo.OrgID, cfg: cfg}
		}
	}

	if target == nil {
		return false
	}

	storageKey := storage.GenerateS3Key(apiKeyInfo.ID, requestID, requestedAt)
	log.BodyS3Key = &storageKey
	upload := buildBodyUpload(storageKey, requestID, requestedAt, req, customHeaders, reqBody, resp)
	h.userBodyStorage.Upload(ctx, target.ownerID, target.cfg, upload)
	return true
}

// storeBodyToGlobalStorage stores the request/response body using the globally
// configured storage backend (S3 or Postgres).
func (h *Handler) storeBodyToGlobalStorage(
	log *models.RequestLog,
	apiKeyInfo *models.APIKeyInfo,
	requestID uuid.UUID,
	requestedAt time.Time,
	req *http.Request,
	customHeaders map[string]string,
	reqBody []byte,
	resp *UpstreamResponse,
) {
	switch h.config.Logging.BodyStorage {
	case "s3":
		if h.s3Storage != nil {
			s3Key := storage.GenerateS3Key(apiKeyInfo.ID, requestID, requestedAt)
			log.BodyS3Key = &s3Key
			upload := buildBodyUpload(s3Key, requestID, requestedAt, req, customHeaders, reqBody, resp)
			h.s3Storage.Upload(upload)
		}
	case "postgres":
		if h.config.Logging.StoreRequestBody {
			body := truncateBody(string(reqBody), h.config.Logging.EffectiveMaxRequestBodySize())
			log.RequestBody = &body
		}
		if h.config.Logging.StoreResponseBody {
			body := truncateBody(string(resp.Body), h.config.Logging.EffectiveMaxResponseBodySize())
			log.ResponseBody = &body
		}
	}
}

func truncateBody(body string, maxSize int) string {
	if len(body) <= maxSize {
		return body
	}
	return body[:maxSize]
}

// flushWriter wraps an http.ResponseWriter and flushes after every Write
// if the underlying writer supports http.Flusher. This ensures SSE chunks
// are delivered to the client immediately.
type flushWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newFlushWriter(w http.ResponseWriter) *flushWriter {
	f, _ := w.(http.Flusher)
	return &flushWriter{w: w, flusher: f}
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if fw.flusher != nil {
		fw.flusher.Flush()
	}
	return n, err
}
