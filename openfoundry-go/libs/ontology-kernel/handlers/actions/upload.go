// UploadActionAttachment — full 1:1 port. Mirrors `pub async fn
// upload_action_attachment` and the `UploadAttachmentRequest` /
// scale-limit cap from the Rust source.
package actions

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
)

// MaxEditBytes mirrors `scale_limits::MAX_EDIT_BYTES` — the per-edit
// ceiling documented in `Scale and property limits.md` (3 MB).
const MaxEditBytes = 3 * 1024 * 1024

// UploadAttachmentRequest mirrors `struct UploadAttachmentRequest`.
type UploadAttachmentRequest struct {
	Filename      string  `json:"filename"`
	ContentType   *string `json:"content_type,omitempty"`
	SizeBytes     uint64  `json:"size_bytes"`
	ContentBase64 *string `json:"content_base64,omitempty"`
}

// UploadActionAttachment mirrors `pub async fn upload_action_attachment`.
// Generates an opaque attachment_rid that the action parameters of
// type `attachment` / `media_reference` thread through; full Foundry
// parity (chunked uploads, signed URLs, scans) is delegated to
// `data-asset-catalog-service`.
func UploadActionAttachment(_ *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body UploadAttachmentRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			invalid(w, "invalid request body")
			return
		}
		if body.Filename == "" || trimSpace(body.Filename) == "" {
			invalid(w, "filename is required")
			return
		}
		if body.SizeBytes == 0 {
			invalid(w, "size_bytes must be greater than zero")
			return
		}
		if body.SizeBytes > MaxEditBytes {
			scaleLimitResponse(w, fmt.Sprintf(
				"attachment of %d bytes exceeds the per-edit scale limit (%d bytes)",
				body.SizeBytes, MaxEditBytes,
			))
			return
		}
		if body.ContentBase64 != nil {
			// Rough sanity check: base64 expands by 4/3, reject obvious
			// overruns without performing the decode (mirrors Rust).
			if (len(*body.ContentBase64)/4)*3 > MaxEditBytes {
				scaleLimitResponse(w, "inline attachment payload exceeds the per-edit limit")
				return
			}
		}

		id, _ := uuid.NewV7()
		rid := "ri.attachments." + id.String()
		writeJSON(w, http.StatusOK, map[string]any{
			"attachment_rid": rid,
			"filename":       body.Filename,
			"content_type":   body.ContentType,
			"size_bytes":     body.SizeBytes,
			"storage_uri":    "attachments://" + rid,
		})
	}
}
