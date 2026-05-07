// Hybrid lexical + semantic ontology search orchestrator.
//
// Mirrors `libs/ontology-kernel/src/domain/search/mod.rs`. The
// pipeline is:
//
//  1. Build the candidate document corpus via
//     `domain.BuildSearchDocuments` (filters by object type / kind).
//  2. Score every document with the lexical `LexicalScore` +
//     heuristic `SemanticScore` so we always have a ranking even
//     when no provider is wired.
//  3. If semantic search is enabled AND a query embedding can be
//     produced (deterministic-hash never fails; remote providers
//     might), rerank a candidate pool with the provider-backed
//     embedding similarity.
//  4. Fuse the two rankings via Reciprocal-Rank Fusion (default)
//     or weighted scoring; surface a [SearchScoreBreakdown] on
//     each result so the front-end can show the explanation.
//
// The Rust source uses `state.stores.search` for the embedding
// rerank fan-out; the Go port doesn't need that field today
// because rerank goes through the in-process http.Client + the
// per-document HTTP call. SearchBackend stays in the
// objects-fulltext path.

package search

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	domain "github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// defaultRRFK mirrors `const DEFAULT_RRF_K`.
const defaultRRFK float32 = 60.0

// hybridStrategy mirrors `enum HybridStrategy`.
type hybridStrategy int

const (
	strategyRRF hybridStrategy = iota
	strategyWeighted
)

// scoredDocument mirrors `struct ScoredDocument`.
type scoredDocument struct {
	document               domain.SearchDocument
	lexicalScore           float32
	heuristicSemanticScore float32
	semanticScore          float32
	titleBonus             float32
}

// OntologySearchDeps groups the dependencies the orchestrator needs.
// The Rust source pulls them from `state: &AppState`; the Go port
// keeps them flat so callers don't need a fully-built AppState to
// run a search (handlers will pass these from the AppState fields
// directly).
type OntologySearchDeps struct {
	DB                        *pgxpool.Pool
	HTTPClient                *http.Client
	ConfiguredEmbeddingProvider string
}

// SearchOntology mirrors `pub async fn search_ontology`.
func SearchOntology(
	ctx context.Context,
	deps OntologySearchDeps,
	docsLoader DocumentsLoader,
	claims *authmw.Claims,
	request models.SearchRequest,
) ([]models.SearchResult, error) {
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return []models.SearchResult{}, nil
	}

	documents, err := docsLoader(ctx, claims, request.ObjectTypeID, request.Kind)
	if err != nil {
		return nil, err
	}
	if len(documents) == 0 {
		return []models.SearchResult{}, nil
	}

	semanticEnabled := true
	if request.Semantic != nil {
		semanticEnabled = *request.Semantic
	}
	limit := 25
	if request.Limit != nil {
		limit = *request.Limit
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	strategy := normalizeHybridStrategy(request.HybridStrategy)

	scored := make([]scoredDocument, 0, len(documents))
	for _, doc := range documents {
		text := semanticTextForDocument(doc)
		fulltext := LexicalScore(query, doc.Title, doc.Body)
		heuristic := SemanticScore(query, text)
		bonus := titlePrefixBonus(query, doc.Title)
		scored = append(scored, scoredDocument{
			document:               doc,
			lexicalScore:           fulltext + bonus,
			heuristicSemanticScore: heuristic,
			semanticScore:          heuristic,
			titleBonus:             bonus,
		})
	}

	if semanticEnabled {
		backend, err := ResolveBackend(ctx, deps.DB, deps.ConfiguredEmbeddingProvider, request.EmbeddingProvider)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve ontology search embedding backend: %s", err)
		}
		queryEmbedding, embedErr := EmbedWithBackend(ctx, deps.HTTPClient, backend, query)
		if embedErr != nil || len(queryEmbedding) == 0 {
			if embedErr != nil {
				warnf("provider-backed ontology search embeddings failed (provider=%s, err=%s); falling back to heuristic semantic ranking",
					BackendReference(backend), embedErr)
			}
		} else {
			pool := semanticCandidatePool(scored, limit, request.SemanticCandidateLimit)
			for _, idx := range pool {
				text := semanticTextForDocument(scored[idx].document)
				score, err := ScoreWithQueryEmbedding(ctx, deps.HTTPClient, backend, queryEmbedding, text)
				if err != nil {
					warnf("semantic rerank embedding failed for ontology search document (id=%s, provider=%s, err=%s); keeping heuristic score",
						scored[idx].document.ID, BackendReference(backend), err)
					continue
				}
				scored[idx].semanticScore = score
			}
		}
	}

	switch strategy {
	case strategyRRF:
		return fuseWithRRF(scored, limit), nil
	case strategyWeighted:
		return fuseWithWeightedScoring(scored, limit), nil
	default:
		return fuseWithRRF(scored, limit), nil
	}
}

// DocumentsLoader is the function the orchestrator consults to build
// the candidate corpus. The default is `domain.BuildSearchDocuments`
// applied against AppState.DB; tests can inject anything that
// satisfies the signature.
type DocumentsLoader func(ctx context.Context, claims *authmw.Claims, objectTypeFilter *uuid.UUID, kindFilter *string) ([]domain.SearchDocument, error)

// normalizeHybridStrategy mirrors `fn normalize_hybrid_strategy`.
func normalizeHybridStrategy(value *string) hybridStrategy {
	if value == nil {
		return strategyRRF
	}
	v := strings.TrimSpace(*value)
	if v == "weighted" {
		return strategyWeighted
	}
	return strategyRRF
}

// titlePrefixBonus mirrors `fn title_prefix_bonus`.
func titlePrefixBonus(query, title string) float32 {
	if strings.HasPrefix(strings.ToLower(title), strings.ToLower(query)) {
		return 0.2
	}
	return 0
}

// semanticTextForDocument mirrors `fn semantic_text_for_document`.
func semanticTextForDocument(document domain.SearchDocument) string {
	parts := []string{}
	if t := strings.TrimSpace(document.Title); t != "" {
		parts = append(parts, t)
	}
	if document.Subtitle != nil {
		if s := strings.TrimSpace(*document.Subtitle); s != "" {
			parts = append(parts, s)
		}
	}
	if s := strings.TrimSpace(document.Snippet); s != "" {
		parts = append(parts, s)
	}
	if b := strings.TrimSpace(document.Body); b != "" {
		parts = append(parts, b)
	}
	combined := strings.Join(parts, "\n")
	return truncateForEmbeddings(combined, 2_400)
}

// truncateForEmbeddings mirrors `fn truncate_for_embeddings`.
func truncateForEmbeddings(content string, maxChars int) string {
	if len([]rune(content)) <= maxChars {
		return content
	}
	runes := []rune(content)
	return string(runes[:maxChars])
}

// semanticCandidatePool mirrors `fn semantic_candidate_pool`.
//
// Builds a pool covering BOTH the top-N lexical hits and the top-N
// heuristic-semantic hits, plus every document that earned a
// title-prefix bonus. The Rust source clamps the pool size to
// `[max(limit, 16), 160]` after defaulting to `limit*4` when the
// caller did not supply one.
func semanticCandidatePool(scored []scoredDocument, limit int, requested *int) []int {
	poolSize := limit * 4
	if poolSize < 32 {
		poolSize = 32
	}
	if requested != nil {
		poolSize = *requested
	}
	lo := limit
	if lo < 16 {
		lo = 16
	}
	if poolSize < lo {
		poolSize = lo
	}
	if poolSize > 160 {
		poolSize = 160
	}

	lexical := rankingIndices(scored, func(a, b scoredDocument) int {
		return cmpFloat(b.lexicalScore, a.lexicalScore)
	})
	heuristic := rankingIndices(scored, func(a, b scoredDocument) int {
		return cmpFloat(b.heuristicSemanticScore, a.heuristicSemanticScore)
	})

	selected := map[int]bool{}
	for i, idx := range lexical {
		if i >= poolSize {
			break
		}
		selected[idx] = true
	}
	for i, idx := range heuristic {
		if i >= poolSize {
			break
		}
		selected[idx] = true
	}
	for i, doc := range scored {
		if doc.titleBonus > 0 {
			selected[i] = true
		}
	}
	pool := make([]int, 0, len(selected))
	for idx := range selected {
		pool = append(pool, idx)
	}
	sort.Slice(pool, func(i, j int) bool {
		return cmpFloat(scored[pool[j]].lexicalScore, scored[pool[i]].lexicalScore) < 0
	})
	return pool
}

// rankingIndices mirrors `fn ranking_indices`. Returns the indices
// of `scored` ordered by the supplied comparator.
func rankingIndices(scored []scoredDocument, compare func(a, b scoredDocument) int) []int {
	out := make([]int, len(scored))
	for i := range scored {
		out[i] = i
	}
	sort.Slice(out, func(i, j int) bool {
		return compare(scored[out[i]], scored[out[j]]) < 0
	})
	return out
}

// rankMap mirrors `fn rank_map`. Position is 1-indexed (Rust
// `position + 1`).
func rankMap(indices []int) map[int]int {
	out := make(map[int]int, len(indices))
	for pos, idx := range indices {
		out[idx] = pos + 1
	}
	return out
}

// reciprocalRank mirrors `fn reciprocal_rank`.
func reciprocalRank(rank int) float32 {
	return 1.0 / (defaultRRFK + float32(rank))
}

// passesSearchThreshold mirrors `fn passes_search_threshold`.
func passesSearchThreshold(doc scoredDocument) bool {
	return doc.lexicalScore >= 0.05 || doc.semanticScore >= 0.55 || doc.titleBonus > 0
}

// fuseWithRRF mirrors `fn fuse_with_rrf`.
func fuseWithRRF(scored []scoredDocument, limit int) []models.SearchResult {
	lexicalRanking := rankingIndices(scored, func(a, b scoredDocument) int {
		return cmpFloat(b.lexicalScore, a.lexicalScore)
	})
	semanticRanking := rankingIndices(scored, func(a, b scoredDocument) int {
		return cmpFloat(b.semanticScore, a.semanticScore)
	})
	lexicalRanks := rankMap(lexicalRanking)
	semanticRanks := rankMap(semanticRanking)

	type fused struct {
		index        int
		lexicalRank  *int
		semanticRank *int
		score        float32
	}
	rows := []fused{}
	for i, doc := range scored {
		if !passesSearchThreshold(doc) {
			continue
		}
		var lr, sr *int
		if v, ok := lexicalRanks[i]; ok {
			lr = &v
		}
		if v, ok := semanticRanks[i]; ok {
			sr = &v
		}
		lrrf := float32(0)
		if lr != nil {
			lrrf = reciprocalRank(*lr)
		}
		srrf := float32(0)
		if sr != nil {
			srrf = reciprocalRank(*sr)
		}
		semClamp := doc.semanticScore
		if semClamp < 0 {
			semClamp = 0
		}
		score := lrrf*1.1 + srrf*1.1 + doc.lexicalScore*0.25 + semClamp*0.25 + doc.titleBonus
		rows = append(rows, fused{i, lr, sr, score})
	}
	sort.Slice(rows, func(i, j int) bool { return cmpFloat(rows[j].score, rows[i].score) < 0 })
	if limit < len(rows) {
		rows = rows[:limit]
	}
	out := make([]models.SearchResult, 0, len(rows))
	for _, r := range rows {
		doc := scored[r.index]
		out = append(out, buildResult(doc, "rrf", r.lexicalRank, r.semanticRank, r.score))
	}
	return out
}

// fuseWithWeightedScoring mirrors `fn fuse_with_weighted_scoring`.
func fuseWithWeightedScoring(scored []scoredDocument, limit int) []models.SearchResult {
	lexicalRanking := rankingIndices(scored, func(a, b scoredDocument) int {
		return cmpFloat(b.lexicalScore, a.lexicalScore)
	})
	semanticRanking := rankingIndices(scored, func(a, b scoredDocument) int {
		return cmpFloat(b.semanticScore, a.semanticScore)
	})
	lexicalRanks := rankMap(lexicalRanking)
	semanticRanks := rankMap(semanticRanking)

	type weighted struct {
		index        int
		lexicalRank  *int
		semanticRank *int
		score        float32
	}
	rows := []weighted{}
	for i, doc := range scored {
		if !passesSearchThreshold(doc) {
			continue
		}
		var lr, sr *int
		if v, ok := lexicalRanks[i]; ok {
			lr = &v
		}
		if v, ok := semanticRanks[i]; ok {
			sr = &v
		}
		semClamp := doc.semanticScore
		if semClamp < 0 {
			semClamp = 0
		}
		heurClamp := doc.heuristicSemanticScore
		if heurClamp < 0 {
			heurClamp = 0
		}
		score := doc.lexicalScore*0.5 + semClamp*0.4 + heurClamp*0.1 + doc.titleBonus
		rows = append(rows, weighted{i, lr, sr, score})
	}
	sort.Slice(rows, func(i, j int) bool { return cmpFloat(rows[j].score, rows[i].score) < 0 })
	if limit < len(rows) {
		rows = rows[:limit]
	}
	out := make([]models.SearchResult, 0, len(rows))
	for _, r := range rows {
		doc := scored[r.index]
		out = append(out, buildResult(doc, "weighted", r.lexicalRank, r.semanticRank, r.score))
	}
	return out
}

// buildResult assembles the [models.SearchResult] from a scored
// document and its rank metadata. Mirrors the Rust closure inside
// `fuse_with_rrf` / `fuse_with_weighted_scoring`.
func buildResult(doc scoredDocument, fusionStrategy string, lexicalRank, semanticRank *int, score float32) models.SearchResult {
	return models.SearchResult{
		Kind:         doc.document.Kind,
		ID:           doc.document.ID,
		ObjectTypeID: doc.document.ObjectTypeID,
		Title:        doc.document.Title,
		Subtitle:     doc.document.Subtitle,
		Snippet:      doc.document.Snippet,
		Score:        score,
		Route:        doc.document.Route,
		Metadata:     doc.document.Metadata,
		ScoreBreakdown: &models.SearchScoreBreakdown{
			FusionStrategy: fusionStrategy,
			LexicalRank:    lexicalRank,
			SemanticRank:   semanticRank,
			LexicalScore:   doc.lexicalScore,
			SemanticScore:  doc.semanticScore,
			TitleBonus:     doc.titleBonus,
		},
	}
}

// cmpFloat returns negative / zero / positive so comparators feeding
// `sort.Slice` get a single integer they can use to derive less-than.
func cmpFloat(a, b float32) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
