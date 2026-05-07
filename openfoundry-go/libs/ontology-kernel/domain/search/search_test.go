package search

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	domain "github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// ---- fulltext.go ----------------------------------------------------------

// libs/ontology-kernel/src/domain/search/fulltext.rs
// `prefers_title_hits`.
func TestLexicalScorePrefersTitleHits(t *testing.T) {
	withTitle := LexicalScore("customer health", "Customer Health", "weekly status board")
	withBody := LexicalScore("customer health", "Overview", "customer health weekly board")
	assert.Greater(t, withTitle, withBody)
}

// libs/ontology-kernel/src/domain/search/fulltext.rs `tokenize` —
// splits on every non-alphanumeric / non-`_-` character and
// lower-cases survivors.
func TestTokenizeShape(t *testing.T) {
	tokens := Tokenize("Customer-health REVIEW.2024_q1")
	assert.Equal(t, []string{"customer-health", "review", "2024_q1"}, tokens)

	assert.Empty(t, Tokenize("   . , !"))
}

// Empty-query short-circuit: zero score regardless of body / title.
func TestLexicalScoreEmptyQueryReturnsZero(t *testing.T) {
	assert.Equal(t, float32(0), LexicalScore("", "anything", "anything"))
	assert.Equal(t, float32(0), LexicalScore("   ", "anything", "anything"))
}

// ---- semantic.go ----------------------------------------------------------

// libs/ontology-kernel/src/domain/search/semantic.rs
// `similar_text_scores_higher_than_unrelated_text`.
func TestSemanticScoreSimilarTextRanksHigher(t *testing.T) {
	related := SemanticScore("payment risk review", "payment risk review workflow")
	unrelated := SemanticScore("payment risk review", "mountain weather and sailing")
	assert.Greater(t, related, unrelated)
}

// libs/ontology-kernel/src/domain/search/semantic.rs
// `deterministic_backend_metadata_is_stable`.
func TestBackendReferenceDeterministic(t *testing.T) {
	assert.Equal(t, "deterministic-hash", BackendReference(EmbeddingBackend{Kind: BackendDeterministicHash}))
	assert.Equal(t, "provider:abc",
		BackendReference(EmbeddingBackend{Kind: BackendProvider, Provider: EmbeddingProvider{Reference: "provider:abc"}}))
}

// libs/ontology-kernel/src/domain/search/semantic.rs `embed_text`
// produces a deterministic 16-dim unit vector. Same content →
// byte-equal vector across runs; the magnitude is 1 for any
// non-empty input.
func TestEmbedTextDeterministicAndUnit(t *testing.T) {
	a := EmbedText("payment risk review")
	b := EmbedText("payment risk review")
	require.Len(t, a, 16)
	assert.Equal(t, a, b)

	var sum float32
	for _, v := range a {
		sum += v * v
	}
	assert.InDelta(t, 1.0, sum, 1e-5, "magnitude squared should be ≈1")
}

// libs/ontology-kernel/src/domain/search/semantic.rs cosine_similarity
// — empty inputs and length mismatches return 0.
func TestCosineSimilarityGuards(t *testing.T) {
	assert.Equal(t, float32(0), CosineSimilarity(nil, nil))
	assert.Equal(t, float32(0), CosineSimilarity([]float32{1}, []float32{1, 1}))
	assert.InDelta(t, 1.0, CosineSimilarity([]float32{1, 0}, []float32{1, 0}), 1e-6)
	assert.InDelta(t, -0.0, CosineSimilarity([]float32{1, 0}, []float32{0, 1}), 1e-6)
}

// libs/ontology-kernel/src/domain/search/semantic.rs
// `normalized_provider_reference`: caller-supplied trimmed string
// trumps the configured default; both empty falls back to the
// configured default.
func TestNormalizedProviderReferencePriority(t *testing.T) {
	configured := "deterministic-hash"
	pref := "  provider:abc  "
	assert.Equal(t, "provider:abc", normalizedProviderReference(&pref, configured))

	empty := "   "
	assert.Equal(t, "deterministic-hash", normalizedProviderReference(&empty, configured))

	assert.Equal(t, "deterministic-hash", normalizedProviderReference(nil, configured))
}

// ---- objects_fulltext.go --------------------------------------------------

// libs/ontology-kernel/src/domain/search/objects_fulltext.rs
// `allowed_markings_for` — admin sees the full set; non-admin
// caller's clearance bounds the set; explicit caller filter is
// passed through the bounded set.
func TestAllowedMarkingsForCascade(t *testing.T) {
	admin := &authmw.Claims{Roles: []string{"admin"}}
	assert.Equal(t, []string{"public", "confidential", "pii"},
		allowedMarkingsFor(admin, nil))

	piiClearance := "pii"
	piiClaims := &authmw.Claims{Attributes: json.RawMessage(`{"classification_clearance":"` + piiClearance + `"}`)}
	assert.Equal(t, []string{"public", "confidential", "pii"},
		allowedMarkingsFor(piiClaims, nil))

	publicOnly := []string{"public"}
	assert.Equal(t, publicOnly,
		allowedMarkingsFor(&authmw.Claims{}, nil))

	// Explicit filter passes through the base allowlist; pii is
	// dropped because the public caller cannot widen scope.
	requested := []string{"public", "pii", "secret"}
	got := allowedMarkingsFor(&authmw.Claims{}, &requested)
	assert.Equal(t, []string{"public"}, got)
}

// libs/ontology-kernel/src/domain/search/objects_fulltext.rs —
// search_hit_to_object_instance pulls id / object_type_id from the
// snippet first, then from the hit fields.
func TestSearchHitToObjectInstanceShapes(t *testing.T) {
	hitID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	typeID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	snippet, err := json.Marshal(map[string]any{
		"id":             hitID.String(),
		"object_type_id": typeID.String(),
		"properties":     map[string]any{"name": "alpha"},
		"marking":        "confidential",
		"created_at":     "2026-05-06T10:00:00Z",
		"updated_at":     "2026-05-06T11:00:00Z",
	})
	require.NoError(t, err)
	got := searchHitToObjectInstance(snippetHit(snippet, hitID, typeID), nil)
	require.NotNil(t, got)
	assert.Equal(t, hitID, got.ID)
	assert.Equal(t, typeID, got.ObjectTypeID)
	assert.Equal(t, "confidential", got.Marking)
	assert.JSONEq(t, `{"name":"alpha"}`, string(got.Properties))
}

// libs/ontology-kernel/src/domain/search/objects_fulltext.rs —
// missing snippet → nil result.
func TestSearchHitToObjectInstanceMissingSnippet(t *testing.T) {
	require.Nil(t, searchHitToObjectInstance(snippetHit(nil, uuid.New(), uuid.New()), nil))
}

// ---- search.go (orchestrator) --------------------------------------------

// libs/ontology-kernel/src/domain/search/mod.rs
// `semantic_text_includes_title_and_body`.
func TestSemanticTextIncludesTitleAndBody(t *testing.T) {
	doc := domain.SearchDocument{Title: "Incident review", Body: "Payment risk escalation", Snippet: "Payment risk escalation"}
	text := semanticTextForDocument(doc)
	assert.True(t, strings.Contains(text, "Incident review"))
	assert.True(t, strings.Contains(text, "Payment risk escalation"))
}

// libs/ontology-kernel/src/domain/search/mod.rs
// `candidate_pool_combines_lexical_and_semantic_recall`.
func TestSemanticCandidatePoolCombinesRankings(t *testing.T) {
	scored := []scoredDocument{
		{document: domain.SearchDocument{Title: "A", Body: "alpha"}, lexicalScore: 0.9, heuristicSemanticScore: 0.1, semanticScore: 0.1},
		{document: domain.SearchDocument{Title: "B", Body: "beta"}, lexicalScore: 0.1, heuristicSemanticScore: 0.9, semanticScore: 0.9},
		{document: domain.SearchDocument{Title: "C", Body: "gamma"}, lexicalScore: 0.2, heuristicSemanticScore: 0.2, semanticScore: 0.2},
	}
	one := 1
	pool := semanticCandidatePool(scored, 1, &one)
	assert.Contains(t, pool, 0, "lexical winner present")
	assert.Contains(t, pool, 1, "heuristic-semantic winner present")
}

// libs/ontology-kernel/src/domain/search/mod.rs
// `rrf_rewards_documents_present_in_both_rankings`.
func TestFuseWithRRFRewardsBothRankings(t *testing.T) {
	scored := []scoredDocument{
		{
			document:               domain.SearchDocument{Title: "Strong lexical and semantic", Body: "alpha"},
			lexicalScore:           0.9,
			heuristicSemanticScore: 0.8,
			semanticScore:          0.8,
		},
		{
			document:               domain.SearchDocument{Title: "Lexical only", Body: "beta"},
			lexicalScore:           0.85,
			heuristicSemanticScore: 0.2,
			semanticScore:          0.2,
		},
		{
			document:               domain.SearchDocument{Title: "Semantic only", Body: "gamma"},
			lexicalScore:           0.2,
			heuristicSemanticScore: 0.85,
			semanticScore:          0.85,
		},
	}
	results := fuseWithRRF(scored, 3)
	require.NotEmpty(t, results)
	assert.Equal(t, "Strong lexical and semantic", results[0].Title)
}

// libs/ontology-kernel/src/domain/search/mod.rs
// `normalize_hybrid_strategy` — defaults to RRF, "weighted" wins.
func TestNormalizeHybridStrategy(t *testing.T) {
	weighted := "weighted"
	assert.Equal(t, strategyWeighted, normalizeHybridStrategy(&weighted))
	rrf := "rrf"
	assert.Equal(t, strategyRRF, normalizeHybridStrategy(&rrf))
	empty := "   "
	assert.Equal(t, strategyRRF, normalizeHybridStrategy(&empty))
	assert.Equal(t, strategyRRF, normalizeHybridStrategy(nil))
}

// libs/ontology-kernel/src/domain/search/mod.rs `passes_search_threshold`.
func TestPassesSearchThreshold(t *testing.T) {
	assert.True(t, passesSearchThreshold(scoredDocument{lexicalScore: 0.05}))
	assert.True(t, passesSearchThreshold(scoredDocument{semanticScore: 0.55}))
	assert.True(t, passesSearchThreshold(scoredDocument{titleBonus: 0.01}))
	assert.False(t, passesSearchThreshold(scoredDocument{lexicalScore: 0.04, semanticScore: 0.54}))
}

// libs/ontology-kernel/src/domain/search/mod.rs `search_ontology` —
// empty query returns immediately with no docs loaded.
func TestSearchOntologyEmptyQueryShortCircuits(t *testing.T) {
	loader := func(ctx context.Context, claims *authmw.Claims, _ *uuid.UUID, _ *string) ([]domain.SearchDocument, error) {
		t.Fatal("loader must not be called when query is empty")
		return nil, nil
	}
	out, err := SearchOntology(context.Background(), OntologySearchDeps{}, loader, &authmw.Claims{},
		models.SearchRequest{Query: "  "})
	require.NoError(t, err)
	assert.Empty(t, out)
}

// libs/ontology-kernel/src/domain/search/mod.rs — when semantic is
// disabled the orchestrator never tries to resolve a backend.
func TestSearchOntologyHybridDisabledSkipsBackendResolution(t *testing.T) {
	docs := []domain.SearchDocument{
		{Kind: "object_instance", ID: uuid.New(), Title: "alpha review", Body: "alpha", Snippet: "alpha", Route: "/x", Metadata: json.RawMessage(`{}`)},
	}
	loader := func(_ context.Context, _ *authmw.Claims, _ *uuid.UUID, _ *string) ([]domain.SearchDocument, error) {
		return docs, nil
	}
	disabled := false
	deps := OntologySearchDeps{} // no DB / HTTPClient — would panic if reached.
	out, err := SearchOntology(context.Background(), deps, loader, &authmw.Claims{},
		models.SearchRequest{Query: "alpha", Semantic: &disabled},
	)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "alpha review", out[0].Title)
}

// ---- helpers -------------------------------------------------------------

func snippetHit(snippet json.RawMessage, hitID, typeID uuid.UUID) storage.SearchHit {
	return storage.SearchHit{
		ID:      storage.ObjectId(hitID.String()),
		TypeID:  storage.TypeId(typeID.String()),
		Snippet: snippet,
	}
}
