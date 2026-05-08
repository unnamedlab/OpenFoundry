// Package cedarauthz is the Go port of libs/authz-cedar.
//
// Backed by github.com/cedar-policy/cedar-go (v1.6.0+, post-1.0; AWS
// maintains it in lock-step with cedar-rust v4 against the same
// conformance test suite).
//
// Per ADR-0027 every service evaluates Cedar policies in-process. The
// schema is bundled at compile time via [SchemaSource]; policies are
// loaded from `pg-policy.cedar_policies` (Postgres adapter follow-up
// slice) and held under a sync.RWMutex so hot-reload via the
// `authz.policy.changed` NATS event swaps the active set atomically.
//
// Two surfaces:
//
//   - [PolicyStore] — pure in-memory policy set + schema. Knows how to
//     parse, validate, and replace its inner *cedar.PolicySet. Safe to
//     use from tests and dev tooling without touching Postgres.
//   - Adapters (postgres / nats / kafka audit / iceberg-policies /
//     schedule-policies / chi-middleware) — follow-up slices.
//
// Policy evaluation itself is a thin wrapper around
// `(*cedar.PolicySet).IsAuthorized`; see [PolicyStore.IsAuthorized].
package cedarauthz

import "errors"

// Sentinel errors. Wrap with fmt.Errorf("...: %w", err) when surfacing
// per-policy detail (use [PolicyParseError] for that).
var (
	ErrSchemaParse = errors.New("invalid bundled cedar schema")
	ErrValidation  = errors.New("policy validation failed")
	ErrBackend     = errors.New("backing store error")
)

// PolicyParseError carries the policy id alongside the underlying parse
// error. Mirrors the Rust `PolicyStoreError::PolicyParse` variant.
type PolicyParseError struct {
	ID    string
	Cause error
}

func (e *PolicyParseError) Error() string {
	return "invalid policy `" + e.ID + "`: " + e.Cause.Error()
}

func (e *PolicyParseError) Unwrap() error { return e.Cause }

// ValidationError carries the aggregated per-policy validation errors.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	if len(e.Errors) == 0 {
		return ErrValidation.Error()
	}
	out := ErrValidation.Error() + ":"
	for _, line := range e.Errors {
		out += "\n  " + line
	}
	return out
}

func (e *ValidationError) Is(target error) bool {
	return target == ErrValidation
}
