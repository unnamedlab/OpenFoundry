// HTTP mocks for stubbing neighbour services in integration tests.
//
// Wraps net/http/httptest with sane defaults so each test can spin up
// a fresh ephemeral server and obtain its base URL to inject into the
// service AppState (e.g. data_asset_catalog_url, retention_policy_url,
// lineage client base, audit sink). Mirrors `libs/testing/src/mocks.rs`
// on the Rust side — the Go module sticks to the standard library since
// httptest is enough for the matching surface.
package testingx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// MockServer is an ephemeral httptest.Server that supports installing
// METHOD+path JSON responses and an optional catch-all default. Safe
// for concurrent route installation while the server is running.
type MockServer struct {
	*httptest.Server

	mu       sync.RWMutex
	routes   map[string]http.HandlerFunc
	fallback http.HandlerFunc
}

// StartNeighbor starts a fresh httptest server on an ephemeral port
// and returns it together with its base URL (no trailing slash).
func StartNeighbor() (*MockServer, string) {
	ms := &MockServer{routes: make(map[string]http.HandlerFunc)}
	ms.Server = httptest.NewServer(http.HandlerFunc(ms.serveHTTP))
	return ms, ms.Server.URL
}

// Close terminates the underlying httptest server. Safe to call
// multiple times.
func (s *MockServer) Close() {
	if s == nil || s.Server == nil {
		return
	}
	s.Server.Close()
	s.Server = nil
}

// BaseURL returns the server's base URL (no trailing slash).
func (s *MockServer) BaseURL() string {
	if s == nil || s.Server == nil {
		return ""
	}
	return s.Server.URL
}

func (s *MockServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	s.mu.RLock()
	h, ok := s.routes[key]
	fb := s.fallback
	s.mu.RUnlock()
	switch {
	case ok:
		h(w, r)
	case fb != nil:
		fb(w, r)
	default:
		http.NotFound(w, r)
	}
}

// InstallDefaultOK installs a catch-all `200 {}` handler so the
// service under test never sees a 404 from the neighbour during tests
// that don't care about the call shape.
func InstallDefaultOK(server *MockServer) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.fallback = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}
}

// InstallJSON installs a typed JSON `200` response on a specific
// METHOD path pair. The body is marshalled once at install time and
// cached; passing an unmarshalable value panics (consistent with the
// rest of this crate's permissive style).
func InstallJSON(server *MockServer, httpMethod, route string, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		panic("testingx.InstallJSON: marshal body: " + err.Error())
	}
	key := strings.ToUpper(httpMethod) + " " + route
	server.mu.Lock()
	defer server.mu.Unlock()
	server.routes[key] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
	}
}
