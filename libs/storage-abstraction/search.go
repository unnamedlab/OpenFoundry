package storageabstraction

// search.go ports the SearchBackend trait surface from
// libs/storage-abstraction/src/repositories.rs (lines ~820-940 +
// the noop::InMemorySearchBackend at ~1941). Keeps the search
// types alongside the storage repositories so consumers can depend
// on a single module — matches the Rust workspace topology where
// `search-abstraction` re-exports these from `storage-abstraction`.

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"sync"
)

// ─── Wire payloads ──────────────────────────────────────────────────────

// SearchQuery is a free-form lexical query as received from the
// API. Mirrors struct SearchQuery.
type SearchQuery struct {
	Tenant  TenantId          `json:"tenant"`
	TypeID  *TypeId           `json:"type_id,omitempty"`
	Q       *string           `json:"q,omitempty"`
	Filters map[string]string `json:"filters,omitempty"`
	Page    Page              `json:"page"`
}

// SearchHit is one hit returned by SearchBackend.Search.
type SearchHit struct {
	ID      ObjectId        `json:"id"`
	TypeID  TypeId          `json:"type_id"`
	Score   float32         `json:"score"`
	Snippet json.RawMessage `json:"snippet,omitempty"`
}

// TextQuery is an OSV2.11 per-type, per-property full-text query.
// Phrase queries preserve token order, Prefix queries match token prefixes,
// and Language selects the analyzer/stemmer configured by the backend.
type TextQuery struct {
	Tenant   TenantId `json:"tenant"`
	TypeID   TypeId   `json:"type_id"`
	Property string   `json:"property"`
	Text     string   `json:"text"`
	Phrase   bool     `json:"phrase,omitempty"`
	Prefix   bool     `json:"prefix,omitempty"`
	Language string   `json:"language,omitempty"`
	Page     Page     `json:"page"`
}

// VectorDistance selects the OSV2.12 embedding distance function.
type VectorDistance string

const (
	VectorDistanceCosine VectorDistance = "cosine"
	VectorDistanceL2     VectorDistance = "l2"
	VectorDistanceDot    VectorDistance = "dot"
)

// HybridQuery combines a BM25-style full-text clause with an ANN vector clause.
type HybridQuery struct {
	Tenant    TenantId          `json:"tenant"`
	TypeID    TypeId            `json:"type_id"`
	Property  string            `json:"property,omitempty"`
	Text      string            `json:"text,omitempty"`
	Embedding []float32         `json:"embedding,omitempty"`
	K         uint32            `json:"k"`
	Distance  VectorDistance    `json:"distance,omitempty"`
	Filters   map[string]string `json:"filters,omitempty"`
}

// FullTextSearchBackend is the optional OSV2.11 surface implemented by
// pluggable search backends that expose per-property inverted indexes.
type FullTextSearchBackend interface {
	SearchText(ctx context.Context, query TextQuery, consistency ReadConsistency) (PagedResult[SearchHit], error)
}

// HybridSearchBackend is the optional OSV2.12 surface for BM25 + ANN queries.
type HybridSearchBackend interface {
	SearchHybrid(ctx context.Context, query HybridQuery, consistency ReadConsistency) ([]SearchHit, error)
}

// IndexDoc is the indexable payload pushed by the funnel. The
// backend MUST discard a write whose Version is older than the
// currently-indexed one for the same (Tenant, ID).
type IndexDoc struct {
	Tenant    TenantId        `json:"tenant"`
	ID        ObjectId        `json:"id"`
	TypeID    TypeId          `json:"type_id"`
	Payload   json.RawMessage `json:"payload"`
	Version   uint64          `json:"version"`
	Embedding []float32       `json:"embedding,omitempty"`
}

// VectorQuery is a kNN / ANN query. `K` is the number of
// neighbours to return; `Filters` are applied BEFORE the kNN
// search.
type VectorQuery struct {
	Tenant    TenantId          `json:"tenant"`
	TypeID    *TypeId           `json:"type_id,omitempty"`
	Embedding []float32         `json:"embedding"`
	K         uint32            `json:"k"`
	Filters   map[string]string `json:"filters,omitempty"`
}

// BulkOutcome is the outcome of SearchBackend.BulkIndex.
type BulkOutcome struct {
	Indexed uint32     `json:"indexed"`
	Failed  []BulkFail `json:"failed,omitempty"`
}

// BulkFail is one per-document failure surfaced by BulkIndex.
type BulkFail struct {
	ID     ObjectId `json:"id"`
	Reason string   `json:"reason"`
}

// ─── SearchBackend interface ───────────────────────────────────────────

// SearchBackend is implemented by Vespa (production) and OpenSearch
// (dev/CI), per ADR-0028. Mirrors the Rust trait verbatim.
//
// Default implementations from the Rust trait (the "vector search
// not supported" stub for SearchVector and the loop-Index fallback
// for BulkIndex) live as helper functions DefaultSearchVector and
// DefaultBulkIndex below — Go interfaces have no default methods,
// so backends without specialised implementations must embed
// `UnimplementedSearchVectorBackend` to inherit the stub.
type SearchBackend interface {
	// Search runs a lexical query. The consistency hint controls
	// whether the backend should wait for in-flight indexing to
	// flush.
	Search(ctx context.Context, query SearchQuery, consistency ReadConsistency) (PagedResult[SearchHit], error)
	// Index (or re-index) one document. Implementations MUST
	// discard a write whose Version is older than the currently-
	// indexed one for the same (Tenant, ID).
	Index(ctx context.Context, doc IndexDoc) error
	// Delete removes a document from the index. Returns (false,
	// nil) when the document was absent.
	Delete(ctx context.Context, tenant TenantId, id ObjectId) (bool, error)
	// SearchVector runs kNN / ANN over the document Embedding
	// field. Backends without vector support should return
	// RepoBackend("vector search not supported by this backend").
	SearchVector(ctx context.Context, query VectorQuery, consistency ReadConsistency) ([]SearchHit, error)
	// BulkIndex indexes a batch of documents, collecting per-
	// document failures. Backends with native bulk APIs should
	// override DefaultBulkIndex.
	BulkIndex(ctx context.Context, docs []IndexDoc) (BulkOutcome, error)
}

// DefaultBulkIndex is the loop-over-Index fallback used by
// backends that lack a native bulk API. Mirrors the Rust default
// impl of trait method bulk_index.
func DefaultBulkIndex(ctx context.Context, b SearchBackend, docs []IndexDoc) (BulkOutcome, error) {
	out := BulkOutcome{Failed: []BulkFail{}}
	for _, d := range docs {
		id := d.ID
		if err := b.Index(ctx, d); err != nil {
			out.Failed = append(out.Failed, BulkFail{ID: id, Reason: err.Error()})
			continue
		}
		out.Indexed++
	}
	return out, nil
}

// ErrVectorSearchUnsupported is the canonical error returned by
// backends without ANN / kNN support. Mirrors the Rust default
// impl of trait method search_vector.
func ErrVectorSearchUnsupported() error {
	return Backend("vector search not supported by this backend")
}

// ─── In-memory backend ─────────────────────────────────────────────────

// InMemorySearchBackend is a thread-safe map-backed SearchBackend
// useful for tests + local-first dev when neither Vespa nor
// OpenSearch is reachable. Mirrors noop::InMemorySearchBackend.
type InMemorySearchBackend struct {
	mu   sync.Mutex
	rows map[inMemKey]IndexDoc
}

type inMemKey struct {
	Tenant TenantId
	ID     ObjectId
}

// NewInMemorySearchBackend returns a freshly-initialised backend.
func NewInMemorySearchBackend() *InMemorySearchBackend {
	return &InMemorySearchBackend{rows: map[inMemKey]IndexDoc{}}
}

// Compile-time interface satisfaction check.
var _ SearchBackend = (*InMemorySearchBackend)(nil)

// Search runs a tenant-scoped lexical query with type + filter
// predicates. Mirrors the Rust impl: empty `q` matches every
// row, otherwise the JSON payload-string is searched for the
// lower-cased query as a substring.
func (b *InMemorySearchBackend) Search(_ context.Context, query SearchQuery, _ ReadConsistency) (PagedResult[SearchHit], error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	q := ""
	if query.Q != nil {
		q = strings.ToLower(*query.Q)
	}
	items := make([]SearchHit, 0)
	for _, d := range b.rows {
		if d.Tenant != query.Tenant {
			continue
		}
		if query.TypeID != nil && d.TypeID != *query.TypeID {
			continue
		}
		if !matchesFilters(d.Payload, query.Filters) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(string(d.Payload)), q) {
			continue
		}
		items = append(items, SearchHit{
			ID:      d.ID,
			TypeID:  d.TypeID,
			Score:   1.0,
			Snippet: append(json.RawMessage{}, d.Payload...),
		})
	}
	limit := uint32(1)
	if query.Page.Size > 1 {
		limit = query.Page.Size
	}
	if uint32(len(items)) > limit {
		items = items[:limit]
	}
	return PagedResult[SearchHit]{Items: items}, nil
}

// Index discards stale writes (existing.Version >= doc.Version)
// per the SearchBackend contract.
func (b *InMemorySearchBackend) Index(_ context.Context, doc IndexDoc) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := inMemKey{Tenant: doc.Tenant, ID: doc.ID}
	if existing, ok := b.rows[key]; ok && existing.Version >= doc.Version {
		return nil
	}
	b.rows[key] = doc
	return nil
}

// Delete removes the row keyed by (tenant, id), reporting whether
// it had been present.
func (b *InMemorySearchBackend) Delete(_ context.Context, tenant TenantId, id ObjectId) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := inMemKey{Tenant: tenant, ID: id}
	if _, ok := b.rows[key]; ok {
		delete(b.rows, key)
		return true, nil
	}
	return false, nil
}

// SearchVector runs cosine-similarity kNN with the optional
// pre-filters (tenant, type, equality). Returns the top-K matches
// sorted by descending score.
func (b *InMemorySearchBackend) SearchVector(_ context.Context, query VectorQuery, _ ReadConsistency) ([]SearchHit, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	type scored struct {
		score float32
		doc   IndexDoc
	}
	bucket := []scored{}
	for _, d := range b.rows {
		if d.Tenant != query.Tenant {
			continue
		}
		if query.TypeID != nil && d.TypeID != *query.TypeID {
			continue
		}
		if !matchesFilters(d.Payload, query.Filters) {
			continue
		}
		if len(d.Embedding) == 0 {
			continue
		}
		bucket = append(bucket, scored{
			score: cosineSim(d.Embedding, query.Embedding),
			doc:   d,
		})
	}
	sort.SliceStable(bucket, func(i, j int) bool { return bucket[i].score > bucket[j].score })
	k := query.K
	if k == 0 {
		k = 1
	}
	if uint32(len(bucket)) > k {
		bucket = bucket[:k]
	}
	out := make([]SearchHit, 0, len(bucket))
	for _, s := range bucket {
		out = append(out, SearchHit{
			ID:      s.doc.ID,
			TypeID:  s.doc.TypeID,
			Score:   s.score,
			Snippet: append(json.RawMessage{}, s.doc.Payload...),
		})
	}
	return out, nil
}

// SearchText runs a per-property token search over in-memory JSON payloads.
// It is intentionally simple but preserves OSV2 semantics for exact phrase,
// token prefix, and default token-any matching used by handler/unit tests.
func (b *InMemorySearchBackend) SearchText(_ context.Context, query TextQuery, _ ReadConsistency) (PagedResult[SearchHit], error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	needle := strings.ToLower(strings.TrimSpace(query.Text))
	items := []SearchHit{}
	for _, d := range b.rows {
		if d.Tenant != query.Tenant || d.TypeID != query.TypeID {
			continue
		}
		value := strings.ToLower(jsonStringProperty(d.Payload, query.Property))
		if !matchesText(value, needle, query.Phrase, query.Prefix) {
			continue
		}
		items = append(items, SearchHit{ID: d.ID, TypeID: d.TypeID, Score: 1, Snippet: append(json.RawMessage{}, d.Payload...)})
	}
	limit := query.Page.Size
	if limit == 0 {
		limit = 1
	}
	if uint32(len(items)) > limit {
		items = items[:limit]
	}
	return PagedResult[SearchHit]{Items: items}, nil
}

// SearchHybrid blends lexical and ANN scores for local tests/dev. Production
// backends map this contract to OpenSearch or Vespa native hybrid queries.
func (b *InMemorySearchBackend) SearchHybrid(_ context.Context, query HybridQuery, _ ReadConsistency) ([]SearchHit, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	type scored struct {
		score float32
		doc   IndexDoc
	}
	needle := strings.ToLower(strings.TrimSpace(query.Text))
	scores := []scored{}
	for _, d := range b.rows {
		if d.Tenant != query.Tenant || d.TypeID != query.TypeID || !matchesFilters(d.Payload, query.Filters) {
			continue
		}
		var score float32
		if needle != "" {
			field := strings.ToLower(string(d.Payload))
			if query.Property != "" {
				field = strings.ToLower(jsonStringProperty(d.Payload, query.Property))
			}
			if strings.Contains(field, needle) {
				score += 1
			}
		}
		if len(query.Embedding) > 0 && len(d.Embedding) > 0 {
			score += vectorScore(d.Embedding, query.Embedding, query.Distance)
		}
		if score > 0 || (needle == "" && len(query.Embedding) == 0) {
			scores = append(scores, scored{score: score, doc: d})
		}
	}
	sort.SliceStable(scores, func(i, j int) bool { return scores[i].score > scores[j].score })
	k := query.K
	if k == 0 {
		k = 1
	}
	if uint32(len(scores)) > k {
		scores = scores[:k]
	}
	out := make([]SearchHit, 0, len(scores))
	for _, s := range scores {
		out = append(out, SearchHit{ID: s.doc.ID, TypeID: s.doc.TypeID, Score: s.score, Snippet: append(json.RawMessage{}, s.doc.Payload...)})
	}
	return out, nil
}

func jsonStringProperty(payload json.RawMessage, property string) string {
	obj := map[string]any{}
	if err := json.Unmarshal(payload, &obj); err != nil {
		return ""
	}
	v, ok := obj[property]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func matchesText(value, needle string, phrase, prefix bool) bool {
	if needle == "" {
		return true
	}
	if phrase {
		return strings.Contains(value, needle)
	}
	tokens := strings.FieldsFunc(value, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') })
	for _, token := range tokens {
		if prefix && strings.HasPrefix(token, needle) {
			return true
		}
		if !prefix && token == needle {
			return true
		}
	}
	return false
}

func vectorScore(a, b []float32, distance VectorDistance) float32 {
	switch distance {
	case VectorDistanceL2:
		return -l2Distance(a, b)
	case VectorDistanceDot:
		return dotProduct(a, b)
	default:
		return cosineSim(a, b)
	}
}

func dotProduct(a, b []float32) float32 {
	var dot float32
	for i := 0; i < len(a) && i < len(b); i++ {
		dot += a[i] * b[i]
	}
	return dot
}

func l2Distance(a, b []float32) float32 {
	var sum float64
	for i := 0; i < len(a) && i < len(b); i++ {
		d := float64(a[i] - b[i])
		sum += d * d
	}
	return float32(math.Sqrt(sum))
}

// BulkIndex delegates to DefaultBulkIndex.
func (b *InMemorySearchBackend) BulkIndex(ctx context.Context, docs []IndexDoc) (BulkOutcome, error) {
	return DefaultBulkIndex(ctx, b, docs)
}

// matchesFilters reports whether every filter has a string-typed
// value in the payload that compares equal. Mirrors the Rust
// `payload.get(k).and_then(|x| x.as_str()) == Some(v.as_str())`.
func matchesFilters(payload json.RawMessage, filters map[string]string) bool {
	if len(filters) == 0 {
		return true
	}
	if len(payload) == 0 {
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		return false
	}
	for k, v := range filters {
		raw, ok := obj[k]
		if !ok {
			return false
		}
		s, ok := raw.(string)
		if !ok || s != v {
			return false
		}
	}
	return true
}

// cosineSim returns the cosine similarity of `a` and `b` clamped
// to [0,1] when degenerate. Mirrors the Rust closure in
// noop::InMemorySearchBackend::search_vector.
func cosineSim(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	denom := float32(math.Sqrt(float64(na))) * float32(math.Sqrt(float64(nb)))
	if denom < math.SmallestNonzeroFloat32 {
		return 0
	}
	return dot / denom
}
