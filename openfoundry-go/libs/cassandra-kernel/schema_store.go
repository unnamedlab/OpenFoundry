package cassandrakernel

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gocql/gocql"

	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// SchemaStore (P2.5.4) is the Cassandra-backed implementation of
// repos.SchemaStore mirroring libs/cassandra-kernel/src/repos.rs::
// CassandraSchemaStore. Two tables in `ontology_objects`:
//
//   - schemas_by_type (type_id, version) PRIMARY KEY — every
//     historical version, written via INSERT IF NOT EXISTS.
//   - schemas_latest (type_id) PRIMARY KEY — the current pointer,
//     promoted via INSERT IF NOT EXISTS / UPDATE IF version=? LWT.
//
// This store is deliberately narrower than the declarative ontology
// catalog: object/link/action definitions remain in pg-schemas;
// SchemaStore stores only versioned JSON Schema payloads used by
// runtime object validation.
type SchemaStore struct {
	session  *gocql.Session
	keyspace string
}

// NewSchemaStore builds a store bound to the standard
// `ontology_objects` keyspace.
func NewSchemaStore(session *gocql.Session) *SchemaStore {
	return &SchemaStore{session: session, keyspace: "ontology_objects"}
}

// NewSchemaStoreWithKeyspace allows a custom keyspace.
func NewSchemaStoreWithKeyspace(session *gocql.Session, keyspace string) *SchemaStore {
	return &SchemaStore{session: session, keyspace: keyspace}
}

// Compile-time interface assertion.
var _ repos.SchemaStore = (*SchemaStore)(nil)

func (s *SchemaStore) cqlInsertVersion() string {
	return fmt.Sprintf(
		`INSERT INTO %s.schemas_by_type
            (type_id, version, json_schema, created_at)
         VALUES (?, ?, ?, ?) IF NOT EXISTS`, s.keyspace)
}

func (s *SchemaStore) cqlDeleteVersion() string {
	return fmt.Sprintf(
		`DELETE FROM %s.schemas_by_type WHERE type_id = ? AND version = ?`, s.keyspace)
}

func (s *SchemaStore) cqlInsertLatest() string {
	return fmt.Sprintf(
		`INSERT INTO %s.schemas_latest
            (type_id, version, json_schema, created_at)
         VALUES (?, ?, ?, ?) IF NOT EXISTS`, s.keyspace)
}

func (s *SchemaStore) cqlUpdateLatestIfVersion() string {
	return fmt.Sprintf(
		`UPDATE %s.schemas_latest SET version = ?, json_schema = ?, created_at = ?
          WHERE type_id = ? IF version = ?`, s.keyspace)
}

func (s *SchemaStore) cqlSelectLatest() string {
	return fmt.Sprintf(
		`SELECT version, json_schema, created_at FROM %s.schemas_latest
          WHERE type_id = ?`, s.keyspace)
}

func (s *SchemaStore) cqlSelectVersion() string {
	return fmt.Sprintf(
		`SELECT json_schema, created_at FROM %s.schemas_by_type
          WHERE type_id = ? AND version = ?`, s.keyspace)
}

// schemaVersionToCQL clamps a version into the CQL `int` range
// (4 bytes signed) and rejects 0 — the contract requires monotonic
// strictly-positive versions. Mirrors fn schema_version_to_cql.
func schemaVersionToCQL(version uint32) (int32, error) {
	if version == 0 {
		return 0, invalidArg("schema version must be greater than zero")
	}
	if version > uint32(maxInt32) {
		return 0, invalidArg("schema version exceeds CQL int range")
	}
	return int32(version), nil
}

const maxInt32 = 1<<31 - 1

// schemaVersionFromCQL is the inverse — backends storing negative
// values are flagged as backend-level corruption.
func schemaVersionFromCQL(v int32) (uint32, error) {
	if v < 0 {
		return 0, repos.Backendf("stored schema version is negative: %d", v)
	}
	return uint32(v), nil
}

// GetLatest returns the latest schema for a type, or nil if absent.
func (s *SchemaStore) GetLatest(
	ctx context.Context,
	typeID repos.TypeId,
	consistency repos.ReadConsistency,
) (*repos.Schema, error) {
	row, err := s.selectLatestRaw(ctx, typeID, consistency)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	version, err := schemaVersionFromCQL(row.version)
	if err != nil {
		return nil, err
	}
	if !json.Valid([]byte(row.jsonSchema)) {
		return nil, repos.Backendf("invalid stored schema JSON: not parseable")
	}
	return &repos.Schema{
		TypeID:      typeID,
		Version:     version,
		JsonSchema:  json.RawMessage([]byte(row.jsonSchema)),
		CreatedAtMs: row.createdAt.UnixMilli(),
	}, nil
}

// GetVersion returns a specific version, or nil if absent.
func (s *SchemaStore) GetVersion(
	ctx context.Context,
	typeID repos.TypeId,
	version uint32,
	consistency repos.ReadConsistency,
) (*repos.Schema, error) {
	v, err := schemaVersionToCQL(version)
	if err != nil {
		return nil, err
	}
	q := s.session.Query(s.cqlSelectVersion(), string(typeID), v).
		WithContext(ctx).
		Consistency(cqlConsistency(consistency))
	var (
		jsonSchema string
		createdAt  time.Time
	)
	if err := q.Scan(&jsonSchema, &createdAt); err != nil {
		if err == gocql.ErrNotFound {
			return nil, nil
		}
		return nil, driverErr(err)
	}
	if !json.Valid([]byte(jsonSchema)) {
		return nil, repos.Backendf("invalid stored schema JSON: not parseable")
	}
	return &repos.Schema{
		TypeID:      typeID,
		Version:     version,
		JsonSchema:  json.RawMessage([]byte(jsonSchema)),
		CreatedAtMs: createdAt.UnixMilli(),
	}, nil
}

// Put appends a new schema version. Implementations MUST reject any
// version ≤ the latest known one — both at the insert side
// (UPDATE IF version=? LWT loop) and via the upfront SELECT.
func (s *SchemaStore) Put(ctx context.Context, schema repos.Schema) error {
	version, err := schemaVersionToCQL(schema.Version)
	if err != nil {
		return err
	}
	current, err := s.selectLatestRaw(ctx, schema.TypeID, repos.Strong())
	if err != nil {
		return err
	}
	if current != nil && version <= current.version {
		return invalidArgf("schema version %d not greater than latest %d", version, current.version)
	}

	rawSchema, err := canonicalJSON(schema.JsonSchema)
	if err != nil {
		return invalidArgf("schema JSON is not serialisable: %v", err)
	}
	createdAt := time.UnixMilli(schema.CreatedAtMs).UTC()

	insertQ := s.session.Query(s.cqlInsertVersion(),
		string(schema.TypeID), version, rawSchema, createdAt).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum).
		SerialConsistency(gocql.LocalSerial)
	row := map[string]any{}
	applied, err := insertQ.MapScanCAS(row)
	if err != nil {
		return driverErr(err)
	}
	if !applied {
		return invalidArgf("schema version %d already exists for type %s", version, schema.TypeID)
	}

	if err := s.promoteLatest(ctx, schema.TypeID, version, rawSchema, createdAt); err != nil {
		s.deleteVersionBestEffort(ctx, schema.TypeID, version)
		return err
	}
	return nil
}

// schemaRow is the internal projection from schemas_latest.
type schemaRow struct {
	version    int32
	jsonSchema string
	createdAt  time.Time
}

func (s *SchemaStore) selectLatestRaw(
	ctx context.Context,
	typeID repos.TypeId,
	consistency repos.ReadConsistency,
) (*schemaRow, error) {
	q := s.session.Query(s.cqlSelectLatest(), string(typeID)).
		WithContext(ctx).
		Consistency(cqlConsistency(consistency))
	var (
		version    int32
		jsonSchema string
		createdAt  time.Time
	)
	if err := q.Scan(&version, &jsonSchema, &createdAt); err != nil {
		if err == gocql.ErrNotFound {
			return nil, nil
		}
		return nil, driverErr(err)
	}
	return &schemaRow{version: version, jsonSchema: jsonSchema, createdAt: createdAt}, nil
}

func (s *SchemaStore) deleteVersionBestEffort(
	ctx context.Context, typeID repos.TypeId, version int32,
) {
	_ = s.session.Query(s.cqlDeleteVersion(), string(typeID), version).
		WithContext(ctx).
		Exec()
}

// promoteLatest moves the schemas_latest pointer to (version,
// jsonSchema, createdAt). First INSERT IF NOT EXISTS; if that
// fails (latest row already exists), CAS-loop UPDATE IF version=?
// up to 8 times with the freshly-read latest version. Mirrors fn
// promote_latest.
func (s *SchemaStore) promoteLatest(
	ctx context.Context,
	typeID repos.TypeId,
	version int32,
	rawSchema string,
	createdAt time.Time,
) error {
	insertQ := s.session.Query(s.cqlInsertLatest(),
		string(typeID), version, rawSchema, createdAt).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum).
		SerialConsistency(gocql.LocalSerial)
	row := map[string]any{}
	applied, err := insertQ.MapScanCAS(row)
	if err != nil {
		return driverErr(err)
	}
	if applied {
		return nil
	}

	for attempt := 0; attempt < 8; attempt++ {
		current, err := s.selectLatestRaw(ctx, typeID, repos.Strong())
		if err != nil {
			return err
		}
		if current == nil {
			continue
		}
		if version <= current.version {
			return invalidArgf("schema version %d not greater than latest %d", version, current.version)
		}
		updateQ := s.session.Query(s.cqlUpdateLatestIfVersion(),
			version, rawSchema, createdAt, string(typeID), current.version).
			WithContext(ctx).
			Consistency(gocql.LocalQuorum).
			SerialConsistency(gocql.LocalSerial)
		row := map[string]any{}
		applied, err := updateQ.MapScanCAS(row)
		if err != nil {
			return driverErr(err)
		}
		if applied {
			return nil
		}
	}
	return repos.Backend("schema latest CAS did not converge after retries")
}
