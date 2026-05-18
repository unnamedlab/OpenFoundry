// Package server wires the HTTP router, middleware and graceful
// shutdown for the knowledge-index service.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/rag"
	aikernel "github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/handlers"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/knowledge-index-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/knowledge-index-service/internal/handler"
)

type Server struct {
	httpServer *http.Server
	cfg        *config.Config
	log        *slog.Logger
}

type options struct {
	pool        *pgxpool.Pool
	store       aikernel.KnowledgeStore
	vectorStore rag.VectorStore
}

// Option customizes production server wiring. Tests can inject stores directly;
// production should pass a pgx pool opened by main.
type Option func(*options)

func WithPostgresPool(pool *pgxpool.Pool) Option {
	return func(o *options) { o.pool = pool }
}

func WithKnowledgeStore(store aikernel.KnowledgeStore) Option {
	return func(o *options) { o.store = store }
}

func WithVectorStore(store rag.VectorStore) Option {
	return func(o *options) { o.vectorStore = store }
}

func New(cfg *config.Config, metrics *observability.Metrics, log *slog.Logger, opts ...Option) (*Server, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if metrics == nil {
		metrics = observability.NewMetrics()
	}
	if log == nil {
		log = slog.Default()
	}

	resolved := options{}
	for _, opt := range opts {
		opt(&resolved)
	}

	knowledgeHandlers, err := buildKnowledgeHandlers(cfg, resolved)
	if err != nil {
		return nil, err
	}

	jwtCfg := authmw.NewJWTConfig(cfg.JWT.Secret).
		WithIssuer(cfg.JWT.Issuer).
		WithAudience(cfg.JWT.Audience)

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Compress(5))
	r.Use(chimw.Timeout(30 * time.Second))

	r.Get("/healthz", handler.Health(cfg.Service.Name, cfg.Service.Version))
	r.Method(http.MethodGet, "/metrics", metrics.Handler())

	knowledge := handler.NewKnowledgeHandler(knowledgeHandlers)
	api := r.With(authmw.Middleware(jwtCfg))
	api.Route("/api/v1/ai/knowledge-bases", knowledge.Mount)

	s := &Server{
		cfg: cfg,
		log: log,
		httpServer: &http.Server{
			Addr:              cfg.Server.Addr,
			Handler:           r,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
	return s, nil
}

func buildKnowledgeHandlers(cfg *config.Config, opts options) (*aikernel.KnowledgeHandlers, error) {
	if opts.store != nil {
		if _, ok := opts.store.(*aikernel.FakeKnowledgeStore); ok && !cfg.AllowFakeStore {
			return nil, errors.New("fake knowledge store requires allow_fake_store=true and is limited to local/test execution")
		}
		return &aikernel.KnowledgeHandlers{Pool: opts.pool, Store: opts.store, VectorStore: opts.vectorStore}, nil
	}
	if opts.pool != nil {
		return &aikernel.KnowledgeHandlers{
			Pool:        opts.pool,
			Store:       aikernel.NewPGKnowledgeStore(opts.pool),
			VectorStore: opts.vectorStore,
		}, nil
	}
	if cfg.AllowFakeStore {
		return &aikernel.KnowledgeHandlers{Store: aikernel.NewFakeKnowledgeStore(), VectorStore: opts.vectorStore}, nil
	}
	if cfg.Database.URL == "" {
		return nil, errors.New("database.url is required for knowledge-index-service production persistence; set allow_fake_store=true only for explicit local/test execution")
	}
	return nil, fmt.Errorf("postgres pool is required when database.url is configured")
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("listening", slog.String("addr", s.cfg.Server.Addr))
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-errCh:
		return err
	}
}

func (s *Server) shutdown() error {
	timeout := 15 * time.Second
	if d, err := time.ParseDuration(s.cfg.Server.ShutdownTimeout); err == nil {
		timeout = d
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	s.log.Info("shutting down")
	return s.httpServer.Shutdown(ctx)
}
