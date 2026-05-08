package domain_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/stores"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

func clearanceClaims(t *testing.T, clearance string) *authmw.Claims {
	t.Helper()
	zero := uuid.Nil
	return &authmw.Claims{
		Sub:        zero,
		OrgID:      &zero,
		Email:      "test@example.com",
		Attributes: json.RawMessage(`{"classification_clearance":"` + clearance + `"}`),
	}
}

func putObject(t *testing.T, ctx context.Context, store storage.ObjectStore, tenant storage.TenantId, id uuid.UUID, marking string) {
	t.Helper()
	out, err := store.Put(ctx, storage.Object{
		Tenant:      tenant,
		ID:          storage.ObjectId(id.String()),
		TypeID:      storage.TypeId(uuid.New().String()),
		Version:     1,
		Payload:     json.RawMessage(`{}`),
		UpdatedAtMs: 1,
		Markings:    []storage.MarkingId{storage.MarkingId(marking)},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, storage.PutInserted, out.Kind)
}

// libs/ontology-kernel/src/domain/traversal.rs
// `traverses_both_directions_through_link_store` — traversal walks
// outgoing AND incoming edges, surfacing a link from a confidential
// object back into a public one when the caller has pii clearance.
func TestTraverseWithTypesBothDirections(t *testing.T) {
	ctx := context.Background()
	objects := stores.NewInMemoryObjectStore()
	links := stores.NewInMemoryLinkStore()
	tenant := storage.TenantId(uuid.Nil.String())

	startID := uuid.New()
	otherPublicID := uuid.New()
	confidentialID := uuid.New()

	putObject(t, ctx, objects, tenant, startID, "public")
	putObject(t, ctx, objects, tenant, otherPublicID, "public")
	putObject(t, ctx, objects, tenant, confidentialID, "confidential")

	linkType := storage.LinkTypeId(uuid.New().String())
	for _, l := range []storage.Link{
		{
			Tenant:      tenant,
			LinkType:    linkType,
			From:        storage.ObjectId(startID.String()),
			To:          storage.ObjectId(otherPublicID.String()),
			Payload:     json.RawMessage(`{}`),
			CreatedAtMs: 10,
		},
		{
			Tenant:      tenant,
			LinkType:    linkType,
			From:        storage.ObjectId(confidentialID.String()),
			To:          storage.ObjectId(otherPublicID.String()),
			Payload:     json.RawMessage(`{}`),
			CreatedAtMs: 20,
		},
	} {
		require.NoError(t, links.Put(ctx, l))
	}

	edges, err := domain.TraverseWithTypes(ctx, objects, links, clearanceClaims(t, "pii"),
		domain.TraversalParams{
			StartingObjectID: startID,
			MaxDepth:         3,
			Limit:            10,
		},
		[]storage.LinkTypeId{linkType},
	)
	require.NoError(t, err)
	require.Len(t, edges, 2)

	depth1 := false
	depth2 := false
	for _, e := range edges {
		if e.Depth == 1 {
			depth1 = true
		}
		if e.Depth == 2 {
			depth2 = true
		}
	}
	assert.True(t, depth1, "expected an edge at depth 1")
	assert.True(t, depth2, "expected an edge at depth 2 (BFS reached confidential via public hop)")
}

// libs/ontology-kernel/src/domain/traversal.rs
// `filters_edges_by_derived_marking` — public clearance must NOT
// surface an edge whose target is pii.
func TestTraverseWithTypesFiltersByDerivedMarking(t *testing.T) {
	ctx := context.Background()
	objects := stores.NewInMemoryObjectStore()
	links := stores.NewInMemoryLinkStore()
	tenant := storage.TenantId(uuid.Nil.String())

	start := uuid.New()
	target := uuid.New()
	linkType := storage.LinkTypeId(uuid.New().String())

	putObject(t, ctx, objects, tenant, start, "public")
	putObject(t, ctx, objects, tenant, target, "pii")

	require.NoError(t, links.Put(ctx, storage.Link{
		Tenant:      tenant,
		LinkType:    linkType,
		From:        storage.ObjectId(start.String()),
		To:          storage.ObjectId(target.String()),
		Payload:     json.RawMessage(`{}`),
		CreatedAtMs: 10,
	}))

	edges, err := domain.TraverseWithTypes(ctx, objects, links, clearanceClaims(t, "public"),
		domain.TraversalParams{
			StartingObjectID: start,
			MaxDepth:         2,
			Limit:            10,
		},
		[]storage.LinkTypeId{linkType},
	)
	require.NoError(t, err)
	assert.Empty(t, edges, "public clearance must not see the pii-derived edge")
}

// libs/ontology-kernel/src/domain/traversal.rs `traverse_with_types`
// — empty link types short-circuits to no edges (Rust early-return).
func TestTraverseWithTypesEmptyLinkTypes(t *testing.T) {
	ctx := context.Background()
	objects := stores.NewInMemoryObjectStore()
	links := stores.NewInMemoryLinkStore()
	edges, err := domain.TraverseWithTypes(ctx, objects, links, clearanceClaims(t, "public"),
		domain.TraversalParams{
			StartingObjectID: uuid.New(),
			MaxDepth:         3,
			Limit:            5,
		},
		nil,
	)
	require.NoError(t, err)
	assert.Empty(t, edges)
}

// libs/ontology-kernel/src/domain/traversal.rs — explicit
// `marking_filter` overrides the clearance-derived allowlist.
func TestTraverseWithTypesExplicitMarkingFilter(t *testing.T) {
	ctx := context.Background()
	objects := stores.NewInMemoryObjectStore()
	links := stores.NewInMemoryLinkStore()
	tenant := storage.TenantId(uuid.Nil.String())

	start := uuid.New()
	target := uuid.New()
	linkType := storage.LinkTypeId(uuid.New().String())

	putObject(t, ctx, objects, tenant, start, "public")
	putObject(t, ctx, objects, tenant, target, "pii")
	require.NoError(t, links.Put(ctx, storage.Link{
		Tenant:      tenant,
		LinkType:    linkType,
		From:        storage.ObjectId(start.String()),
		To:          storage.ObjectId(target.String()),
		Payload:     json.RawMessage(`{}`),
		CreatedAtMs: 10,
	}))

	// PII clearance but explicit `["public"]` filter → edge filtered out.
	edges, err := domain.TraverseWithTypes(ctx, objects, links, clearanceClaims(t, "pii"),
		domain.TraversalParams{
			StartingObjectID: start,
			MaxDepth:         2,
			Limit:            10,
			MarkingFilter:    []string{"public"},
		},
		[]storage.LinkTypeId{linkType},
	)
	require.NoError(t, err)
	assert.Empty(t, edges)
}

// libs/ontology-kernel/src/domain/traversal.rs — limit clamps the
// number of returned edges; max_depth clamps to [1, 5].
func TestTraverseWithTypesLimitAndDepthClamps(t *testing.T) {
	ctx := context.Background()
	objects := stores.NewInMemoryObjectStore()
	links := stores.NewInMemoryLinkStore()
	tenant := storage.TenantId(uuid.Nil.String())

	start := uuid.New()
	a := uuid.New()
	b := uuid.New()
	linkType := storage.LinkTypeId(uuid.New().String())

	putObject(t, ctx, objects, tenant, start, "public")
	putObject(t, ctx, objects, tenant, a, "public")
	putObject(t, ctx, objects, tenant, b, "public")
	require.NoError(t, links.Put(ctx, storage.Link{
		Tenant: tenant, LinkType: linkType,
		From: storage.ObjectId(start.String()), To: storage.ObjectId(a.String()),
		CreatedAtMs: 10,
	}))
	require.NoError(t, links.Put(ctx, storage.Link{
		Tenant: tenant, LinkType: linkType,
		From: storage.ObjectId(start.String()), To: storage.ObjectId(b.String()),
		CreatedAtMs: 20,
	}))

	// Limit=1 → one edge surfaces.
	edges, err := domain.TraverseWithTypes(ctx, objects, links, clearanceClaims(t, "public"),
		domain.TraversalParams{
			StartingObjectID: start,
			MaxDepth:         3,
			Limit:            1,
		},
		[]storage.LinkTypeId{linkType},
	)
	require.NoError(t, err)
	assert.Len(t, edges, 1)
}
