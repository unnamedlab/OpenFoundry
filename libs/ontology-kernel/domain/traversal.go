// Multi-hop graph traversal backed by LinkStore.
//
// Foundry exposes object graphs with a `?graph(depth=N, link_types=[...])`
// style query. The legacy implementation walked the Postgres
// `link_instances` table with a recursive CTE; the migrated version
// performs the expansion through storage-abstraction so the same
// logic works against Cassandra-backed adjacency indexes.
//
// Mirrors `libs/ontology-kernel/src/domain/traversal.rs`.

package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// TraversedEdge mirrors `struct TraversedEdge`.
type TraversedEdge struct {
	LinkID         uuid.UUID `json:"link_id"`
	LinkTypeID     uuid.UUID `json:"link_type_id"`
	SourceObjectID uuid.UUID `json:"source_object_id"`
	TargetObjectID uuid.UUID `json:"target_object_id"`
	Marking        string    `json:"marking"`
	Depth          int32     `json:"depth"`
	CreatedAt      time.Time `json:"created_at"`
}

// TraversalParams mirrors `struct TraversalParams`.
type TraversalParams struct {
	StartingObjectID uuid.UUID
	MaxDepth         int32
	LinkTypeIDs      []uuid.UUID
	MarkingFilter    []string
	Limit            int32
}

// TraversalErrorKind tags the [TraversalError] variants. Mirrors the
// Rust `enum TraversalError { Repo(RepoError), Sql(sqlx::Error) }`.
type TraversalErrorKind int

const (
	// TraversalRepo — error from one of the storage-abstraction
	// stores (LinkStore / ObjectStore).
	TraversalRepo TraversalErrorKind = iota
	// TraversalSQL — error from the pgx call that resolves the
	// universal `link_types` set.
	TraversalSQL
)

// TraversalError mirrors `enum TraversalError`. The `Source` carries
// the underlying error so callers can `errors.Is` / `errors.As`.
type TraversalError struct {
	Kind   TraversalErrorKind
	Source error
}

// Error mirrors the Rust thiserror Display: surfaces the source
// verbatim (`#[error(transparent)]`).
func (e *TraversalError) Error() string {
	if e.Source != nil {
		return e.Source.Error()
	}
	return "traversal error"
}

// Unwrap exposes the underlying error.
func (e *TraversalError) Unwrap() error { return e.Source }

var _ error = (*TraversalError)(nil)

// resolveMarkingFilter mirrors `fn resolve_marking_filter`. Returns
// the explicit caller filter if set + non-empty; otherwise derives
// the allowlist from the caller's clearance using the same hierarchy
// as `domain::access::ensure_object_access` (public ⊂ confidential
// ⊂ pii).
func resolveMarkingFilter(claims *authmw.Claims, markingFilter []string) []string {
	if len(markingFilter) > 0 {
		out := make([]string, len(markingFilter))
		copy(out, markingFilter)
		return out
	}
	if claims != nil && claims.HasRole("admin") {
		return []string{"public", "confidential", "pii"}
	}
	granted := uint8(0)
	if claims != nil {
		granted = ClearanceRank(claims)
	}
	allowed := []string{"public"}
	if granted >= 1 {
		allowed = append(allowed, "confidential")
	}
	if granted >= 2 {
		allowed = append(allowed, "pii")
	}
	return allowed
}

// traversalTenantFromClaims mirrors `fn tenant_from_claims`. Falls
// back to `"default"` when the claim has no org_id.
func traversalTenantFromClaims(claims *authmw.Claims) storage.TenantId {
	if claims != nil && claims.OrgID != nil {
		return storage.TenantId(claims.OrgID.String())
	}
	return storage.TenantId("default")
}

// toTraversalUUID mirrors `fn to_uuid`. Returns
// RepoInvalidArgument with the verbatim Rust message string on
// parse failure.
func toTraversalUUID(kind, value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, storage.Invalidf("invalid %s uuid '%s': %s", kind, value, err)
	}
	return id, nil
}

// createdAtFromMs mirrors `fn created_at_from_ms` — Unix-epoch ms
// → UTC time.Time, with the Rust `unwrap_or(UNIX_EPOCH)` fallback
// for invalid (out-of-range) inputs. time.UnixMilli always
// succeeds for int64, so the fallback is unreachable in Go but
// kept as documentation.
func createdAtFromMs(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// edgeMarkingFromRank mirrors `fn edge_marking_from_rank`.
func edgeMarkingFromRank(rank uint8) string {
	switch rank {
	case 2:
		return "pii"
	case 1:
		return "confidential"
	default:
		return "public"
	}
}

// resolveObjectMarkings mirrors `async fn resolve_object_markings`.
// Caches the markings vector per object id for the duration of a
// single traversal so a node revisited via two link kinds doesn't
// re-hit the ObjectStore.
func resolveObjectMarkings(
	ctx context.Context,
	objects storage.ObjectStore,
	tenant storage.TenantId,
	objectID storage.ObjectId,
	cache map[storage.ObjectId][]string,
) ([]string, error) {
	if v, ok := cache[objectID]; ok {
		out := make([]string, len(v))
		copy(out, v)
		return out, nil
	}
	obj, err := objects.Get(ctx, tenant, objectID, storage.Eventual())
	if err != nil {
		return nil, err
	}
	var resolved []string
	if obj != nil {
		for _, m := range obj.Markings {
			resolved = append(resolved, string(m))
		}
	}
	if len(resolved) == 0 {
		resolved = []string{"public"}
	}
	cache[objectID] = append([]string(nil), resolved...)
	return resolved, nil
}

// deriveEdgeMarking mirrors `async fn derive_edge_marking`. The
// edge's marking is the strongest (highest-rank) marking on either
// endpoint.
func deriveEdgeMarking(
	ctx context.Context,
	objects storage.ObjectStore,
	tenant storage.TenantId,
	source, target storage.ObjectId,
	cache map[storage.ObjectId][]string,
) (string, error) {
	strongest := uint8(0)
	for _, id := range []storage.ObjectId{source, target} {
		markings, err := resolveObjectMarkings(ctx, objects, tenant, id, cache)
		if err != nil {
			return "", err
		}
		for _, m := range markings {
			if r, ok := MarkingRank(m); ok && r > strongest {
				strongest = r
			}
		}
	}
	return edgeMarkingFromRank(strongest), nil
}

// CollectLinks mirrors `pub(crate) async fn collect_links`. Pages
// through outgoing then incoming links across every requested
// link_type until the budget is reached. Exported because the
// graph layer (iter 7c₄) depends on the same primitive.
func CollectLinks(
	ctx context.Context,
	links storage.LinkStore,
	tenant storage.TenantId,
	node storage.ObjectId,
	linkTypes []storage.LinkTypeId,
	budget int,
) ([]storage.Link, error) {
	collected := []storage.Link{}
	clamp := func(n int) uint32 {
		if n < 1 {
			return 1
		}
		if n > 5_000 {
			return 5_000
		}
		return uint32(n)
	}

	for _, linkType := range linkTypes {
		// Outgoing pages.
		var token *string
		for {
			result, err := links.ListOutgoing(ctx, tenant, linkType, node, storage.Page{
				Size:  clamp(budget),
				Token: token,
			}, storage.Eventual())
			if err != nil {
				return nil, err
			}
			collected = append(collected, result.Items...)
			if len(collected) >= budget || result.NextToken == nil {
				break
			}
			token = result.NextToken
		}
		if len(collected) >= budget {
			break
		}

		// Incoming pages.
		token = nil
		for {
			remaining := budget - len(collected)
			result, err := links.ListIncoming(ctx, tenant, linkType, node, storage.Page{
				Size:  clamp(remaining),
				Token: token,
			}, storage.Eventual())
			if err != nil {
				return nil, err
			}
			collected = append(collected, result.Items...)
			if len(collected) >= budget || result.NextToken == nil {
				break
			}
			token = result.NextToken
		}
		if len(collected) >= budget {
			break
		}
	}
	return collected, nil
}

// resolveLinkTypes mirrors `async fn resolve_link_types`. Honours the
// caller's explicit list when present + non-empty; otherwise reads
// every `link_types` row from PG.
func resolveLinkTypes(ctx context.Context, db *pgxpool.Pool, requested []uuid.UUID) ([]storage.LinkTypeId, error) {
	if len(requested) > 0 {
		out := make([]storage.LinkTypeId, 0, len(requested))
		for _, id := range requested {
			out = append(out, storage.LinkTypeId(id.String()))
		}
		return out, nil
	}
	if db == nil {
		return []storage.LinkTypeId{}, nil
	}
	rows, err := db.Query(ctx, `SELECT id FROM link_types ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []storage.LinkTypeId{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, storage.LinkTypeId(id.String()))
	}
	return out, rows.Err()
}

// TraverseWithTypes mirrors `async fn traverse_with_types`. Exported
// so unit tests + the graph layer can drive the traversal without
// re-resolving link_types via PG.
//
// Algorithm: BFS from `params.StartingObjectID` clamped to
// max_depth ∈ [1, 5] and limit ∈ [1, 5_000]. Edges are deduplicated
// via a stable link id derived from (link_type, from, to). Each
// edge's marking is the strongest endpoint marking; edges whose
// marking is not in the resolved allowlist are dropped. Nodes are
// visited at most once per distinct shorter depth (so a node first
// seen at depth 3 doesn't get re-queued via a depth-4 path).
func TraverseWithTypes(
	ctx context.Context,
	objects storage.ObjectStore,
	links storage.LinkStore,
	claims *authmw.Claims,
	params TraversalParams,
	linkTypes []storage.LinkTypeId,
) ([]TraversedEdge, error) {
	maxDepth := clampInt32(params.MaxDepth, 1, 5)
	limitInt := int(clampInt32(params.Limit, 1, 5_000))
	if len(linkTypes) == 0 {
		return []TraversedEdge{}, nil
	}

	tenant := traversalTenantFromClaims(claims)
	allowed := stringSet(resolveMarkingFilter(claims, params.MarkingFilter))
	start := storage.ObjectId(params.StartingObjectID.String())

	edges := []TraversedEdge{}
	type queueEntry struct {
		node  storage.ObjectId
		depth int32
	}
	queue := []queueEntry{{start, 0}}
	seenNodes := map[storage.ObjectId]int32{start: 0}
	seenEdges := map[uuid.UUID]bool{}
	objectMarkingCache := map[storage.ObjectId][]string{}

	for len(queue) > 0 {
		head := queue[0]
		queue = queue[1:]
		if head.depth >= maxDepth || len(edges) >= limitInt {
			continue
		}

		adjacent, err := CollectLinks(ctx, links, tenant, head.node, linkTypes, limitInt-len(edges))
		if err != nil {
			return nil, &TraversalError{Kind: TraversalRepo, Source: err}
		}
		for _, link := range adjacent {
			linkID := StableLinkID(link.LinkType, link.From, link.To)
			edgeDepth := head.depth + 1
			marking, err := deriveEdgeMarking(ctx, objects, tenant, link.From, link.To, objectMarkingCache)
			if err != nil {
				return nil, &TraversalError{Kind: TraversalRepo, Source: err}
			}
			if !allowed[marking] {
				continue
			}
			var neighbour storage.ObjectId
			if link.From == head.node {
				neighbour = link.To
			} else {
				neighbour = link.From
			}

			if seenEdges[linkID] {
				// Already emitted; still consider expanding the
				// neighbour from the shorter-path view if not seen.
			} else {
				seenEdges[linkID] = true
				linkTypeUUID, err := toTraversalUUID("link_type_id", string(link.LinkType))
				if err != nil {
					return nil, &TraversalError{Kind: TraversalRepo, Source: err}
				}
				sourceUUID, err := toTraversalUUID("source_object_id", string(link.From))
				if err != nil {
					return nil, &TraversalError{Kind: TraversalRepo, Source: err}
				}
				targetUUID, err := toTraversalUUID("target_object_id", string(link.To))
				if err != nil {
					return nil, &TraversalError{Kind: TraversalRepo, Source: err}
				}
				edges = append(edges, TraversedEdge{
					LinkID:         linkID,
					LinkTypeID:     linkTypeUUID,
					SourceObjectID: sourceUUID,
					TargetObjectID: targetUUID,
					Marking:        marking,
					Depth:          edgeDepth,
					CreatedAt:      createdAtFromMs(link.CreatedAtMs),
				})
				if len(edges) >= limitInt {
					break
				}
			}

			if edgeDepth < maxDepth {
				known, seen := seenNodes[neighbour]
				if !seen || edgeDepth < known {
					seenNodes[neighbour] = edgeDepth
					queue = append(queue, queueEntry{neighbour, edgeDepth})
				}
			}
		}
	}
	return edges, nil
}

// Traverse mirrors `pub async fn traverse`. Resolves the link-types
// universe (caller-provided list OR every PG `link_types` row) and
// then delegates to [TraverseWithTypes].
func Traverse(
	ctx context.Context,
	db *pgxpool.Pool,
	objects storage.ObjectStore,
	links storage.LinkStore,
	claims *authmw.Claims,
	params TraversalParams,
) ([]TraversedEdge, error) {
	linkTypes, err := resolveLinkTypes(ctx, db, params.LinkTypeIDs)
	if err != nil {
		return nil, &TraversalError{Kind: TraversalSQL, Source: err}
	}
	return TraverseWithTypes(ctx, objects, links, claims, params, linkTypes)
}

func clampInt32(value, lo, hi int32) int32 {
	if value < lo {
		return lo
	}
	if value > hi {
		return hi
	}
	return value
}

// stringSet is provided by media_reference_validator.go in this
// package; we intentionally don't redeclare it.
