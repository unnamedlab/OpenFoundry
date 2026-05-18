package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	aikernel "github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/handlers"
)

type KnowledgeHandler struct{ inner *aikernel.KnowledgeHandlers }

func NewKnowledgeHandler(inner *aikernel.KnowledgeHandlers) *KnowledgeHandler {
	return &KnowledgeHandler{inner: inner}
}
func (h *KnowledgeHandler) Mount(r chi.Router) {
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Get("/{id}", h.Get)
	r.Patch("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	r.Get("/{id}/documents", h.ListDocuments)
	r.Post("/{id}/documents", h.CreateDocument)
	r.Get("/{id}/documents/{document_id}", h.GetDocument)
	r.Delete("/{id}/documents/{document_id}", h.DeleteDocument)
	r.Post("/{id}/search", h.Search)
}
func (h *KnowledgeHandler) List(w http.ResponseWriter, r *http.Request) {
	h.inner.ListKnowledgeBases(w, r)
}
func (h *KnowledgeHandler) Create(w http.ResponseWriter, r *http.Request) {
	h.inner.CreateKnowledgeBase(w, r)
}
func (h *KnowledgeHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if ok {
		h.inner.GetKnowledgeBase(w, r, id)
	}
}
func (h *KnowledgeHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if ok {
		h.inner.UpdateKnowledgeBase(w, r, id)
	}
}
func (h *KnowledgeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if ok {
		h.inner.DeleteKnowledgeBase(w, r, id)
	}
}
func (h *KnowledgeHandler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if ok {
		h.inner.ListDocuments(w, r, id)
	}
}
func (h *KnowledgeHandler) CreateDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if ok {
		h.inner.CreateDocument(w, r, id)
	}
}
func (h *KnowledgeHandler) GetDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	doc, ok := parseID(w, r, "document_id")
	if ok {
		h.inner.GetDocument(w, r, id, doc)
	}
}
func (h *KnowledgeHandler) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	doc, ok := parseID(w, r, "document_id")
	if ok {
		h.inner.DeleteDocument(w, r, id, doc)
	}
}
func (h *KnowledgeHandler) Search(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if ok {
		h.inner.SearchKnowledgeBase(w, r, id)
	}
}
func parseID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		http.Error(w, "invalid "+name, http.StatusBadRequest)
		return uuid.Nil, false
	}
	return id, true
}
