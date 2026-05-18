// Dependency probes and aggregated health.
//
// Each service may register zero or more *dependency probes* at
// startup — typically one per backing store (Postgres, Cassandra,
// Kafka, Lakekeeper, Redis, etc.). A probe is a small function that
// answers "is this dependency reachable right now?". The capability
// registry exposes them under two endpoints:
//
//   - `GET /_meta/deps`   — flat list of dependencies + per-probe status.
//   - `GET /_meta/health` — composite envelope (`status: ok|degraded`)
//     reusing the same probe results.
//
// Both endpoints are unauthenticated, like `/healthz`, and intentionally
// cheap: probes have a per-call timeout (default 1s) and the registry
// never caches results — staleness is the caller's problem (the gateway
// aggregator caches its own fan-out for 30s).
//
// See AGENT-CAPABILITIES-ROADMAP.md (Milestone M1.2).
package capabilities

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// DependencyKind labels what kind of backing store a probe targets.
// Free-form string but conventionally one of: `postgres`, `cassandra`,
// `kafka`, `nats`, `redis`, `lakekeeper`, `s3`, `http`.
type DependencyKind string

// DependencyProbe describes one backing store and how to ping it.
//
// `Probe` MUST honour `ctx` cancellation — the registry imposes a
// per-call timeout to keep `/_meta/health` snappy under partial
// outages. Returning `nil` means healthy; any non-nil error sets the
// dependency's status to `degraded` and surfaces `Error()`.
type DependencyProbe struct {
	Name    string
	Kind    DependencyKind
	Probe   func(ctx context.Context) error
	Timeout time.Duration // optional; 1s default

	// StatusOnSuccess/StatusOnError let runtime dependencies expose
	// capability-oriented states such as "available"/"unavailable"
	// while preserving the default store probe vocabulary of
	// "ok"/"degraded".
	StatusOnSuccess string
	StatusOnError   string
}

// DependencyStatus is the wire shape served by `GET /_meta/deps`.
type DependencyStatus struct {
	Name      string         `json:"name"`
	Kind      DependencyKind `json:"kind,omitempty"`
	Status    string         `json:"status"` // "ok" | "degraded" | "available" | "unavailable"
	LatencyMS int64          `json:"latency_ms"`
	Error     string         `json:"error,omitempty"`
}

// Health is the wire shape served by `GET /_meta/health`.
//
// Status is `ok` when every dependency reported `ok`, `degraded` if
// any probe failed, and stays `ok` when no probes are registered (a
// service with no declared dependencies cannot be partially down at
// the dependency layer).
type Health struct {
	SchemaVersion int                `json:"schema_version"`
	Service       string             `json:"service"`
	Version       string             `json:"version,omitempty"`
	Status        string             `json:"status"`
	GeneratedAt   string             `json:"generated_at"`
	Dependencies  []DependencyStatus `json:"dependencies"`
}

// RegisterDependency adds a probe to the registry. Safe to call
// before [Registry.Mount]; calling after Mount is also safe (the
// handlers read the slice under the same RWMutex), though the typical
// shape is to declare every probe at startup.
//
// Duplicate names are tolerated — the latest registration wins. This
// matches how dependencies are wired at process boot (a single call
// site per backing store).
func (rg *Registry) RegisterDependency(p DependencyProbe) {
	if strings.TrimSpace(p.Name) == "" || p.Probe == nil {
		return
	}
	if p.Timeout <= 0 {
		p.Timeout = time.Second
	}
	if strings.TrimSpace(p.StatusOnSuccess) == "" {
		p.StatusOnSuccess = "ok"
	}
	if strings.TrimSpace(p.StatusOnError) == "" {
		p.StatusOnError = "degraded"
	}
	rg.mu.Lock()
	defer rg.mu.Unlock()
	// Replace by name to keep ordering deterministic.
	for i, existing := range rg.deps {
		if existing.Name == p.Name {
			rg.deps[i] = p
			return
		}
	}
	rg.deps = append(rg.deps, p)
}

// Deps runs every registered probe concurrently and returns one
// [DependencyStatus] per probe, sorted by name.
func (rg *Registry) Deps(ctx context.Context) []DependencyStatus {
	rg.mu.RLock()
	probes := make([]DependencyProbe, len(rg.deps))
	copy(probes, rg.deps)
	rg.mu.RUnlock()

	out := make([]DependencyStatus, len(probes))
	var wg sync.WaitGroup
	for i, p := range probes {
		wg.Add(1)
		go func(i int, p DependencyProbe) {
			defer wg.Done()
			out[i] = runProbe(ctx, p, rg.now)
		}(i, p)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// HealthSnapshot composes [Registry.Deps] into a [Health] envelope.
func (rg *Registry) HealthSnapshot(ctx context.Context) Health {
	deps := rg.Deps(ctx)
	status := "ok"
	for _, d := range deps {
		if d.Status != "ok" && d.Status != "available" {
			status = "degraded"
			break
		}
	}
	return Health{
		SchemaVersion: SchemaVersion,
		Service:       rg.service,
		Version:       rg.version,
		Status:        status,
		GeneratedAt:   rg.now().UTC().Format(time.RFC3339),
		Dependencies:  deps,
	}
}

func runProbe(ctx context.Context, p DependencyProbe, now func() time.Time) DependencyStatus {
	cctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	start := now()
	err := p.Probe(cctx)
	latency := now().Sub(start).Milliseconds()
	st := DependencyStatus{
		Name:      p.Name,
		Kind:      p.Kind,
		Status:    p.StatusOnSuccess,
		LatencyMS: latency,
	}
	if err != nil {
		st.Status = p.StatusOnError
		st.Error = err.Error()
	}
	return st
}

func (rg *Registry) depsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": SchemaVersion,
			"service":        rg.service,
			"generated_at":   rg.now().UTC().Format(time.RFC3339),
			"dependencies":   rg.Deps(r.Context()),
		})
	})
}

func (rg *Registry) healthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := rg.HealthSnapshot(r.Context())
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if snap.Status != "ok" {
			// Surface degradation via HTTP status so naive consumers
			// (k8s readiness, curl loops) can react without parsing.
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(snap)
	})
}
