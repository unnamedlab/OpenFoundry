package handlers

import (
	"encoding/json"
	"net/http"
)

// PromptsNotImplemented is the placeholder for `/api/v1/ai/prompts*`.
// ADR-0030 retired prompt-workflow-service into this binary; the prompt
// CRUD surface from libs/ai-kernel-go has not been wired into the
// agent-runtime HTTP router yet, so the edge-gateway gets a clean 501
// rather than the previous 502 (PROMPT_WORKFLOW_SERVICE_URL upstream
// was unreachable because the service no longer exists).
func (h *Handlers) PromptsNotImplemented(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotImplemented)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":    "not_implemented",
		"service": "agent-runtime-service",
		"adr":     "ADR-0030",
		"detail":  "prompt-workflow-service was retired; the consolidated prompt CRUD surface is not wired yet",
	})
}
