package observability

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

func TestSummarize_PendingWhenNoShares(t *testing.T) {
	got := Summarize(nil, nil, nil, nil)
	if got.BridgeStatus != "pending" {
		t.Fatalf("bridge_status: want pending, got %q", got.BridgeStatus)
	}
	if got.EntryCount != 0 {
		t.Fatalf("entry_count: want 0, got %d", got.EntryCount)
	}
	if got.LatestCursor != "cursor/pending" {
		t.Fatalf("latest_cursor: want cursor/pending, got %q", got.LatestCursor)
	}
	if len(got.Entries) != 0 {
		t.Fatalf("entries: want empty, got %d", len(got.Entries))
	}
}

func TestSummarize_HealthyWithMatchedRows(t *testing.T) {
	contractID := uuid.New()
	peerID := uuid.New()
	shareID := uuid.New()
	last := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	peer := models.PeerOrganization{ID: peerID, Slug: "acme", DisplayName: "Acme"}
	contract := models.SharingContract{ID: contractID, Name: "Claims"}
	share := models.SharedDataset{
		ID:             shareID,
		ContractID:     contractID,
		ConsumerPeerID: peerID,
		DatasetName:    "claims",
		Selector:       json.RawMessage(`{"col":"id"}`),
	}
	status := models.SyncStatus{
		ID:          uuid.New(),
		ShareID:     shareID,
		Status:      "ready",
		AuditCursor: "cursor/abc",
		LastSyncAt:  &last,
	}

	got := Summarize([]models.PeerOrganization{peer}, []models.SharingContract{contract}, []models.SharedDataset{share}, []models.SyncStatus{status})
	if got.BridgeStatus != "healthy" {
		t.Fatalf("bridge_status: want healthy, got %q", got.BridgeStatus)
	}
	if got.EntryCount != 1 {
		t.Fatalf("entry_count: want 1, got %d", got.EntryCount)
	}
	if got.LatestCursor != "cursor/abc" {
		t.Fatalf("latest_cursor: want cursor/abc, got %q", got.LatestCursor)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("entries len: want 1, got %d", len(got.Entries))
	}
	e := got.Entries[0]
	if e.Status != "ready" || e.AuditCursor != "cursor/abc" || e.PeerName != "Acme" || e.ContractName != "Claims" || e.DatasetName != "claims" {
		t.Fatalf("entry shape unexpected: %+v", e)
	}
	if e.LastSyncAt == nil || !e.LastSyncAt.Equal(last) {
		t.Fatalf("last_sync_at not propagated: %+v", e.LastSyncAt)
	}
	wantEvidence := []string{
		"contract:" + contractID.String(),
		"peer:acme",
		`selector:{"col":"id"}`,
	}
	for i, want := range wantEvidence {
		if e.Evidence[i] != want {
			t.Fatalf("evidence[%d]: want %q, got %q", i, want, e.Evidence[i])
		}
	}
}

func TestSummarize_DegradedWhenAnyEntryDegraded(t *testing.T) {
	contractID := uuid.New()
	peerID := uuid.New()
	shareID := uuid.New()
	peer := models.PeerOrganization{ID: peerID, Slug: "p"}
	contract := models.SharingContract{ID: contractID}
	share := models.SharedDataset{ID: shareID, ContractID: contractID, ConsumerPeerID: peerID}
	status := models.SyncStatus{ShareID: shareID, Status: "degraded", AuditCursor: "cursor/x"}

	got := Summarize([]models.PeerOrganization{peer}, []models.SharingContract{contract}, []models.SharedDataset{share}, []models.SyncStatus{status})
	if got.BridgeStatus != "degraded" {
		t.Fatalf("bridge_status: want degraded, got %q", got.BridgeStatus)
	}
}

func TestSummarize_PendingWhenStatusMissing(t *testing.T) {
	contractID := uuid.New()
	peerID := uuid.New()
	shareID := uuid.New()
	peer := models.PeerOrganization{ID: peerID, Slug: "p"}
	contract := models.SharingContract{ID: contractID}
	share := models.SharedDataset{ID: shareID, ContractID: contractID, ConsumerPeerID: peerID}

	got := Summarize([]models.PeerOrganization{peer}, []models.SharingContract{contract}, []models.SharedDataset{share}, nil)
	if got.EntryCount != 1 {
		t.Fatalf("entry_count: want 1, got %d", got.EntryCount)
	}
	if got.Entries[0].Status != "pending" {
		t.Fatalf("entry status: want pending, got %q", got.Entries[0].Status)
	}
	if got.Entries[0].AuditCursor != "cursor/pending" {
		t.Fatalf("entry audit_cursor: want cursor/pending, got %q", got.Entries[0].AuditCursor)
	}
	if got.LatestCursor != "cursor/pending" {
		t.Fatalf("latest_cursor: want cursor/pending, got %q", got.LatestCursor)
	}
}

func TestSummarize_SkipsSharesWithMissingContractOrPeer(t *testing.T) {
	contractID := uuid.New()
	peerID := uuid.New()
	missingID := uuid.New()
	peer := models.PeerOrganization{ID: peerID}
	contract := models.SharingContract{ID: contractID}
	share := models.SharedDataset{ID: uuid.New(), ContractID: missingID, ConsumerPeerID: peerID}
	got := Summarize([]models.PeerOrganization{peer}, []models.SharingContract{contract}, []models.SharedDataset{share}, nil)
	if got.EntryCount != 0 {
		t.Fatalf("entry_count: want 0 (missing contract), got %d", got.EntryCount)
	}
	share2 := models.SharedDataset{ID: uuid.New(), ContractID: contractID, ConsumerPeerID: missingID}
	got = Summarize([]models.PeerOrganization{peer}, []models.SharingContract{contract}, []models.SharedDataset{share2}, nil)
	if got.EntryCount != 0 {
		t.Fatalf("entry_count: want 0 (missing peer), got %d", got.EntryCount)
	}
}

func TestSelectorString(t *testing.T) {
	cases := []struct {
		in   json.RawMessage
		want string
	}{
		{nil, "null"},
		{json.RawMessage(``), "null"},
		{json.RawMessage(`{"col": "id"}`), `{"col":"id"}`},
		{json.RawMessage(`null`), "null"},
		{json.RawMessage(`[1, 2]`), `[1,2]`},
	}
	for _, c := range cases {
		if got := selectorString(c.in); got != c.want {
			t.Fatalf("selectorString(%q): want %q, got %q", string(c.in), c.want, got)
		}
	}
}
