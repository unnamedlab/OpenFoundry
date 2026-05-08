package cedarauthz

import (
	_ "embed"
	"fmt"
	"sync"

	cedar "github.com/cedar-policy/cedar-go"
	xast "github.com/cedar-policy/cedar-go/x/exp/ast"
	"github.com/cedar-policy/cedar-go/x/exp/schema"
	"github.com/cedar-policy/cedar-go/x/exp/schema/resolved"
	cedarvalidate "github.com/cedar-policy/cedar-go/x/exp/schema/validate"
)

// SchemaSource is the bundled OpenFoundry Cedar schema, copied verbatim
// from libs/authz-cedar so the Go and Rust engines validate against the
// same source of truth.
//
//go:embed cedar_schema.cedarschema
var SchemaSource string

// PolicyRecord mirrors the columns of `pg-policy.cedar_policies`:
//
//	id TEXT PRIMARY KEY,
//	version INT NOT NULL,
//	source TEXT NOT NULL,
//	description TEXT,
//	active BOOL NOT NULL,
//	updated_at TIMESTAMPTZ
//
// The Postgres loader (follow-up slice) reads only the latest active
// version per id and feeds them into [PolicyStore.ReplacePolicies].
type PolicyRecord struct {
	ID          string
	Version     int32
	Source      string
	Description *string
}

// PolicyStore is the in-memory Cedar [*cedar.PolicySet] + bundled schema,
// behind a sync.RWMutex.
//
// Cloning the struct is cheap: every PolicyStore handle shares the same
// pointer to the underlying mutex + policy set so ReplacePolicies is
// observed immediately by every reader. The schema is parsed once at
// construction and is immutable.
type PolicyStore struct {
	schema *schema.Schema
	// resolved is the schema in its post-`Resolve()` form, used by the
	// validator. Cached because Resolve() walks the AST.
	resolved *resolved.Schema

	mu       sync.RWMutex
	policies *cedar.PolicySet
}

// NewEmpty builds an empty store using the bundled schema.
func NewEmpty() (*PolicyStore, error) {
	var s schema.Schema
	if err := s.UnmarshalCedar([]byte(SchemaSource)); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSchemaParse, err)
	}
	r, err := s.Resolve()
	if err != nil {
		return nil, fmt.Errorf("%w: resolve: %v", ErrSchemaParse, err)
	}
	return &PolicyStore{
		schema:   &s,
		resolved: r,
		policies: cedar.NewPolicySet(),
	}, nil
}

// NewWithPolicies builds an empty store and immediately loads `records`.
func NewWithPolicies(records []PolicyRecord) (*PolicyStore, error) {
	store, err := NewEmpty()
	if err != nil {
		return nil, err
	}
	if err := store.ReplacePolicies(records); err != nil {
		return nil, err
	}
	return store, nil
}

// Schema returns the bundled (parsed) schema. Cloning is cheap; callers
// receive a pointer to the same parsed AST.
func (p *PolicyStore) Schema() *schema.Schema { return p.schema }

// ResolvedSchema returns the cached resolved schema.
func (p *PolicyStore) ResolvedSchema() *resolved.Schema { return p.resolved }

// ReplacePolicies atomically swaps the active policy set.
//
// Each record is parsed individually so a single bad row reports a
// precise [*PolicyParseError]. The resulting [*cedar.PolicySet] is then
// schema-validated in strict mode; only on success do we swap the
// internal pointer. Concurrent readers therefore never observe a
// partially-applied or invalid state.
func (p *PolicyStore) ReplacePolicies(records []PolicyRecord) error {
	next := cedar.NewPolicySet()
	for _, rec := range records {
		var policy cedar.Policy
		if err := policy.UnmarshalCedar([]byte(rec.Source)); err != nil {
			return &PolicyParseError{ID: rec.ID, Cause: err}
		}
		if !next.Add(cedar.PolicyID(rec.ID), &policy) {
			// Add returns false on duplicate id within the same set.
			return &ValidationError{Errors: []string{
				"duplicate policy id: " + rec.ID,
			}}
		}
	}

	if err := p.validateSet(next); err != nil {
		return err
	}

	p.mu.Lock()
	p.policies = next
	p.mu.Unlock()
	return nil
}

// validateSet runs strict schema validation on every policy in `set`.
// Cedar-go's validator works one policy at a time; we aggregate failures
// so callers see all validation errors in a single pass.
func (p *PolicyStore) validateSet(set *cedar.PolicySet) error {
	v := cedarvalidate.New(p.resolved, cedarvalidate.WithStrict())
	var errs []string
	for id, pol := range set.All() {
		// The top-level cedar.Policy returns AST nodes from "cedar-go/ast"
		// while the experimental validator consumes "cedar-go/x/exp/ast".
		// Both packages share identical memory layout — the cedar-go test
		// suite uses the same direct pointer cast (see
		// internal/testvalidate/testvalidate.go RunPolicyChecks).
		if err := v.Policy(string(id), (*xast.Policy)(pol.AST())); err != nil {
			errs = append(errs, string(id)+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// Len reports the number of currently loaded policies. Safe for concurrent use.
func (p *PolicyStore) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.policies.Map())
}

// IsEmpty is true when no policies are loaded.
func (p *PolicyStore) IsEmpty() bool { return p.Len() == 0 }

// IsAuthorized evaluates `request` against the current policy set. The
// underlying *cedar.PolicySet is sharded per-Add internally so cloning
// the pointer is sufficient: we hold the read lock only long enough to
// snapshot the pointer.
func (p *PolicyStore) IsAuthorized(entities cedar.EntityGetter, request cedar.Request) (cedar.Decision, cedar.Diagnostic) {
	p.mu.RLock()
	set := p.policies
	p.mu.RUnlock()
	return set.IsAuthorized(entities, request)
}

// IsAllowed reduces a Request to a single bool. Convenience for callers
// that don't need diagnostics (use [AuthzEngine.Authorize] when audit
// is required).
func (p *PolicyStore) IsAllowed(entities cedar.EntityGetter, request cedar.Request) bool {
	decision, _ := p.IsAuthorized(entities, request)
	return decision == cedar.Allow
}
