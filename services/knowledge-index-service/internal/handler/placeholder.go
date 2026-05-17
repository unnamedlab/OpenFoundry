package handler

import (
	"encoding/json"
	"net/http"
)

// NotImplemented returns a structured 501 body. The gateway routes
// /api/v1/ai/knowledge-bases* here; until the real implementation
// lands, every request gets this envelope so the frontend can branch
// on `code == "not_implemented"` instead of treating it as a 502.
func NotImplemented(serviceName, milestone string) http.HandlerFunc {
	type body struct {
		Code      string `json:"code"`
		Service   string `json:"service"`
		Milestone string `json:"milestone"`
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(body{
			Code:      "not_implemented",
			Service:   serviceName,
			Milestone: milestone,
		})
	}
}
