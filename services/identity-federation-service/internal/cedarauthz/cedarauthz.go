// Package cedarauthz wires Cedar policy enforcement for
// identity-federation-service (S3.1.i). Mirrors
// services/identity-federation-service/src/cedar_authz.rs verbatim.
//
// Three responsibilities:
//
//  1. Bundled policy bootstrap. The 3 admin policies live on disk
//     at policies/identity_admin.cedar and are baked into the
//     binary via go:embed. BundledPolicyRecords parses them
//     through cedar.NewPolicySetFromBytes and feeds the records to
//     cedarauthz.NewWithPolicies so they are strict-validated
//     against the bundled schema before the engine takes traffic.
//
//  2. Hot-reload subscriber. SpawnPolicyReload hooks the
//     `authz.policy.changed` NATS subject (per ADR-0027) and
//     rewrites the in-memory policy set on every change event.
//
//  3. AdminAuthzGuard middleware (in guard.go) — the chi middleware
//     that handlers compose for /jwks/rotate, /scim/v2/*, etc. It
//     differs from cedarauthz.Guard in one important way: it reads
//     `kind`, `mfa_age_secs` and `groups` from claims.Attributes
//     and wires them as Cedar entity attrs / parent UIDs so the
//     policies in identity_admin.cedar actually match service-
//     account / IdentityKeyRotators calls.
package cedarauthz

import (
	"context"
	"errors"
	"fmt"
	_ "embed"

	cedar "github.com/cedar-policy/cedar-go"
	natsgo "github.com/nats-io/nats.go"

	authzcedar "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
)

// IdentityAdminPoliciesSource is the Cedar source for the 3 admin
// policies, bundled into the binary at compile time.
//
// The file lives at services/identity-federation-service/policies/
// identity_admin.cedar relative to the service module root; the
// embed pragma below references it via the relative path.
//
//go:embed identity_admin.cedar
var IdentityAdminPoliciesSource string

// BundledPolicyRecords parses IdentityAdminPoliciesSource and
// returns one PolicyRecord per Cedar permit/forbid block, ready
// for cedarauthz.NewWithPolicies. Mirrors fn bundled_policy_records.
//
// cedar-go assigns synthetic policy IDs (policy0, policy1, …) when
// loading from text. We override them with the @id annotation when
// present so the records align with the Rust impl (which uses the
// AST policy id, also driven by @id).
func BundledPolicyRecords() ([]authzcedar.PolicyRecord, error) {
	parsed, err := cedar.NewPolicySetFromBytes("identity_admin.cedar", []byte(IdentityAdminPoliciesSource))
	if err != nil {
		return nil, fmt.Errorf("parse identity_admin.cedar: %w", err)
	}
	out := make([]authzcedar.PolicyRecord, 0)
	for synthID, policy := range parsed.Map() {
		annotated := string(policy.Annotations()["id"])
		id := annotated
		if id == "" {
			id = string(synthID)
		}
		out = append(out, authzcedar.PolicyRecord{
			ID:      id,
			Version: 1,
			Source:  string(policy.MarshalCedar()),
		})
	}
	return out, nil
}

// BootstrapEngine builds the AuthzEngine used by the service.
// Uses SlogAuditSink so every decision lands in the standard
// authz.audit slog target (Go-side equivalent of the Rust
// TracingAuditSink) — production swaps in a Kafka sink once
// authorization-policy-service exposes the audit.authz.v1
// publisher.
//
// Mirrors fn bootstrap_engine.
func BootstrapEngine() (*authzcedar.AuthzEngine, error) {
	records, err := BundledPolicyRecords()
	if err != nil {
		return nil, err
	}
	store, err := authzcedar.NewWithPolicies(records)
	if err != nil {
		return nil, err
	}
	return authzcedar.NewEngine(store, authzcedar.SlogAuditSink{}), nil
}

// SpawnPolicyReload subscribes to authz.policy.changed and
// re-loads the bundled policy set on every signal.
//
// The returned cancel function aborts the subscriber loop when
// invoked. On NATS connection errors the function returns the
// error and does not subscribe; the engine keeps serving the
// boot-time policy set.
//
// Mirrors fn spawn_policy_reload.
func SpawnPolicyReload(ctx context.Context, natsURL string, engine *authzcedar.AuthzEngine) (func(), error) {
	if engine == nil {
		return nil, errors.New("cedarauthz: engine is nil")
	}
	conn, err := natsgo.Connect(natsURL)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	subscriber := authzcedar.NewReloadSubscriber(conn)
	store := engine.Store()
	handle, err := subscriber.Run(ctx, func(_ context.Context) (int, error) {
		records, err := BundledPolicyRecords()
		if err != nil {
			return 0, fmt.Errorf("reload parse: %w", err)
		}
		if err := store.ReplacePolicies(records); err != nil {
			return 0, fmt.Errorf("reload apply: %w", err)
		}
		return len(records), nil
	})
	if err != nil {
		conn.Close()
		return nil, err
	}
	return func() {
		_ = handle.Shutdown()
		conn.Close()
	}, nil
}
