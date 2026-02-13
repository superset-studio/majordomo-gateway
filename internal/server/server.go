package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superset-studio/majordomo-gateway/internal/api"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/config"
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
	AdminHandler *api.AdminHandler
	JWTService   *auth.JWTService
	CORSOrigins  []string
}

func New(cfg *config.ServerConfig, proxyHandler *proxy.Handler, checker HealthChecker, apiHandler *api.Handler, resolver *auth.Resolver, adminCfg *AdminConfig) *Server {
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
			r.Group(func(r chi.Router) {
				r.Use(api.JWTAuthMiddleware(adminCfg.JWTService))
				r.Get("/me", adminCfg.AdminHandler.Me)
				r.Put("/me/password", adminCfg.AdminHandler.ChangePassword)
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

	w.Header().Set("Content-Type", "application/json")

	if err := s.healthChecker.Ping(ctx); err != nil {
		slog.Warn("readiness check failed", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
