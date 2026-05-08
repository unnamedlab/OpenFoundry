// Tests for the MultiTableCommit handler — POST /iceberg/v1/transactions/commit
// (ICA-6). Mirrors `#[cfg(test)]` from
// services/iceberg-catalog-service/src/handlers/rest_catalog/transactions.rs
// and the wrapper-level tests in
// services/iceberg-catalog-service/src/domain/foundry_transaction.rs.
//
// Covers:
//
//   - empty `table-changes` is a no-op (mirrors Rust
//     `commit_with_no_pending_ops_is_a_noop`)
//   - happy path returns one CommittedTable per change (mirrors Rust
//     `commit_batches_every_pending_op_in_a_single_request`)
//   - retryable conflict surfaces as 409 with the structured
//     CONFLICTING_CONCURRENT_UPDATE envelope (mirrors Rust
//     `commit_propagates_retryable_conflict`)
//   - schema-incompatible update surfaces as 422 with the diff envelope
//   - chaos: 10 concurrent multi-table commits over 3 overlapping
//     tables — exactly one wins per overlap (covers the FOR UPDATE
//     deadlock-free ordering plus the optimistic-concurrency retry
//     contract).
package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/repo"
)

// multiTableStore extends fakeStore with an in-memory MultiTableCommit
// that simulates Postgres' FOR UPDATE semantics: a single global mutex
// is held for the entire commit so concurrent commits serialise on the
// same lock the real catalog acquires. This lets us assert the
// "exactly one wins per overlap" contract without standing up Postgres.
type multiTableStore struct {
	*fakeStore

	mu          sync.Mutex
	commitErr   error
	chaosTables map[string]*int64
}

func (s *multiTableStore) MultiTableCommit(_ context.Context, _ string, body *models.MultiTableCommitRequest) ([]models.CommittedTable, error) {
	if s.commitErr != nil {
		return nil, s.commitErr
	}
	if body == nil || len(body.TableChanges) == 0 {
		return []models.CommittedTable{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Deterministic lock order (mirrors repo.MultiTableCommit's sort).
	type resolved struct {
		t  *models.IcebergTable
		c  *models.MultiTableChange
		id string
	}
	resolvedSlice := make([]resolved, 0, len(body.TableChanges))
	s.fakeStore.mu.Lock()
	for i := range body.TableChanges {
		change := &body.TableChanges[i]
		key := tableKey(change.Identifier.Namespace, change.Identifier.Name)
		t, ok := s.fakeStore.tables[key]
		if !ok {
			s.fakeStore.mu.Unlock()
			return nil, &repo.RetryableError{
				TableRID:        key,
				Reason:          fmt.Sprintf("table `%s` not found", change.Identifier.Name),
				ConflictingWith: models.ConflictKindUnknown,
			}
		}
		resolvedSlice = append(resolvedSlice, resolved{t: t, c: change, id: t.ID.String()})
	}
	s.fakeStore.mu.Unlock()
	sort.Slice(resolvedSlice, func(i, j int) bool { return resolvedSlice[i].id < resolvedSlice[j].id })

	// Validate assert-ref-snapshot-id requirements against the
	// chaosTables snapshot map (FOR UPDATE simulation).
	for _, item := range resolvedSlice {
		for _, raw := range item.c.Requirements {
			var req map[string]json.RawMessage
			if err := json.Unmarshal(raw, &req); err != nil {
				return nil, err
			}
			var kind string
			_ = json.Unmarshal(req["type"], &kind)
			if kind != "assert-ref-snapshot-id" {
				continue
			}
			var expected *int64
			if v, ok := req["snapshot-id"]; ok && string(v) != "null" {
				var n int64
				if err := json.Unmarshal(v, &n); err == nil {
					expected = &n
				}
			}
			actual := s.chaosTables[item.id]
			if !ptrEq(expected, actual) {
				return nil, &repo.RetryableError{
					TableRID:        item.t.RID,
					Reason:          fmt.Sprintf("assert-ref-snapshot-id failed: expected %v, found %v", deref(expected), deref(actual)),
					ConflictingWith: models.ConflictKindCompaction,
				}
			}
		}
	}

	// Apply add-snapshot updates (advance the per-table snapshot).
	committed := make([]models.CommittedTable, 0, len(resolvedSlice))
	for _, item := range resolvedSlice {
		var newSnap *int64
		for _, raw := range item.c.Updates {
			var update map[string]json.RawMessage
			if err := json.Unmarshal(raw, &update); err != nil {
				return nil, err
			}
			var action string
			_ = json.Unmarshal(update["action"], &action)
			if action != "add-snapshot" {
				continue
			}
			var snap map[string]json.RawMessage
			if err := json.Unmarshal(update["snapshot"], &snap); err != nil {
				return nil, err
			}
			var sid int64
			_ = json.Unmarshal(snap["snapshot-id"], &sid)
			newSnap = &sid
		}
		if newSnap != nil {
			s.chaosTables[item.id] = newSnap
		}
		committed = append(committed, models.CommittedTable{
			Identifier:       models.TableIdentifier{Namespace: item.t.Namespace, Name: item.t.Name},
			TableRID:         item.t.RID,
			NewSnapshotID:    s.chaosTables[item.id],
			MetadataLocation: fmt.Sprintf("s3://test/%s/metadata/v2.metadata.json", item.t.Name),
		})
	}
	return committed, nil
}

func ptrEq(a, b *int64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func newMultiTableStore(t *testing.T, names ...string) *multiTableStore {
	t.Helper()
	fs := newFakeStore()
	chaos := map[string]*int64{}
	for _, name := range names {
		id := uuid.New()
		tab := &models.IcebergTable{
			ID:         id,
			RID:        "ri.foundry.main.iceberg-table." + id.String(),
			Namespace:  []string{"events"},
			Name:       name,
			TableUUID:  uuid.NewString(),
			Markings:   []string{"public"},
			SchemaJSON: json.RawMessage(`{"schema-id":0,"type":"struct","fields":[{"id":1,"name":"id","required":true,"type":"long"}]}`),
		}
		fs.tables[tableKey([]string{"events"}, name)] = tab
		chaos[id.String()] = nil
	}
	return &multiTableStore{fakeStore: fs, chaosTables: chaos}
}

// TestMultiTableCommit_EmptyIsNoOp mirrors Rust's
// `commit_with_no_pending_ops_is_a_noop`: a request with no
// table-changes returns 200 with an empty committed slice and never
// hits the catalog locking path.
func TestMultiTableCommit_EmptyIsNoOp(t *testing.T) {
	t.Parallel()
	store := newMultiTableStore(t)
	h := &handlers.Handlers{Repo: store}

	req := authed("POST", "/iceberg/v1/transactions/commit", `{"table-changes":[]}`)
	rec := httptest.NewRecorder()
	h.MultiTableCommit(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out models.MultiTableCommitResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Empty(t, out.Committed)
}

func TestMultiTableCommit_RequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{Repo: newMultiTableStore(t)}
	req := httptest.NewRequest("POST", "/iceberg/v1/transactions/commit",
		strings.NewReader(`{"table-changes":[]}`))
	rec := httptest.NewRecorder()
	h.MultiTableCommit(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestMultiTableCommit_RejectsMissingNamespace pins the per-change
// validation: every table-change must carry a non-empty namespace and
// table name, mirroring Rust's `if change.identifier.namespace.is_empty()`
// guard before any DB work happens.
func TestMultiTableCommit_RejectsMissingNamespace(t *testing.T) {
	t.Parallel()
	store := newMultiTableStore(t, "logins")
	h := &handlers.Handlers{Repo: store}

	body := `{"table-changes":[{"identifier":{"namespace":[],"name":"logins"}}]}`
	req := authed("POST", "/iceberg/v1/transactions/commit", body)
	rec := httptest.NewRecorder()
	h.MultiTableCommit(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing namespace")
}

// TestMultiTableCommit_HappyPathReturnsAllChanges mirrors Rust's
// `commit_batches_every_pending_op_in_a_single_request` — every
// table-change returns one CommittedTable in the response, in the
// caller-supplied identifier order on the wire (the per-row lock order
// is an internal implementation detail).
func TestMultiTableCommit_HappyPathReturnsAllChanges(t *testing.T) {
	t.Parallel()
	store := newMultiTableStore(t, "logins", "signups")
	h := &handlers.Handlers{Repo: store}

	body := `{"build_rid":"ri.foundry.main.build.42","table-changes":[
		{"identifier":{"namespace":["events"],"name":"logins"},"updates":[{"action":"add-snapshot","snapshot":{"snapshot-id":111}}]},
		{"identifier":{"namespace":["events"],"name":"signups"},"updates":[{"action":"add-snapshot","snapshot":{"snapshot-id":222}}]}
	]}`
	req := authed("POST", "/iceberg/v1/transactions/commit", body)
	rec := httptest.NewRecorder()
	h.MultiTableCommit(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out models.MultiTableCommitResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Len(t, out.Committed, 2)
	names := []string{out.Committed[0].Identifier.Name, out.Committed[1].Identifier.Name}
	assert.ElementsMatch(t, []string{"logins", "signups"}, names)
}

// TestMultiTableCommit_RetryableConflictMapsTo409 mirrors Rust's
// `commit_propagates_retryable_conflict`: a
// repo.RetryableError surfaces as HTTP 409 with the
// CONFLICTING_CONCURRENT_UPDATE envelope, including the structured
// `table_rid` + `conflicting_with` fields the build executor branches
// on.
func TestMultiTableCommit_RetryableConflictMapsTo409(t *testing.T) {
	t.Parallel()
	store := newMultiTableStore(t, "logins")
	store.commitErr = &repo.RetryableError{
		TableRID:        "ri.foundry.main.iceberg-table.abc",
		Reason:          "compaction wrote v3",
		ConflictingWith: models.ConflictKindCompaction,
	}
	h := &handlers.Handlers{Repo: store}

	body := `{"table-changes":[{"identifier":{"namespace":["events"],"name":"logins"}}]}`
	req := authed("POST", "/iceberg/v1/transactions/commit", body)
	rec := httptest.NewRecorder()
	h.MultiTableCommit(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	var env models.RetryableErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, "CONFLICTING_CONCURRENT_UPDATE", env.Error.Type)
	assert.Equal(t, http.StatusConflict, env.Error.Code)
	assert.Equal(t, "ri.foundry.main.iceberg-table.abc", env.Error.TableRID)
	assert.Equal(t, models.ConflictKindCompaction, env.Error.ConflictingWith)
	assert.Contains(t, env.Error.Message, "compaction wrote v3")
}

// TestMultiTableCommit_SchemaIncompatibleMapsTo422 verifies that a
// schema-strict diff surfaces as HTTP 422 with the same envelope the
// single-table CommitTable handler emits — the pipeline-authoring UI's
// "generate ALTER TABLE" CTA reuses the diff payload verbatim, so both
// paths must agree on the wire shape.
func TestMultiTableCommit_SchemaIncompatibleMapsTo422(t *testing.T) {
	t.Parallel()
	store := newMultiTableStore(t, "logins")
	current := json.RawMessage(`{"schema-id":0,"type":"struct","fields":[{"id":1,"name":"id","required":true,"type":"long"}]}`)
	attempted := json.RawMessage(`{"schema-id":0,"type":"struct","fields":[{"id":1,"name":"id","required":true,"type":"string"}]}`)
	store.commitErr = &domain.SchemaIncompatibleError{
		CurrentSchema:   current,
		AttemptedSchema: attempted,
		Diff:            domain.DiffSchemas(current, attempted),
	}
	h := &handlers.Handlers{Repo: store}

	body := `{"table-changes":[{"identifier":{"namespace":["events"],"name":"logins"}}]}`
	req := authed("POST", "/iceberg/v1/transactions/commit", body)
	rec := httptest.NewRecorder()
	h.MultiTableCommit(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	var view map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	errBody := view["error"].(map[string]any)
	assert.Equal(t, "UnprocessableEntityException", errBody["type"])
	diff := errBody["diff"].(map[string]any)
	deltas := diff["deltas"].([]any)
	require.Len(t, deltas, 1)
}

// TestMultiTableCommit_GenericErrorFallsBackToStatusFromErr keeps
// non-typed errors on the pre-ICA-6 statusFromErr heuristic so unknown
// failures still surface in a stable bucket.
func TestMultiTableCommit_GenericErrorFallsBackToStatusFromErr(t *testing.T) {
	t.Parallel()
	store := newMultiTableStore(t, "logins")
	store.commitErr = errors.New("boom")
	h := &handlers.Handlers{Repo: store}

	body := `{"table-changes":[{"identifier":{"namespace":["events"],"name":"logins"}}]}`
	req := authed("POST", "/iceberg/v1/transactions/commit", body)
	rec := httptest.NewRecorder()
	h.MultiTableCommit(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "boom")
}

// TestMultiTableCommit_ChaosExactlyOneWinsPerOverlap pins the
// optimistic-concurrency contract: 10 concurrent commits that all
// reference the same 3 tables and assert the same captured snapshot
// can have at most ONE winner per overlap. The other 9 must surface a
// retryable conflict so the pipeline-build executor re-snapshots and
// retries.
//
// The fake store holds a single global mutex during MultiTableCommit
// to mirror the catalog's FOR UPDATE locking — the first goroutine to
// acquire the lock advances every snapshot; the rest see the new
// snapshot and fail the assert-ref-snapshot-id requirement.
//
// Mirrors the Rust spec § "Job queuing and optimistic concurrency".
func TestMultiTableCommit_ChaosExactlyOneWinsPerOverlap(t *testing.T) {
	t.Parallel()
	store := newMultiTableStore(t, "t0", "t1", "t2")
	h := &handlers.Handlers{Repo: store}

	const goroutines = 10
	var (
		wg        sync.WaitGroup
		successes int32
		conflicts int32
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"build_rid":"build-%d","table-changes":[
				{"identifier":{"namespace":["events"],"name":"t0"},
				 "requirements":[{"type":"assert-ref-snapshot-id","ref":"main","snapshot-id":null}],
				 "updates":[{"action":"add-snapshot","snapshot":{"snapshot-id":%d}}]},
				{"identifier":{"namespace":["events"],"name":"t1"},
				 "requirements":[{"type":"assert-ref-snapshot-id","ref":"main","snapshot-id":null}],
				 "updates":[{"action":"add-snapshot","snapshot":{"snapshot-id":%d}}]},
				{"identifier":{"namespace":["events"],"name":"t2"},
				 "requirements":[{"type":"assert-ref-snapshot-id","ref":"main","snapshot-id":null}],
				 "updates":[{"action":"add-snapshot","snapshot":{"snapshot-id":%d}}]}
			]}`, seed, 1000+seed, 2000+seed, 3000+seed)
			req := authed("POST", "/iceberg/v1/transactions/commit", body)
			rec := httptest.NewRecorder()
			h.MultiTableCommit(rec, req)
			switch rec.Code {
			case http.StatusOK:
				atomic.AddInt32(&successes, 1)
			case http.StatusConflict:
				atomic.AddInt32(&conflicts, 1)
				var env models.RetryableErrorEnvelope
				if err := json.Unmarshal(rec.Body.Bytes(), &env); err == nil {
					assert.Equal(t, "CONFLICTING_CONCURRENT_UPDATE", env.Error.Type)
				}
			}
		}(i)
	}
	wg.Wait()

	assert.EqualValues(t, 1, successes, "exactly one commit must succeed")
	assert.EqualValues(t, goroutines-1, conflicts, "every other commit must surface a 409 retryable conflict")
}
