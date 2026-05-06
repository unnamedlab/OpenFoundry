// Package runtime hosts the REST runtime for media-transform-runtime-service.
//
// Wire-format invariants ported verbatim from Rust:
//   - Request: {kind, mime_type, schema, params, bytes_base64}
//   - Response: {status, kind, output_mime_type, compute_seconds,
//     output_bytes_base64?, output_json?, reason?}
//   - status enum: SCREAMING_SNAKE_CASE — "OK" | "NOT_IMPLEMENTED"
//   - Error envelope: {error, code} with codes:
//     MEDIA_TRANSFORM_UNKNOWN_KIND (400)
//     MEDIA_TRANSFORM_BAD_INPUT    (400)
//     MEDIA_TRANSFORM_HANDLER_ERROR (500)
//
// Foundation slice: every catalog entry returns 501 NOT_IMPLEMENTED
// (the Go-side native handlers land in a follow-up slice; the
// goNativePending reason is propagated to the response so callers
// can degrade. See internal/catalog/catalog.go).
package runtime

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/openfoundry/openfoundry-go/services/media-transform-runtime-service/internal/catalog"
)

// TransformInput is the POST /transform body.
type TransformInput struct {
	Kind        string          `json:"kind"`
	MimeType    string          `json:"mime_type"`
	Schema      string          `json:"schema"`
	Params      json.RawMessage `json:"params,omitempty"`
	BytesBase64 string          `json:"bytes_base64"`
}

// TransformStatus mirrors the SCREAMING_SNAKE_CASE Rust enum.
type TransformStatus string

const (
	StatusOK             TransformStatus = "OK"
	StatusNotImplemented TransformStatus = "NOT_IMPLEMENTED"
)

// TransformOutput is the POST /transform response body.
type TransformOutput struct {
	Status            TransformStatus `json:"status"`
	Kind              string          `json:"kind"`
	OutputMimeType    string          `json:"output_mime_type"`
	ComputeSeconds    uint64          `json:"compute_seconds"`
	OutputBytesBase64 *string         `json:"output_bytes_base64,omitempty"`
	OutputJSON        any             `json:"output_json,omitempty"`
	Reason            *string         `json:"reason,omitempty"`
}

// RuntimeError code constants — pinned by tests so callers can
// switch on them without parsing the english error text.
const (
	CodeUnknownKind   = "MEDIA_TRANSFORM_UNKNOWN_KIND"
	CodeBadInput      = "MEDIA_TRANSFORM_BAD_INPUT"
	CodeHandlerError  = "MEDIA_TRANSFORM_HANDLER_ERROR"
)

// HealthzHandler matches the Rust plain-text "ok" body.
func HealthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

// ListCatalogHandler returns the full catalog as JSON.
func ListCatalogHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, catalog.Catalog)
}

// CatalogEntryHandler returns one entry by key, or 400 with
// MEDIA_TRANSFORM_UNKNOWN_KIND when the key is absent.
func CatalogEntryHandler(getKind func(*http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		kind := getKind(r)
		for _, e := range catalog.Catalog {
			if e.Key == kind {
				writeJSON(w, http.StatusOK, e)
				return
			}
		}
		writeError(w, http.StatusBadRequest, CodeUnknownKind, "unknown transformation kind `"+kind+"`")
	}
}

// TransformHandler routes by catalog status. NotImplemented and
// External both surface as 200 OK with status="NOT_IMPLEMENTED" +
// reason — matching Rust so callers degrade gracefully on the
// envelope rather than parsing 501 bodies.
func TransformHandler(w http.ResponseWriter, r *http.Request) {
	var body TransformInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadInput, "request body is not valid JSON: "+err.Error())
		return
	}

	status, ok := catalog.Lookup(body.Kind)
	if !ok {
		writeError(w, http.StatusBadRequest, CodeUnknownKind, "unknown transformation kind `"+body.Kind+"`")
		return
	}

	switch status.Kind {
	case catalog.StatusNotImplemented:
		reason := status.Reason
		writeJSON(w, http.StatusOK, TransformOutput{
			Status:         StatusNotImplemented,
			Kind:           body.Kind,
			OutputMimeType: body.MimeType,
			ComputeSeconds: 0,
			Reason:         &reason,
		})
	case catalog.StatusExternal:
		reason := "external binary `" + status.Binary + "` is not wired yet — handler will land in a follow-up PR"
		writeJSON(w, http.StatusOK, TransformOutput{
			Status:         StatusNotImplemented,
			Kind:           body.Kind,
			OutputMimeType: body.MimeType,
			ComputeSeconds: 0,
			Reason:         &reason,
		})
	case catalog.StatusNative:
		// Foundation: no native handlers are wired yet. Once the
		// follow-up slice lands, dispatch into handlers.Dispatch
		// here and return StatusOK with the encoded output bytes.
		// For now: defensive 501 (this branch is unreachable while
		// the catalog has zero Native entries; kept for the slice
		// that flips them).
		if _, err := decodeBase64(body.BytesBase64); err != nil {
			writeError(w, http.StatusBadRequest, CodeBadInput, "base64 decode failed: "+err.Error())
			return
		}
		reason := "Go native handler lands in follow-up slice (golang.org/x/image port)"
		writeJSON(w, http.StatusOK, TransformOutput{
			Status:         StatusNotImplemented,
			Kind:           body.Kind,
			OutputMimeType: body.MimeType,
			ComputeSeconds: 0,
			Reason:         &reason,
		})
	}
}

// --- helpers ------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": message,
		"code":  code,
	})
}

// decodeBase64 is exposed so tests can pin the codec to stdlib
// behaviour. The Rust runtime hand-rolled a stripped-down codec for
// dep-graph reasons; on the Go side we simply use stdlib base64.
func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// EncodeBase64 is the symmetric helper the native-handler slice will
// use to base64-encode the output bytes.
func EncodeBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
