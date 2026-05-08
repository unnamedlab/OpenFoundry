package storageabstraction

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// --- Errors --------------------------------------------------------------

func TestErrorMessagesMatchRustFormat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want string
	}{
		{NotFound("aircraft/AC-1"), "not found: aircraft/AC-1"},
		{Invalid("page token malformed"), "invalid argument: page token malformed"},
		{TenantScope("tenant=x foreign read"), "tenant scope violation: tenant=x foreign read"},
		{Backend("connection reset"), "backend error: connection reset"},
	}
	for _, c := range cases {
		assert.EqualError(t, c.err, c.want)
	}
}

func TestErrorClassificationHelpers(t *testing.T) {
	t.Parallel()
	nf := NotFound("x")
	inv := Invalid("y")
	ts := TenantScope("z")
	be := Backend("w")

	assert.True(t, IsNotFound(nf))
	assert.False(t, IsNotFound(inv))
	assert.True(t, IsInvalidArgument(inv))
	assert.False(t, IsInvalidArgument(nf))
	assert.True(t, IsTenantScope(ts))
	assert.False(t, IsTenantScope(be))
	assert.True(t, IsBackendError(be))
	assert.False(t, IsBackendError(ts))
}

func TestErrorClassificationUnwrapsThroughFmtErrorf(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("get aircraft: %w", NotFound("AC-1"))
	assert.True(t, IsNotFound(wrapped))
	assert.False(t, IsBackendError(wrapped))
}

func TestErrorClassificationUnwrapsThroughErrorsJoin(t *testing.T) {
	t.Parallel()
	combined := errors.Join(errors.New("network"), Backend("dns"))
	assert.True(t, IsBackendError(combined))
}

func TestInvalidfFormatsArgs(t *testing.T) {
	t.Parallel()
	err := Invalidf("page size %d out of range %d..%d", 9000, 1, 5000)
	assert.EqualError(t, err, "invalid argument: page size 9000 out of range 1..5000")
}

// --- Read consistency ----------------------------------------------------

func TestReadConsistencyFactories(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ConsistencyStrong, Strong().Level)
	assert.Equal(t, ConsistencyEventual, Eventual().Level)

	bs := BoundedStaleness(750 * time.Millisecond)
	assert.Equal(t, ConsistencyBoundedStaleness, bs.Level)
	assert.Equal(t, 750*time.Millisecond, bs.Staleness)
}

func TestReadConsistencyDefaultIsStrong(t *testing.T) {
	t.Parallel()
	// Mirrors `impl Default for ReadConsistency` in Rust.
	var rc ReadConsistency
	assert.Equal(t, ConsistencyStrong, rc.Level)
}

// --- Put outcome ---------------------------------------------------------

func TestPutOutcomeFactories(t *testing.T) {
	t.Parallel()
	ins := Inserted()
	assert.Equal(t, PutInserted, ins.Kind)
	assert.Zero(t, ins.PreviousVersion)
	assert.Zero(t, ins.NewVersion)

	upd := Updated(7, 8)
	assert.Equal(t, PutUpdated, upd.Kind)
	assert.Equal(t, uint64(7), upd.PreviousVersion)
	assert.Equal(t, uint64(8), upd.NewVersion)

	conflict := VersionConflict(3, 4)
	assert.Equal(t, PutVersionConflict, conflict.Kind)
	assert.Equal(t, uint64(3), conflict.ExpectedVersion)
	assert.Equal(t, uint64(4), conflict.ActualVersion)
}

// --- Compile-time assertions that the never* witnesses below
//     satisfy each store interface.

func TestStoreInterfacesAreImplementable(t *testing.T) {
	t.Parallel()
	var _ ObjectStore = (*neverObjectStore)(nil)
	var _ LinkStore = (*neverLinkStore)(nil)
	var _ SchemaStore = (*neverSchemaStore)(nil)
	var _ SessionStore = (*neverSessionStore)(nil)
	var _ ActionLogStore = (*neverActionLogStore)(nil)
}

// never* types satisfy each store interface by panicking — they
// exist only as compile-time witnesses that the interface shapes
// resolve cleanly.

type neverObjectStore struct{}

func (neverObjectStore) Get(_ context.Context, _ TenantId, _ ObjectId, _ ReadConsistency) (*Object, error) {
	panic("never")
}
func (neverObjectStore) Put(_ context.Context, _ Object, _ *uint64) (PutOutcome, error) {
	panic("never")
}
func (neverObjectStore) Delete(_ context.Context, _ TenantId, _ ObjectId) (bool, error) {
	panic("never")
}
func (neverObjectStore) ListByType(_ context.Context, _ TenantId, _ TypeId, _ Page, _ ReadConsistency) (PagedResult[Object], error) {
	panic("never")
}
func (neverObjectStore) ListByOwner(_ context.Context, _ TenantId, _ OwnerId, _ Page, _ ReadConsistency) (PagedResult[Object], error) {
	panic("never")
}
func (neverObjectStore) ListByMarking(_ context.Context, _ TenantId, _ MarkingId, _ Page, _ ReadConsistency) (PagedResult[Object], error) {
	panic("never")
}

type neverLinkStore struct{}

func (neverLinkStore) Put(_ context.Context, _ Link) error { panic("never") }
func (neverLinkStore) Delete(_ context.Context, _ TenantId, _ LinkTypeId, _, _ ObjectId) (bool, error) {
	panic("never")
}
func (neverLinkStore) ListOutgoing(_ context.Context, _ TenantId, _ LinkTypeId, _ ObjectId, _ Page, _ ReadConsistency) (PagedResult[Link], error) {
	panic("never")
}
func (neverLinkStore) ListIncoming(_ context.Context, _ TenantId, _ LinkTypeId, _ ObjectId, _ Page, _ ReadConsistency) (PagedResult[Link], error) {
	panic("never")
}

type neverSchemaStore struct{}

func (neverSchemaStore) GetLatest(_ context.Context, _ TypeId, _ ReadConsistency) (*Schema, error) {
	panic("never")
}
func (neverSchemaStore) GetVersion(_ context.Context, _ TypeId, _ uint32, _ ReadConsistency) (*Schema, error) {
	panic("never")
}
func (neverSchemaStore) Put(_ context.Context, _ Schema) error { panic("never") }

type neverSessionStore struct{}

func (neverSessionStore) Get(_ context.Context, _ TenantId, _ string, _ ReadConsistency) (*Session, error) {
	panic("never")
}
func (neverSessionStore) Put(_ context.Context, _ Session) error                       { panic("never") }
func (neverSessionStore) Revoke(_ context.Context, _ TenantId, _ string) (bool, error) { panic("never") }

type neverActionLogStore struct{}

func (neverActionLogStore) Append(_ context.Context, _ ActionLogEntry) error { panic("never") }
func (neverActionLogStore) ListRecent(_ context.Context, _ TenantId, _ Page, _ ReadConsistency) (PagedResult[ActionLogEntry], error) {
	panic("never")
}
func (neverActionLogStore) ListForObject(_ context.Context, _ TenantId, _ ObjectId, _ Page, _ ReadConsistency) (PagedResult[ActionLogEntry], error) {
	panic("never")
}
func (neverActionLogStore) ListForAction(_ context.Context, _ TenantId, _ string, _ Page, _ ReadConsistency) (PagedResult[ActionLogEntry], error) {
	panic("never")
}
