package models

import (
	"encoding/json"

	"github.com/google/uuid"
)

// SearchRequest mirrors `libs/ontology-kernel/src/models/search.rs`
// `struct SearchRequest`.
type SearchRequest struct {
	Query                   string     `json:"query"`
	Kind                    *string    `json:"kind,omitempty"`
	ObjectTypeID            *uuid.UUID `json:"object_type_id,omitempty"`
	Limit                   *int       `json:"limit,omitempty"`
	Semantic                *bool      `json:"semantic,omitempty"`
	HybridStrategy          *string    `json:"hybrid_strategy,omitempty"`
	EmbeddingProvider       *string    `json:"embedding_provider,omitempty"`
	SemanticCandidateLimit  *int       `json:"semantic_candidate_limit,omitempty"`
}

// SearchScoreBreakdown mirrors `struct SearchScoreBreakdown`.
type SearchScoreBreakdown struct {
	FusionStrategy string  `json:"fusion_strategy"`
	LexicalRank    *int    `json:"lexical_rank"`
	SemanticRank   *int    `json:"semantic_rank"`
	LexicalScore   float32 `json:"lexical_score"`
	SemanticScore  float32 `json:"semantic_score"`
	TitleBonus     float32 `json:"title_bonus"`
}

// SearchResult mirrors `struct SearchResult`. The Rust struct uses
// `#[serde(skip_serializing_if = "Option::is_none")]` on
// `score_breakdown` — `omitempty` on `*SearchScoreBreakdown` reproduces
// that.
type SearchResult struct {
	Kind           string                `json:"kind"`
	ID             uuid.UUID             `json:"id"`
	ObjectTypeID   *uuid.UUID            `json:"object_type_id"`
	Title          string                `json:"title"`
	Subtitle       *string               `json:"subtitle"`
	Snippet        string                `json:"snippet"`
	Score          float32               `json:"score"`
	Route          string                `json:"route"`
	Metadata       json.RawMessage       `json:"metadata"`
	ScoreBreakdown *SearchScoreBreakdown `json:"score_breakdown,omitempty"`
}

// SearchResponse mirrors `struct SearchResponse`.
type SearchResponse struct {
	Query string         `json:"query"`
	Total int            `json:"total"`
	Data  []SearchResult `json:"data"`
}

// KnnObjectsRequest mirrors `struct KnnObjectsRequest`.
type KnnObjectsRequest struct {
	PropertyName    string     `json:"property_name"`
	AnchorObjectID  *uuid.UUID `json:"anchor_object_id,omitempty"`
	QueryVector     *[]float32 `json:"query_vector,omitempty"`
	Limit           *int       `json:"limit,omitempty"`
	Metric          *string    `json:"metric,omitempty"`
	ExcludeAnchor   *bool      `json:"exclude_anchor,omitempty"`
}

// KnnObjectResult mirrors `struct KnnObjectResult`. Distance carries
// `skip_serializing_if = "Option::is_none"`.
type KnnObjectResult struct {
	Object   json.RawMessage `json:"object"`
	Score    float32         `json:"score"`
	Distance *float32        `json:"distance,omitempty"`
}

// KnnObjectsResponse mirrors `struct KnnObjectsResponse`.
type KnnObjectsResponse struct {
	PropertyName string            `json:"property_name"`
	Metric       string            `json:"metric"`
	Total        int               `json:"total"`
	Data         []KnnObjectResult `json:"data"`
}
