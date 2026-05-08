package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListResponseEnvelope(t *testing.T) {
	t.Parallel()
	resp := ListResponse[string]{Items: []string{"a", "b"}}
	b, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":["a","b"]}`, string(b),
		"federation list envelope must be {\"items\":[…]} matching Rust models::ListResponse")
}

func TestSyncStatusJSONShape(t *testing.T) {
	t.Parallel()
	s := SyncStatus{
		ID:                 uuid.New(),
		ShareID:            uuid.New(),
		Mode:               "stream",
		Status:             "ready",
		RowsReplicated:     1000,
		BacklogRows:        5,
		EncryptedInTransit: true,
		EncryptedAtRest:    true,
		KeyVersion:         "v1",
		AuditCursor:        "abc",
		UpdatedAt:          time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(s)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "stream", got["mode"])
	assert.Equal(t, "ready", got["status"])
	assert.Equal(t, true, got["encrypted_in_transit"])
	assert.Equal(t, true, got["encrypted_at_rest"])
	assert.Equal(t, "v1", got["key_version"])
	assert.Equal(t, float64(1000), got["rows_replicated"])
	assert.Equal(t, float64(5), got["backlog_rows"])
}

func TestNexusOverviewFields(t *testing.T) {
	t.Parallel()
	o := NexusOverview{
		PeerCount: 2, ActivePeerCount: 1, ContractCount: 4, ActiveContractCount: 3,
		PrivateSpaceCount: 7, SharedSpaceCount: 2, ShareCount: 9,
		FederatedAccessCount: 5, EncryptedShareCount: 8, ReplicationReadyCount: 6,
		PendingSchemaReviews: 1, AuditBridgeStatus: "ok",
	}
	b, err := json.Marshal(o)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"peer_count":2,"active_peer_count":1,"contract_count":4,"active_contract_count":3,
		"private_space_count":7,"shared_space_count":2,"share_count":9,
		"federated_access_count":5,"encrypted_share_count":8,"replication_ready_count":6,
		"pending_schema_reviews":1,"audit_bridge_status":"ok","latest_sync_at":null
	}`, string(b))
}
