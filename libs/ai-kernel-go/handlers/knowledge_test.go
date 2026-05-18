package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/rag"
	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

func TestCreateKnowledgeBase_RejectsEmptyName(t *testing.T) {
	t.Parallel()
	h := &KnowledgeHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"   "}`))
	w := httptest.NewRecorder()
	h.CreateKnowledgeBase(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "knowledge base name is required", body.Error)
}

func TestCreateKnowledgeBase_RejectsBadJSON(t *testing.T) {
	t.Parallel()
	h := &KnowledgeHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	h.CreateKnowledgeBase(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSearchKnowledgeBase_RejectsEmptyQuery(t *testing.T) {
	t.Parallel()
	h := &KnowledgeHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"query":"  "}`))
	w := httptest.NewRecorder()
	h.SearchKnowledgeBase(w, req, uuid.New())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "search query is required", body.Error)
}

func TestSearchKnowledgeBase_RejectsBadJSON(t *testing.T) {
	t.Parallel()
	h := &KnowledgeHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	h.SearchKnowledgeBase(w, req, uuid.New())
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRawMessagePtrTreatsEmptyAsNil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, rawMessagePtr(nil))
	assert.Nil(t, rawMessagePtr(json.RawMessage("")))
	provided := json.RawMessage(`{"x":1}`)
	got := rawMessagePtr(provided)
	require.NotNil(t, got)
	assert.JSONEq(t, `{"x":1}`, string(*got))
}

func TestKnowledgeHandlers_CRUDSearchDeleteWithInjectedStores(t *testing.T) {
	t.Parallel()
	store := NewFakeKnowledgeStore()
	vectors := rag.NewFakeVectorStore()
	h := &KnowledgeHandlers{Store: store, VectorStore: vectors}

	createKBReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Ops KB","tags":["ops"]}`))
	createKBW := httptest.NewRecorder()
	h.CreateKnowledgeBase(createKBW, createKBReq)
	require.Equal(t, http.StatusOK, createKBW.Code)
	var kb models.KnowledgeBase
	require.NoError(t, json.NewDecoder(createKBW.Body).Decode(&kb))
	assert.Equal(t, "Ops KB", kb.Name)
	assert.Zero(t, kb.DocumentCount)
	assert.Zero(t, kb.ChunkCount)
	assert.Equal(t, models.DefaultEmbeddingProvider, kb.EmbeddingProvider)
	assert.Equal(t, models.DefaultChunkingStrategy, kb.ChunkingStrategy)

	listKBW := httptest.NewRecorder()
	h.ListKnowledgeBases(listKBW, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, listKBW.Code)
	var listKB models.ListKnowledgeBasesResponse
	require.NoError(t, json.NewDecoder(listKBW.Body).Decode(&listKB))
	require.Len(t, listKB.Data, 1)

	getKBW := httptest.NewRecorder()
	h.GetKnowledgeBase(getKBW, httptest.NewRequest(http.MethodGet, "/", nil), kb.ID)
	require.Equal(t, http.StatusOK, getKBW.Code)

	createDocReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"title":"Runbook","content":"Restart the foundry scheduler when queue lag grows.\n\nInspect workflow retries and task leases.","metadata":{"owner":"sre"}}`))
	createDocW := httptest.NewRecorder()
	h.CreateDocument(createDocW, createDocReq, kb.ID)
	require.Equal(t, http.StatusOK, createDocW.Code)
	var doc models.KnowledgeDocument
	require.NoError(t, json.NewDecoder(createDocW.Body).Decode(&doc))
	assert.Equal(t, "Runbook", doc.Title)
	assert.Equal(t, "indexed", doc.Status)
	require.NotEmpty(t, doc.Chunks)

	getKBAfterDocW := httptest.NewRecorder()
	h.GetKnowledgeBase(getKBAfterDocW, httptest.NewRequest(http.MethodGet, "/", nil), kb.ID)
	require.Equal(t, http.StatusOK, getKBAfterDocW.Code)
	var kbAfterDoc models.KnowledgeBase
	require.NoError(t, json.NewDecoder(getKBAfterDocW.Body).Decode(&kbAfterDoc))
	assert.Equal(t, int64(1), kbAfterDoc.DocumentCount)
	assert.Equal(t, int64(doc.ChunkCount), kbAfterDoc.ChunkCount)

	listDocsW := httptest.NewRecorder()
	h.ListDocuments(listDocsW, httptest.NewRequest(http.MethodGet, "/", nil), kb.ID)
	require.Equal(t, http.StatusOK, listDocsW.Code)
	var listDocs models.ListKnowledgeDocumentsResponse
	require.NoError(t, json.NewDecoder(listDocsW.Body).Decode(&listDocs))
	require.Len(t, listDocs.Data, 1)

	getDocW := httptest.NewRecorder()
	h.GetDocument(getDocW, httptest.NewRequest(http.MethodGet, "/", nil), kb.ID, doc.ID)
	require.Equal(t, http.StatusOK, getDocW.Code)

	searchW := httptest.NewRecorder()
	h.SearchKnowledgeBase(searchW, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"query":"scheduler queue lag","top_k":3,"min_score":0}`)), kb.ID)
	require.Equal(t, http.StatusOK, searchW.Code)
	var search models.SearchKnowledgeBaseResponse
	require.NoError(t, json.NewDecoder(searchW.Body).Decode(&search))
	require.NotEmpty(t, search.Results)
	assert.Equal(t, "vector-store", search.SearchProvider)
	assert.Equal(t, "vector", search.SearchMode)
	assert.Equal(t, doc.ID, search.Results[0].DocumentID)

	deleteDocW := httptest.NewRecorder()
	h.DeleteDocument(deleteDocW, httptest.NewRequest(http.MethodDelete, "/", nil), kb.ID, doc.ID)
	require.Equal(t, http.StatusOK, deleteDocW.Code)

	getKBAfterDeleteDocW := httptest.NewRecorder()
	h.GetKnowledgeBase(getKBAfterDeleteDocW, httptest.NewRequest(http.MethodGet, "/", nil), kb.ID)
	require.Equal(t, http.StatusOK, getKBAfterDeleteDocW.Code)
	var kbAfterDeleteDoc models.KnowledgeBase
	require.NoError(t, json.NewDecoder(getKBAfterDeleteDocW.Body).Decode(&kbAfterDeleteDoc))
	assert.Zero(t, kbAfterDeleteDoc.DocumentCount)
	assert.Zero(t, kbAfterDeleteDoc.ChunkCount)

	searchAfterDeleteW := httptest.NewRecorder()
	h.SearchKnowledgeBase(searchAfterDeleteW, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"query":"scheduler queue lag","min_score":0}`)), kb.ID)
	require.Equal(t, http.StatusOK, searchAfterDeleteW.Code)
	var searchAfterDelete models.SearchKnowledgeBaseResponse
	require.NoError(t, json.NewDecoder(searchAfterDeleteW.Body).Decode(&searchAfterDelete))
	assert.Empty(t, searchAfterDelete.Results)

	deleteKBW := httptest.NewRecorder()
	h.DeleteKnowledgeBase(deleteKBW, httptest.NewRequest(http.MethodDelete, "/", nil), kb.ID)
	require.Equal(t, http.StatusOK, deleteKBW.Code)

	missingKBW := httptest.NewRecorder()
	h.GetKnowledgeBase(missingKBW, httptest.NewRequest(http.MethodGet, "/", nil), kb.ID)
	assert.Equal(t, http.StatusNotFound, missingKBW.Code)
}

func TestCreateDocument_RejectsEmptyTitleOrContent(t *testing.T) {
	t.Parallel()
	store := NewFakeKnowledgeStore()
	kb, err := store.CreateKnowledgeBase(context.Background(), models.KnowledgeBase{
		ID:                uuid.New(),
		Name:              "KB",
		Status:            models.DefaultKnowledgeStatus,
		EmbeddingProvider: models.DefaultEmbeddingProvider,
		ChunkingStrategy:  models.DefaultChunkingStrategy,
	})
	require.NoError(t, err)
	h := &KnowledgeHandlers{Store: store}

	w := httptest.NewRecorder()
	h.CreateDocument(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"title":" ","content":"hello"}`)), kb.ID)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "document title and content are required", body.Error)
}

func TestSearchKnowledgeBase_DefaultsOnDecode(t *testing.T) {
	t.Parallel()
	var req models.SearchKnowledgeBaseRequest
	require.NoError(t, json.Unmarshal([]byte(`{"query":"hello"}`), &req))
	assert.Equal(t, models.DefaultSearchTopK, req.TopK)
	assert.Equal(t, models.DefaultSearchMinScore, req.MinScore)
}

func TestSearchKnowledgeBase_UsesPersistentDocumentFallbackWithoutVectorStore(t *testing.T) {
	t.Parallel()
	store := NewFakeKnowledgeStore()
	h := &KnowledgeHandlers{Store: store}

	kb, err := store.CreateKnowledgeBase(context.Background(), models.KnowledgeBase{
		ID:                uuid.New(),
		Name:              "Fallback KB",
		Status:            models.DefaultKnowledgeStatus,
		EmbeddingProvider: models.DefaultEmbeddingProvider,
		ChunkingStrategy:  models.DefaultChunkingStrategy,
	})
	require.NoError(t, err)

	createDocW := httptest.NewRecorder()
	h.CreateDocument(createDocW, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"title":"Scheduler","content":"The scheduler queue lag runbook restarts workers after checking leases."}`)), kb.ID)
	require.Equal(t, http.StatusOK, createDocW.Code)
	var doc models.KnowledgeDocument
	require.NoError(t, json.NewDecoder(createDocW.Body).Decode(&doc))

	searchW := httptest.NewRecorder()
	h.SearchKnowledgeBase(searchW, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"query":"queue lag leases","top_k":2,"min_score":0}`)), kb.ID)
	require.Equal(t, http.StatusOK, searchW.Code)
	var search models.SearchKnowledgeBaseResponse
	require.NoError(t, json.NewDecoder(searchW.Body).Decode(&search))
	require.NotEmpty(t, search.Results)
	assert.Equal(t, "persistent-documents", search.SearchProvider)
	assert.Equal(t, "deterministic-rag", search.SearchMode)
	assert.Equal(t, doc.ID, search.Results[0].DocumentID)
}
