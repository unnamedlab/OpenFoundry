package meta

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/services/edge-gateway-service/internal/config"
)

// fakeService spins up an httptest server that serves a single
// capabilities snapshot. It tracks the number of requests so cache
// behaviour is observable.
func fakeService(t *testing.T, name string, hits *int32) *httptest.Server {
	t.Helper()
	snap := capabilities.Snapshot{
		SchemaVersion: capabilities.SchemaVersion,
		Service:       name,
		Version:       "test",
		GeneratedAt:   "2026-01-01T00:00:00Z",
		Capabilities: []capabilities.Capability{{
			ID: name + ".ping.get", Service: name, Method: http.MethodGet,
			Path: "/api/" + name + "/ping", Stable: true, Summary: "ping",
		}},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_meta/capabilities" {
			http.NotFound(w, r)
			return
		}
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snap)
	}))
}

func TestAggregator_FanOutAndCache(t *testing.T) {
	t.Parallel()
	var hitsA, hitsB int32
	a := fakeService(t, "alpha", &hitsA)
	defer a.Close()
	b := fakeService(t, "beta", &hitsB)
	defer b.Close()

	// Build minimal UpstreamURLs — most fields stay empty (skipped by
	// enumerate) and we hijack two arbitrary slots for the fakes.
	cfg := config.UpstreamURLs{
		IdentityFederation:  a.URL,
		AuthorizationPolicy: b.URL,
	}
	agg := New(cfg, 30*time.Second)
	w := httptest.NewRecorder()
	agg.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/_meta/capabilities", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Services) != 2 {
		t.Fatalf("want 2 services, got %d", len(resp.Services))
	}
	for _, s := range resp.Services {
		if s.Status != "ok" {
			t.Fatalf("service %q status=%s err=%s", s.Service, s.Status, s.Error)
		}
		if len(s.Capabilities) != 1 {
			t.Fatalf("service %q capabilities=%v", s.Service, s.Capabilities)
		}
	}

	// Second call must hit the cache — upstreams stay at 1 hit each.
	w2 := httptest.NewRecorder()
	agg.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/v1/_meta/capabilities", nil))
	if h := atomic.LoadInt32(&hitsA); h != 1 {
		t.Fatalf("alpha upstream called %d times, want 1 (cache miss)", h)
	}
	if h := atomic.LoadInt32(&hitsB); h != 1 {
		t.Fatalf("beta upstream called %d times, want 1 (cache miss)", h)
	}
}

func TestAggregator_UpstreamErrorIsReported(t *testing.T) {
	t.Parallel()
	cfg := config.UpstreamURLs{IdentityFederation: "http://127.0.0.1:1"} // unroutable
	agg := New(cfg, 30*time.Second)
	w := httptest.NewRecorder()
	agg.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Services) != 1 || resp.Services[0].Status != "error" {
		t.Fatalf("want one error entry, got %+v", resp.Services)
	}
	if resp.Services[0].Error == "" {
		t.Fatalf("error message must be set")
	}
}

func TestAggregator_CacheExpires(t *testing.T) {
	t.Parallel()
	var hits int32
	srv := fakeService(t, "alpha", &hits)
	defer srv.Close()
	agg := New(config.UpstreamURLs{IdentityFederation: srv.URL}, 10*time.Millisecond)

	// Inject a controllable clock so we don't depend on real time.
	now := time.Now()
	agg.now = func() time.Time { return now }

	ctx := context.Background()
	_ = agg.snapshot(ctx, "/_meta/capabilities", true)
	_ = agg.snapshot(ctx, "/_meta/capabilities", true)
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Fatalf("first two calls should share cache, hits=%d", h)
	}
	now = now.Add(time.Second) // jump well past TTL
	_ = agg.snapshot(ctx, "/_meta/capabilities", true)
	if h := atomic.LoadInt32(&hits); h != 2 {
		t.Fatalf("expired cache should refetch, hits=%d", h)
	}
}

func TestEnumerate_SkipsEmptyAndDedupes(t *testing.T) {
	t.Parallel()
	u := config.UpstreamURLs{
		IdentityFederation:  "http://shared:50112",
		OauthIntegration:    "http://shared:50112", // same URL, dropped
		AuthorizationPolicy: "http://other:50093",
	}
	got := enumerate(u)
	if len(got) != 2 {
		t.Fatalf("want 2 unique upstreams, got %d (%+v)", len(got), got)
	}
	for _, e := range got {
		if !strings.HasPrefix(e.url, "http://") {
			t.Fatalf("bad url %q", e.url)
		}
	}
}
