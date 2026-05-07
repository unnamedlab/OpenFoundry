package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
