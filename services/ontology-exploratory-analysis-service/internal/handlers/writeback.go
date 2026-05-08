// Writeback proposal handler. Mirrors `pub async fn propose_writeback`
// in src/handlers.rs: a POST appends a single entry to the
// ActionLogStore under the `exploratory.writeback_proposed` kind and
// returns the materialised proposal with status `"pending"`.

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	storageabstraction "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
	"github.com/openfoundry/openfoundry-go/services/ontology-exploratory-analysis-service/internal/models"
)

// MountWriteback attaches the writeback proposal route. Mirrors the
// Rust `propose_writeback` handler. Must only be called once h.Actions
// is non-nil — Rust AppState always carries an ActionLogStore.
func (h *Handlers) MountWriteback(r chi.Router) {
	if h.Actions == nil {
		panic("handlers.MountWriteback: Actions is nil")
	}
	r.Post("/api/v1/writeback", h.ProposeWriteback)
}

// ProposeWriteback mirrors Rust `pub async fn propose_writeback`.
func (h *Handlers) ProposeWriteback(w http.ResponseWriter, r *http.Request) {
	var body models.WritebackProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		plainText(w, http.StatusBadRequest, err.Error())
		return
	}

	id, err := uuid.NewV7()
	if err != nil {
		plainText(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := h.nowMs()
	proposal := models.WritebackProposal{
		ID:         id,
		ObjectType: body.ObjectType,
		ObjectID:   body.ObjectID,
		Patch:      body.Patch,
		Note:       body.Note,
		Status:     "pending",
		CreatedAt:  datetimeFromMs(now),
	}

	payload, err := json.Marshal(struct {
		ProposalID uuid.UUID       `json:"proposal_id"`
		ObjectType string          `json:"object_type"`
		ObjectID   string          `json:"object_id"`
		Patch      json.RawMessage `json:"patch"`
		Note       *string         `json:"note"`
		Status     string          `json:"status"`
	}{
		ProposalID: proposal.ID,
		ObjectType: proposal.ObjectType,
		ObjectID:   proposal.ObjectID,
		Patch:      nullableRaw(proposal.Patch),
		Note:       proposal.Note,
		Status:     proposal.Status,
	})
	if err != nil {
		plainText(w, http.StatusInternalServerError, err.Error())
		return
	}

	eventID := fmt.Sprintf("exploratory-writeback:%s", proposal.ID)
	objectID := storageabstraction.ObjectId(proposal.ObjectID)
	entry := storageabstraction.ActionLogEntry{
		Tenant:       h.Tenant,
		EventID:      &eventID,
		ActionID:     proposal.ID.String(),
		Kind:         writebackKind,
		Subject:      h.Subject,
		Object:       &objectID,
		Payload:      payload,
		RecordedAtMs: now,
	}

	if err := h.Actions.Append(r.Context(), entry); err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, proposal)
}
