package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storageabstraction "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// TestWritebackProposalAppendsToActionLogStore is the Go port of Rust
// `writeback_proposal_appends_to_action_log_store` in handlers.rs.
func TestWritebackProposalAppendsToActionLogStore(t *testing.T) {
	t.Parallel()
	h, _, actions := newTestHandlers(t)
	r := newRouter(h)

	objectID := uuid.Must(uuid.NewV7()).String()
	note := "analyst correction"

	resp := doJSON(t, r, http.MethodPost, "/api/v1/writeback", map[string]any{
		"object_type": "aircraft",
		"object_id":   objectID,
		"patch":       map[string]any{"tail": "N123OF"},
		"note":        note,
	})
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	resp.Body.Close()
	assert.Equal(t, "pending", body["status"])
	assert.Equal(t, "aircraft", body["object_type"])
	assert.Equal(t, objectID, body["object_id"])
	assert.Equal(t, note, body["note"])

	entries, err := actions.ListRecent(
		t.Context(),
		h.Tenant,
		storageabstraction.Page{Size: 10},
		storageabstraction.Strong(),
	)
	require.NoError(t, err)
	require.Len(t, entries.Items, 1)
	assert.Equal(t, "exploratory.writeback_proposed", entries.Items[0].Kind)
	require.NotNil(t, entries.Items[0].Object)
	assert.Equal(t, objectID, string(*entries.Items[0].Object))

	var payload map[string]any
	require.NoError(t, json.Unmarshal(entries.Items[0].Payload, &payload))
	assert.Equal(t, "pending", payload["status"])
	assert.Equal(t, objectID, payload["object_id"])
	assert.Equal(t, "aircraft", payload["object_type"])
	assert.Equal(t, note, payload["note"])

	// event_id derives from the proposal id (Rust:
	// `format!("exploratory-writeback:{}", proposal.id)`).
	require.NotNil(t, entries.Items[0].EventID)
	proposalID, _ := body["id"].(string)
	assert.True(t, strings.HasPrefix(*entries.Items[0].EventID, "exploratory-writeback:"))
	assert.Equal(t, "exploratory-writeback:"+proposalID, *entries.Items[0].EventID)
	assert.Equal(t, proposalID, entries.Items[0].ActionID)
	assert.Equal(t, h.Subject, entries.Items[0].Subject)
}

// TestWritebackProposalAcceptsNullNote covers the Option<String>=None
// branch — Rust `body.note: None` → payload["note"] = null and the
// proposal response also carries a null note.
func TestWritebackProposalAcceptsNullNote(t *testing.T) {
	t.Parallel()
	h, _, actions := newTestHandlers(t)
	r := newRouter(h)

	objectID := uuid.Must(uuid.NewV7()).String()
	resp := doJSON(t, r, http.MethodPost, "/api/v1/writeback", map[string]any{
		"object_type": "vessel",
		"object_id":   objectID,
		"patch":       map[string]any{"name": "Discovery"},
	})
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	resp.Body.Close()
	assert.Nil(t, body["note"], "note should serialise as null when absent")

	entries, err := actions.ListRecent(
		t.Context(),
		h.Tenant,
		storageabstraction.Page{Size: 10},
		storageabstraction.Strong(),
	)
	require.NoError(t, err)
	require.Len(t, entries.Items, 1)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(entries.Items[0].Payload, &payload))
	assert.Nil(t, payload["note"], "logged note should also be null")
}

// TestWritebackProposalCreatesDistinctEntries — each new POST
// generates a fresh proposal id, which seeds a unique event_id of the
// form `exploratory-writeback:<uuid>`. The InMemoryActionLogStore
// dedupes by (tenant, effective_event_id), so two proposals on the
// same object MUST still produce two distinct log entries.
func TestWritebackProposalCreatesDistinctEntries(t *testing.T) {
	t.Parallel()
	h, _, actions := newTestHandlers(t)
	r := newRouter(h)

	objectID := uuid.Must(uuid.NewV7()).String()
	for range 2 {
		resp := doJSON(t, r, http.MethodPost, "/api/v1/writeback", map[string]any{
			"object_type": "aircraft",
			"object_id":   objectID,
			"patch":       map[string]any{"tail": "N123OF"},
		})
		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		resp.Body.Close()
	}

	entries, err := actions.ListRecent(
		t.Context(),
		h.Tenant,
		storageabstraction.Page{Size: 10},
		storageabstraction.Strong(),
	)
	require.NoError(t, err)
	require.Len(t, entries.Items, 2)
	require.NotNil(t, entries.Items[0].EventID)
	require.NotNil(t, entries.Items[1].EventID)
	assert.NotEqual(t, *entries.Items[0].EventID, *entries.Items[1].EventID,
		"each proposal id should yield a unique event_id")
}
