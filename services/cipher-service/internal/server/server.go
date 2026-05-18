// Package server wires the HTTP router, observability and graceful
// shutdown for cipher-service Milestone A. Shape mirrors
// docs/templates/service-skeleton so platform tooling stays uniform;
// the route table reflects the gateway's `/api/v1/auth/cipher` prefix.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/libs/observability"

	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/handler"
)

// Permission keys protecting each route. The gateway maps these onto
// Cedar policies once libs/authz-cedar-go integrates (CIP.4 in
// Milestone B). For now they are enforced at the middleware via
// libs/auth-middleware RequirePermissions, with admin role bypass.
const (
	PermKeysRead  = "cipher.keys.read"
	PermKeysAdmin = "cipher.keys.admin"
	PermEncrypt   = "cipher.encrypt"
	PermDecrypt   = "cipher.decrypt"
)

// Server bundles the lifecycle of the HTTP listener.
type Server struct {
	httpServer *http.Server
	cfg        *config.Config
	log        *slog.Logger
}

// New builds a Server with all middleware and routes mounted.
//
// `state` carries the cipher-service dependency bag (repo, KMS, audit
// recorder); construction lives in cmd/cipher-service/main.go so the
// wiring choices (Postgres connection, KMS backend) stay close to
// startup.
func New(cfg *config.Config, state *handler.State, metrics *observability.Metrics, log *slog.Logger, probes ...capabilities.DependencyProbe) (*Server, error) {
	jwtCfg := authmw.NewJWTConfig(cfg.JWT.Secret).
		WithIssuer(cfg.JWT.Issuer).
		WithAudience(cfg.JWT.Audience)

	r := BuildRouter(cfg, state, metrics, jwtCfg, probes...)

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

// BuildRouter assembles the chi router. Exposed so handler tests can
// exercise the full stack via httptest.NewServer without booting the
// listener.
func BuildRouter(cfg *config.Config, state *handler.State, metrics *observability.Metrics, jwtCfg *authmw.JWTConfig, probes ...capabilities.DependencyProbe) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Compress(5))
	r.Use(chimw.Timeout(30 * time.Second))

	caps := capabilities.New(cfg.Service.Name, cfg.Service.Version)

	// Public endpoints (no auth).
	r.Get("/healthz", handler.Health(cfg.Service.Name, cfg.Service.Version))
	if metrics != nil {
		r.Method(http.MethodGet, "/metrics", metrics.Handler())
	}
	for _, p := range probes {
		caps.RegisterDependency(p)
	}
	caps.Mount(r)

	api := r.With(authmw.Middleware(jwtCfg))
	mountAPIRoutes(api, caps, state)
	return r
}

// Run blocks until the listener returns or `ctx` is cancelled.
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

// mountAPIRoutes wires the Milestone A cipher routes under
// /api/v1/auth/cipher. Every route is gated by libs/auth-middleware's
// RequirePermissions; admin role bypass is honoured per the package
// contract.
func mountAPIRoutes(r chi.Router, caps *capabilities.Registry, state *handler.State) {
	const basePath = "/api/v1/auth/cipher"

	// Registry / read paths.
	read := r.With(authmw.RequirePermissions(PermKeysRead))
	caps.MustRegister(read, capabilities.Capability{
		ID:           "cipher.algorithms.list",
		Method:       http.MethodGet,
		Path:         basePath + "/algorithms",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "List built-in cipher algorithms and envelope metadata.",
		Tags:         []string{"cipher", "algorithms"},
	}, http.HandlerFunc(state.ListAlgorithms))
	caps.MustRegister(read, capabilities.Capability{
		ID:           "cipher.keys.list",
		Method:       http.MethodGet,
		Path:         basePath + "/keys",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "List cipher keys for the caller's tenant (paginated).",
		Tags:         []string{"cipher", "keys"},
	}, http.HandlerFunc(state.ListKeys))
	caps.MustRegister(read, capabilities.Capability{
		ID:           "cipher.keys.get",
		Method:       http.MethodGet,
		Path:         basePath + "/keys/{id}",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Fetch a cipher key's registry metadata (never material).",
		Tags:         []string{"cipher", "keys"},
	}, http.HandlerFunc(state.GetKey))

	// Admin paths.
	admin := r.With(authmw.RequirePermissions(PermKeysAdmin))
	caps.MustRegister(admin, capabilities.Capability{
		ID:           "cipher.keys.create",
		Method:       http.MethodPost,
		Path:         basePath + "/keys",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Create a tenant-scoped cipher key.",
		Tags:         []string{"cipher", "keys"},
	}, http.HandlerFunc(state.CreateKey))
	caps.MustRegister(admin, capabilities.Capability{ID: "cipher.peppers.create", Method: http.MethodPost, Path: basePath + "/peppers", Stable: true, RequiresAuth: true, Summary: "Create a tenant-scoped pepper registry entry.", Tags: []string{"cipher", "peppers"}}, http.HandlerFunc(state.CreatePepper))
	caps.MustRegister(admin, capabilities.Capability{ID: "cipher.peppers.rotate", Method: http.MethodPost, Path: basePath + "/peppers/{id}/rotate", Stable: true, RequiresAuth: true, Summary: "Rotate a pepper's material.", Tags: []string{"cipher", "peppers"}}, http.HandlerFunc(state.RotatePepper))
	caps.MustRegister(admin, capabilities.Capability{
		ID:           "cipher.keys.update",
		Method:       http.MethodPatch,
		Path:         basePath + "/keys/{id}",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Update mutable cipher key resource metadata.",
		Tags:         []string{"cipher", "keys"},
	}, http.HandlerFunc(state.UpdateKey))
	caps.MustRegister(admin, capabilities.Capability{
		ID:           "cipher.keys.delete",
		Method:       http.MethodDelete,
		Path:         basePath + "/keys/{id}",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Delete a cipher key resource and its version rows.",
		Tags:         []string{"cipher", "keys"},
	}, http.HandlerFunc(state.DeleteKey))
	caps.MustRegister(admin, capabilities.Capability{
		ID:           "cipher.keys.rotate",
		Method:       http.MethodPost,
		Path:         basePath + "/keys/{id}/rotate",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Append a new version to a cipher key; older versions stay decryptable.",
		Tags:         []string{"cipher", "keys"},
	}, http.HandlerFunc(state.RotateKey))
	caps.MustRegister(admin, capabilities.Capability{
		ID:           "cipher.keys.rotate_new",
		Method:       http.MethodPost,
		Path:         basePath + "/keys/{id}/rotate-new",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Create a successor cipher key id preserving policy and metadata.",
		Tags:         []string{"cipher", "keys"},
	}, http.HandlerFunc(state.RotateKeyToNewID))
	caps.MustRegister(admin, capabilities.Capability{ID: "cipher.keys.wrap_for_promotion", Method: http.MethodPost, Path: basePath + "/keys/{id}/wrap-for-promotion", Stable: true, RequiresAuth: true, Summary: "Plan cross-environment key wrapping for Apollo promotion.", Tags: []string{"cipher", "keys", "promotion"}}, http.HandlerFunc(state.WrapKeyForPromotion))
	caps.MustRegister(admin, capabilities.Capability{
		ID:           "cipher.keys.revoke",
		Method:       http.MethodPost,
		Path:         basePath + "/keys/{id}/revoke",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Revoke a cipher key (hard-stop encrypt and decrypt).",
		Tags:         []string{"cipher", "keys"},
	}, http.HandlerFunc(state.RevokeKey))
	caps.MustRegister(admin, capabilities.Capability{
		ID:           "cipher.keys.retire",
		Method:       http.MethodPost,
		Path:         basePath + "/keys/{id}/retire",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Retire a cipher key (decrypt-only).",
		Tags:         []string{"cipher", "keys"},
	}, http.HandlerFunc(state.RetireKey))

	// Encrypt / decrypt paths gated by their own permissions.
	encrypt := r.With(authmw.RequirePermissions(PermEncrypt))
	caps.MustRegister(encrypt, capabilities.Capability{
		ID:           "cipher.encrypt.batch",
		Method:       http.MethodPost,
		Path:         basePath + "/encrypt",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Encrypt one value or batch-encrypt up to 64 values; every operation is audited.",
		Tags:         []string{"cipher", "batch"},
	}, http.HandlerFunc(state.Encrypt))
	caps.MustRegister(encrypt, capabilities.Capability{ID: "cipher.encrypt.batch.v2", Method: http.MethodPost, Path: basePath + "/encrypt-batch", Stable: true, RequiresAuth: true, Summary: "Batch encrypt up to 64 values preserving input order.", Tags: []string{"cipher", "batch"}}, http.HandlerFunc(state.EncryptBatch))

	tokenize := r.With(authmw.RequirePermissions(PermEncrypt))
	caps.MustRegister(tokenize, capabilities.Capability{ID: "cipher.tokenize", Method: http.MethodPost, Path: basePath + "/tokenize", Stable: true, RequiresAuth: true, Summary: "Tokenize plaintext with a pepper-backed stable hash.", Tags: []string{"cipher", "tokenize"}}, http.HandlerFunc(state.Tokenize))

	decrypt := r.With(authmw.RequirePermissions(PermDecrypt))
	caps.MustRegister(decrypt, capabilities.Capability{
		ID:           "cipher.decrypt.batch",
		Method:       http.MethodPost,
		Path:         basePath + "/decrypt",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Decrypt one envelope or batch-decrypt up to 64 envelopes after policy and marking checks.",
		Tags:         []string{"cipher", "batch"},
	}, http.HandlerFunc(state.Decrypt))
	caps.MustRegister(decrypt, capabilities.Capability{ID: "cipher.decrypt.batch.v2", Method: http.MethodPost, Path: basePath + "/decrypt-batch", Stable: true, RequiresAuth: true, Summary: "Batch decrypt up to 64 envelopes preserving input order.", Tags: []string{"cipher", "batch"}}, http.HandlerFunc(state.DecryptBatch))
	caps.MustRegister(decrypt, capabilities.Capability{ID: "cipher.decrypt.stream", Method: http.MethodPost, Path: basePath + "/decrypt-stream", Stable: true, RequiresAuth: true, Summary: "Streaming newline-delimited decrypt for dataset column reads.", Tags: []string{"cipher", "streaming"}}, http.HandlerFunc(state.DecryptStream))
}
