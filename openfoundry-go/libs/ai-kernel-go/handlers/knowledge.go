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
	Pool *pgxpool.Pool
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
	row := h.Pool.QueryRow(ctx,
		`SELECT `+knowledgeBaseColumns+` FROM ai_knowledge_bases WHERE id = $1`, id)
	kb, err := scanKnowledgeBase(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &kb, nil
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
	rows, err := h.Pool.Query(r.Context(),
		`SELECT `+knowledgeBaseColumns+` FROM ai_knowledge_bases
          ORDER BY updated_at DESC, created_at DESC`)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	out := make([]models.KnowledgeBase, 0)
	for rows.Next() {
		kb, err := scanKnowledgeBase(rows)
		if err != nil {
			dbError(w, err)
			return
		}
		out = append(out, kb)
	}
	writeJSON(w, http.StatusOK, models.ListKnowledgeBasesResponse{Data: out})
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

	description := derefString(body.Description, "")
	status := derefString(body.Status, models.DefaultKnowledgeStatus)
	embeddingProvider := derefString(body.EmbeddingProvider, models.DefaultEmbeddingProvider)
	chunkingStrategy := derefString(body.ChunkingStrategy, models.DefaultChunkingStrategy)
	tags := body.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, _ := json.Marshal(tags)

	row := h.Pool.QueryRow(r.Context(),
		`INSERT INTO ai_knowledge_bases
              (id, name, description, status, embedding_provider,
               chunking_strategy, tags, document_count, chunk_count)
            VALUES ($1, $2, $3, $4, $5, $6, $7, 0, 0)
            RETURNING `+knowledgeBaseColumns,
		uuid.New(), strings.TrimSpace(body.Name), description, status,
		embeddingProvider, chunkingStrategy, tagsJSON)
	kb, err := scanKnowledgeBase(row)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, kb)
}

// UpdateKnowledgeBase handles `PATCH /api/v1/knowledge-bases/{id}`.
func (h *KnowledgeHandlers) UpdateKnowledgeBase(w http.ResponseWriter, r *http.Request, kbID uuid.UUID) {
	current, err := h.loadKnowledgeBase(r.Context(), kbID)
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

	name := derefString(body.Name, current.Name)
	desc := derefString(body.Description, current.Description)
	status := derefString(body.Status, current.Status)
	embeddingProvider := derefString(body.EmbeddingProvider, current.EmbeddingProvider)
	chunkingStrategy := derefString(body.ChunkingStrategy, current.ChunkingStrategy)
	tags := current.Tags
	if body.Tags != nil {
		tags = *body.Tags
	}
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, _ := json.Marshal(tags)

	row := h.Pool.QueryRow(r.Context(),
		`UPDATE ai_knowledge_bases SET
            name = $2, description = $3, status = $4,
            embedding_provider = $5, chunking_strategy = $6, tags = $7,
            updated_at = NOW()
          WHERE id = $1
          RETURNING `+knowledgeBaseColumns,
		kbID, name, desc, status, embeddingProvider, chunkingStrategy, tagsJSON)
	kb, err := scanKnowledgeBase(row)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, kb)
}

// ListDocuments handles `GET /api/v1/knowledge-bases/{id}/documents`.
func (h *KnowledgeHandlers) ListDocuments(w http.ResponseWriter, r *http.Request, kbID uuid.UUID) {
	kb, err := h.loadKnowledgeBase(r.Context(), kbID)
	if err != nil {
		dbError(w, err)
		return
	}
	if kb == nil {
		writeError(w, http.StatusNotFound, "knowledge base not found")
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT `+knowledgeDocumentColumns+` FROM ai_knowledge_documents
          WHERE knowledge_base_id = $1
          ORDER BY updated_at DESC, created_at DESC`, kbID)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	out := make([]models.KnowledgeDocument, 0)
	for rows.Next() {
		d, err := scanKnowledgeDocument(rows)
		if err != nil {
			dbError(w, err)
			return
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, models.ListKnowledgeDocumentsResponse{Data: out})
}

// CreateDocument handles `POST /api/v1/knowledge-bases/{id}/documents`.
// Validates title + content, resolves the optional embedding provider,
// chunks the content, embeds each chunk, and stores the document.
// Bumps the parent KB's document_count + chunk_count atomically.
func (h *KnowledgeHandlers) CreateDocument(w http.ResponseWriter, r *http.Request, kbID uuid.UUID) {
	kb, err := h.loadKnowledgeBase(r.Context(), kbID)
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
	var chunks []models.KnowledgeChunk
	if provider != nil {
		maxChars := 520
		if kb.ChunkingStrategy == "fine" {
			maxChars = 320
		}
		metadata, _ := json.Marshal(map[string]string{
			"strategy":           kb.ChunkingStrategy,
			"embedding_provider": kb.EmbeddingProvider,
		})
		for _, c := range rag.ChunkText(body.Content, maxChars) {
			emb, err := llm.EmbedText(r.Context(), provider, c.Text)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
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
	} else {
		chunks = rag.IndexDocument(documentID, body.Content, kb.ChunkingStrategy)
	}
	if chunks == nil {
		chunks = []models.KnowledgeChunk{}
	}

	metadataJSON := jsonOrEmptyObject(rawMessagePtr(body.Metadata))
	chunksJSON, _ := json.Marshal(chunks)

	row := h.Pool.QueryRow(r.Context(),
		`INSERT INTO ai_knowledge_documents
              (id, knowledge_base_id, title, content, source_uri,
               metadata, status, chunk_count, chunks)
            VALUES ($1, $2, $3, $4, $5, $6, 'indexed', $7, $8)
            RETURNING `+knowledgeDocumentColumns,
		documentID, kbID, strings.TrimSpace(body.Title), body.Content,
		body.SourceURI, metadataJSON, int32(len(chunks)), chunksJSON)
	doc, err := scanKnowledgeDocument(row)
	if err != nil {
		dbError(w, err)
		return
	}

	if _, err := h.Pool.Exec(r.Context(),
		`UPDATE ai_knowledge_bases
            SET document_count = document_count + 1,
                chunk_count    = chunk_count + $2,
                updated_at     = NOW()
          WHERE id = $1`, kbID, int64(len(chunks))); err != nil {
		dbError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, doc)
}

// SearchKnowledgeBase handles `POST /api/v1/knowledge-bases/{id}/search`.
// Embeds the query (via the resolved provider or the offline embedder)
// and runs SearchWithEmbedding over all the KB's documents.
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

	kb, err := h.loadKnowledgeBase(r.Context(), kbID)
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

	rows, err := h.Pool.Query(r.Context(),
		`SELECT `+knowledgeDocumentColumns+` FROM ai_knowledge_documents
          WHERE knowledge_base_id = $1
          ORDER BY updated_at DESC, created_at DESC`, kbID)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	docs := make([]models.KnowledgeDocument, 0)
	for rows.Next() {
		d, err := scanKnowledgeDocument(rows)
		if err != nil {
			dbError(w, err)
			return
		}
		docs = append(docs, d)
	}

	var queryEmbedding []float32
	if provider != nil {
		queryEmbedding, err = llm.EmbedText(r.Context(), provider, body.Query)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		queryEmbedding = rag.EmbedText(body.Query)
	}
	results := rag.SearchWithEmbedding(queryEmbedding, docs, body.TopK, body.MinScore)
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

func rawMessagePtr(m json.RawMessage) *json.RawMessage {
	if len(m) == 0 {
		return nil
	}
	return &m
}
