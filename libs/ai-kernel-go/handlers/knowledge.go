package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/llm"
	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/rag"
	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

// KnowledgeHandlers exposes the RAG knowledge-base CRUD + document
// indexing + search surface. Mirrors libs/ai-kernel/src/handlers/
// knowledge.rs:
//   - GET    list_knowledge_bases
//   - POST   create_knowledge_base
//   - PATCH  update_knowledge_base
//   - GET    list_documents
//   - POST   create_document
//   - POST   search_knowledge_base
type KnowledgeHandlers struct {
	Pool        *pgxpool.Pool
	Store       KnowledgeStore
	VectorStore rag.VectorStore
}

const knowledgeBaseColumns = `id, name, description, status,
                              embedding_provider, chunking_strategy,
                              tags, document_count, chunk_count,
                              created_at, updated_at`

const knowledgeDocumentColumns = `id, knowledge_base_id, title, content,
                                  source_uri, metadata, status,
                                  chunk_count, chunks, created_at, updated_at`

const providerColumns = `id, name, provider_type, model_name, endpoint_url,
                         api_mode, credential_reference, enabled,
                         load_balance_weight, max_output_tokens,
                         cost_tier, tags, route_rules, health_state,
                         created_at, updated_at`

func scanKnowledgeBase(s toolScanner) (models.KnowledgeBase, error) {
	var kb models.KnowledgeBase
	var description, tagsRaw []byte
	if err := s.Scan(
		&kb.ID, &kb.Name, &description, &kb.Status,
		&kb.EmbeddingProvider, &kb.ChunkingStrategy,
		&tagsRaw, &kb.DocumentCount, &kb.ChunkCount,
		&kb.CreatedAt, &kb.UpdatedAt,
	); err != nil {
		return kb, err
	}
	kb.Description = string(description)
	if len(tagsRaw) > 0 {
		_ = json.Unmarshal(tagsRaw, &kb.Tags)
	}
	if kb.Tags == nil {
		kb.Tags = []string{}
	}
	return kb, nil
}

func scanKnowledgeDocument(s toolScanner) (models.KnowledgeDocument, error) {
	var d models.KnowledgeDocument
	var sourceURI *string
	var metadataRaw, chunksRaw []byte
	if err := s.Scan(
		&d.ID, &d.KnowledgeBaseID, &d.Title, &d.Content,
		&sourceURI, &metadataRaw, &d.Status,
		&d.ChunkCount, &chunksRaw, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return d, err
	}
	d.SourceURI = sourceURI
	if len(metadataRaw) > 0 {
		d.Metadata = metadataRaw
	} else {
		d.Metadata = json.RawMessage("{}")
	}
	if len(chunksRaw) > 0 {
		_ = json.Unmarshal(chunksRaw, &d.Chunks)
	}
	if d.Chunks == nil {
		d.Chunks = []models.KnowledgeChunk{}
	}
	return d, nil
}

func scanProvider(s toolScanner) (models.LlmProvider, error) {
	var p models.LlmProvider
	var credRef *string
	var tagsRaw, routeRulesRaw, healthRaw []byte
	if err := s.Scan(
		&p.ID, &p.Name, &p.ProviderType, &p.ModelName, &p.EndpointURL,
		&p.APIMode, &credRef, &p.Enabled,
		&p.LoadBalanceWeight, &p.MaxOutputTokens,
		&p.CostTier, &tagsRaw, &routeRulesRaw, &healthRaw,
		&p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return p, err
	}
	p.CredentialReference = credRef
	p.CredentialConfigured = credRef != nil && strings.TrimSpace(*credRef) != ""
	if len(tagsRaw) > 0 {
		_ = json.Unmarshal(tagsRaw, &p.Tags)
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if len(routeRulesRaw) > 0 {
		_ = json.Unmarshal(routeRulesRaw, &p.RouteRules)
	}
	if len(healthRaw) > 0 {
		_ = json.Unmarshal(healthRaw, &p.HealthState)
	}
	return p, nil
}

func (h *KnowledgeHandlers) loadKnowledgeBase(ctx context.Context, id uuid.UUID) (*models.KnowledgeBase, error) {
	return h.knowledgeStore().GetKnowledgeBase(ctx, id)
}

func (h *KnowledgeHandlers) loadProvider(ctx context.Context, id uuid.UUID) (*models.LlmProvider, error) {
	row := h.Pool.QueryRow(ctx,
		`SELECT `+providerColumns+` FROM ai_providers WHERE id = $1`, id)
	p, err := scanProvider(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// resolveEmbeddingProvider mirrors Rust resolve_embedding_provider:
// returns (nil, nil) if the reference is not "provider:<uuid>", or the
// LlmProvider row if it is, or (nil, error) shaped as 404 if the
// provider ID doesn't exist. The caller distinguishes the 404 case
// via the wroteResponse boolean to avoid double-writing.
func (h *KnowledgeHandlers) resolveEmbeddingProvider(w http.ResponseWriter, r *http.Request, providerReference string) (*models.LlmProvider, bool) {
	rest, ok := strings.CutPrefix(providerReference, "provider:")
	if !ok {
		return nil, false
	}
	providerID, err := uuid.Parse(rest)
	if err != nil {
		return nil, false
	}
	p, err := h.loadProvider(r.Context(), providerID)
	if err != nil {
		dbError(w, err)
		return nil, true
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "embedding provider not found")
		return nil, true
	}
	return p, false
}

// ListKnowledgeBases handles `GET /api/v1/knowledge-bases`.
func (h *KnowledgeHandlers) ListKnowledgeBases(w http.ResponseWriter, r *http.Request) {
	bases, err := h.knowledgeStore().ListKnowledgeBases(r.Context())
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, models.ListKnowledgeBasesResponse{Data: bases})
}

// CreateKnowledgeBase handles `POST /api/v1/knowledge-bases`.
func (h *KnowledgeHandlers) CreateKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	var body models.CreateKnowledgeBaseRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "knowledge base name is required")
		return
	}

	kb := models.KnowledgeBase{
		ID:                uuid.New(),
		Name:              strings.TrimSpace(body.Name),
		Description:       derefString(body.Description, ""),
		Status:            derefString(body.Status, models.DefaultKnowledgeStatus),
		EmbeddingProvider: derefString(body.EmbeddingProvider, models.DefaultEmbeddingProvider),
		ChunkingStrategy:  derefString(body.ChunkingStrategy, models.DefaultChunkingStrategy),
		Tags:              body.Tags,
	}
	if kb.Tags == nil {
		kb.Tags = []string{}
	}
	created, err := h.knowledgeStore().CreateKnowledgeBase(r.Context(), kb)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, created)
}

// GetKnowledgeBase handles `GET /api/v1/knowledge-bases/{id}`.
func (h *KnowledgeHandlers) GetKnowledgeBase(w http.ResponseWriter, r *http.Request, kbID uuid.UUID) {
	kb, err := h.knowledgeStore().GetKnowledgeBase(r.Context(), kbID)
	if err != nil {
		dbError(w, err)
		return
	}
	if kb == nil {
		writeError(w, http.StatusNotFound, "knowledge base not found")
		return
	}
	writeJSON(w, http.StatusOK, kb)
}

// UpdateKnowledgeBase handles `PATCH /api/v1/knowledge-bases/{id}`.
func (h *KnowledgeHandlers) UpdateKnowledgeBase(w http.ResponseWriter, r *http.Request, kbID uuid.UUID) {
	current, err := h.knowledgeStore().GetKnowledgeBase(r.Context(), kbID)
	if err != nil {
		dbError(w, err)
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, "knowledge base not found")
		return
	}

	var body models.UpdateKnowledgeBaseRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated := *current
	updated.Name = derefString(body.Name, current.Name)
	updated.Description = derefString(body.Description, current.Description)
	updated.Status = derefString(body.Status, current.Status)
	updated.EmbeddingProvider = derefString(body.EmbeddingProvider, current.EmbeddingProvider)
	updated.ChunkingStrategy = derefString(body.ChunkingStrategy, current.ChunkingStrategy)
	if body.Tags != nil {
		updated.Tags = *body.Tags
	}
	if updated.Tags == nil {
		updated.Tags = []string{}
	}

	kb, err := h.knowledgeStore().UpdateKnowledgeBase(r.Context(), updated)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, kb)
}

// DeleteKnowledgeBase handles `DELETE /api/v1/knowledge-bases/{id}`.
func (h *KnowledgeHandlers) DeleteKnowledgeBase(w http.ResponseWriter, r *http.Request, kbID uuid.UUID) {
	kb, err := h.knowledgeStore().GetKnowledgeBase(r.Context(), kbID)
	if err != nil {
		dbError(w, err)
		return
	}
	if kb == nil {
		writeError(w, http.StatusNotFound, "knowledge base not found")
		return
	}
	if err := h.knowledgeStore().DeleteKnowledgeBase(r.Context(), kbID); err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// ListDocuments handles `GET /api/v1/knowledge-bases/{id}/documents`.
func (h *KnowledgeHandlers) ListDocuments(w http.ResponseWriter, r *http.Request, kbID uuid.UUID) {
	kb, err := h.knowledgeStore().GetKnowledgeBase(r.Context(), kbID)
	if err != nil {
		dbError(w, err)
		return
	}
	if kb == nil {
		writeError(w, http.StatusNotFound, "knowledge base not found")
		return
	}
	docs, err := h.knowledgeStore().ListDocuments(r.Context(), kbID)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, models.ListKnowledgeDocumentsResponse{Data: docs})
}

// GetDocument handles `GET /api/v1/knowledge-bases/{id}/documents/{document_id}`.
func (h *KnowledgeHandlers) GetDocument(w http.ResponseWriter, r *http.Request, kbID, documentID uuid.UUID) {
	doc, err := h.knowledgeStore().GetDocument(r.Context(), kbID, documentID)
	if err != nil {
		dbError(w, err)
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, "knowledge document not found")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// CreateDocument handles `POST /api/v1/knowledge-bases/{id}/documents`.
// Validates title + content, resolves the optional embedding provider,
// chunks the content, embeds each chunk, persists the document, and indexes
// it in the optional VectorStore.
func (h *KnowledgeHandlers) CreateDocument(w http.ResponseWriter, r *http.Request, kbID uuid.UUID) {
	kb, err := h.knowledgeStore().GetKnowledgeBase(r.Context(), kbID)
	if err != nil {
		dbError(w, err)
		return
	}
	if kb == nil {
		writeError(w, http.StatusNotFound, "knowledge base not found")
		return
	}

	var body models.CreateKnowledgeDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Title) == "" || strings.TrimSpace(body.Content) == "" {
		writeError(w, http.StatusBadRequest, "document title and content are required")
		return
	}

	provider, wrote := h.resolveEmbeddingProvider(w, r, kb.EmbeddingProvider)
	if wrote {
		return
	}

	documentID := uuid.New()
	chunks, err := h.indexChunks(r.Context(), documentID, *kb, body.Content, provider)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if chunks == nil {
		chunks = []models.KnowledgeChunk{}
	}

	doc := models.KnowledgeDocument{
		ID:              documentID,
		KnowledgeBaseID: kbID,
		Title:           strings.TrimSpace(body.Title),
		Content:         body.Content,
		SourceURI:       body.SourceURI,
		Metadata:        jsonOrEmptyObject(rawMessagePtr(body.Metadata)),
		Status:          "indexed",
		ChunkCount:      int32(len(chunks)),
		Chunks:          chunks,
	}
	created, err := h.knowledgeStore().CreateDocument(r.Context(), doc)
	if err != nil {
		dbError(w, err)
		return
	}
	if h.VectorStore != nil {
		if err := h.VectorStore.UpsertDocument(r.Context(), created); err != nil {
			dbError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, created)
}

// DeleteDocument handles `DELETE /api/v1/knowledge-bases/{id}/documents/{document_id}`.
func (h *KnowledgeHandlers) DeleteDocument(w http.ResponseWriter, r *http.Request, kbID, documentID uuid.UUID) {
	doc, err := h.knowledgeStore().GetDocument(r.Context(), kbID, documentID)
	if err != nil {
		dbError(w, err)
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, "knowledge document not found")
		return
	}
	if err := h.knowledgeStore().DeleteDocument(r.Context(), kbID, documentID); err != nil {
		dbError(w, err)
		return
	}
	if h.VectorStore != nil {
		if err := h.VectorStore.DeleteDocument(r.Context(), kbID, documentID); err != nil {
			dbError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// SearchKnowledgeBase handles `POST /api/v1/knowledge-bases/{id}/search`.
// Embeds the query (via the resolved provider or the offline embedder) and
// retrieves from the injectable VectorStore when configured, otherwise from
// documents persisted in the KnowledgeStore.
func (h *KnowledgeHandlers) SearchKnowledgeBase(w http.ResponseWriter, r *http.Request, kbID uuid.UUID) {
	var body models.SearchKnowledgeBaseRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Query) == "" {
		writeError(w, http.StatusBadRequest, "search query is required")
		return
	}

	kb, err := h.knowledgeStore().GetKnowledgeBase(r.Context(), kbID)
	if err != nil {
		dbError(w, err)
		return
	}
	if kb == nil {
		writeError(w, http.StatusNotFound, "knowledge base not found")
		return
	}

	provider, wrote := h.resolveEmbeddingProvider(w, r, kb.EmbeddingProvider)
	if wrote {
		return
	}
	queryEmbedding, err := h.queryEmbedding(r.Context(), body.Query, provider)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var results []models.KnowledgeSearchResult
	if h.VectorStore != nil {
		results, err = h.VectorStore.Search(r.Context(), kbID, queryEmbedding, body.TopK, body.MinScore)
	} else {
		var docs []models.KnowledgeDocument
		docs, err = h.knowledgeStore().ListDocuments(r.Context(), kbID)
		if err == nil {
			results = rag.SearchWithEmbedding(queryEmbedding, docs, body.TopK, body.MinScore)
		}
	}
	if err != nil {
		dbError(w, err)
		return
	}
	if results == nil {
		results = []models.KnowledgeSearchResult{}
	}

	writeJSON(w, http.StatusOK, models.SearchKnowledgeBaseResponse{
		KnowledgeBaseID: kbID,
		Query:           body.Query,
		Results:         results,
		RetrievedAt:     time.Now().UTC(),
	})
}

func (h *KnowledgeHandlers) indexChunks(ctx context.Context, documentID uuid.UUID, kb models.KnowledgeBase, content string, provider *models.LlmProvider) ([]models.KnowledgeChunk, error) {
	if provider == nil {
		return rag.IndexDocument(documentID, content, kb.ChunkingStrategy), nil
	}
	maxChars := 520
	if kb.ChunkingStrategy == "fine" {
		maxChars = 320
	}
	metadata, _ := json.Marshal(map[string]string{
		"strategy":           kb.ChunkingStrategy,
		"embedding_provider": kb.EmbeddingProvider,
	})
	chunks := make([]models.KnowledgeChunk, 0)
	for _, c := range rag.ChunkText(content, maxChars) {
		emb, err := llm.EmbedText(ctx, provider, c.Text)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, models.KnowledgeChunk{
			ID:         fmt.Sprintf("%s-%d", documentID, c.Position),
			Position:   c.Position,
			Text:       c.Text,
			TokenCount: int32(len(strings.Fields(c.Text))),
			Embedding:  emb,
			Metadata:   metadata,
		})
	}
	return chunks, nil
}

func (h *KnowledgeHandlers) queryEmbedding(ctx context.Context, query string, provider *models.LlmProvider) ([]float32, error) {
	if provider != nil {
		return llm.EmbedText(ctx, provider, query)
	}
	return rag.EmbedText(query), nil
}

func rawMessagePtr(m json.RawMessage) *json.RawMessage {
	if len(m) == 0 {
		return nil
	}
	return &m
}
