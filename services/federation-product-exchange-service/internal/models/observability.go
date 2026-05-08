package models

import (
	"time"

	"github.com/google/uuid"
)

// SharingContract mirrors `models::contract::SharingContract` from Rust.
// Minimal shape required by FPE-8 observability ports; FPE-2 will extend
// repository / handler usage on top of the same struct.
type SharingContract struct {
	ID                uuid.UUID  `json:"id"`
	PeerID            uuid.UUID  `json:"peer_id"`
	Name              string     `json:"name"`
	Description       string     `json:"description"`
	DatasetLocator    string     `json:"dataset_locator"`
	AllowedPurposes   []string   `json:"allowed_purposes"`
	DataClasses       []string   `json:"data_classes"`
	ResidencyRegion   string     `json:"residency_region"`
	QueryTemplate     string     `json:"query_template"`
	MaxRowsPerQuery   int64      `json:"max_rows_per_query"`
	ReplicationMode   string     `json:"replication_mode"`
	EncryptionProfile string     `json:"encryption_profile"`
	RetentionDays     int32      `json:"retention_days"`
	Status            string     `json:"status"`
	SignedAt          *time.Time `json:"signed_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// AccessGrant mirrors `models::access_grant::AccessGrant` from Rust. It is
// the per-consumer authorization record bound to a SharedDataset.
type AccessGrant struct {
	ID              uuid.UUID `json:"id"`
	ShareID         uuid.UUID `json:"share_id"`
	PeerID          uuid.UUID `json:"peer_id"`
	QueryTemplate   string    `json:"query_template"`
	MaxRowsPerQuery int64     `json:"max_rows_per_query"`
	CanReplicate    bool      `json:"can_replicate"`
	AllowedPurposes []string  `json:"allowed_purposes"`
	ExpiresAt       time.Time `json:"expires_at"`
	IssuedAt        time.Time `json:"issued_at"`
}

// AuditBridgeEntry mirrors `models::sync_status::AuditBridgeEntry`.
type AuditBridgeEntry struct {
	ShareID      uuid.UUID  `json:"share_id"`
	DatasetName  string     `json:"dataset_name"`
	PeerName     string     `json:"peer_name"`
	ContractName string     `json:"contract_name"`
	AuditCursor  string     `json:"audit_cursor"`
	LastSyncAt   *time.Time `json:"last_sync_at"`
	Status       string     `json:"status"`
	Evidence     []string   `json:"evidence"`
}

// AuditBridgeSummary mirrors `models::sync_status::AuditBridgeSummary`.
type AuditBridgeSummary struct {
	BridgeStatus string             `json:"bridge_status"`
	EntryCount   int64              `json:"entry_count"`
	LatestCursor string             `json:"latest_cursor"`
	Entries      []AuditBridgeEntry `json:"entries"`
}

// EncryptionPosture mirrors `models::sync_status::EncryptionPosture`.
type EncryptionPosture struct {
	ShareID            uuid.UUID `json:"share_id"`
	TransportCipher    string    `json:"transport_cipher"`
	AtRestCipher       string    `json:"at_rest_cipher"`
	KeyVersion         string    `json:"key_version"`
	Profile            string    `json:"profile"`
	EncryptedInTransit bool      `json:"encrypted_in_transit"`
	EncryptedAtRest    bool      `json:"encrypted_at_rest"`
	Recommendation     string    `json:"recommendation"`
}
