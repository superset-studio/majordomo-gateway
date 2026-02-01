package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superset-studio/majordomo-gateway/internal/config"
	"github.com/superset-studio/majordomo-gateway/internal/proxy"
)

type Server struct {
	httpServer *http.Server
	config     *config.ServerConfig
}

func New(cfg *config.ServerConfig, proxyHandler *proxy.Handler) *Server {
	router := chi.NewRouter()

	router.Use(Recovery)
	router.Use(RequestID)
	router.Use(Logger)

	router.Get("/health", healthHandler)
	router.Handle("/*", proxyHandler)

	return &Server{
		httpServer: &http.Server{
			Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
			Handler:      router,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
		},
		config: cfg,
	}
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
