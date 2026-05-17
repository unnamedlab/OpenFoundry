package handler

import (
	"encoding/json"
	"net/http"
)

// NotImplemented returns a 501 with the canonical stub payload. Every
// gateway-mapped route lands here until the corresponding milestone in
// docs/migration/foundry-cipher-1to1-checklist.md ships a real handler.
//
// `milestone` should match the section of the checklist that owns the
// missing capability (e.g. "A" for the credible-field-encryption batch).
func NotImplemented(milestone string) http.HandlerFunc {
	body := map[string]string{
		"code":      "not_implemented",
		"service":   "cipher-service",
		"milestone": milestone,
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(body)
	}
}
