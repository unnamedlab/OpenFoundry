package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// KnowledgeChunk is a single retrievable chunk of a document.
type KnowledgeChunk struct {
	ID         string          `json:"id"`
	Position   int32           `json:"position"`
	Text       string          `json:"text"`
	TokenCount int32           `json:"token_count"`
	Embedding  []float32       `json:"embedding"`
	Metadata   json.RawMessage `json:"metadata"`
}

// KnowledgeDocument carries the full document body + its chunks.
type KnowledgeDocument struct {
	ID              uuid.UUID        `json:"id"`
	KnowledgeBaseID uuid.UUID        `json:"knowledge_base_id"`
	Title           string           `json:"title"`
	Content         string           `json:"content"`
	SourceURI       *string          `json:"source_uri"`
	Metadata        json.RawMessage  `json:"metadata"`
	Status          string           `json:"status"`
	ChunkCount      int32            `json:"chunk_count"`
	Chunks          []KnowledgeChunk `json:"chunks"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// KnowledgeBase is the catalog row for one KB.
type KnowledgeBase struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	Status            string    `json:"status"`
	EmbeddingProvider string    `json:"embedding_provider"`
	ChunkingStrategy  string    `json:"chunking_strategy"`
	Tags              []string  `json:"tags"`
	DocumentCount     int64     `json:"document_count"`
	ChunkCount        int64     `json:"chunk_count"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// KnowledgeSearchResult is one citation hit returned by RAG queries.
type KnowledgeSearchResult struct {
	KnowledgeBaseID uuid.UUID       `json:"knowledge_base_id"`
	DocumentID      uuid.UUID       `json:"document_id"`
	DocumentTitle   string          `json:"document_title"`
	ChunkID         string          `json:"chunk_id"`
	Score           float32         `json:"score"`
	Excerpt         string          `json:"excerpt"`
	SourceURI       *string         `json:"source_uri"`
	Metadata        json.RawMessage `json:"metadata"`
}

type ListKnowledgeBasesResponse struct {
	Data []KnowledgeBase `json:"data"`
}

type ListKnowledgeDocumentsResponse struct {
	Data []KnowledgeDocument `json:"data"`
}

// CreateKnowledgeBaseRequest defaults: status="active",
// embedding_provider="deterministic-hash", chunking_strategy="balanced".
type CreateKnowledgeBaseRequest struct {
	Name              string   `json:"name"`
	Description       *string  `json:"description"`
	Status            *string  `json:"status"`
	EmbeddingProvider *string  `json:"embedding_provider"`
	ChunkingStrategy  *string  `json:"chunking_strategy"`
	Tags              []string `json:"tags"`
}

type UpdateKnowledgeBaseRequest struct {
	Name              *string   `json:"name"`
	Description       *string   `json:"description"`
	Status            *string   `json:"status"`
	EmbeddingProvider *string   `json:"embedding_provider"`
	ChunkingStrategy  *string   `json:"chunking_strategy"`
	Tags              *[]string `json:"tags"`
}

type CreateKnowledgeDocumentRequest struct {
	Title     string          `json:"title"`
	Content   string          `json:"content"`
	SourceURI *string         `json:"source_uri"`
	Metadata  json.RawMessage `json:"metadata"`
}

// SearchKnowledgeBaseRequest defaults: top_k=5, min_score=0.55.
type SearchKnowledgeBaseRequest struct {
	Query    string  `json:"query"`
	TopK     uint32  `json:"top_k"`
	MinScore float32 `json:"min_score"`
}

func (r *SearchKnowledgeBaseRequest) UnmarshalJSON(data []byte) error {
	type alias SearchKnowledgeBaseRequest
	aux := struct {
		TopK     *uint32  `json:"top_k"`
		MinScore *float32 `json:"min_score"`
		*alias
	}{
		alias: (*alias)(r),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.TopK == nil {
		r.TopK = DefaultSearchTopK
	} else {
		r.TopK = *aux.TopK
	}
	if aux.MinScore == nil {
		r.MinScore = DefaultSearchMinScore
	} else {
		r.MinScore = *aux.MinScore
	}
	return nil
}

type SearchKnowledgeBaseResponse struct {
	KnowledgeBaseID uuid.UUID               `json:"knowledge_base_id"`
	Query           string                  `json:"query"`
	Results         []KnowledgeSearchResult `json:"results"`
	RetrievedAt     time.Time               `json:"retrieved_at"`
	SearchProvider  string                  `json:"search_provider,omitempty"`
	SearchMode      string                  `json:"search_mode,omitempty"`
}

const (
	DefaultKnowledgeStatus           = "active"
	DefaultEmbeddingProvider         = "deterministic-hash"
	DefaultChunkingStrategy          = "balanced"
	DefaultSearchTopK        uint32  = 5
	DefaultSearchMinScore    float32 = 0.55
)

func (r *CreateKnowledgeBaseRequest) UnmarshalJSON(data []byte) error {
	type alias CreateKnowledgeBaseRequest
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	if r.Description == nil {
		r.Description = ptrOf("")
	}
	if r.Status == nil {
		r.Status = ptrOf(DefaultKnowledgeStatus)
	}
	if r.EmbeddingProvider == nil {
		r.EmbeddingProvider = ptrOf(DefaultEmbeddingProvider)
	}
	if r.ChunkingStrategy == nil {
		r.ChunkingStrategy = ptrOf(DefaultChunkingStrategy)
	}
	if r.Tags == nil {
		r.Tags = []string{}
	}
	return nil
}

func (r *CreateKnowledgeDocumentRequest) UnmarshalJSON(data []byte) error {
	type alias CreateKnowledgeDocumentRequest
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	r.Metadata = defaultRawMessage(r.Metadata, emptyJSONObject())
	return nil
}
