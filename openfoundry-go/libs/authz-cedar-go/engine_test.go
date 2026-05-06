package cedarauthz_test

import (
	"context"
	"sync"
	"testing"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cedarauthz "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
)

// recordingSink captures every emitted event in-memory. Used to assert
// fire-and-forget audit emission without tying tests to a real Kafka.
type recordingSink struct {
	mu     sync.Mutex
	events []cedarauthz.AuthzAuditEvent
}

func (r *recordingSink) Emit(_ context.Context, e cedarauthz.AuthzAuditEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recordingSink) snapshot() []cedarauthz.AuthzAuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]cedarauthz.AuthzAuditEvent, len(r.events))
	copy(out, r.events)
	return out
}

// Helper: build a minimal entity store covering the principal, action,
// and resource referenced by the test. Uses cedar-go's typed builder
// API to mirror what `cedar_policy::Entities` looks like in Rust.
func mkEntities(t *testing.T, items ...cedar.Entity) cedar.EntityMap {
	t.Helper()
	m := cedar.EntityMap{}
	for i := range items {
		m[items[i].UID] = items[i]
	}
	return m
}

func mkUser(uid cedar.EntityUID, tenant string, clearances []cedar.EntityUID, roles []string) cedar.Entity {
	clearSet := make([]cedar.Value, 0, len(clearances))
	for _, c := range clearances {
		clearSet = append(clearSet, c)
	}
	roleSet := make([]cedar.Value, 0, len(roles))
	for _, r := range roles {
		roleSet = append(roleSet, cedar.String(r))
	}
	return cedar.Entity{
		UID: uid,
		Attributes: cedar.NewRecord(cedar.RecordMap{
			"tenant":     cedar.String(tenant),
			"clearances": cedar.NewSet(clearSet...),
			"roles":      cedar.NewSet(roleSet...),
		}),
	}
}

func mkMarking(uid cedar.EntityUID, name string) cedar.Entity {
	return cedar.Entity{
		UID: uid,
		Attributes: cedar.NewRecord(cedar.RecordMap{
			"name": cedar.String(name),
		}),
	}
}

func mkDataset(uid cedar.EntityUID, rid, tenant string, markings []cedar.EntityUID) cedar.Entity {
	markSet := make([]cedar.Value, 0, len(markings))
	for _, m := range markings {
		markSet = append(markSet, m)
	}
	return cedar.Entity{
		UID: uid,
		Attributes: cedar.NewRecord(cedar.RecordMap{
			"rid":      cedar.String(rid),
			"tenant":   cedar.String(tenant),
			"markings": cedar.NewSet(markSet...),
		}),
	}
}

// ─── End-to-end Allow/Deny ────────────────────────────────────────────

func TestEngineAuthorizeAllowOnClearanceMatch(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewWithPolicies([]cedarauthz.PolicyRecord{{
		ID: "permit-cleared-readers",
		Source: `
			permit(
			  principal,
			  action == Action::"read",
			  resource is Dataset
			) when {
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}})
	require.NoError(t, err)
	rec := &recordingSink{}
	eng := cedarauthz.NewEngine(store, rec)

	publicMark := types.NewEntityUID("Marking", "public")
	user := types.NewEntityUID("User", "alice")
	dataset := types.NewEntityUID("Dataset", "ds-1")
	action := types.NewEntityUID("Action", "read")

	entities := mkEntities(t,
		mkMarking(publicMark, "public"),
		mkUser(user, "acme", []cedar.EntityUID{publicMark}, []string{"reader"}),
		mkDataset(dataset, "ri.dataset.acme.ds-1", "acme", []cedar.EntityUID{publicMark}),
	)

	out, err := eng.Authorize(context.Background(), user, action, dataset, cedar.NewRecord(cedar.RecordMap{}), entities)
	require.NoError(t, err)
	require.True(t, out.IsAllow(), "decision: %v / diagnostics: %v", out.Decision, out.Diagnostics)
	assert.Contains(t, out.PolicyIDs, "permit-cleared-readers")

	// Audit emission is fire-and-forget; a tiny sleep would race. Use
	// require.Eventually to wait for the goroutine to land its event.
	require.Eventually(t, func() bool {
		return len(rec.snapshot()) == 1
	}, 1_000_000_000, 1_000_000) // 1s budget, 1ms tick.
	ev := rec.snapshot()[0]
	assert.Equal(t, "allow", ev.Decision)
	assert.Equal(t, user.String(), ev.Principal)
	assert.Equal(t, action.String(), ev.Action)
	assert.Equal(t, dataset.String(), ev.Resource)
	assert.Contains(t, ev.PolicyIDs, "permit-cleared-readers")
}

func TestEngineAuthorizeDenyOnMissingPolicy(t *testing.T) {
	t.Parallel()
	// Empty policy set → default deny.
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	eng := cedarauthz.NewEngineNoopAudit(store)

	user := types.NewEntityUID("User", "bob")
	dataset := types.NewEntityUID("Dataset", "ds-2")
	action := types.NewEntityUID("Action", "read")

	entities := mkEntities(t,
		mkUser(user, "acme", nil, nil),
		mkDataset(dataset, "ri.dataset.acme.ds-2", "acme", nil),
	)

	out, err := eng.Authorize(context.Background(), user, action, dataset, cedar.NewRecord(cedar.RecordMap{}), entities)
	require.NoError(t, err)
	assert.False(t, out.IsAllow())
	assert.Equal(t, cedar.Deny, out.Decision)
}

func TestEngineAuthorizeDenyOnInsufficientClearance(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewWithPolicies([]cedarauthz.PolicyRecord{{
		ID: "permit-cleared-readers",
		Source: `
			permit(
			  principal,
			  action == Action::"read",
			  resource is Dataset
			) when {
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}})
	require.NoError(t, err)
	eng := cedarauthz.NewEngineNoopAudit(store)

	publicMark := types.NewEntityUID("Marking", "public")
	piiMark := types.NewEntityUID("Marking", "pii")
	user := types.NewEntityUID("User", "carol")
	dataset := types.NewEntityUID("Dataset", "ds-3")
	action := types.NewEntityUID("Action", "read")

	// User cleared for "public" only; dataset requires "pii" → deny.
	entities := mkEntities(t,
		mkMarking(publicMark, "public"),
		mkMarking(piiMark, "pii"),
		mkUser(user, "acme", []cedar.EntityUID{publicMark}, nil),
		mkDataset(dataset, "ri.dataset.acme.ds-3", "acme", []cedar.EntityUID{piiMark}),
	)

	out, err := eng.Authorize(context.Background(), user, action, dataset, cedar.NewRecord(cedar.RecordMap{}), entities)
	require.NoError(t, err)
	assert.Equal(t, cedar.Deny, out.Decision)
	assert.Empty(t, out.PolicyIDs, "no permit reason recorded on deny")
}
