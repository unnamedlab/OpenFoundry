package handlers

// spaces.go ports services/tenancy-organizations-service/src/handlers/spaces.rs.
//
// The Rust handler exposes the Nexus space CRUD surface against
// state.nexus_db (the federation Postgres pool). It validates peer
// references against the nexus_peers table, enforces the static
// space_kind/status enums, and round-trips member_peer_ids +
// governance_tags through the JSONB columns. Wire-format and error
// strings are byte-exact with the Rust source.
//
// The handler holds a *pgxpool.Pool directly (mirroring the Rust
// `state.nexus_db` field) instead of going through the foundation
// repo.Repo — this matches Rust's inline-SQL style and keeps the SQL
// adjacency to the handler logic.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
)

// SpacesHandlers owns the nexus_spaces HTTP surface. The Pool is the
// same shared pgxpool used by the rest of the service — the Rust crate
// carries a separate `nexus_db` handle, but the Go port runs against a
// single DATABASE_URL so we reuse the foundation pool.
type SpacesHandlers struct {
	Pool *pgxpool.Pool
}

// ─── list ──────────────────────────────────────────────────────────────

// ListSpaces mirrors Rust `list_spaces` — the response envelope is
// `{ "items": [...] }` to stay byte-exact with `ListResponse<NexusSpace>`.
func (h *SpacesHandlers) ListSpaces(w http.ResponseWriter, r *http.Request) {
	items, err := loadSpaces(r.Context(), h.Pool)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("database error: %s", err))
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.NexusSpace]{Items: items})
}

// ─── create ────────────────────────────────────────────────────────────

// CreateSpace mirrors Rust `create_space`. Validation, ID minting and
// JSONB serialisation happen up-front; the returned record is the row
// freshly reloaded from Postgres so callers see the canonical
// representation (including server-side defaults).
func (h *SpacesHandlers) CreateSpace(w http.ResponseWriter, r *http.Request) {
	var req models.CreateSpaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}

	if status, msg := validateSpaceRequest(r.Context(), h.Pool,
		req.Slug, req.DisplayName, req.SpaceKind,
		req.OwnerPeerID, req.MemberPeerIDs, req.Status,
	); msg != "" {
		writeJSONErr(w, status, msg)
		return
	}

	id := ids.New()
	now := time.Now().UTC()
	memberPeerIDs, err := jsonMarshalSlice(req.MemberPeerIDs)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	governanceTags, err := jsonMarshalStringSlice(req.GovernanceTags)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO nexus_spaces (id, slug, display_name, description, space_kind, owner_peer_id, region, member_peer_ids, governance_tags, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11, $12)`,
		id, req.Slug, req.DisplayName, req.Description, req.SpaceKind,
		req.OwnerPeerID, req.Region, memberPeerIDs, governanceTags,
		req.Status, now, now,
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("database error: %s", err))
		return
	}

	space, err := loadSpaceByID(r.Context(), h.Pool, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("database error: %s", err))
		return
	}
	if space == nil {
		writeJSONErr(w, http.StatusInternalServerError, "created space could not be reloaded")
		return
	}
	writeJSON(w, http.StatusOK, space)
}

// ─── update ────────────────────────────────────────────────────────────

// UpdateSpace mirrors Rust `update_space`. PATCH semantics: every
// optional field falls through to the existing column when absent.
// Validation runs against the merged payload before any UPDATE.
func (h *SpacesHandlers) UpdateSpace(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	var req models.UpdateSpaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}

	current, err := loadSpaceByID(r.Context(), h.Pool, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("database error: %s", err))
		return
	}
	if current == nil {
		writeJSONErr(w, http.StatusNotFound, "space not found")
		return
	}

	ownerPeerID := current.OwnerPeerID
	if req.OwnerPeerID != nil {
		ownerPeerID = req.OwnerPeerID
	}
	memberPeerIDs := append([]uuid.UUID(nil), current.MemberPeerIDs...)
	if req.MemberPeerIDs != nil {
		memberPeerIDs = append([]uuid.UUID(nil), (*req.MemberPeerIDs)...)
	}
	displayName := current.DisplayName
	if req.DisplayName != nil {
		displayName = *req.DisplayName
	}
	status := current.Status
	if req.Status != nil {
		status = *req.Status
	}

	if errStatus, msg := validateSpaceRequest(r.Context(), h.Pool,
		current.Slug, displayName, current.SpaceKind,
		ownerPeerID, memberPeerIDs, status,
	); msg != "" {
		writeJSONErr(w, errStatus, msg)
		return
	}

	description := current.Description
	if req.Description != nil {
		description = *req.Description
	}
	region := current.Region
	if req.Region != nil {
		region = *req.Region
	}
	governanceTags := current.GovernanceTags
	if req.GovernanceTags != nil {
		governanceTags = *req.GovernanceTags
	}
	memberPeerIDsJSON, err := jsonMarshalSlice(memberPeerIDs)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	governanceTagsJSON, err := jsonMarshalStringSlice(governanceTags)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now().UTC()
	_, err = h.Pool.Exec(r.Context(),
		`UPDATE nexus_spaces
		 SET display_name = $2,
		     description = $3,
		     owner_peer_id = $4,
		     region = $5,
		     member_peer_ids = $6::jsonb,
		     governance_tags = $7::jsonb,
		     status = $8,
		     updated_at = $9
		 WHERE id = $1`,
		id, displayName, description, ownerPeerID, region,
		memberPeerIDsJSON, governanceTagsJSON, status, now,
	)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("database error: %s", err))
		return
	}

	space, err := loadSpaceByID(r.Context(), h.Pool, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("database error: %s", err))
		return
	}
	if space == nil {
		writeJSONErr(w, http.StatusInternalServerError, "updated space could not be reloaded")
		return
	}
	writeJSON(w, http.StatusOK, space)
}

// ─── validation ────────────────────────────────────────────────────────

// validateSpaceRequest mirrors Rust `validate_space_request`. The
// returned status is the HTTP code to write; an empty msg means
// validation passed. Error strings are byte-exact with the Rust source
// so federated callers see identical payloads regardless of language.
func validateSpaceRequest(
	ctx context.Context,
	pool *pgxpool.Pool,
	slug, displayName, spaceKind string,
	ownerPeerID *uuid.UUID,
	memberPeerIDs []uuid.UUID,
	status string,
) (int, string) {
	if strings.TrimSpace(slug) == "" || strings.TrimSpace(displayName) == "" {
		return http.StatusBadRequest, "space slug and display name are required"
	}
	if spaceKind != "private" && spaceKind != "shared" {
		return http.StatusBadRequest, "space_kind must be private or shared"
	}
	if status != "draft" && status != "active" && status != "paused" {
		return http.StatusBadRequest, "space status must be draft, active or paused"
	}

	if ownerPeerID == nil && len(memberPeerIDs) == 0 {
		return 0, ""
	}

	knownPeerIDs, err := loadPeerIDSet(ctx, pool)
	if err != nil {
		return http.StatusInternalServerError, fmt.Sprintf("database error: %s", err)
	}
	if ownerPeerID != nil {
		if _, ok := knownPeerIDs[*ownerPeerID]; !ok {
			return http.StatusBadRequest, "owner_peer_id does not exist"
		}
	}
	for _, peerID := range memberPeerIDs {
		if _, ok := knownPeerIDs[peerID]; !ok {
			return http.StatusBadRequest, "member_peer_ids contains unknown peer references"
		}
	}
	return 0, ""
}

// ─── DB helpers ────────────────────────────────────────────────────────

// loadSpaces mirrors Rust `load_spaces` — fetched in updated_at DESC
// order to match the Rust query's ORDER BY clause.
func loadSpaces(ctx context.Context, pool *pgxpool.Pool) ([]models.NexusSpace, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, slug, display_name, description, space_kind, owner_peer_id, region, member_peer_ids, governance_tags, status, created_at, updated_at
		 FROM nexus_spaces
		 ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.NexusSpace, 0)
	for rows.Next() {
		s, err := scanSpaceRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// loadSpaceByID mirrors Rust `load_space_row` followed by
// `NexusSpace::try_from(row)` — the Go scan does both in one shot.
func loadSpaceByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*models.NexusSpace, error) {
	row := pool.QueryRow(ctx,
		`SELECT id, slug, display_name, description, space_kind, owner_peer_id, region, member_peer_ids, governance_tags, status, created_at, updated_at
		 FROM nexus_spaces WHERE id = $1`,
		id,
	)
	s, err := scanSpaceRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// loadPeerIDSet pulls just the peer IDs for FK validation. The Rust
// helper `load_peers` returns the full PeerOrganization decode, but
// `validate_space_request` only consults `peer.id`, so this trimmed
// query keeps the spaces handler independent of the (yet-to-be-ported)
// peers handler while staying byte-equivalent on the validation
// outcomes.
func loadPeerIDSet(ctx context.Context, pool *pgxpool.Pool) (map[uuid.UUID]struct{}, error) {
	rows, err := pool.Query(ctx, `SELECT id FROM nexus_peers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]struct{}{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}

// rowScanner is the minimal Scan-only contract shared by *pgx.Row and
// pgx.Rows so scanSpaceRow works for both single-row QueryRow and
// streamed Query result sets.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanSpaceRow accepts anything Scan-able — pgx.Row from QueryRow and
// pgx.Rows from Query both satisfy rowScanner.
func scanSpaceRow(r rowScanner) (*models.NexusSpace, error) {
	s := &models.NexusSpace{}
	var memberPeerIDs, governanceTags []byte
	if err := r.Scan(
		&s.ID, &s.Slug, &s.DisplayName, &s.Description, &s.SpaceKind,
		&s.OwnerPeerID, &s.Region, &memberPeerIDs, &governanceTags,
		&s.Status, &s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(memberPeerIDs, &s.MemberPeerIDs); err != nil {
		return nil, fmt.Errorf("invalid member_peer_ids: %s", err)
	}
	if err := json.Unmarshal(governanceTags, &s.GovernanceTags); err != nil {
		return nil, fmt.Errorf("invalid governance_tags: %s", err)
	}
	if s.MemberPeerIDs == nil {
		s.MemberPeerIDs = []uuid.UUID{}
	}
	if s.GovernanceTags == nil {
		s.GovernanceTags = []string{}
	}
	return s, nil
}

// jsonMarshalSlice serialises the member_peer_ids slice. nil collapses
// to "[]" so the JSONB column never holds a SQL NULL — matching the
// Rust `serde_json::to_value(&Vec::new())` behaviour.
func jsonMarshalSlice(v []uuid.UUID) ([]byte, error) {
	if v == nil {
		v = []uuid.UUID{}
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode member_peer_ids: %s", err)
	}
	return out, nil
}

// jsonMarshalStringSlice serialises governance_tags. Same nil → "[]"
// rule as jsonMarshalSlice.
func jsonMarshalStringSlice(v []string) ([]byte, error) {
	if v == nil {
		v = []string{}
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode governance_tags: %s", err)
	}
	return out, nil
}
