// Package gateway provides a public API for constructing and running a majordomo
// gateway server. It wraps all internal package wiring so that external modules
// (e.g. the enterprise binary) can build a fully configured server without
// importing internal packages directly.
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/superset-studio/majordomo-gateway/extension"
	"github.com/superset-studio/majordomo-gateway/internal/api"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/claudecode"
	"github.com/superset-studio/majordomo-gateway/internal/config"
	apiemail "github.com/superset-studio/majordomo-gateway/internal/email"
	"github.com/superset-studio/majordomo-gateway/internal/experiment"
	"github.com/superset-studio/majordomo-gateway/internal/pricing"
	"github.com/superset-studio/majordomo-gateway/internal/proxy"
	"github.com/superset-studio/majordomo-gateway/internal/secrets"
	"github.com/superset-studio/majordomo-gateway/internal/server"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// Server is a fully initialized gateway server ready to start.
type Server struct {
	inner    *server.Server
	store    *storage.PostgresStorage
	pricing  *pricing.Service
	s3       *storage.S3BodyStorage
	enricher extension.RequestEnricher
}

// Start begins listening for HTTP requests. It blocks until the server stops.
func (s *Server) Start() error {
	return s.inner.Start()
}

// ShutdownWithTimeout gracefully stops the server, drains the enricher's work
// queue (if it implements extension.Closer), then closes the store and pricing
// service. The provided timeout bounds the entire shutdown sequence.
func (s *Server) ShutdownWithTimeout(timeout time.Duration) error {
	err := s.inner.ShutdownWithTimeout(timeout)

	if s.enricher != nil {
		if c, ok := s.enricher.(extension.Closer); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if shutdownErr := c.Shutdown(ctx); shutdownErr != nil {
				slog.Warn("enricher shutdown error", "error", shutdownErr)
			}
		}
	}

	s.pricing.Close()
	s.store.Close()
	if s.s3 != nil {
		s.s3.Close()
	}

	return err
}

// buildOptions holds the options passed to Build.
type buildOptions struct {
	configPath      string
	policyEnforcer  extension.PolicyEnforcer
	requestEnricher extension.RequestEnricher
}

// Option configures a gateway Server at build time.
type Option func(*buildOptions)

// WithConfigPath sets the path to the majordomo YAML config file.
// If not set, Build looks for majordomo.yaml in the working directory and
// /etc/majordomo, matching the OSS binary behaviour.
func WithConfigPath(path string) Option {
	return func(o *buildOptions) { o.configPath = path }
}

// WithPolicyEnforcer attaches a synchronous pre-proxy policy enforcer.
func WithPolicyEnforcer(e extension.PolicyEnforcer) Option {
	return func(o *buildOptions) { o.policyEnforcer = e }
}

// WithRequestEnricher attaches an async post-response enricher.
func WithRequestEnricher(e extension.RequestEnricher) Option {
	return func(o *buildOptions) { o.requestEnricher = e }
}

// Build initializes all gateway dependencies and returns a Server ready to
// call Start() on. It mirrors the setup performed by the OSS `serve` command.
func Build(ctx context.Context, opts ...Option) (*Server, error) {
	o := &buildOptions{}
	for _, opt := range opts {
		opt(o)
	}

	cfg, err := config.Load(o.configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Logging.Level),
	})))

	store, err := storage.NewPostgresStorage(ctx, cfg.Storage.Postgres.DSN(), cfg.Storage.Postgres.MaxConns, &storage.PostgresStorageConfig{
		HLLFlushInterval:   cfg.Metadata.HLLFlushInterval,
		ActiveKeysCacheTTL: cfg.Metadata.ActiveKeysCacheTTL,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	pricingSvc := pricing.NewService(
		cfg.Pricing.RemoteURL,
		cfg.Pricing.FallbackFile,
		cfg.Pricing.AliasesFile,
		cfg.Pricing.RefreshInterval,
	)

	var s3Storage *storage.S3BodyStorage
	if cfg.S3.Enabled {
		s3Storage, err = storage.NewS3BodyStorage(ctx, storage.S3Config{
			Bucket:          cfg.S3.Bucket,
			Region:          cfg.S3.Region,
			Endpoint:        cfg.S3.Endpoint,
			AccessKeyID:     cfg.S3.AccessKeyID,
			SecretAccessKey: cfg.S3.SecretAccessKey,
		})
		if err != nil {
			store.Close()
			pricingSvc.Close()
			return nil, fmt.Errorf("initialize S3 storage: %w", err)
		}
		slog.Info("S3 body storage enabled", "bucket", cfg.S3.Bucket, "region", cfg.S3.Region)
	}

	resolver := auth.NewResolver(store)

	var proxyResolver *auth.ProxyResolver
	var apiHandler *api.Handler
	if cfg.Secrets.EncryptionKey != "" {
		secretStore, err := secrets.NewAESStore(cfg.Secrets.EncryptionKey)
		if err != nil {
			store.Close()
			pricingSvc.Close()
			return nil, fmt.Errorf("initialize secret store: %w", err)
		}
		proxyResolver = auth.NewProxyResolver(store, secretStore)
		apiHandler = api.NewHandler(store, secretStore)
		slog.Info("proxy key support enabled")
	}

	sessionMgr := claudecode.NewSessionManager(store)
	claudeHandler := api.NewClaudeSessionHandler(sessionMgr, store)

	userBodyStorage := storage.NewUserBodyStorage()

	var proxySecretStore secrets.SecretStore
	if cfg.Secrets.EncryptionKey != "" {
		proxySecretStore, _ = secrets.NewAESStore(cfg.Secrets.EncryptionKey)
	}

	handlerOpts := []proxy.HandlerOption{}
	if o.policyEnforcer != nil {
		handlerOpts = append(handlerOpts, proxy.WithPolicyEnforcer(o.policyEnforcer))
	}
	if o.requestEnricher != nil {
		handlerOpts = append(handlerOpts, proxy.WithRequestEnricher(o.requestEnricher))
	}

	var experimentRouter *experiment.Router
	if cfg.Experiments.Enabled {
		experimentRouter = experiment.NewRouter(store, cfg.Experiments.CacheTTL)
		handlerOpts = append(handlerOpts, proxy.WithExperimentRouter(experimentRouter))
		slog.Info("experiment routing (A/B testing) enabled")
	}

	proxyHandler := proxy.NewHandler(
		store, s3Storage, userBodyStorage, store, store,
		proxySecretStore, pricingSvc, resolver, proxyResolver, sessionMgr, cfg,
		handlerOpts...,
	)

	var adminCfg *server.AdminConfig
	var usageHandler *api.UsageHandler
	var metadataHandler *api.MetadataHandler
	var claudeAnalyticsHandler *api.ClaudeAnalyticsHandler
	var replayHandler *api.ReplayHandler
	var evalHandler *api.EvalHandler
	var experimentHandler *api.ExperimentHandler

	if cfg.JWT.Secret != "" {
		jwtSvc := auth.NewJWTService(cfg.JWT.Secret, cfg.JWT.Expiry)

		var adminSecretStore secrets.SecretStore
		if cfg.Secrets.EncryptionKey != "" {
			adminSecretStore, err = secrets.NewAESStore(cfg.Secrets.EncryptionKey)
			if err != nil {
				store.Close()
				pricingSvc.Close()
				return nil, fmt.Errorf("initialize admin secret store: %w", err)
			}
		}

		var emailSender api.EmailSender
		frontendURL := cfg.Email.FrontendURL
		if frontendURL == "" {
			frontendURL = cfg.OAuth.FrontendURL
			if frontendURL == "" {
				frontendURL = cfg.Server.BaseURL
			}
		}
		if cfg.Email.ResendAPIKey != "" && cfg.Email.From != "" {
			emailSender = apiemail.NewResendSender(cfg.Email.ResendAPIKey, cfg.Email.From)
		}

		adminHandler := api.NewAdminHandler(store, store, store, store, store, adminSecretStore, jwtSvc, store, store, emailSender, frontendURL)
		usageHandler = api.NewUsageHandler(store, store, store, store, adminSecretStore, s3Storage, userBodyStorage)
		metadataHandler = api.NewMetadataHandler(store, store)
		claudeAnalyticsHandler = api.NewClaudeAnalyticsHandler(store, store)
		replayHandler = api.NewReplayHandler(store, store)
		evalHandler = api.NewEvalHandler(store, store)
		if experimentRouter != nil {
			experimentHandler = api.NewExperimentHandler(store, store, experimentRouter)
		}

		var orgHandler *api.OrgHandler
		if adminSecretStore != nil {
			orgHandler = api.NewOrgHandler(store, store, adminSecretStore, jwtSvc, store, emailSender, frontendURL)
		}

		waitlistHandler := api.NewWaitlistHandler(store, emailSender)

		adminCfg = &server.AdminConfig{
			AdminHandler:    adminHandler,
			OrgHandler:      orgHandler,
			WaitlistHandler: waitlistHandler,
			JWTService:      jwtSvc,
			CORSOrigins:     cfg.CORS.AllowedOrigins,
		}

		if cfg.OAuth.GitHub.ClientID != "" || cfg.OAuth.Google.ClientID != "" {
			gatewayBaseURL := cfg.Server.BaseURL
			if gatewayBaseURL == "" {
				gatewayBaseURL = fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
			}
			oauthHandler := api.NewOAuthHandler(store, store, jwtSvc, cfg.OAuth, gatewayBaseURL)
			adminCfg.OAuthHandler = oauthHandler
		}

		slog.Info("admin web UI enabled")
	}

	srv := server.New(
		&cfg.Server, proxyHandler, store, apiHandler, resolver,
		adminCfg, claudeHandler, usageHandler, metadataHandler,
		claudeAnalyticsHandler, replayHandler, evalHandler, experimentHandler,
	)

	return &Server{
		inner:    srv,
		store:    store,
		pricing:  pricingSvc,
		s3:       s3Storage,
		enricher: o.requestEnricher,
	}, nil
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
