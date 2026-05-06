package cedarauthz

import (
	"context"
	"time"

	cedar "github.com/cedar-policy/cedar-go"
)

// AuthzEngine orchestrates policy evaluation, audit emission, and
// entity hydration. Composition mirrors the Rust impl:
//
//	AuthzEngine
//	  ├── *PolicyStore   (policy set + bundled schema)
//	  └── AuthzAuditSink (audit emission, fire-and-forget)
//
// Authorize is the canonical entry point. Callers that need raw
// diagnostics without audit emission can call PolicyStore.IsAuthorized
// directly.
type AuthzEngine struct {
	store *PolicyStore
	audit AuthzAuditSink
}

// NewEngine builds an engine from a [*PolicyStore] and an audit sink.
// Pass [NoopAuditSink]{} for tests.
func NewEngine(store *PolicyStore, audit AuthzAuditSink) *AuthzEngine {
	if audit == nil {
		audit = NoopAuditSink{}
	}
	return &AuthzEngine{store: store, audit: audit}
}

// NewEngineNoopAudit is a convenience constructor matching the Rust
// `with_noop_audit` helper.
func NewEngineNoopAudit(store *PolicyStore) *AuthzEngine {
	return NewEngine(store, NoopAuditSink{})
}

// Store returns the underlying policy store handle.
func (e *AuthzEngine) Store() *PolicyStore { return e.store }

// Audit returns the configured sink.
func (e *AuthzEngine) Audit() AuthzAuditSink { return e.audit }

// AuthorizeOutcome is the result returned by [AuthzEngine.Authorize].
type AuthorizeOutcome struct {
	Decision    cedar.Decision
	PolicyIDs   []string
	Diagnostics []string
}

// IsAllow reports whether the decision was Allow.
func (o *AuthorizeOutcome) IsAllow() bool { return o.Decision == cedar.Allow }

// Authorize evaluates a Cedar request and emits an audit event in the
// background. Audit emission runs in a goroutine so a slow sink can't
// stall the request hot path — matches the Rust `tokio::spawn` pattern.
//
// Callers that need synchronous audit must call the sink directly.
func (e *AuthzEngine) Authorize(
	ctx context.Context,
	principal cedar.EntityUID,
	action cedar.EntityUID,
	resource cedar.EntityUID,
	context_ cedar.Record,
	entities cedar.EntityGetter,
) (*AuthorizeOutcome, error) {
	req := cedar.Request{
		Principal: principal,
		Action:    action,
		Resource:  resource,
		Context:   context_,
	}
	decision, diag := e.store.IsAuthorized(entities, req)

	policyIDs := make([]string, 0, len(diag.Reasons))
	for _, r := range diag.Reasons {
		policyIDs = append(policyIDs, string(r.PolicyID))
	}
	diagnostics := make([]string, 0, len(diag.Errors))
	for _, d := range diag.Errors {
		diagnostics = append(diagnostics, d.String())
	}

	decisionStr := "deny"
	if decision == cedar.Allow {
		decisionStr = "allow"
	}

	event := AuthzAuditEvent{
		Timestamp:   time.Now().UTC(),
		Principal:   principal.String(),
		Action:      action.String(),
		Resource:    resource.String(),
		Decision:    decisionStr,
		PolicyIDs:   append([]string(nil), policyIDs...),
		Diagnostics: append([]string(nil), diagnostics...),
	}
	go e.audit.Emit(detachContext(ctx), event)

	return &AuthorizeOutcome{
		Decision:    decision,
		PolicyIDs:   policyIDs,
		Diagnostics: diagnostics,
	}, nil
}

// detachContext returns a new context that carries cancellation/values
// from `parent` for cancellation purposes only. The Rust impl uses
// `tokio::spawn` which doesn't propagate the request span; we mirror
// that by giving the audit goroutine a fresh background context so
// request cancellation doesn't cancel audit emission mid-write.
func detachContext(_ context.Context) context.Context {
	return context.Background()
}
