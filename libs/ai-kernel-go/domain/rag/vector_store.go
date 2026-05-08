package rag

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

// VectorStore is the injectable retrieval index behind knowledge search.
// Implementations can back chunks with pgvector, Vespa, an external service,
// or the deterministic in-memory fake used by tests.
type VectorStore interface {
	UpsertDocument(ctx context.Context, document models.KnowledgeDocument) error
	DeleteDocument(ctx context.Context, knowledgeBaseID, documentID uuid.UUID) error
	Search(ctx context.Context, knowledgeBaseID uuid.UUID, queryEmbedding []float32, topK uint32, minScore float32) ([]models.KnowledgeSearchResult, error)
}

// FakeVectorStore is an in-memory VectorStore for unit tests and local wiring.
// It stores complete documents and delegates ranking to SearchWithEmbedding so
// behavior matches the deterministic Rust-compatible RAG retriever.
type FakeVectorStore struct {
	mu        sync.RWMutex
	documents map[uuid.UUID]map[uuid.UUID]models.KnowledgeDocument
}

func NewFakeVectorStore() *FakeVectorStore {
	return &FakeVectorStore{documents: map[uuid.UUID]map[uuid.UUID]models.KnowledgeDocument{}}
}

func (s *FakeVectorStore) UpsertDocument(_ context.Context, document models.KnowledgeDocument) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.documents == nil {
		s.documents = map[uuid.UUID]map[uuid.UUID]models.KnowledgeDocument{}
	}
	byKB := s.documents[document.KnowledgeBaseID]
	if byKB == nil {
		byKB = map[uuid.UUID]models.KnowledgeDocument{}
		s.documents[document.KnowledgeBaseID] = byKB
	}
	byKB[document.ID] = document
	return nil
}

func (s *FakeVectorStore) DeleteDocument(_ context.Context, knowledgeBaseID, documentID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.documents == nil {
		return nil
	}
	if byKB := s.documents[knowledgeBaseID]; byKB != nil {
		delete(byKB, documentID)
	}
	return nil
}

func (s *FakeVectorStore) Search(_ context.Context, knowledgeBaseID uuid.UUID, queryEmbedding []float32, topK uint32, minScore float32) ([]models.KnowledgeSearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	byKB := s.documents[knowledgeBaseID]
	docs := make([]models.KnowledgeDocument, 0, len(byKB))
	for _, doc := range byKB {
		docs = append(docs, doc)
	}
	return SearchWithEmbedding(queryEmbedding, docs, topK, minScore), nil
}
