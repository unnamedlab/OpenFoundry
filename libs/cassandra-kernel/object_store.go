package cassandrakernel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"

	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// ObjectStore (P2.5.2) is the Cassandra-backed implementation of
// repos.ObjectStore mirroring libs/cassandra-kernel/src/repos.rs::
// CassandraObjectStore. It writes against the `objects_by_id`
// primary table plus three secondary index tables
// (`objects_by_type`, `objects_by_owner`, `objects_by_marking`).
//
// Optimistic concurrency is enforced with single-row LWTs
// (INSERT IF NOT EXISTS / UPDATE IF revision_number = ?) per
// ADR-0020 §"opt-in strong reads".
//
// Index writes are best-effort fan-out (3 unbatched executes
// across 3 partitions). Drift is repaired by tools/of-cli reindex.
type ObjectStore struct {
	session  *gocql.Session
	keyspace string
}

// NewObjectStore builds a store bound to the standard
// `ontology_objects` keyspace.
func NewObjectStore(session *gocql.Session) *ObjectStore {
	return &ObjectStore{session: session, keyspace: "ontology_objects"}
}

// NewObjectStoreWithKeyspace allows a custom keyspace (multi-tenant
// override).
func NewObjectStoreWithKeyspace(session *gocql.Session, keyspace string) *ObjectStore {
	return &ObjectStore{session: session, keyspace: keyspace}
}

// Compile-time interface assertion.
var _ repos.ObjectStore = (*ObjectStore)(nil)

// CQL prepared statements — gocql caches statements automatically
// per session, so we just re-issue the same string every call.
// These accessors make the SQL site-of-truth obvious.

func (s *ObjectStore) cqlInsertIfNotExists() string {
	return fmt.Sprintf(
		`INSERT INTO %s.objects_by_id
            (tenant, object_id, type_id, owner_id, properties, marking,
             organization_id, revision_number, created_at, updated_at, deleted)
         VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, false) IF NOT EXISTS`,
		s.keyspace)
}

func (s *ObjectStore) cqlUpdateIfVersion() string {
	return fmt.Sprintf(
		`UPDATE %s.objects_by_id
            SET type_id = ?, owner_id = ?, properties = ?, marking = ?,
                organization_id = ?, revision_number = ?, updated_at = ?,
                deleted = false
          WHERE tenant = ? AND object_id = ?
          IF revision_number = ?`, s.keyspace)
}

func (s *ObjectStore) cqlSelectByID() string {
	return fmt.Sprintf(
		`SELECT type_id, owner_id, properties, marking, organization_id,
                revision_number, created_at, updated_at, deleted
           FROM %s.objects_by_id WHERE tenant = ? AND object_id = ?`,
		s.keyspace)
}

func (s *ObjectStore) cqlSoftDeleteByID() string {
	return fmt.Sprintf(
		`UPDATE %s.objects_by_id SET deleted = true, updated_at = ?
          WHERE tenant = ? AND object_id = ?`, s.keyspace)
}

func (s *ObjectStore) cqlInsertIndexByType() string {
	return fmt.Sprintf(
		`INSERT INTO %s.objects_by_type
            (tenant, type_id, updated_at, object_id, owner_id, marking,
             properties_summary, deleted)
         VALUES (?, ?, ?, ?, ?, ?, ?, false)`, s.keyspace)
}

func (s *ObjectStore) cqlInsertIndexByOwner() string {
	return fmt.Sprintf(
		`INSERT INTO %s.objects_by_owner
            (tenant, owner_id, type_id, object_id, updated_at, deleted)
         VALUES (?, ?, ?, ?, ?, false)`, s.keyspace)
}

func (s *ObjectStore) cqlInsertIndexByMarking() string {
	return fmt.Sprintf(
		`INSERT INTO %s.objects_by_marking
            (tenant, marking_id, object_id, type_id, owner_id, updated_at, deleted)
         VALUES (?, ?, ?, ?, ?, ?, false)`, s.keyspace)
}

func (s *ObjectStore) cqlSelectByType() string {
	return fmt.Sprintf(
		`SELECT object_id, owner_id, marking, properties_summary,
                updated_at, deleted
           FROM %s.objects_by_type WHERE tenant = ? AND type_id = ?`,
		s.keyspace)
}

func (s *ObjectStore) cqlSelectByOwner() string {
	return fmt.Sprintf(
		`SELECT object_id, type_id, updated_at, deleted
           FROM %s.objects_by_owner WHERE tenant = ? AND owner_id = ?`,
		s.keyspace)
}

func (s *ObjectStore) cqlSelectByMarking() string {
	return fmt.Sprintf(
		`SELECT object_id, type_id, owner_id, updated_at, deleted
           FROM %s.objects_by_marking WHERE tenant = ? AND marking_id = ?`,
		s.keyspace)
}

// Get fetches one object by (tenant, id).
func (s *ObjectStore) Get(
	ctx context.Context,
	tenant repos.TenantId,
	id repos.ObjectId,
	consistency repos.ReadConsistency,
) (*repos.Object, error) {
	objectUUID, err := parseUUID("object_id", string(id))
	if err != nil {
		return nil, err
	}

	q := s.session.Query(s.cqlSelectByID(), tenantStr(tenant), objectUUID).
		WithContext(ctx).
		Consistency(cqlConsistency(consistency))

	var (
		typeID       string
		ownerID      gocql.UUID
		properties   string
		marking      []string
		orgID        *gocql.UUID
		revision     int64
		createdAt    time.Time
		updatedAt    time.Time
		deleted      bool
	)
	err = q.Scan(&typeID, &ownerID, &properties, &marking, &orgID, &revision, &createdAt, &updatedAt, &deleted)
	if err == gocql.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, driverErr(err)
	}
	if deleted {
		return nil, nil
	}

	var payload json.RawMessage = []byte(properties)
	if !json.Valid(payload) {
		return nil, repos.Backendf("invalid stored JSON: not parseable")
	}

	var orgStr *string
	if orgID != nil {
		s := orgID.String()
		orgStr = &s
	}
	createdMs := createdAt.UnixMilli()
	owner := repos.OwnerId(ownerID.String())
	markings := make([]repos.MarkingId, 0, len(marking))
	for _, m := range marking {
		markings = append(markings, repos.MarkingId(m))
	}

	return &repos.Object{
		Tenant:         tenant,
		ID:             id,
		TypeID:         repos.TypeId(typeID),
		Version:        uint64(revision),
		Payload:        payload,
		OrganizationID: orgStr,
		CreatedAtMs:    &createdMs,
		UpdatedAtMs:    updatedAt.UnixMilli(),
		Owner:          &owner,
		Markings:       markings,
	}, nil
}

// Put inserts or updates with optimistic concurrency.
func (s *ObjectStore) Put(
	ctx context.Context,
	obj repos.Object,
	expectedVersion *uint64,
) (repos.PutOutcome, error) {
	objectUUID, err := parseUUID("object_id", string(obj.ID))
	if err != nil {
		return repos.PutOutcome{}, err
	}
	if obj.Owner == nil {
		return repos.PutOutcome{}, invalidArg("Object.owner is required by the Cassandra schema")
	}
	ownerUUID, err := parseUUID("owner", string(*obj.Owner))
	if err != nil {
		return repos.PutOutcome{}, err
	}
	orgUUID, err := parseUUIDOpt("organization_id", obj.OrganizationID)
	if err != nil {
		return repos.PutOutcome{}, err
	}
	properties, err := canonicalJSON(obj.Payload)
	if err != nil {
		return repos.PutOutcome{}, invalidArgf("payload is not serialisable: %v", err)
	}
	markings := make([]string, 0, len(obj.Markings))
	for _, m := range obj.Markings {
		markings = append(markings, string(m))
	}
	updatedAt := time.UnixMilli(obj.UpdatedAtMs).UTC()
	createdAt := updatedAt
	if obj.CreatedAtMs != nil {
		createdAt = time.UnixMilli(*obj.CreatedAtMs).UTC()
	}

	if expectedVersion == nil {
		// INSERT IF NOT EXISTS path.
		applied, actual, err := s.applyInsert(ctx, tenantStr(obj.Tenant), objectUUID,
			string(obj.TypeID), ownerUUID, properties, markings, orgUUID,
			createdAt, updatedAt)
		if err != nil {
			return repos.PutOutcome{}, err
		}
		if !applied {
			return repos.VersionConflict(0, actual), nil
		}
		if err := s.writeIndexes(ctx, tenantStr(obj.Tenant), objectUUID,
			string(obj.TypeID), ownerUUID, markings,
			truncateSummary(properties), updatedAt); err != nil {
			return repos.PutOutcome{}, err
		}
		return repos.Inserted(), nil
	}

	expected := *expectedVersion
	newVersion := int64(expected) + 1
	applied, actual, err := s.applyUpdate(ctx, tenantStr(obj.Tenant), objectUUID,
		string(obj.TypeID), ownerUUID, properties, markings, orgUUID,
		newVersion, updatedAt, int64(expected))
	if err != nil {
		return repos.PutOutcome{}, err
	}
	if !applied {
		if actual == 0 {
			actual = expected // best-effort fallback when LWT didn't surface a value
		}
		return repos.VersionConflict(expected, actual), nil
	}
	if err := s.writeIndexes(ctx, tenantStr(obj.Tenant), objectUUID,
		string(obj.TypeID), ownerUUID, markings,
		truncateSummary(properties), updatedAt); err != nil {
		return repos.PutOutcome{}, err
	}
	return repos.Updated(expected, uint64(newVersion)), nil
}

func (s *ObjectStore) applyInsert(
	ctx context.Context,
	tenant string,
	objectUUID gocql.UUID,
	typeID string,
	ownerUUID gocql.UUID,
	properties string,
	markings []string,
	orgUUID *gocql.UUID,
	createdAt, updatedAt time.Time,
) (applied bool, actualVersion uint64, err error) {
	q := s.session.Query(s.cqlInsertIfNotExists(),
		tenant, objectUUID, typeID, ownerUUID, properties, markings,
		orgUUID, createdAt, updatedAt).
		WithContext(ctx).
		SerialConsistency(gocql.LocalSerial)

	row := map[string]any{}
	applied, err = q.MapScanCAS(row)
	if err != nil {
		return false, 0, driverErr(err)
	}
	if !applied {
		actualVersion = revisionFromCAS(row)
	}
	return applied, actualVersion, nil
}

func (s *ObjectStore) applyUpdate(
	ctx context.Context,
	tenant string,
	objectUUID gocql.UUID,
	typeID string,
	ownerUUID gocql.UUID,
	properties string,
	markings []string,
	orgUUID *gocql.UUID,
	newVersion int64,
	updatedAt time.Time,
	expectedVersion int64,
) (applied bool, actualVersion uint64, err error) {
	q := s.session.Query(s.cqlUpdateIfVersion(),
		typeID, ownerUUID, properties, markings, orgUUID,
		newVersion, updatedAt,
		tenant, objectUUID, expectedVersion).
		WithContext(ctx).
		SerialConsistency(gocql.LocalSerial)

	row := map[string]any{}
	applied, err = q.MapScanCAS(row)
	if err != nil {
		return false, 0, driverErr(err)
	}
	if !applied {
		actualVersion = revisionFromCAS(row)
	}
	return applied, actualVersion, nil
}

// revisionFromCAS pulls revision_number out of a non-applied LWT
// result map. gocql's MapScanCAS populates the map with the columns
// the IF clause projects on conflict; revision_number is what our
// IF UPDATE compares so we project it on every miss.
func revisionFromCAS(row map[string]any) uint64 {
	if v, ok := row["revision_number"]; ok {
		switch n := v.(type) {
		case int64:
			return uint64(n)
		case int:
			return uint64(n)
		case int32:
			return uint64(n)
		}
	}
	// Fall back to any int64 column projected by the conflict — the
	// Rust impl had the same best-effort behaviour.
	for _, v := range row {
		if n, ok := v.(int64); ok {
			return uint64(n)
		}
	}
	return 0
}

// writeIndexes performs the 3-table fan-out write that the Rust
// impl does. We do not LOGGED-batch because the partitions sit
// across 3 tables (LOGGED batch is the worst case per ADR-0020).
// Drift between the primary and the indexes is repaired by the
// of-cli reindex job.
func (s *ObjectStore) writeIndexes(
	ctx context.Context,
	tenant string,
	objectUUID gocql.UUID,
	typeID string,
	ownerUUID gocql.UUID,
	markings []string,
	propertiesSummary string,
	updatedAt time.Time,
) error {
	if err := s.session.Query(s.cqlInsertIndexByType(),
		tenant, typeID, updatedAt, objectUUID, ownerUUID, markings, propertiesSummary).
		WithContext(ctx).Exec(); err != nil {
		return driverErr(err)
	}
	if err := s.session.Query(s.cqlInsertIndexByOwner(),
		tenant, ownerUUID, typeID, objectUUID, updatedAt).
		WithContext(ctx).Exec(); err != nil {
		return driverErr(err)
	}
	for _, m := range markings {
		if err := s.session.Query(s.cqlInsertIndexByMarking(),
			tenant, m, objectUUID, typeID, ownerUUID, updatedAt).
			WithContext(ctx).Exec(); err != nil {
			return driverErr(err)
		}
	}
	return nil
}

// Delete soft-deletes the object by flipping `deleted = true` on
// the primary row. Index tombstoning happens lazily via the
// `deleted` column on each index row (consumers filter on it).
func (s *ObjectStore) Delete(
	ctx context.Context,
	tenant repos.TenantId,
	id repos.ObjectId,
) (bool, error) {
	objectUUID, err := parseUUID("object_id", string(id))
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	if err := s.session.Query(s.cqlSoftDeleteByID(), now, tenantStr(tenant), objectUUID).
		WithContext(ctx).Exec(); err != nil {
		return false, driverErr(err)
	}
	// Cassandra does not surface rows_affected for non-LWT writes.
	// Caller is responsible for treating double-deletes as no-ops.
	return true, nil
}

// ListByType pages through every object of a given type.
func (s *ObjectStore) ListByType(
	ctx context.Context,
	tenant repos.TenantId,
	typeID repos.TypeId,
	page repos.Page,
	consistency repos.ReadConsistency,
) (repos.PagedResult[repos.Object], error) {
	pagingState, err := decodePagingState(page.Token)
	if err != nil {
		return repos.PagedResult[repos.Object]{}, err
	}

	q := s.session.Query(s.cqlSelectByType(), tenantStr(tenant), string(typeID)).
		WithContext(ctx).
		Consistency(cqlConsistency(consistency)).
		PageSize(clampPageSize(page.Size))
	if len(pagingState) > 0 {
		q = q.PageState(pagingState)
	}
	iter := q.Iter()

	out := repos.PagedResult[repos.Object]{Items: []repos.Object{}}
	var (
		objectID  gocql.UUID
		ownerID   gocql.UUID
		marking   []string
		summary   *string
		updatedAt time.Time
		deleted   bool
	)
	for iter.Scan(&objectID, &ownerID, &marking, &summary, &updatedAt, &deleted) {
		if deleted {
			continue
		}
		var payload json.RawMessage = []byte("{}")
		if summary != nil && json.Valid([]byte(*summary)) {
			payload = json.RawMessage([]byte(*summary))
		}
		owner := repos.OwnerId(ownerID.String())
		markings := make([]repos.MarkingId, 0, len(marking))
		for _, m := range marking {
			markings = append(markings, repos.MarkingId(m))
		}
		updatedMs := updatedAt.UnixMilli()
		out.Items = append(out.Items, repos.Object{
			Tenant:         tenant,
			ID:             repos.ObjectId(objectID.String()),
			TypeID:         typeID,
			Version:        0, // index row does not carry the revision; caller `Get`s for that.
			Payload:        payload,
			OrganizationID: organizationIDFromTenant(tenant),
			CreatedAtMs:    &updatedMs,
			UpdatedAtMs:    updatedMs,
			Owner:          &owner,
			Markings:       markings,
		})
	}
	if pageBytes := iter.PageState(); len(pageBytes) > 0 {
		out.NextToken = encodePagingState(pageBytes)
	}
	if err := iter.Close(); err != nil {
		return repos.PagedResult[repos.Object]{}, driverErr(err)
	}
	return out, nil
}

// ListByOwner pages through every object owned by `owner`.
func (s *ObjectStore) ListByOwner(
	ctx context.Context,
	tenant repos.TenantId,
	owner repos.OwnerId,
	page repos.Page,
	consistency repos.ReadConsistency,
) (repos.PagedResult[repos.Object], error) {
	ownerUUID, err := parseUUID("owner_id", string(owner))
	if err != nil {
		return repos.PagedResult[repos.Object]{}, err
	}
	pagingState, err := decodePagingState(page.Token)
	if err != nil {
		return repos.PagedResult[repos.Object]{}, err
	}

	q := s.session.Query(s.cqlSelectByOwner(), tenantStr(tenant), ownerUUID).
		WithContext(ctx).
		Consistency(cqlConsistency(consistency)).
		PageSize(clampPageSize(page.Size))
	if len(pagingState) > 0 {
		q = q.PageState(pagingState)
	}
	iter := q.Iter()

	out := repos.PagedResult[repos.Object]{Items: []repos.Object{}}
	var (
		objectID  gocql.UUID
		typeID    string
		updatedAt time.Time
		deleted   bool
	)
	for iter.Scan(&objectID, &typeID, &updatedAt, &deleted) {
		if deleted {
			continue
		}
		ownerCopy := owner
		updatedMs := updatedAt.UnixMilli()
		out.Items = append(out.Items, repos.Object{
			Tenant:         tenant,
			ID:             repos.ObjectId(objectID.String()),
			TypeID:         repos.TypeId(typeID),
			Version:        0,
			Payload:        json.RawMessage("{}"),
			OrganizationID: organizationIDFromTenant(tenant),
			CreatedAtMs:    &updatedMs,
			UpdatedAtMs:    updatedMs,
			Owner:          &ownerCopy,
			Markings:       nil,
		})
	}
	if pageBytes := iter.PageState(); len(pageBytes) > 0 {
		out.NextToken = encodePagingState(pageBytes)
	}
	if err := iter.Close(); err != nil {
		return repos.PagedResult[repos.Object]{}, driverErr(err)
	}
	return out, nil
}

// ListByMarking pages through every object bearing `marking`.
func (s *ObjectStore) ListByMarking(
	ctx context.Context,
	tenant repos.TenantId,
	marking repos.MarkingId,
	page repos.Page,
	consistency repos.ReadConsistency,
) (repos.PagedResult[repos.Object], error) {
	pagingState, err := decodePagingState(page.Token)
	if err != nil {
		return repos.PagedResult[repos.Object]{}, err
	}

	q := s.session.Query(s.cqlSelectByMarking(), tenantStr(tenant), string(marking)).
		WithContext(ctx).
		Consistency(cqlConsistency(consistency)).
		PageSize(clampPageSize(page.Size))
	if len(pagingState) > 0 {
		q = q.PageState(pagingState)
	}
	iter := q.Iter()

	out := repos.PagedResult[repos.Object]{Items: []repos.Object{}}
	var (
		objectID  gocql.UUID
		typeID    string
		ownerID   gocql.UUID
		updatedAt time.Time
		deleted   bool
	)
	for iter.Scan(&objectID, &typeID, &ownerID, &updatedAt, &deleted) {
		if deleted {
			continue
		}
		owner := repos.OwnerId(ownerID.String())
		markingCopy := marking
		updatedMs := updatedAt.UnixMilli()
		out.Items = append(out.Items, repos.Object{
			Tenant:         tenant,
			ID:             repos.ObjectId(objectID.String()),
			TypeID:         repos.TypeId(typeID),
			Version:        0,
			Payload:        json.RawMessage("{}"),
			OrganizationID: organizationIDFromTenant(tenant),
			CreatedAtMs:    &updatedMs,
			UpdatedAtMs:    updatedMs,
			Owner:          &owner,
			Markings:       []repos.MarkingId{markingCopy},
		})
	}
	if pageBytes := iter.PageState(); len(pageBytes) > 0 {
		out.NextToken = encodePagingState(pageBytes)
	}
	if err := iter.Close(); err != nil {
		return repos.PagedResult[repos.Object]{}, driverErr(err)
	}
	return out, nil
}

// canonicalJSON normalises a possibly-empty json.RawMessage into a
// canonical JSON string for storage. Empty/nil → "{}". Validates the
// input is parseable so we don't write corrupted JSON.
func canonicalJSON(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "{}", nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "{}", nil
	}
	if !json.Valid(raw) {
		return "", fmt.Errorf("payload is not valid JSON")
	}
	// Round-trip through Marshal to canonicalise key ordering.
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	out, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
