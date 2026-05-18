package capabilities

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Router is the minimal subset of `chi.Router` the registry needs to
// mount routes. Defined locally so the package does not depend on chi
// directly — any router that exposes `Method` works (chi.Mux,
// chi.Router, custom mocks for tests).
type Router interface {
	Method(method, pattern string, h http.Handler)
}

// Registry collects [Capability] entries for a single service and
// exposes them under `/_meta/capabilities`.
//
// Concurrency: [Registry.Register] and [Registry.Snapshot] are safe
// to call from multiple goroutines. In practice services register
// during startup (single-threaded) and serve from many request
// goroutines after that.
type Registry struct {
	service string
	version string
	now     func() time.Time

	mu    sync.RWMutex
	items map[string]Capability // keyed by Capability.ID
	deps  []DependencyProbe
}

// New builds an empty registry owned by `service` (e.g.
// `ontology-actions-service`). `version` is surfaced in the snapshot
// so agents can correlate a capability list with a deployment.
//
// `service` must be non-empty; we panic on empty input because this
// is a programmer error caught at process start, not a runtime
// failure.
func New(service, version string) *Registry {
	if strings.TrimSpace(service) == "" {
		panic("capabilities: service name must not be empty")
	}
	return &Registry{
		service: service,
		version: version,
		now:     time.Now,
		items:   make(map[string]Capability),
	}
}

// Register validates `cap`, fills in `cap.Service` from the registry,
// and mounts `handler` on `r` for the declared method+path.
//
// The handler is mounted with `r.Method` so chi's
// `Get`/`Post`/`Route` API is unaffected — callers can still use the
// idiomatic chi shape elsewhere; Register exists for routes that want
// agent visibility.
//
// Returns [ErrDuplicateCapability] if a capability with the same ID
// was already registered, and [ErrInvalidCapability] for any other
// schema violation. Both are programmer errors; callers should
// `panic` or surface them at startup, never silently swallow.
func (rg *Registry) Register(r Router, cap Capability, handler http.Handler) error {
	if r == nil {
		return fmt.Errorf("%w: nil router", ErrInvalidCapability)
	}
	if handler == nil {
		return fmt.Errorf("%w: nil handler for %s", ErrInvalidCapability, cap.ID)
	}
	cap.Service = rg.service
	cap.Method = strings.ToUpper(cap.Method)
	if err := cap.Validate(); err != nil {
		return err
	}

	rg.mu.Lock()
	if _, exists := rg.items[cap.ID]; exists {
		rg.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrDuplicateCapability, cap.ID)
	}
	rg.items[cap.ID] = cap
	rg.mu.Unlock()

	r.Method(cap.Method, cap.Path, handler)
	return nil
}

// MustRegister wraps [Registry.Register] and panics on any error.
// Use it at startup when registration failure should crash the
// process (the typical case).
func (rg *Registry) MustRegister(r Router, cap Capability, handler http.Handler) {
	if err := rg.Register(r, cap, handler); err != nil {
		panic(fmt.Sprintf("capabilities: register %q failed: %v", cap.ID, err))
	}
}

// Snapshot is the JSON shape served by `/_meta/capabilities`.
// Stable across patch releases — see [SchemaVersion].
type Snapshot struct {
	SchemaVersion int                `json:"schema_version"`
	Service       string             `json:"service"`
	Version       string             `json:"version,omitempty"`
	GeneratedAt   string             `json:"generated_at"`
	Capabilities  []Capability       `json:"capabilities"`
	Dependencies  []DependencyStatus `json:"dependencies,omitempty"`
}

// Snapshot returns the registry's current capability list as a
// stable, deterministically ordered [Snapshot]. Capabilities are
// sorted by ID so byte-for-byte snapshot diffs are meaningful in CI.
func (rg *Registry) Snapshot() Snapshot {
	rg.mu.RLock()
	out := make([]Capability, 0, len(rg.items))
	for _, c := range rg.items {
		out = append(out, c)
	}
	rg.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return Snapshot{
		SchemaVersion: SchemaVersion,
		Service:       rg.service,
		Version:       rg.version,
		GeneratedAt:   rg.now().UTC().Format(time.RFC3339),
		Capabilities:  out,
	}
}

// SnapshotWithDependencies returns the capability list plus current
// dependency probe states. It is used by the HTTP handler so
// /_meta/capabilities communicates whether runtime-backed capabilities
// are actually available in this process.
func (rg *Registry) SnapshotWithDependencies(ctx context.Context) Snapshot {
	snap := rg.Snapshot()
	snap.Dependencies = rg.Deps(ctx)
	return snap
}

// Handler returns the HTTP handler that serves `GET
// /_meta/capabilities`. Callers wire it themselves so they control
// the mount point and any additional middleware (CORS, caching, …).
//
// The handler intentionally requires no authentication: the
// capability surface is meant to be discoverable. Sensitive details
// (proto messages of admin-only routes, etc.) belong in the body
// schema, not in the catalog.
func (rg *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(rg.SnapshotWithDependencies(r.Context()))
	})
}

// Mount registers the four agent-facing meta endpoints on `r` and
// records each as a stable capability so the catalog includes them.
//
// Endpoints:
//
//   - `GET /_meta/capabilities` — the catalog itself.
//   - `GET /_meta/version`      — build provenance (M1.3).
//   - `GET /_meta/health`       — composite health envelope (M1.2).
//   - `GET /_meta/deps`         — raw per-dependency probe results (M1.2).
//
// All four are unauthenticated by design (parity with `/healthz`).
func (rg *Registry) Mount(r Router) {
	rg.MustRegister(r, Capability{
		ID:           "_meta.capabilities.list",
		Method:       http.MethodGet,
		Path:         "/_meta/capabilities",
		Stable:       true,
		RequiresAuth: false,
		Summary:      "List every capability this service exposes.",
		Tags:         []string{"meta"},
	}, rg.Handler())
	rg.MustRegister(r, Capability{
		ID:           "_meta.version.get",
		Method:       http.MethodGet,
		Path:         "/_meta/version",
		Stable:       true,
		RequiresAuth: false,
		Summary:      "Build provenance for this service binary.",
		Tags:         []string{"meta"},
	}, rg.versionHandler())
	rg.MustRegister(r, Capability{
		ID:           "_meta.health.get",
		Method:       http.MethodGet,
		Path:         "/_meta/health",
		Stable:       true,
		RequiresAuth: false,
		Summary:      "Composite health envelope including dependency probes.",
		Tags:         []string{"meta"},
	}, rg.healthHandler())
	rg.MustRegister(r, Capability{
		ID:           "_meta.deps.get",
		Method:       http.MethodGet,
		Path:         "/_meta/deps",
		Stable:       true,
		RequiresAuth: false,
		Summary:      "Raw per-dependency probe results.",
		Tags:         []string{"meta"},
	}, rg.depsHandler())
}
