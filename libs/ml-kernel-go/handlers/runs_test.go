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

func TestUpdateRun_RejectsBadJSON(t *testing.T) {
	t.Parallel()
	// loadRun panics on nil pool, so this only exercises the path
	// when the JSON decode is reached. We rely on recover() because
	// the loadRun call happens first; the body parse happens after.
	// To exercise just the JSON-error path we'd need pgxmock — skip.
	t.Skip("requires pgxmock; covered by integration tests")
}

func TestCreateRun_RejectsBadJSON(t *testing.T) {
	t.Parallel()
	h := &ExperimentsHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	h.CreateRun(w, req, uuid.New())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.NotEmpty(t, body.Error)
}

func TestCreateRun_RejectsEmptyNameStrict(t *testing.T) {
	t.Parallel()
	h := &ExperimentsHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":""}`))
	w := httptest.NewRecorder()
	h.CreateRun(w, req, uuid.New())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "run name is required", body.Error)
}
