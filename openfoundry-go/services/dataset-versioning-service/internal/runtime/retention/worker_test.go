package retention_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	domainretention "github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/domain/retention"
	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/models"
	runtimeretention "github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/runtime/retention"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

type archiveCall struct {
	row        models.RetentionRow
	graceUntil time.Time
	payload    models.JSONValue
}

type fakeStore struct {
	mu         sync.Mutex
	rows       []models.RetentionRow
	loadErr    error
	archived   []archiveCall
	archiveErr error
	archiveOK  map[uuid.UUID]bool
	idempotent map[uuid.UUID]bool
}

func (s *fakeStore) LoadRetentionRows(_ context.Context) ([]models.RetentionRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	out := make([]models.RetentionRow, len(s.rows))
	copy(out, s.rows)
	return out, nil
}

func (s *fakeStore) ArchiveBranch(_ context.Context, row models.RetentionRow, graceUntil time.Time, payload models.JSONValue) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.archiveErr != nil {
		return false, s.archiveErr
	}
	s.archived = append(s.archived, archiveCall{row: row, graceUntil: graceUntil, payload: payload})
	if v, ok := s.idempotent[row.ID]; ok && !v {
		return false, nil
	}
	if v, ok := s.archiveOK[row.ID]; ok {
		return v, nil
	}
	return true, nil
}

func (s *fakeStore) archivedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.archived)
}

type countingGauge struct {
	mu   sync.Mutex
	last int
}

func (g *countingGauge) Set(v int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.last = v
}

func (g *countingGauge) Last() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.last
}

type countingCounter struct {
	mu       sync.Mutex
	byReason map[string]int
}

func (c *countingCounter) Inc(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byReason == nil {
		c.byReason = map[string]int{}
	}
	c.byReason[reason]++
}

func (c *countingCounter) Get(reason string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byReason[reason]
}

func ts(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func uid(n uint64) uuid.UUID {
	var b [16]byte
	for i := 7; i >= 0; i-- {
		b[15-i] = byte(n >> (i * 8))
	}
	return uuid.UUID(b)
}

func ttlRow(id uint64, parent uint64, ttl int32, lastActivity time.Time) models.RetentionRow {
	pid := uid(parent)
	t := ttl
	return models.RetentionRow{
		ID:             uid(id),
		ParentBranchID: &pid,
		Policy:         models.RetentionPolicyTTLDays,
		TTLDays:        &t,
		LastActivityAt: lastActivity,
		IsRoot:         false,
	}
}

func TestRunOnceArchivesOnlyEligibleRows(t *testing.T) {
	now := ts(2026, time.June, 1)
	stale := ttlRow(10, 99, 30, ts(2026, time.January, 1))
	fresh := ttlRow(11, 99, 30, ts(2026, time.May, 31))
	master := models.RetentionRow{ID: uid(99), Policy: models.RetentionPolicyForever, IsRoot: true}

	store := &fakeStore{rows: []models.RetentionRow{stale, fresh, master}}
	gauge := &countingGauge{}
	counter := &countingCounter{}
	w := runtimeretention.New(store)
	w.Clock = &fakeClock{now: now}
	w.EligibleGauge = gauge
	w.ArchivedTotal = counter

	archived, err := w.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, archived)
	require.Len(t, store.archived, 1)
	require.Equal(t, stale.ID, store.archived[0].row.ID)
	require.Equal(t, now.Add(7*24*time.Hour), store.archived[0].graceUntil)
	require.Equal(t, 1, counter.Get("ttl"))
	// Mirrors Rust eligible_gauge — set from the pre-tick snapshot, so
	// the row we just archived is still counted as backlog for this tick.
	require.Equal(t, 1, gauge.Last())

	var payload map[string]any
	require.NoError(t, json.Unmarshal(store.archived[0].payload, &payload))
	require.Equal(t, "ttl", payload["reason"])
	require.Equal(t, "TTL_DAYS", payload["policy"])
	require.EqualValues(t, 30, payload["ttl_days"])
}

func TestRunOnceSkipsBranchesWithOpenTransactions(t *testing.T) {
	now := ts(2026, time.June, 1)
	row := ttlRow(10, 99, 30, ts(2026, time.January, 1))
	row.HasOpenTransaction = true
	store := &fakeStore{rows: []models.RetentionRow{row}}
	w := runtimeretention.New(store)
	w.Clock = &fakeClock{now: now}

	archived, err := w.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, archived)
	require.Empty(t, store.archived)
}

func TestRunOnceSkipsAlreadyArchivedRows(t *testing.T) {
	now := ts(2026, time.June, 1)
	row := ttlRow(10, 99, 30, ts(2026, time.January, 1))
	archivedAt := ts(2026, time.May, 1)
	row.ArchivedAt = &archivedAt
	store := &fakeStore{rows: []models.RetentionRow{row}}
	w := runtimeretention.New(store)
	w.Clock = &fakeClock{now: now}

	archived, err := w.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, archived)
	require.Empty(t, store.archived)
}

func TestRunOnceIdempotencyDoesNotIncrementCounter(t *testing.T) {
	now := ts(2026, time.June, 1)
	row := ttlRow(10, 99, 30, ts(2026, time.January, 1))
	store := &fakeStore{
		rows:       []models.RetentionRow{row},
		idempotent: map[uuid.UUID]bool{row.ID: false},
	}
	counter := &countingCounter{}
	w := runtimeretention.New(store)
	w.Clock = &fakeClock{now: now}
	w.ArchivedTotal = counter

	archived, err := w.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, archived)
	require.Equal(t, 0, counter.Get("ttl"))
}

func TestRunOncePropagatesLoadError(t *testing.T) {
	store := &fakeStore{loadErr: errors.New("boom")}
	w := runtimeretention.New(store)
	w.Clock = &fakeClock{now: ts(2026, time.June, 1)}

	_, err := w.RunOnce(context.Background())
	require.ErrorContains(t, err, "boom")
}

func TestRunOnceStopsOnArchiveError(t *testing.T) {
	now := ts(2026, time.June, 1)
	a := ttlRow(10, 99, 30, ts(2026, time.January, 1))
	b := ttlRow(11, 99, 30, ts(2026, time.January, 1))
	store := &fakeStore{
		rows:       []models.RetentionRow{a, b},
		archiveErr: errors.New("explode"),
	}
	w := runtimeretention.New(store)
	w.Clock = &fakeClock{now: now}

	_, err := w.RunOnce(context.Background())
	require.ErrorContains(t, err, "explode")
}

func TestRunLoopFiresOnEveryTickAndStopsOnContextCancel(t *testing.T) {
	now := ts(2026, time.June, 1)
	row := ttlRow(10, 99, 30, ts(2026, time.January, 1))
	store := &fakeStore{rows: []models.RetentionRow{row}}
	w := runtimeretention.New(store)
	w.Clock = &fakeClock{now: now}
	w.TickInterval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.RunLoop(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool {
		return store.archivedCount() >= 1
	}, time.Second, time.Millisecond, "expected at least one archive call")

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunLoop did not return after cancel")
	}
}

// Verifies the worker uses the resolver, including INHERITED chains.
func TestRunOnceArchivesInheritedTTLChild(t *testing.T) {
	now := ts(2026, time.June, 1)
	one := uint64(1)
	master := models.RetentionRow{ID: uid(1), Policy: models.RetentionPolicyForever, IsRoot: true}
	ttl := int32(30)
	parentID := uid(2)
	parent := models.RetentionRow{ID: parentID, ParentBranchID: refUID(one), Policy: models.RetentionPolicyTTLDays, TTLDays: &ttl, LastActivityAt: now, IsRoot: false}
	leafID := uint64(3)
	leaf := models.RetentionRow{ID: uid(leafID), ParentBranchID: &parentID, Policy: models.RetentionPolicyInherited, LastActivityAt: ts(2026, time.January, 1), IsRoot: false}

	store := &fakeStore{rows: []models.RetentionRow{master, parent, leaf}}
	gauge := &countingGauge{}
	w := runtimeretention.New(store)
	w.Clock = &fakeClock{now: now}
	w.EligibleGauge = gauge

	archived, err := w.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, archived)
	require.Equal(t, leaf.ID, store.archived[0].row.ID)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(store.archived[0].payload, &payload))
	require.Equal(t, "TTL_DAYS", payload["policy"])
	require.EqualValues(t, 30, payload["ttl_days"])
}

func refUID(n uint64) *uuid.UUID {
	id := uid(n)
	return &id
}

// Confirms the fake-clock plumbing actually drives eligibility.
func TestFakeClockAdvanceFlipsEligibility(t *testing.T) {
	row := ttlRow(10, 99, 30, ts(2026, time.January, 1))
	store := &fakeStore{rows: []models.RetentionRow{row}}
	clk := &fakeClock{now: ts(2026, time.January, 5)}
	w := runtimeretention.New(store)
	w.Clock = clk

	// Before TTL has lapsed.
	archived, err := w.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, archived)

	// Advance past TTL.
	clk.Set(ts(2026, time.July, 1))
	archived, err = w.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, archived)
}

// Sanity: the resolver/eligibility helpers are reachable through the
// re-exported domain package and behave consistently.
func TestDomainPackageRoundTripsThroughWorker(t *testing.T) {
	now := ts(2026, time.June, 1)
	row := ttlRow(10, 99, 30, ts(2026, time.January, 1))
	idx := domainretention.IndexRows([]models.RetentionRow{row})
	eff := domainretention.ResolveEffective(row, idx)
	require.True(t, domainretention.IsArchiveEligible(row, eff, now))
}
