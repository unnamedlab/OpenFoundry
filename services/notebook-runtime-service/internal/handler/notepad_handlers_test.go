package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/notebook-runtime-service/internal/models"
	nbrepo "github.com/openfoundry/openfoundry-go/services/notebook-runtime-service/internal/repo"
)

func mountNotepadRouter(s *State) chi.Router {
	r := chi.NewRouter()
	r.Get("/api/v1/notepad/documents", s.ListDocuments)
	r.Post("/api/v1/notepad/documents", s.CreateDocument)
	r.Get("/api/v1/notepad/documents/{document_id}", s.GetDocument)
	r.Patch("/api/v1/notepad/documents/{document_id}", s.UpdateDocument)
	r.Delete("/api/v1/notepad/documents/{document_id}", s.DeleteDocument)
	r.Get("/api/v1/notepad/documents/{document_id}/presence", s.ListPresence)
	r.Post("/api/v1/notepad/documents/{document_id}/presence", s.UpsertPresence)
	r.Post("/api/v1/notepad/documents/{document_id}/export", s.ExportDocument)
	return r
}

func newNotepadTestState() *State {
	return &State{NotepadRepo: nbrepo.NewInMemoryNotepadRepository()}
}

func TestNotepadDocumentCRUDComplete(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	state := newNotepadTestState()
	r := mountNotepadRouter(state)

	createBody := []byte(`{"title":"  Launch Plan  ","description":"Roadmap","content":"# Hello","template_key":" plan ","widgets":[{"title":"Chart"}]}`)
	w := httptest.NewRecorder()
	req := withClaims(httptest.NewRequest(http.MethodPost, "/api/v1/notepad/documents", bytes.NewReader(createBody)), owner)
	req.ContentLength = int64(len(createBody))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var created models.NotepadDocument
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("create json: %v", err)
	}
	if created.Title != "Launch Plan" || created.Description != "Roadmap" || created.OwnerID != owner || created.TemplateKey == nil || *created.TemplateKey != "plan" {
		t.Fatalf("created drift: %+v", created)
	}

	w = httptest.NewRecorder()
	req = withClaims(httptest.NewRequest(http.MethodGet, "/api/v1/notepad/documents?search=launch", nil), owner)
	r.ServeHTTP(w, req)
	var list struct {
		Data  []models.NotepadDocument `json:"data"`
		Total int64                    `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("list json: %v", err)
	}
	if w.Code != http.StatusOK || list.Total != 1 || len(list.Data) != 1 || list.Data[0].ID != created.ID {
		t.Fatalf("list drift status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = withClaims(httptest.NewRequest(http.MethodGet, "/api/v1/notepad/documents/"+created.ID.String(), nil), owner)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", w.Code, w.Body.String())
	}

	updateBody := []byte(`{"title":"Updated","content":"updated body","widgets":[{"kind":"metric"}]}`)
	w = httptest.NewRecorder()
	req = withClaims(httptest.NewRequest(http.MethodPatch, "/api/v1/notepad/documents/"+created.ID.String(), bytes.NewReader(updateBody)), owner)
	req.ContentLength = int64(len(updateBody))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", w.Code, w.Body.String())
	}
	var updated models.NotepadDocument
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if updated.Title != "Updated" || updated.Content != "updated body" || !strings.Contains(string(updated.Widgets), "metric") {
		t.Fatalf("updated drift: %+v widgets=%s", updated, updated.Widgets)
	}

	w = httptest.NewRecorder()
	req = withClaims(httptest.NewRequest(http.MethodDelete, "/api/v1/notepad/documents/"+created.ID.String(), nil), owner)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = withClaims(httptest.NewRequest(http.MethodGet, "/api/v1/notepad/documents/"+created.ID.String(), nil), owner)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("post-delete get status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestNotepadAuthAndOwnerChecks(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	other := uuid.New()
	state := newNotepadTestState()
	r := mountNotepadRouter(state)

	body := []byte(`{"title":"Private"}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notepad/documents", bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = withClaims(httptest.NewRequest(http.MethodPost, "/api/v1/notepad/documents", bytes.NewReader(body)), owner)
	req.ContentLength = int64(len(body))
	r.ServeHTTP(w, req)
	var created models.NotepadDocument
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	w = httptest.NewRecorder()
	req = withClaims(httptest.NewRequest(http.MethodGet, "/api/v1/notepad/documents/"+created.ID.String(), nil), other)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner get status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = withClaims(httptest.NewRequest(http.MethodPatch, "/api/v1/notepad/documents/"+created.ID.String(), bytes.NewReader([]byte(`{"title":"stolen"}`))), other)
	req.ContentLength = int64(len(`{"title":"stolen"}`))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner update status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestNotepadPresenceUpsertAndList(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	state := newNotepadTestState()
	r := mountNotepadRouter(state)

	createBody := []byte(`{"title":"Presence"}`)
	w := httptest.NewRecorder()
	req := withClaims(httptest.NewRequest(http.MethodPost, "/api/v1/notepad/documents", bytes.NewReader(createBody)), owner)
	req.ContentLength = int64(len(createBody))
	r.ServeHTTP(w, req)
	var doc models.NotepadDocument
	_ = json.Unmarshal(w.Body.Bytes(), &doc)

	presenceBody := []byte(`{"session_id":"s1","display_name":" Alice ","cursor_label":"L4","color":"#111111"}`)
	w = httptest.NewRecorder()
	req = withClaims(httptest.NewRequest(http.MethodPost, "/api/v1/notepad/documents/"+doc.ID.String()+"/presence", bytes.NewReader(presenceBody)), owner)
	req.ContentLength = int64(len(presenceBody))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("presence status=%d body=%s", w.Code, w.Body.String())
	}
	var presence models.NotepadPresence
	_ = json.Unmarshal(w.Body.Bytes(), &presence)
	if presence.SessionID != "s1" || presence.DisplayName != "Alice" || presence.CursorLabel != "L4" || presence.Color != "#111111" || presence.UserID != owner {
		t.Fatalf("presence drift: %+v", presence)
	}

	presenceBody = []byte(`{"session_id":"s1","display_name":"Alice 2"}`)
	w = httptest.NewRecorder()
	req = withClaims(httptest.NewRequest(http.MethodPost, "/api/v1/notepad/documents/"+doc.ID.String()+"/presence", bytes.NewReader(presenceBody)), owner)
	req.ContentLength = int64(len(presenceBody))
	r.ServeHTTP(w, req)
	var updated models.NotepadPresence
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if updated.ID != presence.ID || updated.DisplayName != "Alice 2" || updated.Color != "#0f766e" {
		t.Fatalf("upsert drift: before=%+v after=%+v", presence, updated)
	}

	w = httptest.NewRecorder()
	req = withClaims(httptest.NewRequest(http.MethodGet, "/api/v1/notepad/documents/"+doc.ID.String()+"/presence", nil), owner)
	r.ServeHTTP(w, req)
	var list struct {
		Data []models.NotepadPresence `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if w.Code != http.StatusOK || len(list.Data) != 1 || list.Data[0].ID != presence.ID {
		t.Fatalf("presence list drift status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestExportDocumentUsesPersistedDocument(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	state := newNotepadTestState()
	r := mountNotepadRouter(state)

	createBody := []byte(`{"title":"Export Me","description":"Desc","content":"# Heading\nBody"}`)
	w := httptest.NewRecorder()
	req := withClaims(httptest.NewRequest(http.MethodPost, "/api/v1/notepad/documents", bytes.NewReader(createBody)), owner)
	req.ContentLength = int64(len(createBody))
	r.ServeHTTP(w, req)
	var doc models.NotepadDocument
	_ = json.Unmarshal(w.Body.Bytes(), &doc)

	w = httptest.NewRecorder()
	req = withClaims(httptest.NewRequest(http.MethodPost, "/api/v1/notepad/documents/"+doc.ID.String()+"/export", nil), owner)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", w.Code, w.Body.String())
	}
	var payload models.NotepadExportPayload
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("export json: %v", err)
	}
	if payload.FileName != "export-me.html" || !strings.Contains(payload.HTML, "<h1>Heading</h1>") || payload.PreviewExcerpt != "# Heading" {
		t.Fatalf("export drift: %+v", payload)
	}
}
