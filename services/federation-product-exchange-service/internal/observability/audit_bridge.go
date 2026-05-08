// Package observability ports the federation-product-exchange Rust
// `domain::{audit_bridge,encryption,access_proxy}` modules. Functions
// are pure (no HTTP endpoints, no I/O); they are invoked by handlers
// that already loaded the relevant rows from the repo layer.
package observability

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

// Summarize ports `domain::audit_bridge::summarize` 1:1.
//
// For each share that resolves to both a contract and a peer it emits an
// AuditBridgeEntry with status / cursor pulled from the matching SyncStatus
// (defaulting to "pending" / "cursor/pending" when absent). The bridge_status
// is "degraded" if any entry is degraded, "pending" if no entries, otherwise
// "healthy". The latest_cursor mirrors the first entry's cursor in iteration
// order — same as the Rust source.
func Summarize(
	peers []models.PeerOrganization,
	contracts []models.SharingContract,
	shares []models.SharedDataset,
	syncStatuses []models.SyncStatus,
) models.AuditBridgeSummary {
	entries := make([]models.AuditBridgeEntry, 0, len(shares))
	for i := range shares {
		share := &shares[i]
		contract := findContract(contracts, share.ContractID)
		if contract == nil {
			continue
		}
		peer := findPeer(peers, share.ConsumerPeerID)
		if peer == nil {
			continue
		}
		status := findSyncStatus(syncStatuses, share.ID)

		auditCursor := "cursor/pending"
		statusLabel := "pending"
		var lastSyncAt *time.Time
		if status != nil {
			auditCursor = status.AuditCursor
			statusLabel = status.Status
			lastSyncAt = status.LastSyncAt
		}

		entries = append(entries, models.AuditBridgeEntry{
			ShareID:      share.ID,
			DatasetName:  share.DatasetName,
			PeerName:     peer.DisplayName,
			ContractName: contract.Name,
			AuditCursor:  auditCursor,
			LastSyncAt:   lastSyncAt,
			Status:       statusLabel,
			Evidence: []string{
				fmt.Sprintf("contract:%s", contract.ID),
				fmt.Sprintf("peer:%s", peer.Slug),
				fmt.Sprintf("selector:%s", selectorString(share.Selector)),
			},
		})
	}

	bridgeStatus := "healthy"
	if anyDegraded(entries) {
		bridgeStatus = "degraded"
	} else if len(entries) == 0 {
		bridgeStatus = "pending"
	}

	latestCursor := "cursor/pending"
	if len(entries) > 0 {
		latestCursor = entries[0].AuditCursor
	}

	return models.AuditBridgeSummary{
		BridgeStatus: bridgeStatus,
		EntryCount:   int64(len(entries)),
		LatestCursor: latestCursor,
		Entries:      entries,
	}
}

func findContract(contracts []models.SharingContract, id uuid.UUID) *models.SharingContract {
	for i := range contracts {
		if contracts[i].ID == id {
			return &contracts[i]
		}
	}
	return nil
}

func findPeer(peers []models.PeerOrganization, id uuid.UUID) *models.PeerOrganization {
	for i := range peers {
		if peers[i].ID == id {
			return &peers[i]
		}
	}
	return nil
}

func findSyncStatus(statuses []models.SyncStatus, shareID uuid.UUID) *models.SyncStatus {
	for i := range statuses {
		if statuses[i].ShareID == shareID {
			return &statuses[i]
		}
	}
	return nil
}

func anyDegraded(entries []models.AuditBridgeEntry) bool {
	for i := range entries {
		if entries[i].Status == "degraded" {
			return true
		}
	}
	return false
}

// selectorString mirrors Rust's `format!("{}", serde_json::Value)` Display
// impl: compact JSON, with `null` as the canonical empty form.
func selectorString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "null"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
