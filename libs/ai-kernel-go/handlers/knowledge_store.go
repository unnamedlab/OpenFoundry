package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

// KnowledgeStore is the persistence boundary for knowledge-base metadata and
// documents. Handlers depend on this interface so tests and agent-runtime
// follow-up wiring can use deterministic in-memory stores instead of Postgres.
type KnowledgeStore interface {
	ListKnowledgeBases(ctx context.Context) ([]models.KnowledgeBase, error)
	CreateKnowledgeBase(ctx context.Context, kb models.KnowledgeBase) (models.KnowledgeBase, error)
	GetKnowledgeBase(ctx context.Context, id uuid.UUID) (*models.KnowledgeBase, error)
	UpdateKnowledgeBase(ctx context.Context, kb models.KnowledgeBase) (models.KnowledgeBase, error)
	DeleteKnowledgeBase(ctx context.Context, id uuid.UUID) error
	ListDocuments(ctx context.Context, knowledgeBaseID uuid.UUID) ([]models.KnowledgeDocument, error)
	CreateDocument(ctx context.Context, doc models.KnowledgeDocument) (models.KnowledgeDocument, error)
	GetDocument(ctx context.Context, knowledgeBaseID, documentID uuid.UUID) (*models.KnowledgeDocument, error)
	DeleteDocument(ctx context.Context, knowledgeBaseID, documentID uuid.UUID) error
}

func (h *KnowledgeHandlers) knowledgeStore() KnowledgeStore {
	if h.Store != nil {
		return h.Store
	}
	return &PGKnowledgeStore{Pool: h.Pool}
}

// PGKnowledgeStore implements KnowledgeStore on the ai_knowledge_* tables.
type PGKnowledgeStore struct {
	Pool *pgxpool.Pool
}

func (s *PGKnowledgeStore) ListKnowledgeBases(ctx context.Context) ([]models.KnowledgeBase, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+knowledgeBaseColumns+` FROM ai_knowledge_bases ORDER BY updated_at DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.KnowledgeBase, 0)
	for rows.Next() {
		kb, err := scanKnowledgeBase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, kb)
	}
	return out, rows.Err()
}

func (s *PGKnowledgeStore) CreateKnowledgeBase(ctx context.Context, kb models.KnowledgeBase) (models.KnowledgeBase, error) {
	tagsJSON, _ := json.Marshal(kb.Tags)
	row := s.Pool.QueryRow(ctx,
		`INSERT INTO ai_knowledge_bases
              (id, name, description, status, embedding_provider, chunking_strategy, tags, document_count, chunk_count)
            VALUES ($1, $2, $3, $4, $5, $6, $7, 0, 0)
            RETURNING `+knowledgeBaseColumns,
		kb.ID, kb.Name, kb.Description, kb.Status, kb.EmbeddingProvider, kb.ChunkingStrategy, tagsJSON)
	return scanKnowledgeBase(row)
}

func (s *PGKnowledgeStore) GetKnowledgeBase(ctx context.Context, id uuid.UUID) (*models.KnowledgeBase, error) {
	row := s.Pool.QueryRow(ctx, `SELECT `+knowledgeBaseColumns+` FROM ai_knowledge_bases WHERE id = $1`, id)
	kb, err := scanKnowledgeBase(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &kb, nil
}

func (s *PGKnowledgeStore) UpdateKnowledgeBase(ctx context.Context, kb models.KnowledgeBase) (models.KnowledgeBase, error) {
	tagsJSON, _ := json.Marshal(kb.Tags)
	row := s.Pool.QueryRow(ctx,
		`UPDATE ai_knowledge_bases SET
            name = $2, description = $3, status = $4,
            embedding_provider = $5, chunking_strategy = $6, tags = $7,
            updated_at = NOW()
          WHERE id = $1
          RETURNING `+knowledgeBaseColumns,
		kb.ID, kb.Name, kb.Description, kb.Status, kb.EmbeddingProvider, kb.ChunkingStrategy, tagsJSON)
	return scanKnowledgeBase(row)
}

func (s *PGKnowledgeStore) DeleteKnowledgeBase(ctx context.Context, id uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM ai_knowledge_bases WHERE id = $1`, id)
	return err
}

func (s *PGKnowledgeStore) ListDocuments(ctx context.Context, knowledgeBaseID uuid.UUID) ([]models.KnowledgeDocument, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT `+knowledgeDocumentColumns+` FROM ai_knowledge_documents WHERE knowledge_base_id = $1 ORDER BY updated_at DESC, created_at DESC`, knowledgeBaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.KnowledgeDocument, 0)
	for rows.Next() {
		doc, err := scanKnowledgeDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

func (s *PGKnowledgeStore) CreateDocument(ctx context.Context, doc models.KnowledgeDocument) (models.KnowledgeDocument, error) {
	metadataJSON := jsonOrEmptyObject(rawMessagePtr(doc.Metadata))
	chunksJSON, _ := json.Marshal(doc.Chunks)
	row := s.Pool.QueryRow(ctx,
		`INSERT INTO ai_knowledge_documents
              (id, knowledge_base_id, title, content, source_uri, metadata, status, chunk_count, chunks)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
            RETURNING `+knowledgeDocumentColumns,
		doc.ID, doc.KnowledgeBaseID, doc.Title, doc.Content, doc.SourceURI, metadataJSON, doc.Status, doc.ChunkCount, chunksJSON)
	created, err := scanKnowledgeDocument(row)
	if err != nil {
		return created, err
	}
	_, err = s.Pool.Exec(ctx,
		`UPDATE ai_knowledge_bases
            SET document_count = document_count + 1, chunk_count = chunk_count + $2, updated_at = NOW()
          WHERE id = $1`, doc.KnowledgeBaseID, int64(doc.ChunkCount))
	return created, err
}

func (s *PGKnowledgeStore) GetDocument(ctx context.Context, knowledgeBaseID, documentID uuid.UUID) (*models.KnowledgeDocument, error) {
	row := s.Pool.QueryRow(ctx,
		`SELECT `+knowledgeDocumentColumns+` FROM ai_knowledge_documents WHERE knowledge_base_id = $1 AND id = $2`, knowledgeBaseID, documentID)
	doc, err := scanKnowledgeDocument(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (s *PGKnowledgeStore) DeleteDocument(ctx context.Context, knowledgeBaseID, documentID uuid.UUID) error {
	var chunkCount int64
	err := s.Pool.QueryRow(ctx,
		`DELETE FROM ai_knowledge_documents WHERE knowledge_base_id = $1 AND id = $2 RETURNING chunk_count`, knowledgeBaseID, documentID).Scan(&chunkCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx,
		`UPDATE ai_knowledge_bases
            SET document_count = GREATEST(document_count - 1, 0),
                chunk_count = GREATEST(chunk_count - $2, 0),
                updated_at = NOW()
          WHERE id = $1`, knowledgeBaseID, chunkCount)
	return err
}

// FakeKnowledgeStore is an in-memory KnowledgeStore for handler tests.
type FakeKnowledgeStore struct {
	mu        sync.RWMutex
	bases     map[uuid.UUID]models.KnowledgeBase
	documents map[uuid.UUID]map[uuid.UUID]models.KnowledgeDocument
}

func NewFakeKnowledgeStore() *FakeKnowledgeStore {
	return &FakeKnowledgeStore{
		bases:     map[uuid.UUID]models.KnowledgeBase{},
		documents: map[uuid.UUID]map[uuid.UUID]models.KnowledgeDocument{},
	}
}

func (s *FakeKnowledgeStore) ListKnowledgeBases(_ context.Context) ([]models.KnowledgeBase, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.KnowledgeBase, 0, len(s.bases))
	for _, kb := range s.bases {
		out = append(out, kb)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *FakeKnowledgeStore) CreateKnowledgeBase(_ context.Context, kb models.KnowledgeBase) (models.KnowledgeBase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensure()
	now := time.Now().UTC()
	if kb.ID == uuid.Nil {
		kb.ID = uuid.New()
	}
	kb.CreatedAt = now
	kb.UpdatedAt = now
	s.bases[kb.ID] = kb
	return kb, nil
}

func (s *FakeKnowledgeStore) GetKnowledgeBase(_ context.Context, id uuid.UUID) (*models.KnowledgeBase, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	kb, ok := s.bases[id]
	if !ok {
		return nil, nil
	}
	return &kb, nil
}

func (s *FakeKnowledgeStore) UpdateKnowledgeBase(_ context.Context, kb models.KnowledgeBase) (models.KnowledgeBase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.bases[kb.ID]
	if !ok {
		return models.KnowledgeBase{}, pgx.ErrNoRows
	}
	kb.CreatedAt = current.CreatedAt
	kb.DocumentCount = current.DocumentCount
	kb.ChunkCount = current.ChunkCount
	kb.UpdatedAt = time.Now().UTC()
	s.bases[kb.ID] = kb
	return kb, nil
}

func (s *FakeKnowledgeStore) DeleteKnowledgeBase(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bases, id)
	delete(s.documents, id)
	return nil
}

func (s *FakeKnowledgeStore) ListDocuments(_ context.Context, knowledgeBaseID uuid.UUID) ([]models.KnowledgeDocument, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	byKB := s.documents[knowledgeBaseID]
	out := make([]models.KnowledgeDocument, 0, len(byKB))
	for _, doc := range byKB {
		out = append(out, doc)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *FakeKnowledgeStore) CreateDocument(_ context.Context, doc models.KnowledgeDocument) (models.KnowledgeDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensure()
	kb, ok := s.bases[doc.KnowledgeBaseID]
	if !ok {
		return models.KnowledgeDocument{}, pgx.ErrNoRows
	}
	now := time.Now().UTC()
	if doc.ID == uuid.Nil {
		doc.ID = uuid.New()
	}
	doc.CreatedAt = now
	doc.UpdatedAt = now
	byKB := s.documents[doc.KnowledgeBaseID]
	if byKB == nil {
		byKB = map[uuid.UUID]models.KnowledgeDocument{}
		s.documents[doc.KnowledgeBaseID] = byKB
	}
	byKB[doc.ID] = doc
	kb.DocumentCount++
	kb.ChunkCount += int64(doc.ChunkCount)
	kb.UpdatedAt = now
	s.bases[kb.ID] = kb
	return doc, nil
}

func (s *FakeKnowledgeStore) GetDocument(_ context.Context, knowledgeBaseID, documentID uuid.UUID) (*models.KnowledgeDocument, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if byKB := s.documents[knowledgeBaseID]; byKB != nil {
		if doc, ok := byKB[documentID]; ok {
			return &doc, nil
		}
	}
	return nil, nil
}

func (s *FakeKnowledgeStore) DeleteDocument(_ context.Context, knowledgeBaseID, documentID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	byKB := s.documents[knowledgeBaseID]
	if byKB == nil {
		return nil
	}
	doc, ok := byKB[documentID]
	if !ok {
		return nil
	}
	delete(byKB, documentID)
	kb := s.bases[knowledgeBaseID]
	if kb.DocumentCount > 0 {
		kb.DocumentCount--
	}
	kb.ChunkCount -= int64(doc.ChunkCount)
	if kb.ChunkCount < 0 {
		kb.ChunkCount = 0
	}
	kb.UpdatedAt = time.Now().UTC()
	s.bases[knowledgeBaseID] = kb
	return nil
}

func (s *FakeKnowledgeStore) ensure() {
	if s.bases == nil {
		s.bases = map[uuid.UUID]models.KnowledgeBase{}
	}
	if s.documents == nil {
		s.documents = map[uuid.UUID]map[uuid.UUID]models.KnowledgeDocument{}
	}
}
