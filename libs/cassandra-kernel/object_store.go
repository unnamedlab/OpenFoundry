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
	session       *gocql.Session
	keyspace      string
	indexKeyspace string
}

// NewObjectStore builds a store bound to the standard
// `ontology_objects` keyspace.
func NewObjectStore(session *gocql.Session) *ObjectStore {
	return &ObjectStore{session: session, keyspace: "ontology_objects", indexKeyspace: "ontology_indexes"}
}

// NewObjectStoreWithKeyspace allows a custom keyspace (multi-tenant
// override).
func NewObjectStoreWithKeyspace(session *gocql.Session, keyspace string) *ObjectStore {
	return &ObjectStore{session: session, keyspace: keyspace, indexKeyspace: "ontology_indexes"}
}

// NewObjectStoreWithKeyspaces allows callers to bind the object rows and OSV2
// property-index rows to separate keyspaces.
func NewObjectStoreWithKeyspaces(session *gocql.Session, objectKeyspace, indexKeyspace string) *ObjectStore {
	return &ObjectStore{session: session, keyspace: objectKeyspace, indexKeyspace: indexKeyspace}
}

// Compile-time interface assertion.
var _ repos.ObjectStore = (*ObjectStore)(nil)

// CQL prepared statements — gocql caches statements automatically
// per session, so we just re-issue the same string every call.
// These accessors make the SQL site-of-truth obvious.

func (s *ObjectStore) cqlInsertIfNotExists() string {
	return fmt.Sprintf(
		`INSERT INTO %s.objects_by_id
            (tenant, type_id, primary_key_hash, object_id, rid, primary_key,
             owner_id, properties_blob, markings_blob, organizations,
             revision_number, created_at, updated_at, last_updated,
             last_updater, deleted)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, false) IF NOT EXISTS`,
		s.keyspace)
}

func (s *ObjectStore) cqlUpdateIfVersion() string {
	return fmt.Sprintf(
		`UPDATE %s.objects_by_id
            SET rid = ?, primary_key = ?, owner_id = ?, properties_blob = ?,
                markings_blob = ?, organizations = ?, revision_number = ?,
                updated_at = ?, last_updated = ?, last_updater = ?, deleted = false
          WHERE tenant = ? AND type_id = ? AND primary_key_hash = ? AND object_id = ?
          IF revision_number = ?`, s.keyspace)
}

func (s *ObjectStore) cqlSelectByID() string {
	return fmt.Sprintf(
		`SELECT type_id, owner_id, properties_blob, markings_blob, organizations,
                revision_number, created_at, updated_at, deleted
           FROM %s.objects_by_id_by_rid WHERE tenant = ? AND object_id = ?`,
		s.keyspace)
}

func (s *ObjectStore) cqlSelectByTypePrimaryKey() string {
	return fmt.Sprintf(
		`SELECT owner_id, properties_blob, markings_blob, organizations,
                revision_number, created_at, updated_at, deleted
           FROM %s.objects_by_id
          WHERE tenant = ? AND type_id = ? AND primary_key_hash = ? AND object_id = ?`,
		s.keyspace)
}

func (s *ObjectStore) cqlSoftDeleteByTypePrimaryKey() string {
	return fmt.Sprintf(
		`UPDATE %s.objects_by_id SET deleted = true, updated_at = ?, last_updated = ?
          WHERE tenant = ? AND type_id = ? AND primary_key_hash = ? AND object_id = ?`,
		s.keyspace)
}

func (s *ObjectStore) cqlSoftDeleteByID() string {
	return fmt.Sprintf(
		`UPDATE %s.objects_by_id_by_rid SET deleted = true, updated_at = ?, last_updated = ?
          WHERE tenant = ? AND object_id = ?`, s.keyspace)
}

func (s *ObjectStore) cqlInsertByRID() string {
	return fmt.Sprintf(
		`INSERT INTO %s.objects_by_id_by_rid
            (tenant, object_id, type_id, primary_key_hash, rid, primary_key,
             owner_id, properties_blob, markings_blob, organizations,
             revision_number, created_at, updated_at, last_updated,
             last_updater, deleted)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, false)`,
		s.keyspace)
}

func (s *ObjectStore) cqlInsertIndexByType() string {
	return fmt.Sprintf(
		`INSERT INTO %s.objects_by_type
            (tenant, type_id, primary_key_hash, updated_at, object_id, owner_id,
             markings_blob, properties_summary, deleted)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, false)`, s.keyspace)
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
		`SELECT object_id, owner_id, markings_blob, properties_summary,
                updated_at, deleted
           FROM %s.objects_by_type WHERE tenant = ? AND type_id = ? AND primary_key_hash = ?`,
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
		typeID         string
		ownerID        gocql.UUID
		propertiesBlob []byte
		markingsBlob   []byte
		orgID          *gocql.UUID
		revision       int64
		createdAt      time.Time
		updatedAt      time.Time
		deleted        bool
	)
	err = q.Scan(&typeID, &ownerID, &propertiesBlob, &markingsBlob, &orgID, &revision, &createdAt, &updatedAt, &deleted)
	if err == gocql.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, driverErr(err)
	}
	if deleted {
		return nil, nil
	}

	payload, err := decodePropertiesBlob(propertiesBlob)
	if err != nil {
		return nil, repos.Backendf("invalid stored properties_blob: %v", err)
	}
	marking, err := decodeMarkingsBlob(markingsBlob)
	if err != nil {
		return nil, repos.Backendf("invalid stored markings_blob: %v", err)
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

// GetByTypeAndPrimaryKey fetches one object through the OSV2 hot-row partition
// `(tenant, type_id, primary_key_hash)`.
func (s *ObjectStore) GetByTypeAndPrimaryKey(
	ctx context.Context,
	tenant repos.TenantId,
	typeID repos.TypeId,
	primaryKey string,
	consistency repos.ReadConsistency,
) (*repos.Object, error) {
	objectUUID, err := parseUUID("primary_key", primaryKey)
	if err != nil {
		return nil, err
	}
	q := s.session.Query(s.cqlSelectByTypePrimaryKey(), tenantStr(tenant), string(typeID), primaryKeyHashBucket(primaryKey), objectUUID).
		WithContext(ctx).
		Consistency(cqlConsistency(consistency))
	var (
		ownerID        gocql.UUID
		propertiesBlob []byte
		markingsBlob   []byte
		orgID          *gocql.UUID
		revision       int64
		createdAt      time.Time
		updatedAt      time.Time
		deleted        bool
	)
	if err := q.Scan(&ownerID, &propertiesBlob, &markingsBlob, &orgID, &revision, &createdAt, &updatedAt, &deleted); err != nil {
		if err == gocql.ErrNotFound {
			return nil, nil
		}
		return nil, driverErr(err)
	}
	if deleted {
		return nil, nil
	}
	payload, err := decodePropertiesBlob(propertiesBlob)
	if err != nil {
		return nil, repos.Backendf("invalid stored properties_blob: %v", err)
	}
	marking, err := decodeMarkingsBlob(markingsBlob)
	if err != nil {
		return nil, repos.Backendf("invalid stored markings_blob: %v", err)
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
		ID:             repos.ObjectId(objectUUID.String()),
		TypeID:         typeID,
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
	propertiesBlob, err := encodePropertiesBlob(obj.Payload)
	if err != nil {
		return repos.PutOutcome{}, invalidArgf("payload is not serialisable as OSV2 properties_blob: %v", err)
	}
	markings := make([]string, 0, len(obj.Markings))
	for _, m := range obj.Markings {
		markings = append(markings, string(m))
	}
	markingsBlob, err := encodeMarkingsBlob(markings)
	if err != nil {
		return repos.PutOutcome{}, invalidArgf("markings are not serialisable as OSV2 markings_blob: %v", err)
	}
	updatedAt := time.UnixMilli(obj.UpdatedAtMs).UTC()
	createdAt := updatedAt
	if obj.CreatedAtMs != nil {
		createdAt = time.UnixMilli(*obj.CreatedAtMs).UTC()
	}
	primaryKey := string(obj.ID)

	if expectedVersion == nil {
		applied, actual, err := s.applyInsert(ctx, tenantStr(obj.Tenant), objectUUID,
			string(obj.TypeID), ownerUUID, propertiesBlob, markingsBlob, orgUUID,
			createdAt, updatedAt, primaryKey)
		if err != nil {
			return repos.PutOutcome{}, err
		}
		if !applied {
			return repos.VersionConflict(0, actual), nil
		}
		if err := s.writeObjectRIDMirror(ctx, tenantStr(obj.Tenant), objectUUID, string(obj.TypeID), ownerUUID, propertiesBlob, markingsBlob, orgUUID, uint64(1), createdAt, updatedAt, primaryKey); err != nil {
			return repos.PutOutcome{}, err
		}
		if err := s.writeIndexes(ctx, tenantStr(obj.Tenant), objectUUID,
			string(obj.TypeID), ownerUUID, markings, markingsBlob,
			truncateSummary(properties), updatedAt); err != nil {
			return repos.PutOutcome{}, err
		}
		if err := s.writePropertyIndexes(ctx, tenantStr(obj.Tenant), objectUUID, string(obj.TypeID), primaryKey, obj.Payload, updatedAt); err != nil {
			return repos.PutOutcome{}, err
		}
		return repos.Inserted(), nil
	}

	expected := *expectedVersion
	newVersion := int64(expected) + 1
	applied, actual, err := s.applyUpdate(ctx, tenantStr(obj.Tenant), objectUUID,
		string(obj.TypeID), ownerUUID, propertiesBlob, markingsBlob, orgUUID,
		newVersion, updatedAt, int64(expected), primaryKey)
	if err != nil {
		return repos.PutOutcome{}, err
	}
	if !applied {
		if actual == 0 {
			actual = expected
		}
		return repos.VersionConflict(expected, actual), nil
	}
	if err := s.writeObjectRIDMirror(ctx, tenantStr(obj.Tenant), objectUUID, string(obj.TypeID), ownerUUID, propertiesBlob, markingsBlob, orgUUID, uint64(newVersion), createdAt, updatedAt, primaryKey); err != nil {
		return repos.PutOutcome{}, err
	}
	if err := s.writeIndexes(ctx, tenantStr(obj.Tenant), objectUUID,
		string(obj.TypeID), ownerUUID, markings, markingsBlob,
		truncateSummary(properties), updatedAt); err != nil {
		return repos.PutOutcome{}, err
	}
	if err := s.writePropertyIndexes(ctx, tenantStr(obj.Tenant), objectUUID, string(obj.TypeID), primaryKey, obj.Payload, updatedAt); err != nil {
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
	propertiesBlob []byte,
	markingsBlob []byte,
	orgUUID *gocql.UUID,
	createdAt, updatedAt time.Time,
	primaryKey string,
) (applied bool, actualVersion uint64, err error) {
	q := s.session.Query(s.cqlInsertIfNotExists(),
		tenant, typeID, primaryKeyHashBucket(primaryKey), objectUUID, objectUUID.String(), primaryKey,
		ownerUUID, propertiesBlob, markingsBlob, orgUUID, createdAt, updatedAt, updatedAt,
		ownerUUID.String()).
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
	propertiesBlob []byte,
	markingsBlob []byte,
	orgUUID *gocql.UUID,
	newVersion int64,
	updatedAt time.Time,
	expectedVersion int64,
	primaryKey string,
) (applied bool, actualVersion uint64, err error) {
	q := s.session.Query(s.cqlUpdateIfVersion(),
		objectUUID.String(), primaryKey, ownerUUID, propertiesBlob, markingsBlob, orgUUID,
		newVersion, updatedAt, updatedAt, ownerUUID.String(),
		tenant, typeID, primaryKeyHashBucket(primaryKey), objectUUID, expectedVersion).
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

func (s *ObjectStore) writeObjectRIDMirror(
	ctx context.Context,
	tenant string,
	objectUUID gocql.UUID,
	typeID string,
	ownerUUID gocql.UUID,
	propertiesBlob []byte,
	markingsBlob []byte,
	orgUUID *gocql.UUID,
	version uint64,
	createdAt, updatedAt time.Time,
	primaryKey string,
) error {
	if err := s.session.Query(s.cqlInsertByRID(),
		tenant, objectUUID, typeID, primaryKeyHashBucket(primaryKey), objectUUID.String(), primaryKey,
		ownerUUID, propertiesBlob, markingsBlob, orgUUID, int64(version), createdAt, updatedAt, updatedAt,
		ownerUUID.String()).
		WithContext(ctx).Exec(); err != nil {
		return driverErr(err)
	}
	return nil
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
	markingsBlob []byte,
	propertiesSummary string,
	updatedAt time.Time,
) error {
	if err := s.session.Query(s.cqlInsertIndexByType(),
		tenant, typeID, primaryKeyHashBucket(objectUUID.String()), updatedAt, objectUUID, ownerUUID, markingsBlob, propertiesSummary).
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
	existing, err := s.Get(ctx, tenant, id, repos.Strong())
	if err != nil {
		return false, err
	}
	if existing == nil {
		return false, nil
	}
	now := time.Now().UTC()
	if err := s.session.Query(s.cqlSoftDeleteByID(), now, now, tenantStr(tenant), objectUUID).
		WithContext(ctx).Exec(); err != nil {
		return false, driverErr(err)
	}
	if err := s.session.Query(s.cqlSoftDeleteByTypePrimaryKey(), now, now, tenantStr(tenant), string(existing.TypeID), primaryKeyHashBucket(string(id)), objectUUID).
		WithContext(ctx).Exec(); err != nil {
		return false, driverErr(err)
	}
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
	// OSV2.1 buckets type partitions by primary_key_hash. The current repository
	// cursor is still one opaque token, so this read fans out over the 64 buckets
	// and returns a deterministic bounded page. Stable cross-bucket cursors land
	// with the OSV2.5 pagination work.
	limit := clampPageSize(page.Size)
	out := repos.PagedResult[repos.Object]{Items: []repos.Object{}}
	for bucket := 0; bucket < objectHashBuckets && len(out.Items) < limit; bucket++ {
		q := s.session.Query(s.cqlSelectByType(), tenantStr(tenant), string(typeID), bucket).
			WithContext(ctx).
			Consistency(cqlConsistency(consistency)).
			PageSize(limit)
		iter := q.Iter()
		var (
			objectID     gocql.UUID
			ownerID      gocql.UUID
			markingsBlob []byte
			summary      *string
			updatedAt    time.Time
			deleted      bool
		)
		for iter.Scan(&objectID, &ownerID, &markingsBlob, &summary, &updatedAt, &deleted) {
			if deleted {
				continue
			}
			var payload json.RawMessage = []byte("{}")
			if summary != nil && json.Valid([]byte(*summary)) {
				payload = json.RawMessage([]byte(*summary))
			}
			owner := repos.OwnerId(ownerID.String())
			marking, err := decodeMarkingsBlob(markingsBlob)
			if err != nil {
				iter.Close()
				return repos.PagedResult[repos.Object]{}, repos.Backendf("invalid stored markings_blob: %v", err)
			}
			markings := make([]repos.MarkingId, 0, len(marking))
			for _, m := range marking {
				markings = append(markings, repos.MarkingId(m))
			}
			updatedMs := updatedAt.UnixMilli()
			out.Items = append(out.Items, repos.Object{
				Tenant:         tenant,
				ID:             repos.ObjectId(objectID.String()),
				TypeID:         typeID,
				Version:        0,
				Payload:        payload,
				OrganizationID: organizationIDFromTenant(tenant),
				CreatedAtMs:    &updatedMs,
				UpdatedAtMs:    updatedMs,
				Owner:          &owner,
				Markings:       markings,
			})
			if len(out.Items) >= limit {
				break
			}
		}
		if err := iter.Close(); err != nil {
			return repos.PagedResult[repos.Object]{}, driverErr(err)
		}
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

func (s *ObjectStore) cqlSelectPropertyIndexExact() string {
	return fmt.Sprintf(
		`SELECT object_id FROM %s.object_property_index
          WHERE tenant = ? AND type_id = ? AND property_id = ? AND primary_key_hash = ? AND value_key = ?`,
		s.indexKeyspace)
}

func (s *ObjectStore) cqlSelectPropertyIndexRangeLower() string {
	return fmt.Sprintf(
		`SELECT object_id FROM %s.object_property_index
          WHERE tenant = ? AND type_id = ? AND property_id = ? AND primary_key_hash = ? AND value_key >= ?`,
		s.indexKeyspace)
}

func (s *ObjectStore) cqlSelectPropertyIndexRangeUpper() string {
	return fmt.Sprintf(
		`SELECT object_id FROM %s.object_property_index
          WHERE tenant = ? AND type_id = ? AND property_id = ? AND primary_key_hash = ? AND value_key <= ?`,
		s.indexKeyspace)
}

func (s *ObjectStore) cqlSelectPropertyIndexRangeBetween() string {
	return fmt.Sprintf(
		`SELECT object_id FROM %s.object_property_index
          WHERE tenant = ? AND type_id = ? AND property_id = ? AND primary_key_hash = ? AND value_key >= ? AND value_key <= ?`,
		s.indexKeyspace)
}

func (s *ObjectStore) cqlInsertPropertyIndex() string {
	return fmt.Sprintf(
		`INSERT INTO %s.object_property_index
            (tenant, type_id, property_id, primary_key_hash, value_kind,
             value_key, null_value, object_id, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.indexKeyspace)
}

func (s *ObjectStore) writePropertyIndexes(ctx context.Context, tenant string, objectUUID gocql.UUID, typeID string, primaryKey string, payload json.RawMessage, updatedAt time.Time) error {
	terms, err := propertyIndexTerms(payload)
	if err != nil {
		return invalidArgf("payload is not indexable as OSV2 property terms: %v", err)
	}
	bucket := primaryKeyHashBucket(primaryKey)
	for _, term := range terms {
		if err := s.session.Query(s.cqlInsertPropertyIndex(),
			tenant, typeID, term.PropertyID, bucket, term.ValueKind, term.ValueKey, term.NullValue, objectUUID, updatedAt).
			WithContext(ctx).Exec(); err != nil {
			return driverErr(err)
		}
	}
	return nil
}

// QueryByProperty executes an OSV2.7 property-index lookup and hydrates matching
// object rows through GetByTypeAndPrimaryKey. It supports exact, range,
// starts_with and IN-list predicates by fanning out across primary-key buckets.
func (s *ObjectStore) QueryByProperty(ctx context.Context, tenant repos.TenantId, typeID repos.TypeId, predicate repos.PropertyPredicate, page repos.Page, consistency repos.ReadConsistency) (repos.PagedResult[repos.Object], error) {
	limit := clampPageSize(page.Size)
	ids, err := s.queryPropertyIndexIDs(ctx, tenant, typeID, predicate, limit)
	if err != nil {
		return repos.PagedResult[repos.Object]{}, err
	}
	out := repos.PagedResult[repos.Object]{Items: []repos.Object{}}
	for _, id := range ids {
		obj, err := s.GetByTypeAndPrimaryKey(ctx, tenant, typeID, string(id), consistency)
		if err != nil {
			return repos.PagedResult[repos.Object]{}, err
		}
		if obj != nil && matchesRepoPropertyPredicate(obj.Payload, predicate) {
			out.Items = append(out.Items, *obj)
		}
	}
	return out, nil
}

func matchesRepoPropertyPredicate(payload json.RawMessage, predicate repos.PropertyPredicate) bool {
	var props map[string]any
	if err := json.Unmarshal(payload, &props); err != nil {
		return false
	}
	actual, ok := props[predicate.PropertyName]
	if !ok {
		actual = nil
	}
	actualTerm, ok := propertyIndexTermForValue(propertyID(predicate.PropertyName), actual)
	if !ok {
		return false
	}
	op := strings.ToLower(strings.TrimSpace(predicate.Operator))
	if op == "" {
		op = "equals"
	}
	switch op {
	case "equals", "eq", "=":
		expectedTerm, ok := propertyIndexTermForValue(propertyID(predicate.PropertyName), predicate.Value)
		return ok && actualTerm.ValueKey == expectedTerm.ValueKey
	case "gte", ">=":
		expectedTerm, ok := propertyIndexTermForValue(propertyID(predicate.PropertyName), predicate.Value)
		return ok && actualTerm.ValueKey >= expectedTerm.ValueKey
	case "gt", ">":
		expectedTerm, ok := propertyIndexTermForValue(propertyID(predicate.PropertyName), predicate.Value)
		return ok && actualTerm.ValueKey > expectedTerm.ValueKey
	case "lte", "<=":
		expectedTerm, ok := propertyIndexTermForValue(propertyID(predicate.PropertyName), predicate.Value)
		return ok && actualTerm.ValueKey <= expectedTerm.ValueKey
	case "lt", "<":
		expectedTerm, ok := propertyIndexTermForValue(propertyID(predicate.PropertyName), predicate.Value)
		return ok && actualTerm.ValueKey < expectedTerm.ValueKey
	case "in":
		switch values := predicate.Value.(type) {
		case []any:
			for _, value := range values {
				if matchesRepoPropertyPredicate(payload, repos.PropertyPredicate{PropertyName: predicate.PropertyName, Operator: "equals", Value: value}) {
					return true
				}
			}
		case []string:
			for _, value := range values {
				if matchesRepoPropertyPredicate(payload, repos.PropertyPredicate{PropertyName: predicate.PropertyName, Operator: "equals", Value: value}) {
					return true
				}
			}
		}
		return false
	case "starts_with", "prefix":
		prefix, ok := predicate.Value.(string)
		return ok && strings.HasPrefix(actualTerm.ValueKey, strings.ToLower(strings.TrimSpace(prefix)))
	default:
		return false
	}
}

func (s *ObjectStore) queryPropertyIndexIDs(ctx context.Context, tenant repos.TenantId, typeID repos.TypeId, predicate repos.PropertyPredicate, limit int) ([]repos.ObjectId, error) {
	property := propertyID(predicate.PropertyName)
	op := strings.ToLower(strings.TrimSpace(predicate.Operator))
	if op == "" {
		op = "equals"
	}
	seen := map[repos.ObjectId]struct{}{}
	out := make([]repos.ObjectId, 0, limit)
	addRows := func(query string, args ...any) error {
		iter := s.session.Query(query, args...).WithContext(ctx).PageSize(limit).Iter()
		var objectID gocql.UUID
		for iter.Scan(&objectID) {
			id := repos.ObjectId(objectID.String())
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
			if len(out) >= limit {
				break
			}
		}
		if err := iter.Close(); err != nil {
			return driverErr(err)
		}
		return nil
	}
	queryExactTerm := func(value any) error {
		term, ok := propertyIndexTermForValue(property, value)
		if !ok {
			return nil
		}
		for bucket := 0; bucket < objectHashBuckets && len(out) < limit; bucket++ {
			if err := addRows(s.cqlSelectPropertyIndexExact(), tenantStr(tenant), string(typeID), property, bucket, term.ValueKey); err != nil {
				return err
			}
		}
		return nil
	}
	switch op {
	case "equals", "eq", "=":
		return out, queryExactTerm(predicate.Value)
	case "in":
		switch values := predicate.Value.(type) {
		case []any:
			for _, value := range values {
				if err := queryExactTerm(value); err != nil || len(out) >= limit {
					return out, err
				}
			}
		case []string:
			for _, value := range values {
				if err := queryExactTerm(value); err != nil || len(out) >= limit {
					return out, err
				}
			}
		}
		return out, nil
	case "starts_with", "prefix":
		prefix, ok := predicate.Value.(string)
		if !ok {
			return out, nil
		}
		lo := strings.ToLower(strings.TrimSpace(prefix))
		hi := lo + "ÿ"
		for bucket := 0; bucket < objectHashBuckets && len(out) < limit; bucket++ {
			if err := addRows(s.cqlSelectPropertyIndexRangeBetween(), tenantStr(tenant), string(typeID), property, bucket, lo, hi); err != nil {
				return out, err
			}
		}
		return out, nil
	case "gte", ">=", "gt", ">", "lte", "<=", "lt", "<":
		term, ok := propertyIndexTermForValue(property, predicate.Value)
		if !ok {
			return out, nil
		}
		for bucket := 0; bucket < objectHashBuckets && len(out) < limit; bucket++ {
			var err error
			switch op {
			case "gte", ">=", "gt", ">":
				err = addRows(s.cqlSelectPropertyIndexRangeLower(), tenantStr(tenant), string(typeID), property, bucket, term.ValueKey)
			default:
				err = addRows(s.cqlSelectPropertyIndexRangeUpper(), tenantStr(tenant), string(typeID), property, bucket, term.ValueKey)
			}
			if err != nil {
				return out, err
			}
		}
		return out, nil
	default:
		return out, nil
	}
}
