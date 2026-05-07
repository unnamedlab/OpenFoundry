package productdistribution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

type Repository interface {
	ListPeers(ctx context.Context) ([]models.PeerOrganization, error)
	CreatePeer(ctx context.Context, req models.CreatePeerRequest) (*models.PeerOrganization, error)
	GetPeer(ctx context.Context, id uuid.UUID) (*models.PeerOrganization, error)
	UpdatePeer(ctx context.Context, id uuid.UUID, req models.UpdatePeerRequest) (*models.PeerOrganization, error)
	DeletePeer(ctx context.Context, id uuid.UUID) error
	ListShareManifests(ctx context.Context) ([]models.ShareManifest, error)
	CreateShareManifest(ctx context.Context, req models.CreateShareRequest) (*models.ShareManifest, error)
	GetShareManifest(ctx context.Context, id uuid.UUID) (*models.ShareManifest, error)
	ListSyncStatuses(ctx context.Context) ([]models.SyncStatus, error)
	UpdateSyncStatus(ctx context.Context, shareID uuid.UUID, req models.SyncStatusUpdateRequest) (*models.SyncStatus, error)
}

type PGXRepository struct{ Pool *pgxpool.Pool }

func NewPGXRepository(pool *pgxpool.Pool) *PGXRepository { return &PGXRepository{Pool: pool} }

func (r *PGXRepository) ListPeers(ctx context.Context) ([]models.PeerOrganization, error) {
	rows, err := r.Pool.Query(ctx, peerSelect+` ORDER BY updated_at DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	peers := []models.PeerOrganization{}
	for rows.Next() {
		peer, err := scanPeer(rows)
		if err != nil {
			return nil, err
		}
		peers = append(peers, *peer)
	}
	return peers, rows.Err()
}

func (r *PGXRepository) CreatePeer(ctx context.Context, req models.CreatePeerRequest) (*models.PeerOrganization, error) {
	if err := ValidateCreatePeer(req); err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	now := time.Now().UTC()
	if strings.TrimSpace(req.OrganizationType) == "" {
		req.OrganizationType = "partner"
	}
	sharedScopes, _ := json.Marshal(nonNilStrings(req.SharedScopes))
	adminContacts, _ := json.Marshal(nonNilStrings(req.AdminContacts))
	row := r.Pool.QueryRow(ctx, `
INSERT INTO nexus_peers (id, slug, display_name, organization_type, region, endpoint_url, auth_mode, trust_level, public_key_fingerprint, shared_scopes, status, lifecycle_stage, admin_contacts, last_handshake_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, 'pending', 'onboarding', $11::jsonb, NULL, $12, $13)
RETURNING id, slug, display_name, organization_type, region, endpoint_url, auth_mode, trust_level, public_key_fingerprint, shared_scopes, status, lifecycle_stage, admin_contacts, last_handshake_at, created_at, updated_at`,
		id, strings.TrimSpace(req.Slug), strings.TrimSpace(req.DisplayName), strings.TrimSpace(req.OrganizationType), strings.TrimSpace(req.Region), strings.TrimSpace(req.EndpointURL), strings.TrimSpace(req.AuthMode), strings.TrimSpace(req.TrustLevel), strings.TrimSpace(req.PublicKeyFingerprint), sharedScopes, adminContacts, now, now)
	peer, err := scanPeer(row)
	if err != nil {
		return nil, mapPGError(err)
	}
	return peer, nil
}

func (r *PGXRepository) GetPeer(ctx context.Context, id uuid.UUID) (*models.PeerOrganization, error) {
	peer, err := scanPeer(r.Pool.QueryRow(ctx, peerSelect+` WHERE id = $1`, id))
	if err != nil {
		return nil, mapPGError(err)
	}
	return peer, nil
}

func (r *PGXRepository) UpdatePeer(ctx context.Context, id uuid.UUID, req models.UpdatePeerRequest) (*models.PeerOrganization, error) {
	current, err := r.GetPeer(ctx, id)
	if err != nil {
		return nil, err
	}
	applyPeerUpdate(current, req)
	now := time.Now().UTC()
	sharedScopes, _ := json.Marshal(current.SharedScopes)
	adminContacts, _ := json.Marshal(current.AdminContacts)
	row := r.Pool.QueryRow(ctx, `
UPDATE nexus_peers
SET display_name = $2, organization_type = $3, region = $4, endpoint_url = $5, trust_level = $6,
    shared_scopes = $7::jsonb, status = $8, lifecycle_stage = $9, admin_contacts = $10::jsonb, updated_at = $11
WHERE id = $1
RETURNING id, slug, display_name, organization_type, region, endpoint_url, auth_mode, trust_level, public_key_fingerprint, shared_scopes, status, lifecycle_stage, admin_contacts, last_handshake_at, created_at, updated_at`,
		id, current.DisplayName, current.OrganizationType, current.Region, current.EndpointURL, current.TrustLevel, sharedScopes, current.Status, current.LifecycleStage, adminContacts, now)
	peer, err := scanPeer(row)
	if err != nil {
		return nil, mapPGError(err)
	}
	return peer, nil
}

func (r *PGXRepository) DeletePeer(ctx context.Context, id uuid.UUID) error {
	result, err := r.Pool.Exec(ctx, `DELETE FROM nexus_peers WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PGXRepository) ListShareManifests(ctx context.Context) ([]models.ShareManifest, error) {
	shares, err := r.listShares(ctx)
	if err != nil {
		return nil, err
	}
	statuses, err := r.syncStatusByShare(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]models.ShareManifest, 0, len(shares))
	for _, share := range shares {
		status := statuses[share.ID]
		items = append(items, models.ShareManifest{Share: share, SyncStatus: status})
	}
	return items, nil
}

func (r *PGXRepository) CreateShareManifest(ctx context.Context, req models.CreateShareRequest) (*models.ShareManifest, error) {
	contract, err := r.getContract(ctx, req.ContractID)
	if err != nil {
		return nil, err
	}
	provider, err := r.GetPeer(ctx, req.ProviderPeerID)
	if err != nil {
		return nil, fmt.Errorf("%w: provider peer not found", ErrValidation)
	}
	consumer, err := r.GetPeer(ctx, req.ConsumerPeerID)
	if err != nil {
		return nil, fmt.Errorf("%w: consumer peer not found", ErrValidation)
	}
	now := time.Now().UTC()
	if err := ValidateCreateShare(req, *contract, *provider, *consumer, now); err != nil {
		return nil, err
	}
	ensureShareJSONDefaults(&req)
	shareID, _ := uuid.NewV7()
	if shareID == uuid.Nil {
		shareID = uuid.New()
	}
	grantID, _ := uuid.NewV7()
	if grantID == uuid.Nil {
		grantID = uuid.New()
	}
	syncID, _ := uuid.NewV7()
	if syncID == uuid.Nil {
		syncID = uuid.New()
	}
	var sampleRows []json.RawMessage
	_ = json.Unmarshal(req.SampleRows, &sampleRows)
	nextSyncAt := now.Add(4 * time.Hour)
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
INSERT INTO nexus_shares (id, contract_id, provider_peer_id, consumer_peer_id, provider_space_id, consumer_space_id, dataset_name, selector, provider_schema, consumer_schema, sample_rows, replication_mode, status, last_sync_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10::jsonb, $11::jsonb, $12, 'active', NULL, $13, $14)`,
		shareID, req.ContractID, req.ProviderPeerID, req.ConsumerPeerID, req.ProviderSpaceID, req.ConsumerSpaceID, strings.TrimSpace(req.DatasetName), req.Selector, req.ProviderSchema, req.ConsumerSchema, req.SampleRows, req.ReplicationMode, now, now)
	if err != nil {
		return nil, err
	}
	allowedPurposes, _ := json.Marshal(contract.AllowedPurposes)
	_, err = tx.Exec(ctx, `
INSERT INTO nexus_access_grants (id, share_id, peer_id, query_template, max_rows_per_query, can_replicate, allowed_purposes, expires_at, issued_at)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9)`,
		grantID, shareID, req.ConsumerPeerID, contract.QueryTemplate, contract.MaxRowsPerQuery, req.ReplicationMode != "query_only", allowedPurposes, contract.ExpiresAt, now)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO nexus_sync_statuses (id, share_id, mode, status, rows_replicated, backlog_rows, encrypted_in_transit, encrypted_at_rest, key_version, last_sync_at, next_sync_at, audit_cursor, updated_at)
VALUES ($1, $2, $3, 'ready', 0, $4, TRUE, TRUE, $5, NULL, $6, $7, $8)`,
		syncID, shareID, req.ReplicationMode, int64(len(sampleRows)), contract.EncryptionProfile, nextSyncAt, "cursor/"+shareID.String(), now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetShareManifest(ctx, shareID)
}

func (r *PGXRepository) GetShareManifest(ctx context.Context, id uuid.UUID) (*models.ShareManifest, error) {
	share, err := r.getShare(ctx, id)
	if err != nil {
		return nil, err
	}
	status, err := r.getSyncStatusByShare(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	return &models.ShareManifest{Share: *share, SyncStatus: status}, nil
}

func (r *PGXRepository) ListSyncStatuses(ctx context.Context) ([]models.SyncStatus, error) {
	rows, err := r.Pool.Query(ctx, syncStatusSelect+` ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	statuses := []models.SyncStatus{}
	for rows.Next() {
		status, err := scanSyncStatus(rows)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, *status)
	}
	return statuses, rows.Err()
}

func (r *PGXRepository) UpdateSyncStatus(ctx context.Context, shareID uuid.UUID, req models.SyncStatusUpdateRequest) (*models.SyncStatus, error) {
	current, err := r.getSyncStatusByShare(ctx, shareID)
	if err != nil {
		return nil, err
	}
	applySyncStatusUpdate(current, req)
	current.UpdatedAt = time.Now().UTC()
	row := r.Pool.QueryRow(ctx, `
UPDATE nexus_sync_statuses
SET status = $2, rows_replicated = $3, backlog_rows = $4, encrypted_in_transit = $5, encrypted_at_rest = $6,
    key_version = $7, last_sync_at = $8, next_sync_at = $9, audit_cursor = $10, updated_at = $11
WHERE share_id = $1
RETURNING id, share_id, mode, status, rows_replicated, backlog_rows, encrypted_in_transit, encrypted_at_rest, key_version, last_sync_at, next_sync_at, audit_cursor, updated_at`,
		shareID, current.Status, current.RowsReplicated, current.BacklogRows, current.EncryptedInTransit, current.EncryptedAtRest, current.KeyVersion, current.LastSyncAt, current.NextSyncAt, current.AuditCursor, current.UpdatedAt)
	status, err := scanSyncStatus(row)
	if err != nil {
		return nil, mapPGError(err)
	}
	_, err = r.Pool.Exec(ctx, `UPDATE nexus_shares SET last_sync_at = $2, updated_at = $3 WHERE id = $1`, shareID, current.LastSyncAt, current.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return status, nil
}

const peerSelect = `SELECT id, slug, display_name, organization_type, region, endpoint_url, auth_mode, trust_level, public_key_fingerprint, shared_scopes, status, lifecycle_stage, admin_contacts, last_handshake_at, created_at, updated_at FROM nexus_peers`
const shareSelect = `SELECT id, contract_id, provider_peer_id, consumer_peer_id, provider_space_id, consumer_space_id, dataset_name, selector, provider_schema, consumer_schema, sample_rows, replication_mode, status, last_sync_at, created_at, updated_at FROM nexus_shares`
const syncStatusSelect = `SELECT id, share_id, mode, status, rows_replicated, backlog_rows, encrypted_in_transit, encrypted_at_rest, key_version, last_sync_at, next_sync_at, audit_cursor, updated_at FROM nexus_sync_statuses`

type sharingContract struct {
	ID                uuid.UUID
	PeerID            uuid.UUID
	AllowedPurposes   []string
	QueryTemplate     string
	MaxRowsPerQuery   int64
	ReplicationMode   string
	EncryptionProfile string
	Status            string
	ExpiresAt         time.Time
}

func (r *PGXRepository) getContract(ctx context.Context, id uuid.UUID) (*sharingContract, error) {
	var contract sharingContract
	var allowedPurposes []byte
	err := r.Pool.QueryRow(ctx, `SELECT id, peer_id, allowed_purposes, query_template, max_rows_per_query, replication_mode, encryption_profile, status, expires_at FROM nexus_contracts WHERE id = $1`, id).
		Scan(&contract.ID, &contract.PeerID, &allowedPurposes, &contract.QueryTemplate, &contract.MaxRowsPerQuery, &contract.ReplicationMode, &contract.EncryptionProfile, &contract.Status, &contract.ExpiresAt)
	if err != nil {
		return nil, mapPGError(err)
	}
	if err := json.Unmarshal(allowedPurposes, &contract.AllowedPurposes); err != nil {
		return nil, fmt.Errorf("decode allowed_purposes: %w", err)
	}
	return &contract, nil
}

func (r *PGXRepository) listShares(ctx context.Context) ([]models.SharedDataset, error) {
	rows, err := r.Pool.Query(ctx, shareSelect+` ORDER BY updated_at DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shares := []models.SharedDataset{}
	for rows.Next() {
		share, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		shares = append(shares, *share)
	}
	return shares, rows.Err()
}

func (r *PGXRepository) getShare(ctx context.Context, id uuid.UUID) (*models.SharedDataset, error) {
	share, err := scanShare(r.Pool.QueryRow(ctx, shareSelect+` WHERE id = $1`, id))
	if err != nil {
		return nil, mapPGError(err)
	}
	return share, nil
}

func (r *PGXRepository) syncStatusByShare(ctx context.Context) (map[uuid.UUID]*models.SyncStatus, error) {
	statuses, err := r.ListSyncStatuses(ctx)
	if err != nil {
		return nil, err
	}
	byShare := map[uuid.UUID]*models.SyncStatus{}
	for i := range statuses {
		status := statuses[i]
		byShare[status.ShareID] = &status
	}
	return byShare, nil
}

func (r *PGXRepository) getSyncStatusByShare(ctx context.Context, shareID uuid.UUID) (*models.SyncStatus, error) {
	status, err := scanSyncStatus(r.Pool.QueryRow(ctx, syncStatusSelect+` WHERE share_id = $1`, shareID))
	if err != nil {
		return nil, mapPGError(err)
	}
	return status, nil
}

type scanner interface{ Scan(dest ...any) error }

func scanPeer(row scanner) (*models.PeerOrganization, error) {
	var peer models.PeerOrganization
	var sharedScopes, adminContacts []byte
	if err := row.Scan(&peer.ID, &peer.Slug, &peer.DisplayName, &peer.OrganizationType, &peer.Region, &peer.EndpointURL, &peer.AuthMode, &peer.TrustLevel, &peer.PublicKeyFingerprint, &sharedScopes, &peer.Status, &peer.LifecycleStage, &adminContacts, &peer.LastHandshakeAt, &peer.CreatedAt, &peer.UpdatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(sharedScopes, &peer.SharedScopes); err != nil {
		return nil, fmt.Errorf("decode shared_scopes: %w", err)
	}
	if err := json.Unmarshal(adminContacts, &peer.AdminContacts); err != nil {
		return nil, fmt.Errorf("decode admin_contacts: %w", err)
	}
	return &peer, nil
}

func scanShare(row scanner) (*models.SharedDataset, error) {
	var share models.SharedDataset
	var selector, providerSchema, consumerSchema, sampleRows []byte
	if err := row.Scan(&share.ID, &share.ContractID, &share.ProviderPeerID, &share.ConsumerPeerID, &share.ProviderSpaceID, &share.ConsumerSpaceID, &share.DatasetName, &selector, &providerSchema, &consumerSchema, &sampleRows, &share.ReplicationMode, &share.Status, &share.LastSyncAt, &share.CreatedAt, &share.UpdatedAt); err != nil {
		return nil, err
	}
	share.Selector = append(json.RawMessage(nil), selector...)
	share.ProviderSchema = append(json.RawMessage(nil), providerSchema...)
	share.ConsumerSchema = append(json.RawMessage(nil), consumerSchema...)
	share.SampleRows = append(json.RawMessage(nil), sampleRows...)
	return &share, nil
}

func scanSyncStatus(row scanner) (*models.SyncStatus, error) {
	var status models.SyncStatus
	if err := row.Scan(&status.ID, &status.ShareID, &status.Mode, &status.Status, &status.RowsReplicated, &status.BacklogRows, &status.EncryptedInTransit, &status.EncryptedAtRest, &status.KeyVersion, &status.LastSyncAt, &status.NextSyncAt, &status.AuditCursor, &status.UpdatedAt); err != nil {
		return nil, err
	}
	return &status, nil
}

func ensureShareJSONDefaults(req *models.CreateShareRequest) {
	if len(req.Selector) == 0 {
		req.Selector = json.RawMessage(`{}`)
	}
	if len(req.ProviderSchema) == 0 {
		req.ProviderSchema = json.RawMessage(`{}`)
	}
	if len(req.ConsumerSchema) == 0 {
		req.ConsumerSchema = json.RawMessage(`{}`)
	}
	if len(req.SampleRows) == 0 {
		req.SampleRows = json.RawMessage(`[]`)
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func applyPeerUpdate(peer *models.PeerOrganization, req models.UpdatePeerRequest) {
	if req.DisplayName != nil {
		peer.DisplayName = *req.DisplayName
	}
	if req.OrganizationType != nil && strings.TrimSpace(*req.OrganizationType) != "" {
		peer.OrganizationType = *req.OrganizationType
	}
	if req.Region != nil {
		peer.Region = *req.Region
	}
	if req.EndpointURL != nil {
		peer.EndpointURL = *req.EndpointURL
	}
	if req.TrustLevel != nil {
		peer.TrustLevel = *req.TrustLevel
	}
	if req.SharedScopes != nil {
		peer.SharedScopes = *req.SharedScopes
	}
	if req.Status != nil {
		peer.Status = *req.Status
	}
	if req.LifecycleStage != nil {
		peer.LifecycleStage = *req.LifecycleStage
	}
	if req.AdminContacts != nil {
		peer.AdminContacts = *req.AdminContacts
	}
}

func applySyncStatusUpdate(status *models.SyncStatus, req models.SyncStatusUpdateRequest) {
	if req.Status != nil {
		status.Status = *req.Status
	}
	if req.RowsReplicated != nil {
		status.RowsReplicated = *req.RowsReplicated
	}
	if req.BacklogRows != nil {
		status.BacklogRows = *req.BacklogRows
	}
	if req.EncryptedInTransit != nil {
		status.EncryptedInTransit = *req.EncryptedInTransit
	}
	if req.EncryptedAtRest != nil {
		status.EncryptedAtRest = *req.EncryptedAtRest
	}
	if req.KeyVersion != nil {
		status.KeyVersion = *req.KeyVersion
	}
	if req.LastSyncAt != nil {
		status.LastSyncAt = req.LastSyncAt
	}
	if req.NextSyncAt != nil {
		status.NextSyncAt = req.NextSyncAt
	}
	if req.AuditCursor != nil {
		status.AuditCursor = *req.AuditCursor
	}
}

func mapPGError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: duplicate product distribution resource", ErrValidation)
	}
	return err
}
