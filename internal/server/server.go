package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superset-studio/majordomo-gateway/internal/api"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/config"
	"github.com/superset-studio/majordomo-gateway/internal/httputil"
	"github.com/superset-studio/majordomo-gateway/internal/proxy"
)

// HealthChecker can verify that a backing resource is reachable.
type HealthChecker interface {
	Ping(ctx context.Context) error
}

type Server struct {
	httpServer    *http.Server
	config        *config.ServerConfig
	healthChecker HealthChecker
}

type AdminConfig struct {
	AdminHandler    *api.AdminHandler
	OAuthHandler    *api.OAuthHandler
	OrgHandler      *api.OrgHandler
	WaitlistHandler *api.WaitlistHandler
	JWTService      *auth.JWTService
	CORSOrigins     []string
}

func New(cfg *config.ServerConfig, proxyHandler *proxy.Handler, checker HealthChecker, apiHandler *api.Handler, resolver *auth.Resolver, adminCfg *AdminConfig, claudeHandler *api.ClaudeSessionHandler, usageHandler *api.UsageHandler, metadataHandler *api.MetadataHandler, claudeAnalyticsHandler *api.ClaudeAnalyticsHandler, replayHandler *api.ReplayHandler, evalHandler *api.EvalHandler) *Server {
	s := &Server{
		config:        cfg,
		healthChecker: checker,
	}

	router := chi.NewRouter()

	router.Use(Recovery)
	router.Use(RequestID)
	router.Use(Logger)

	if adminCfg != nil && len(adminCfg.CORSOrigins) > 0 {
		router.Use(CORSMiddleware(adminCfg.CORSOrigins))
	}

	router.Get("/health", healthHandler)
	router.Get("/readyz", s.readyzHandler)

	if adminCfg != nil && adminCfg.AdminHandler != nil && adminCfg.JWTService != nil {
		router.Route("/api/v1/admin", func(r chi.Router) {
			r.Post("/login", adminCfg.AdminHandler.Login)
			r.Post("/signup", adminCfg.AdminHandler.Signup)
			r.Post("/email/verify", adminCfg.AdminHandler.VerifyEmail)
			r.Post("/email/verify/resend", adminCfg.AdminHandler.ResendVerification)
			r.Post("/password/reset-request", adminCfg.AdminHandler.RequestPasswordReset)
			r.Post("/password/reset", adminCfg.AdminHandler.ResetPassword)

			if adminCfg.OrgHandler != nil {
				r.Post("/orgs/signup", adminCfg.OrgHandler.OrgSignup)
			}

			if adminCfg.WaitlistHandler != nil {
				r.Post("/waitlist", adminCfg.WaitlistHandler.JoinWaitlist)
			}

			if adminCfg.OAuthHandler != nil {
				r.Get("/auth/github", adminCfg.OAuthHandler.GitHubLogin)
				r.Get("/auth/github/callback", adminCfg.OAuthHandler.GitHubCallback)
				r.Get("/auth/google", adminCfg.OAuthHandler.GoogleLogin)
				r.Get("/auth/google/callback", adminCfg.OAuthHandler.GoogleCallback)
			}

			r.Group(func(r chi.Router) {
				r.Use(api.JWTAuthMiddleware(adminCfg.JWTService))
				r.Get("/me", adminCfg.AdminHandler.Me)
				r.Put("/me/password", adminCfg.AdminHandler.ChangePassword)
				r.Get("/me/s3-config", adminCfg.AdminHandler.GetS3Config)
				r.Put("/me/s3-config", adminCfg.AdminHandler.UpdateS3Config)
				r.Delete("/me/s3-config", adminCfg.AdminHandler.DeleteS3Config)
				r.Get("/me/cloud-storage-config", adminCfg.AdminHandler.GetCloudStorageConfig)
				r.Put("/me/cloud-storage-config", adminCfg.AdminHandler.UpdateCloudStorageConfig)
				r.Delete("/me/cloud-storage-config", adminCfg.AdminHandler.DeleteCloudStorageConfig)
				r.Get("/me/provider-keys", adminCfg.AdminHandler.ListProviderKeys)
				r.Put("/me/provider-keys/{provider}", adminCfg.AdminHandler.SetProviderKey)
				r.Delete("/me/provider-keys/{provider}", adminCfg.AdminHandler.DeleteProviderKey)
				r.Get("/api-keys", adminCfg.AdminHandler.ListAPIKeys)
				r.Post("/api-keys", adminCfg.AdminHandler.CreateAPIKey)
				r.Get("/api-keys/{id}", adminCfg.AdminHandler.GetAPIKey)
				r.Put("/api-keys/{id}", adminCfg.AdminHandler.UpdateAPIKey)
				r.Delete("/api-keys/{id}", adminCfg.AdminHandler.RevokeAPIKey)
				r.Get("/api-keys/{id}/proxy-keys", adminCfg.AdminHandler.ListProxyKeys)
				r.Post("/api-keys/{id}/proxy-keys", adminCfg.AdminHandler.CreateProxyKey)
				r.Get("/api-keys/{id}/proxy-keys/{pkId}", adminCfg.AdminHandler.GetProxyKey)
				r.Delete("/api-keys/{id}/proxy-keys/{pkId}", adminCfg.AdminHandler.RevokeProxyKey)
				r.Get("/api-keys/{id}/proxy-keys/{pkId}/providers", adminCfg.AdminHandler.ListProviderMappings)
				r.Put("/api-keys/{id}/proxy-keys/{pkId}/providers/{provider}", adminCfg.AdminHandler.SetProviderMapping)
				r.Delete("/api-keys/{id}/proxy-keys/{pkId}/providers/{provider}", adminCfg.AdminHandler.DeleteProviderMapping)

				if usageHandler != nil {
					r.Post("/usage/summary", usageHandler.GetSummary)
					r.Post("/usage/daily", usageHandler.GetDailyUsage)
					r.Post("/usage/models", usageHandler.GetModelBreakdown)
					r.Post("/usage/api-keys", usageHandler.GetAPIKeyBreakdown)
					r.Post("/usage/requests", usageHandler.ListRequests)
					r.Get("/usage/requests/{id}", usageHandler.GetRequestDetail)
					r.Get("/usage/requests/{id}/body", usageHandler.GetRequestBody)
					r.Post("/usage/metadata/{keyName}", usageHandler.GetMetadataBreakdown)
				}

				if claudeAnalyticsHandler != nil {
				r.Post("/claude/summary", claudeAnalyticsHandler.GetSummary)
				r.Post("/claude/daily", claudeAnalyticsHandler.GetDailyStats)
				r.Post("/claude/sessions", claudeAnalyticsHandler.ListSessions)
				r.Post("/claude/tools", claudeAnalyticsHandler.GetToolUsage)
				r.Post("/claude/performance", claudeAnalyticsHandler.GetPerformance)
				r.Post("/claude/models", claudeAnalyticsHandler.GetModelUsage)
				r.Post("/claude/api-keys", claudeAnalyticsHandler.GetAPIKeyBreakdown)
				r.Get("/claude/sessions/{id}", claudeAnalyticsHandler.GetSessionDetail)
			}

			if metadataHandler != nil {
					r.Get("/metadata-keys", metadataHandler.ListMetadataKeys)
					r.Put("/api-keys/{id}/metadata-keys/{keyName}", metadataHandler.UpdateMetadataKey)
				}

				if replayHandler != nil {
					r.Get("/replay/providers", replayHandler.ListProviders)
					r.Post("/replay/runs", replayHandler.CreateRun)
					r.Get("/replay/runs", replayHandler.ListRuns)
					r.Get("/replay/runs/{id}", replayHandler.GetRun)
					r.Post("/replay/runs/{id}/cancel", replayHandler.CancelRun)
					r.Get("/replay/runs/{id}/results", replayHandler.ListResults)
					r.Get("/replay/runs/{id}/results/{resultId}", replayHandler.GetResult)
				}

				if evalHandler != nil {
					r.Post("/eval/sets", evalHandler.CreateEvalSet)
					r.Get("/eval/sets", evalHandler.ListEvalSets)
					r.Get("/eval/sets/{id}", evalHandler.GetEvalSet)
					r.Put("/eval/sets/{id}", evalHandler.UpdateEvalSet)
					r.Delete("/eval/sets/{id}", evalHandler.DeleteEvalSet)
					r.Post("/eval/sets/{id}/items", evalHandler.AddItems)
					r.Post("/eval/sets/{id}/items/from-filters", evalHandler.AddItemsFromFilters)
					r.Delete("/eval/sets/{id}/items/{requestId}", evalHandler.RemoveItem)
					r.Get("/eval/sets/{id}/items", evalHandler.ListItems)

					r.Post("/eval/runs", evalHandler.CreateRun)
					r.Get("/eval/runs", evalHandler.ListRuns)
					r.Get("/eval/runs/{id}", evalHandler.GetRun)
					r.Post("/eval/runs/{id}/cancel", evalHandler.CancelRun)
					r.Get("/eval/runs/{id}/results", evalHandler.ListResults)
					r.Get("/eval/runs/{id}/results/{resultId}", evalHandler.GetResult)
				}

				if adminCfg.OrgHandler != nil {
					r.Get("/orgs/current", adminCfg.OrgHandler.GetCurrentOrg)
					r.Put("/orgs/current", adminCfg.OrgHandler.UpdateOrg)

					r.Get("/orgs/current/members", adminCfg.OrgHandler.ListMembers)
					r.Put("/orgs/current/members/{userId}/role", adminCfg.OrgHandler.UpdateMemberRole)
					r.Delete("/orgs/current/members/{userId}", adminCfg.OrgHandler.RemoveMember)

					r.Post("/orgs/current/invites", adminCfg.OrgHandler.CreateInvite)
					r.Get("/orgs/current/invites", adminCfg.OrgHandler.ListPendingInvites)
					r.Delete("/orgs/current/invites/{id}", adminCfg.OrgHandler.RevokeInvite)

					r.Post("/invites/{token}/accept", adminCfg.OrgHandler.AcceptInvite)

					r.Get("/orgs/current/s3-config", adminCfg.OrgHandler.GetOrgS3Config)
					r.Put("/orgs/current/s3-config", adminCfg.OrgHandler.UpdateOrgS3Config)
					r.Delete("/orgs/current/s3-config", adminCfg.OrgHandler.ClearOrgS3Config)
					r.Get("/orgs/current/cloud-storage-config", adminCfg.OrgHandler.GetOrgCloudStorageConfig)
					r.Put("/orgs/current/cloud-storage-config", adminCfg.OrgHandler.UpdateOrgCloudStorageConfig)
					r.Delete("/orgs/current/cloud-storage-config", adminCfg.OrgHandler.ClearOrgCloudStorageConfig)
				}
			})
		})
	}

	if apiHandler != nil {
		router.Route("/api/v1", func(r chi.Router) {
			r.Use(api.AuthMiddleware(resolver))
			r.Post("/proxy-keys", apiHandler.CreateProxyKey)
			r.Get("/proxy-keys", apiHandler.ListProxyKeys)
			r.Get("/proxy-keys/{id}", apiHandler.GetProxyKey)
			r.Delete("/proxy-keys/{id}", apiHandler.RevokeProxyKey)
			r.Put("/proxy-keys/{id}/providers/{provider}", apiHandler.SetProviderMapping)
			r.Delete("/proxy-keys/{id}/providers/{provider}", apiHandler.DeleteProviderMapping)
			r.Get("/proxy-keys/{id}/providers", apiHandler.ListProviderMappings)
		})
	}

	if claudeHandler != nil {
		router.Route("/api/v1/claude-sessions", func(r chi.Router) {
			r.Use(api.AuthMiddleware(resolver))
			r.Post("/", claudeHandler.StartSession)
			r.Get("/", claudeHandler.ListSessions)
			r.Get("/{id}", claudeHandler.GetSession)
			r.Post("/{id}/end", claudeHandler.EndSession)
			r.Get("/{id}/requests", claudeHandler.ListSessionRequests)
		})
	}

	router.Handle("/*", proxyHandler)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	return s
}

func (s *Server) Start() error {
	slog.Info("starting server", "addr", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("shutting down server")
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) ShutdownWithTimeout(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.Shutdown(ctx)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (s *Server) readyzHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := s.healthChecker.Ping(ctx); err != nil {
		slog.Warn("readiness check failed", "error", err)
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": err.Error()})
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
