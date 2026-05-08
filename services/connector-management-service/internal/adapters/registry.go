package adapters

import (
	"fmt"
	"sort"
	"sync"
)

// Registry binds connector_type strings to [Factory] instances. It mirrors
// the implicit dispatch that lives in
// `services/connector-management-service/src/domain/discovery.rs`, where
// the `match connection.connector_type.as_str()` arms are spread across
// the `connectors::*` modules. The Go side promotes that dispatch to a
// first-class object so per-connector packages can `Register` themselves
// in their package init and the dispatcher in
// `internal/domain/discovery.go` can stay agnostic to which connectors are
// linked into a given binary.
//
// Registry is safe for concurrent reads; mutation (Register / Unregister)
// is intended for startup wiring and tests but is also goroutine-safe.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewRegistry returns an empty [Registry]. Per-connector packages call
// [Registry.Register] during process bootstrap to bind themselves.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register binds factory to connectorType. Returns [ErrAlreadyRegistered]
// if the type already has a factory — re-registration is a programmer
// error (typically a duplicate package init in tests) so the registry
// surfaces it instead of silently overwriting.
//
// connectorType matches the value stored in `connections.connector_type`
// — i.e. the same string Rust matches on in `discover_sources` /
// `query_virtual_table`. Do not lowercase or rewrite it; "postgresql" and
// "postgres" are distinct routes in the Rust dispatcher and must stay
// distinct here.
func (r *Registry) Register(connectorType string, factory Factory) error {
	if connectorType == "" {
		return fmt.Errorf("adapters: connector_type must be non-empty")
	}
	if factory == nil {
		return fmt.Errorf("adapters: factory for %q must be non-nil", connectorType)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[connectorType]; exists {
		return fmt.Errorf("%w: %s", ErrAlreadyRegistered, connectorType)
	}
	r.factories[connectorType] = factory
	return nil
}

// MustRegister is the panic-on-error form of [Registry.Register], suited
// to package init blocks where a registration failure is unrecoverable.
func (r *Registry) MustRegister(connectorType string, factory Factory) {
	if err := r.Register(connectorType, factory); err != nil {
		panic(err)
	}
}

// Unregister removes the factory bound to connectorType. Returns
// [ErrAdapterNotFound] if no factory is bound. Primarily intended for
// tests that need to swap implementations.
func (r *Registry) Unregister(connectorType string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[connectorType]; !exists {
		return fmt.Errorf("%w: %s", ErrAdapterNotFound, connectorType)
	}
	delete(r.factories, connectorType)
	return nil
}

// Get returns the [Factory] bound to connectorType, or
// [ErrAdapterNotFound] (wrapped with the type name) when no factory is
// registered. Callers that need a ready-to-use [ConnectorAdapter] usually
// prefer [Registry.Lookup].
func (r *Registry) Get(connectorType string) (Factory, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, ok := r.factories[connectorType]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrAdapterNotFound, connectorType)
	}
	return factory, nil
}

// Has reports whether a factory is bound to connectorType. Cheaper than
// [Registry.Get] when the caller only needs presence (e.g. building a
// supported-connectors list for a /capabilities response).
func (r *Registry) Has(connectorType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[connectorType]
	return ok
}

// Lookup is the convenience form of [Registry.Get] that asks the factory
// for a fresh [ConnectorAdapter]. Returns the same [ErrAdapterNotFound]
// envelope as [Registry.Get] so callers can errors.Is-match against it.
func (r *Registry) Lookup(connectorType string) (ConnectorAdapter, error) {
	factory, err := r.Get(connectorType)
	if err != nil {
		return nil, err
	}
	return factory.New(), nil
}

// Names returns the registered connector_type values in lexicographic
// order — handy for /capabilities responses, debug pages, and tests that
// want a deterministic snapshot of which adapters are linked in.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.factories))
	for name := range r.factories {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
