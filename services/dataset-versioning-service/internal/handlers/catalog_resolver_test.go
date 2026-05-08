package handlers_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/models"
)

// TestInMemoryLineageClient verifies the test fixture returns a copy so the
// caller cannot mutate the configured edges by accident.
func TestInMemoryLineageClient(t *testing.T) {
	t.Parallel()
	client := &handlers.InMemoryLineageClient{Edges: map[string][]string{
		"ri.b": {"ri.a"},
	}}
	parents, err := client.Upstream(context.Background(), "ri.b")
	require.NoError(t, err)
	require.Equal(t, []string{"ri.a"}, parents)
	parents[0] = "ri.tampered"
	parents2, err := client.Upstream(context.Background(), "ri.b")
	require.NoError(t, err)
	assert.Equal(t, []string{"ri.a"}, parents2, "fixture must not be mutated by caller")

	empty, err := client.Upstream(context.Background(), "ri.unknown")
	require.NoError(t, err)
	assert.Empty(t, empty)
}

// TestMarkingResolverDirectOnly exercises the path where a dataset has no
// upstream — only its own direct markings are returned.
func TestMarkingResolverDirectOnly(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	loader := func(_ context.Context, _ string) ([]models.EffectiveMarking, error) {
		return []models.EffectiveMarking{models.NewDirectMarking(id)}, nil
	}
	resolver := handlers.NewMarkingResolver(loader, &handlers.InMemoryLineageClient{})
	out, err := resolver.Compute(context.Background(), "ri.leaf")
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.True(t, out[0].IsDirect())
	assert.Equal(t, id, out[0].ID)
}

// TestMarkingResolverInheritsFromUpstream covers the core invariant: a
// marking attached to an upstream dataset surfaces on the child as
// "inherited_from_upstream" tagged with the immediate parent RID.
func TestMarkingResolverInheritsFromUpstream(t *testing.T) {
	t.Parallel()
	parentMarking := uuid.New()
	childMarking := uuid.New()
	loader := func(_ context.Context, rid string) ([]models.EffectiveMarking, error) {
		switch rid {
		case "ri.parent":
			return []models.EffectiveMarking{models.NewDirectMarking(parentMarking)}, nil
		case "ri.child":
			return []models.EffectiveMarking{models.NewDirectMarking(childMarking)}, nil
		}
		return nil, nil
	}
	lineage := &handlers.InMemoryLineageClient{Edges: map[string][]string{
		"ri.child": {"ri.parent"},
	}}
	resolver := handlers.NewMarkingResolver(loader, lineage)
	out, err := resolver.Compute(context.Background(), "ri.child")
	require.NoError(t, err)
	require.Len(t, out, 2)
	// Direct markings precede inherited ones (Rust ordering).
	assert.True(t, out[0].IsDirect())
	assert.Equal(t, childMarking, out[0].ID)
	assert.True(t, out[1].IsInherited())
	assert.Equal(t, parentMarking, out[1].ID)
	assert.Equal(t, "ri.parent", out[1].UpstreamRID())
}

// TestMarkingResolverDedupesAcrossDiamond confirms two upstream paths that
// converge on the same marking ID still produce a single inherited entry per
// (id, upstream_rid) pair.
func TestMarkingResolverDedupesAcrossDiamond(t *testing.T) {
	t.Parallel()
	shared := uuid.New()
	loader := func(_ context.Context, rid string) ([]models.EffectiveMarking, error) {
		switch rid {
		case "ri.grandparent":
			return []models.EffectiveMarking{models.NewDirectMarking(shared)}, nil
		default:
			return nil, nil
		}
	}
	lineage := &handlers.InMemoryLineageClient{Edges: map[string][]string{
		"ri.child":   {"ri.parent_a", "ri.parent_b"},
		"ri.parent_a": {"ri.grandparent"},
		"ri.parent_b": {"ri.grandparent"},
	}}
	resolver := handlers.NewMarkingResolver(loader, lineage)
	out, err := resolver.Compute(context.Background(), "ri.child")
	require.NoError(t, err)
	// Two inherited entries: one tagged with ri.parent_a, one with ri.parent_b.
	require.Len(t, out, 2)
	upstreams := map[string]bool{out[0].UpstreamRID(): true, out[1].UpstreamRID(): true}
	assert.True(t, upstreams["ri.parent_a"])
	assert.True(t, upstreams["ri.parent_b"])
}

// TestMarkingResolverDetectsCycles ensures a parent-of-self loop returns the
// MarkingResolveError cycle variant rather than recursing forever.
func TestMarkingResolverDetectsCycles(t *testing.T) {
	t.Parallel()
	loader := func(_ context.Context, _ string) ([]models.EffectiveMarking, error) {
		return nil, nil
	}
	lineage := &handlers.InMemoryLineageClient{Edges: map[string][]string{
		"ri.a": {"ri.b"},
		"ri.b": {"ri.a"},
	}}
	resolver := handlers.NewMarkingResolver(loader, lineage)
	_, err := resolver.Compute(context.Background(), "ri.a")
	require.Error(t, err)
	var resolveErr *models.MarkingResolveError
	require.True(t, errors.As(err, &resolveErr))
	assert.True(t, resolveErr.IsCycle())
	assert.NotEmpty(t, resolveErr.RID)
}

// TestMarkingResolverWrapsLineageErrors covers the error-tagging path: a
// failure from the lineage client must surface as MarkingResolveErrorKindLineage
// with the RID we were resolving when the call failed.
func TestMarkingResolverWrapsLineageErrors(t *testing.T) {
	t.Parallel()
	loader := func(_ context.Context, _ string) ([]models.EffectiveMarking, error) {
		return nil, nil
	}
	resolver := handlers.NewMarkingResolver(loader, brokenLineageClient{err: errors.New("boom")})
	_, err := resolver.Compute(context.Background(), "ri.x")
	require.Error(t, err)
	var resolveErr *models.MarkingResolveError
	require.True(t, errors.As(err, &resolveErr))
	assert.Equal(t, models.MarkingResolveErrorKindLineage, resolveErr.Kind)
	assert.Equal(t, "ri.x", resolveErr.RID)
}

// TestMarkingResolverCachesResults verifies a second call hits the cache —
// the loader and lineage client must each be invoked exactly once.
func TestMarkingResolverCachesResults(t *testing.T) {
	t.Parallel()
	var loaderCalls, lineageCalls int32
	loader := func(_ context.Context, _ string) ([]models.EffectiveMarking, error) {
		atomic.AddInt32(&loaderCalls, 1)
		return nil, nil
	}
	lineage := countingLineageClient{counter: &lineageCalls}
	resolver := handlers.NewMarkingResolverWithTTL(loader, lineage, 250*time.Millisecond)
	for i := 0; i < 5; i++ {
		_, err := resolver.Compute(context.Background(), "ri.cached")
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&loaderCalls))
	assert.Equal(t, int32(1), atomic.LoadInt32(&lineageCalls))

	resolver.Invalidate("ri.cached")
	_, err := resolver.Compute(context.Background(), "ri.cached")
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&loaderCalls), "invalidate forces a recompute")
	assert.Equal(t, int32(2), atomic.LoadInt32(&lineageCalls))
}

// TestDedupeMarkingsKeepsDistinctSources matches the Rust unit test in
// data_asset_catalog::domain::markings::tests::dedupe_collapses_identical_pairs.
func TestDedupeMarkingsKeepsDistinctSources(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	in := []models.EffectiveMarking{
		models.NewDirectMarking(id),
		models.NewDirectMarking(id),
		models.NewInheritedMarking(id, "ri.up"),
		models.NewInheritedMarking(id, "ri.up"),
		models.NewInheritedMarking(id, "ri.other"),
	}
	out := models.DedupeMarkings(in)
	assert.Len(t, out, 3)
}

// TestNormalisePageInputs covers the bound-clamping behaviour of the
// catalog list pagination port.
func TestNormalisePageInputs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		page        *int64
		perPage     *int64
		wantPage    int64
		wantPerPage int64
	}{
		{"defaults", nil, nil, 1, 20},
		{"floor page", ptrI64(0), ptrI64(20), 1, 20},
		{"clamp per_page low", nil, ptrI64(0), 1, 1},
		{"clamp per_page high", nil, ptrI64(500), 1, 100},
		{"explicit", ptrI64(7), ptrI64(50), 7, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, perPage := models.NormalisePageInputs(tc.page, tc.perPage)
			assert.Equal(t, tc.wantPage, page)
			assert.Equal(t, tc.wantPerPage, perPage)
		})
	}
}

// TestTotalPagesCeilsCorrectly mirrors the Rust ceil(total / per_page).
func TestTotalPagesCeilsCorrectly(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(0), models.TotalPages(0, 20))
	assert.Equal(t, int64(1), models.TotalPages(1, 20))
	assert.Equal(t, int64(1), models.TotalPages(20, 20))
	assert.Equal(t, int64(2), models.TotalPages(21, 20))
	assert.Equal(t, int64(0), models.TotalPages(5, 0))
}

// ---------------------------------------------------------------------------
// Tiny helpers / fakes
// ---------------------------------------------------------------------------

func ptrI64(v int64) *int64 { return &v }

type brokenLineageClient struct{ err error }

func (c brokenLineageClient) Upstream(_ context.Context, _ string) ([]string, error) {
	return nil, c.err
}

type countingLineageClient struct{ counter *int32 }

func (c countingLineageClient) Upstream(_ context.Context, _ string) ([]string, error) {
	atomic.AddInt32(c.counter, 1)
	return nil, nil
}
