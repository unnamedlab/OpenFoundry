// Package models holds typed scaffolding for the federation /
// product-exchange plane. Foundation slice ships only the most
// commonly referenced shared types — full per-sub-domain models
// (marketplace, marketplace_catalog, product_distribution) land
// in follow-up slices alongside their handlers + repos.
package models

import (
	"time"

	"github.com/google/uuid"
)

// ListResponse[T] is the canonical {"items": [...]} envelope the
// federation HTTP surface uses for list endpoints. Mirrors Rust
// `models::ListResponse<T>`.
type ListResponse[T any] struct {
	Items []T `json:"items"`
}

// SyncStatus mirrors the `synchronisation_status` row carried by
// every share. Status / mode / encrypted-* are wire-format
// discriminators surfaced on the federation UI.
type SyncStatus struct {
	ID                  uuid.UUID  `json:"id"`
	ShareID             uuid.UUID  `json:"share_id"`
	Mode                string     `json:"mode"`
	Status              string     `json:"status"`
	RowsReplicated      int64      `json:"rows_replicated"`
	BacklogRows         int64      `json:"backlog_rows"`
	EncryptedInTransit  bool       `json:"encrypted_in_transit"`
	EncryptedAtRest     bool       `json:"encrypted_at_rest"`
	KeyVersion          string     `json:"key_version"`
	LastSyncAt          *time.Time `json:"last_sync_at"`
	NextSyncAt          *time.Time `json:"next_sync_at"`
	AuditCursor         string     `json:"audit_cursor"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// NexusOverview is the canonical summary payload the federation UI
// renders on the landing card. Counter shape preserved verbatim
// from Rust.
type NexusOverview struct {
	PeerCount             int64      `json:"peer_count"`
	ActivePeerCount       int64      `json:"active_peer_count"`
	ContractCount         int64      `json:"contract_count"`
	ActiveContractCount   int64      `json:"active_contract_count"`
	PrivateSpaceCount     int64      `json:"private_space_count"`
	SharedSpaceCount      int64      `json:"shared_space_count"`
	ShareCount            int64      `json:"share_count"`
	FederatedAccessCount  int64      `json:"federated_access_count"`
	EncryptedShareCount   int64      `json:"encrypted_share_count"`
	ReplicationReadyCount int64      `json:"replication_ready_count"`
	PendingSchemaReviews  int64      `json:"pending_schema_reviews"`
	AuditBridgeStatus     string     `json:"audit_bridge_status"`
	LatestSyncAt          *time.Time `json:"latest_sync_at"`
}
