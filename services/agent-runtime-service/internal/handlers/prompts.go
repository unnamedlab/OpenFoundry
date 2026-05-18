package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	aikernel "github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/handlers"
)

func (h *Handlers) promptHandlers() *aikernel.PromptsHandlers {
	return &aikernel.PromptsHandlers{Pool: h.Repo.Pool}
}
func (h *Handlers) ListPrompts(w http.ResponseWriter, r *http.Request) {
	h.promptHandlers().ListPrompts(w, r)
}
func (h *Handlers) CreatePrompt(w http.ResponseWriter, r *http.Request) {
	h.promptHandlers().CreatePrompt(w, r)
}
func (h *Handlers) GetPrompt(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePromptID(w, r)
	if !ok {
		return
	}
	h.promptHandlers().GetPrompt(w, r, id)
}
func (h *Handlers) UpdatePrompt(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePromptID(w, r)
	if !ok {
		return
	}
	h.promptHandlers().UpdatePrompt(w, r, id)
}
func (h *Handlers) RenderPrompt(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePromptID(w, r)
	if !ok {
		return
	}
	h.promptHandlers().RenderPrompt(w, r, id)
}
func parsePromptID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid prompt id")
		return uuid.Nil, false
	}
	return id, true
}
