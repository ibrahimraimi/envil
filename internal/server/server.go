package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ibrahimraimi/envil/internal/auth"
	"github.com/ibrahimraimi/envil/internal/store"
)

type Config struct {
	ListenAddr     string
	RateLimitRPS   float64
	RateLimitBurst int
	MaxBlobBytes   int
}

type Server struct {
	httpServer *http.Server
	store      store.Store
	logger     *slog.Logger
}

func NewServer(cfg Config, st store.Store, nonces auth.NonceTracker, logger *slog.Logger) *Server {
	router := BuildRouter(cfg, st, nonces, logger)

	httpServer := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return &Server{
		httpServer: httpServer,
		store:      st,
		logger:     logger,
	}
}

func BuildRouter(cfg Config, st store.Store, nonces auth.NonceTracker, logger *slog.Logger) http.Handler {
	router := chi.NewRouter()

	rateLimiter := NewIPRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst)
	router.Use(RecoveryMiddleware(logger))
	router.Use(LoggingMiddleware(logger))
	router.Use(RateLimitMiddleware(rateLimiter))

	router.Route("/v1", func(r chi.Router) {
		r.Get("/healthz", HandleHealthCheck)

		r.Group(func(accountRouter chi.Router) {
			accountRouter.Use(auth.RequireSignatureOnly(nonces))
			accountRouter.Post("/accounts", HandleRegisterAccount(st))
		})

		r.Group(func(authRouter chi.Router) {
			authRouter.Use(auth.RequireAuth(st, nonces))

			authRouter.Delete("/account", HandleDeleteAccount(st))

			authRouter.Get("/projects", HandleListProjects(st))
			authRouter.Put("/projects/{id}", HandleUpsertProject(st))
			authRouter.Delete("/projects/{id}", HandleDeleteProject(st))

			authRouter.Post("/projects/{id}/backups", HandleCreateBackup(st))
			authRouter.Get("/projects/{id}/backups", HandleListBackups(st))
			authRouter.Get("/projects/{id}/backups/{version}", HandleGetBackup(st))
		})
	})

	return router
}

func (s *Server) Start() error {
	s.logger.Info("starting envil server", slog.String("addr", s.httpServer.Addr))
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down envil server")
	return s.httpServer.Shutdown(ctx)
}
