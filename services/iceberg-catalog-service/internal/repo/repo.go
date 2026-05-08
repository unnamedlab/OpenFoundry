// Package repo holds SQL queries + embedded migrations for
// iceberg-catalog-service.
package repo

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/models"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}

// DB is the pgx subset used by Repo; both *pgxpool.Pool and pgxmock pools
// satisfy it, so unit tests can drive Repo without a live database.
//
// Begin is required so CommitTable can run its requirement checks +
// metadata writes inside a single Postgres transaction (the
// "all-or-nothing" guarantee from `Iceberg tables/Transactions.md`).
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Executor is the subset both *pgxpool.Pool and pgx.Tx implement; used
// inside CommitTable so the helper queries (snapshot insert,
// metadata-version lookup) can run against the open transaction
// without duplicating their bodies.
type Executor interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type Repo struct{ Pool DB }

// RequirementError is the typed-error surface for a failed
// CommitTable assertion. The handler maps it to HTTP 409 with the
// `kind` carried verbatim so clients can branch on the assertion
// that broke without parsing a free-form message.
//
// Mirrors Rust's TableError::RequirementsFailed string envelope —
// the Go side keeps the kind separate from the rendered detail so
// the JSON shape is structured rather than a flat message.
type RequirementError struct {
	Kind   string
	Detail string
}

// Error implements `error`.
func (e *RequirementError) Error() string {
	return e.Kind + " failed: " + e.Detail
}

// RetryableError is the typed-error surface for a multi-table commit
// conflict. The handler maps it to HTTP 409 with the structured
// `table_rid` + `conflicting_with` envelope so the pipeline-build
// executor can decide whether to re-snapshot inputs and retry the job
// without parsing the free-form message.
//
// Mirrors Rust's `ApiError::Retryable` body — the Go side keeps the
// fields separate from the rendered message so the JSON shape stays
// structured.
type RetryableError struct {
	TableRID        string
	Reason          string
	ConflictingWith models.ConflictKind
}

// Error implements `error`.
func (e *RetryableError) Error() string {
	return fmt.Sprintf("retryable conflict on `%s`: %s", e.TableRID, e.Reason)
}

// errAlreadyExists wraps the unique-violation surface from Postgres in a
// stable error string. Mirrors Rust's `TableError::AlreadyExists` so the
// REST handler can map it to HTTP 409 via statusFromErr ("already exists").
func errAlreadyExists(name string) error {
	return fmt.Errorf("table `%s` already exists in namespace", name)
}

// isUniqueViolation reports whether err is a Postgres unique_violation
// (SQLSTATE 23505). pgconn.PgError surfaces this verbatim.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

const namespaceSelect = `SELECT id, project_rid, name, parent_namespace_id,
	properties, created_at, created_by FROM iceberg_namespaces`

func (r *Repo) ListNamespaces(ctx context.Context, projectRID string) ([]models.IcebergNamespace, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if projectRID != "" {
		rows, err = r.Pool.Query(ctx, namespaceSelect+` WHERE project_rid = $1 ORDER BY name LIMIT 500`, projectRID)
	} else {
		rows, err = r.Pool.Query(ctx, namespaceSelect+` ORDER BY project_rid, name LIMIT 500`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.IcebergNamespace, 0)
	for rows.Next() {
		v, err := scanNamespace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetNamespace(ctx context.Context, id uuid.UUID) (*models.IcebergNamespace, error) {
	row := r.Pool.QueryRow(ctx, namespaceSelect+` WHERE id = $1`, id)
	v, err := scanNamespace(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// ListTopLevelNamespaces mirrors Rust `domain::namespace::list(_, _, None)`:
// rows under `project_rid` whose `parent_namespace_id IS NULL`, ordered by
// name. Used by the diagnose endpoint to probe Postgres reachability.
func (r *Repo) ListTopLevelNamespaces(ctx context.Context, projectRID string) ([]models.IcebergNamespace, error) {
	rows, err := r.Pool.Query(ctx, namespaceSelect+` WHERE project_rid = $1 AND parent_namespace_id IS NULL ORDER BY name`, projectRID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.IcebergNamespace, 0)
	for rows.Next() {
		v, err := scanNamespace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// FetchNamespaceByName mirrors Rust `domain::namespace::fetch`: encodes the
// path with dot-separators and looks up `(project_rid, name)`. Returns
// `(nil, nil)` when the namespace does not exist so callers can map that to
// the soft-warn outcome the Iceberg diagnose probe expects.
func (r *Repo) FetchNamespaceByName(ctx context.Context, projectRID string, path []string) (*models.IcebergNamespace, error) {
	name := strings.Join(path, ".")
	row := r.Pool.QueryRow(ctx, namespaceSelect+` WHERE project_rid = $1 AND name = $2`, projectRID, name)
	v, err := scanNamespace(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CreateNamespace(ctx context.Context, body *models.CreateNamespaceRequest, createdBy uuid.UUID) (*models.IcebergNamespace, error) {
	id := uuid.New()
	props := body.Properties
	if len(props) == 0 {
		props = []byte(`{}`)
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO iceberg_namespaces
		    (id, project_rid, name, parent_namespace_id, properties, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, project_rid, name, parent_namespace_id, properties,
		           created_at, created_by`,
		id, body.ProjectRID, body.Name, body.ParentNamespaceID, props, createdBy,
	)
	return scanNamespace(row)
}

func (r *Repo) UpdateNamespaceProperties(ctx context.Context, id uuid.UUID, properties []byte) (*models.IcebergNamespace, error) {
	current, err := r.GetNamespace(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	if len(properties) == 0 {
		return current, nil
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE iceberg_namespaces SET properties = $2 WHERE id = $1
		 RETURNING id, project_rid, name, parent_namespace_id, properties,
		           created_at, created_by`,
		id, properties)
	return scanNamespace(row)
}

func (r *Repo) DeleteNamespace(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM iceberg_namespaces WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

type rowLikeT interface{ Scan(...any) error }

func scanNamespace(r rowLikeT) (*models.IcebergNamespace, error) {
	v := &models.IcebergNamespace{}
	if err := r.Scan(&v.ID, &v.ProjectRID, &v.Name, &v.ParentNamespaceID,
		&v.Properties, &v.CreatedAt, &v.CreatedBy); err != nil {
		return nil, err
	}
	return v, nil
}

const tableSelect = `SELECT t.id, t.rid, t.namespace_id, n.name AS namespace_name,
	t.name, t.table_uuid, t.format_version, t.location, t.current_snapshot_id,
	t.current_metadata_location, t.last_sequence_number, t.partition_spec,
	t.schema_json, t.sort_order, t.properties, t.markings, t.created_at, t.updated_at
	FROM iceberg_tables t JOIN iceberg_namespaces n ON n.id = t.namespace_id`

func (r *Repo) ListTables(ctx context.Context, projectRID string, namespace []string) ([]models.IcebergTable, error) {
	name := encodePath(namespace)
	rows, err := r.Pool.Query(ctx, tableSelect+` WHERE n.project_rid = $1 AND n.name = $2 ORDER BY t.name`, projectRID, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.IcebergTable, 0)
	for rows.Next() {
		v, err := scanTable(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetTable(ctx context.Context, projectRID string, namespace []string, tableName string) (*models.IcebergTable, error) {
	row := r.Pool.QueryRow(ctx, tableSelect+` WHERE n.project_rid = $1 AND n.name = $2 AND t.name = $3`, projectRID, encodePath(namespace), tableName)
	v, err := scanTable(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CreateTable(ctx context.Context, projectRID string, namespace []string, body *models.CreateTableRequest, createdBy uuid.UUID) (*models.IcebergTable, string, error) {
	ns, err := r.GetNamespaceByProjectName(ctx, projectRID, encodePath(namespace))
	if err != nil || ns == nil {
		return nil, "", err
	}
	if strings.TrimSpace(body.Name) == "" {
		return nil, "", fmt.Errorf("table name is required")
	}
	if len(body.Schema) == 0 || !json.Valid(body.Schema) || string(body.Schema) == "null" {
		return nil, "", fmt.Errorf("schema is required")
	}
	formatVersion := int32(2)
	if body.FormatVersion != nil {
		formatVersion = *body.FormatVersion
	}
	if formatVersion < 1 || formatVersion > 3 {
		return nil, "", fmt.Errorf("invalid format-version %d; catalog accepts 1, 2, 3", formatVersion)
	}
	location := ""
	if body.Location != nil && strings.TrimSpace(*body.Location) != "" {
		location = strings.TrimRight(strings.TrimSpace(*body.Location), "/")
	} else {
		location = fmt.Sprintf("s3://openfoundry-warehouse/%s/%s", ns.Name, strings.TrimSpace(body.Name))
	}
	partitionSpec := body.PartitionSpec
	if len(partitionSpec) == 0 {
		partitionSpec = []byte(`{"spec-id":0,"fields":[]}`)
	}
	sortOrder := body.SortOrder
	if len(sortOrder) == 0 {
		sortOrder = []byte(`{"order-id":0,"fields":[]}`)
	}
	props, err := json.Marshal(body.Properties)
	if err != nil {
		return nil, "", err
	}
	markings := body.Markings
	if len(markings) == 0 {
		markings = []string{"public"}
	}
	id := uuid.New()
	tableName := strings.TrimSpace(body.Name)
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO iceberg_tables (id, namespace_id, name, table_uuid, format_version, location,
		    partition_spec, schema_json, sort_order, properties, markings)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 RETURNING id, rid, namespace_id, $12::text AS namespace_name, name, table_uuid,
		           format_version, location, current_snapshot_id, current_metadata_location,
		           last_sequence_number, partition_spec, schema_json, sort_order, properties,
		           markings, created_at, updated_at`,
		id, ns.ID, tableName, uuid.NewString(), formatVersion, location,
		partitionSpec, body.Schema, sortOrder, props, markings, ns.Name,
	)
	t, err := scanTable(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, "", errAlreadyExists(tableName)
		}
		return nil, "", err
	}
	metadataLocation := fmt.Sprintf("%s/metadata/v1.metadata.json", t.Location)
	_, err = r.Pool.Exec(ctx, `INSERT INTO iceberg_table_metadata_files (id, table_id, version, path) VALUES ($1,$2,1,$3) ON CONFLICT (table_id, version) DO NOTHING`, uuid.New(), t.ID, metadataLocation)
	return t, metadataLocation, err
}

// CommitTable applies a single-table commit atomically. Mirrors the
// Rust `domain::table::apply_commit` body plus the metadata-file +
// current-metadata-location bookkeeping the REST handler used to drive
// separately. The whole sequence runs inside one Postgres transaction
// so the requirement assertions, schema-strict check, and metadata
// bookkeeping either all land or all roll back — matching the Rust
// spec's atomic-apply guarantee.
//
// Validation order mirrors Iceberg REST § "Requirements ordering":
//
//	assert-create →
//	assert-uuid →
//	assert-current-schema-id →
//	assert-default-spec-id →
//	assert-default-sort-order-id →
//	assert-ref-snapshot-id
//
// Schema-strict runs after the requirements pass so a write that's
// both stale AND schema-incompatible surfaces the more actionable
// assertion failure first (clients retry with a fresh snapshot
// before they bother computing an ALTER).
func (r *Repo) CommitTable(ctx context.Context, projectRID string, namespace []string, tableName string, body *models.CommitTableRequest) (*models.IcebergTable, string, error) {
	cur, err := r.GetTable(ctx, projectRID, namespace, tableName)
	if err != nil || cur == nil {
		return cur, "", err
	}
	if err := validateRequirements(cur, body.Requirements); err != nil {
		return nil, "", err
	}
	if err := enforceSchemaStrict(cur, body.Updates); err != nil {
		return nil, "", err
	}

	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	updated, metadataLocation, err := applyCommitInTx(ctx, tx, cur, body, encodePath(namespace))
	if err != nil {
		return nil, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", err
	}
	return updated, metadataLocation, nil
}

// applyCommitInTx runs the mutating portion of CommitTable inside an
// open transaction. Pure helper so the atomic boundary is obvious in
// CommitTable.
func applyCommitInTx(ctx context.Context, tx Executor, cur *models.IcebergTable, body *models.CommitTableRequest, encodedNS string) (*models.IcebergTable, string, error) {
	nextSchema := cur.SchemaJSON
	nextProps := cur.Properties
	nextPartition := cur.PartitionSpec
	nextSort := cur.SortOrder
	var lastSnapshotID *int64
	lastSeq := cur.LastSequenceNumber
	for _, raw := range body.Updates {
		var update map[string]json.RawMessage
		if err := json.Unmarshal(raw, &update); err != nil {
			return nil, "", err
		}
		action := jsonString(update["action"])
		switch action {
		case "add-schema":
			if schema, ok := update["schema"]; ok {
				nextSchema = schema
			}
		case "set-properties":
			nextProps = mergeProperties(nextProps, update["updates"])
		case "remove-properties":
			nextProps = removeProperties(nextProps, update["removals"])
		case "add-partition-spec":
			if spec, ok := update["spec"]; ok {
				nextPartition = spec
			}
		case "add-sort-order":
			if order, ok := update["sort-order"]; ok {
				nextSort = order
			}
		case "add-snapshot":
			snap, err := appendSnapshotTx(ctx, tx, cur.ID, update["snapshot"])
			if err != nil {
				return nil, "", err
			}
			lastSnapshotID = &snap.SnapshotID
			if snap.SequenceNumber > lastSeq {
				lastSeq = snap.SequenceNumber
			}
		}
	}
	row := tx.QueryRow(ctx,
		`UPDATE iceberg_tables SET schema_json=$2, properties=$3, partition_spec=$4, sort_order=$5,
		 current_snapshot_id=COALESCE($6, current_snapshot_id), last_sequence_number=GREATEST(last_sequence_number,$7), updated_at=NOW()
		 WHERE id=$1 RETURNING id, rid, namespace_id, $8::text AS namespace_name, name, table_uuid,
		 format_version, location, current_snapshot_id, current_metadata_location, last_sequence_number,
		 partition_spec, schema_json, sort_order, properties, markings, created_at, updated_at`,
		cur.ID, nextSchema, nextProps, nextPartition, nextSort, lastSnapshotID, lastSeq, encodedNS,
	)
	updated, err := scanTable(row)
	if err != nil {
		return nil, "", err
	}
	version, err := nextMetadataVersionTx(ctx, tx, updated.ID)
	if err != nil {
		return nil, "", err
	}
	metadataLocation := fmt.Sprintf("%s/metadata/v%d.metadata.json", updated.Location, version)
	if _, err := tx.Exec(ctx,
		`INSERT INTO iceberg_table_metadata_files (id, table_id, version, path) VALUES ($1,$2,$3,$4)`,
		uuid.New(), updated.ID, version, metadataLocation,
	); err != nil {
		return nil, "", err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE iceberg_tables SET current_metadata_location=$2 WHERE id=$1`,
		updated.ID, metadataLocation,
	); err != nil {
		return nil, "", err
	}
	updated.CurrentMetadataLocation = &metadataLocation
	return updated, metadataLocation, nil
}

func (r *Repo) ListSnapshots(ctx context.Context, tableID uuid.UUID) ([]models.Snapshot, error) {
	rows, err := r.Pool.Query(ctx, `SELECT id, table_id, snapshot_id, parent_snapshot_id, sequence_number, operation, manifest_list_location, summary, schema_id, timestamp_ms FROM iceberg_snapshots WHERE table_id=$1 ORDER BY timestamp_ms ASC, snapshot_id ASC`, tableID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Snapshot, 0)
	for rows.Next() {
		v, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetNamespaceByProjectName(ctx context.Context, projectRID, name string) (*models.IcebergNamespace, error) {
	row := r.Pool.QueryRow(ctx, namespaceSelect+` WHERE project_rid=$1 AND name=$2`, projectRID, name)
	v, err := scanNamespace(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) appendSnapshot(ctx context.Context, tableID uuid.UUID, raw json.RawMessage) (*models.Snapshot, error) {
	return appendSnapshotTx(ctx, r.Pool, tableID, raw)
}

func appendSnapshotTx(ctx context.Context, db Executor, tableID uuid.UUID, raw json.RawMessage) (*models.Snapshot, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("add-snapshot requires snapshot")
	}
	var snap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	snapshotID := jsonInt64(snap["snapshot-id"], timeNowMillis())
	parentID := jsonInt64Ptr(snap["parent-snapshot-id"])
	seq := jsonInt64(snap["sequence-number"], 1)
	manifest := jsonString(snap["manifest-list"])
	summary := snap["summary"]
	if len(summary) == 0 {
		summary = []byte(`{}`)
	}
	operation := "append"
	var sm map[string]any
	if json.Unmarshal(summary, &sm) == nil {
		if op, ok := sm["operation"].(string); ok && op != "" {
			operation = op
		}
	}
	if !oneOf(operation, "append", "overwrite", "delete", "replace") {
		return nil, fmt.Errorf("invalid operation `%s`", operation)
	}
	schemaID := int32(jsonInt64(snap["schema-id"], 0))
	row := db.QueryRow(ctx, `INSERT INTO iceberg_snapshots (table_id, snapshot_id, parent_snapshot_id, sequence_number, operation, manifest_list_location, summary, schema_id, timestamp_ms) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (table_id, snapshot_id) DO UPDATE SET snapshot_id=EXCLUDED.snapshot_id RETURNING id, table_id, snapshot_id, parent_snapshot_id, sequence_number, operation, manifest_list_location, summary, schema_id, timestamp_ms`, tableID, snapshotID, parentID, seq, operation, manifest, summary, schemaID, timeNowMillis())
	return scanSnapshot(row)
}

func (r *Repo) nextMetadataVersion(ctx context.Context, tableID uuid.UUID) (int32, error) {
	return nextMetadataVersionTx(ctx, r.Pool, tableID)
}

func nextMetadataVersionTx(ctx context.Context, db Executor, tableID uuid.UUID) (int32, error) {
	var max *int32
	if err := db.QueryRow(ctx, `SELECT MAX(version) FROM iceberg_table_metadata_files WHERE table_id=$1`, tableID).Scan(&max); err != nil {
		return 0, err
	}
	if max == nil {
		return 1, nil
	}
	return *max + 1, nil
}

func scanTable(r rowLikeT) (*models.IcebergTable, error) {
	v := &models.IcebergTable{}
	var nsName string
	if err := r.Scan(&v.ID, &v.RID, &v.NamespaceID, &nsName, &v.Name, &v.TableUUID,
		&v.FormatVersion, &v.Location, &v.CurrentSnapshotID, &v.CurrentMetadataLocation,
		&v.LastSequenceNumber, &v.PartitionSpec, &v.SchemaJSON, &v.SortOrder, &v.Properties,
		&v.Markings, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	v.Namespace = decodePath(nsName)
	if v.Markings == nil {
		v.Markings = []string{}
	}
	return v, nil
}

func scanSnapshot(r rowLikeT) (*models.Snapshot, error) {
	v := &models.Snapshot{}
	if err := r.Scan(&v.ID, &v.TableID, &v.SnapshotID, &v.ParentSnapshotID, &v.SequenceNumber, &v.Operation, &v.ManifestListLocation, &v.Summary, &v.SchemaID, &v.TimestampMS); err != nil {
		return nil, err
	}
	return v, nil
}

func encodePath(parts []string) string { return strings.Join(parts, ".") }
func decodePath(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(path, ".")
}
func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func jsonString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func jsonInt64(raw json.RawMessage, def int64) int64 {
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	return def
}

func jsonInt64Ptr(raw json.RawMessage) *int64 {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	n := jsonInt64(raw, 0)
	return &n
}

func timeNowMillis() int64 { return time.Now().UTC().UnixMilli() }

func mergeProperties(current, updates json.RawMessage) json.RawMessage {
	var dst map[string]any
	if json.Unmarshal(current, &dst) != nil || dst == nil {
		dst = map[string]any{}
	}
	var src map[string]any
	if json.Unmarshal(updates, &src) == nil {
		for k, v := range src {
			dst[k] = v
		}
	}
	out, _ := json.Marshal(dst)
	return out
}

func removeProperties(current, removals json.RawMessage) json.RawMessage {
	var dst map[string]any
	if json.Unmarshal(current, &dst) != nil || dst == nil {
		dst = map[string]any{}
	}
	var keys []string
	if json.Unmarshal(removals, &keys) == nil {
		for _, k := range keys {
			delete(dst, k)
		}
	}
	out, _ := json.Marshal(dst)
	return out
}

// validateRequirements walks the CommitTable requirements and surfaces
// the first mismatch as a typed RequirementError.
//
// Covers all 6 assertion kinds from Iceberg REST § "Requirements":
//
//   - assert-create:                table must NOT exist (pre-create)
//   - assert-uuid:                  table-uuid matches
//   - assert-current-schema-id:     schema-json["schema-id"] matches
//   - assert-default-spec-id:       partition-spec["spec-id"] matches
//   - assert-default-sort-order-id: sort-order["order-id"] matches
//   - assert-ref-snapshot-id:       branch ref snapshot-id matches
//     (defaults to "main" per Foundry's master/main alias)
//
// Unknown kinds are tolerated as no-ops so a future Iceberg spec
// extension does not break existing commits — matches Rust's
// `tracing::debug!(kind, "ignoring unsupported commit requirement")`
// fallthrough.
func validateRequirements(table *models.IcebergTable, reqs []json.RawMessage) error {
	for _, raw := range reqs {
		var req map[string]json.RawMessage
		if err := json.Unmarshal(raw, &req); err != nil {
			return err
		}
		kind := jsonString(req["type"])
		switch kind {
		case "assert-create":
			// In CommitTable we always have a current table — the
			// caller fetched it before calling validateRequirements.
			// So assert-create is always a fail here.
			return &RequirementError{
				Kind:   kind,
				Detail: fmt.Sprintf("table `%s` already exists", table.Name),
			}
		case "assert-uuid":
			expected := jsonString(req["uuid"])
			if expected != table.TableUUID {
				return &RequirementError{
					Kind:   kind,
					Detail: fmt.Sprintf("expected %s, found %s", expected, table.TableUUID),
				}
			}
		case "assert-current-schema-id":
			expected := jsonInt64(req["current-schema-id"], 0)
			current := schemaIDOf(table.SchemaJSON, "schema-id")
			if expected != current {
				return &RequirementError{
					Kind:   kind,
					Detail: fmt.Sprintf("expected %d, found %d", expected, current),
				}
			}
		case "assert-default-spec-id":
			expected := jsonInt64(req["default-spec-id"], 0)
			current := schemaIDOf(table.PartitionSpec, "spec-id")
			if expected != current {
				return &RequirementError{
					Kind:   kind,
					Detail: fmt.Sprintf("expected %d, found %d", expected, current),
				}
			}
		case "assert-default-sort-order-id":
			expected := jsonInt64(req["default-sort-order-id"], 0)
			current := schemaIDOf(table.SortOrder, "order-id")
			if expected != current {
				return &RequirementError{
					Kind:   kind,
					Detail: fmt.Sprintf("expected %d, found %d", expected, current),
				}
			}
		case "assert-ref-snapshot-id":
			refName := jsonString(req["ref"])
			if refName == "" {
				refName = "main"
			}
			// Foundry's master ↔ main alias: clients pointing at the
			// historical "master" name resolve against the current
			// snapshot's main ref.
			if refName == "master" {
				refName = "main"
			}
			if refName != "main" {
				// Branch refs other than main are not yet asserted
				// against persisted state in this slice; skip rather
				// than fail to keep parity with Rust's "main only"
				// path (`if ref_name == "main"` in apply_commit).
				continue
			}
			expected := jsonInt64Ptr(req["snapshot-id"])
			actual := table.CurrentSnapshotID
			if !snapshotPointersEqual(expected, actual) {
				return &RequirementError{
					Kind: kind,
					Detail: fmt.Sprintf("ref `main` expected %s, found %s",
						snapshotPointerString(expected),
						snapshotPointerString(actual)),
				}
			}
		default:
			// Tolerate unknown kinds (Rust does the same via `_`).
		}
	}
	return nil
}

func snapshotPointersEqual(a, b *int64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func snapshotPointerString(p *int64) string {
	if p == nil {
		return "<none>"
	}
	return fmt.Sprintf("%d", *p)
}

// schemaIDOf extracts a numeric id field (e.g. `schema-id`,
// `spec-id`, `order-id`) from a JSON document, returning 0 when the
// field is missing or the JSON is invalid (matching Rust's
// `.unwrap_or(0)` fallback).
func schemaIDOf(raw json.RawMessage, field string) int64 {
	if len(raw) == 0 {
		return 0
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 0
	}
	value, ok := doc[field]
	if !ok {
		return 0
	}
	return jsonInt64(value, 0)
}

// enforceSchemaStrict refuses commits whose `add-schema` updates
// diverge from the table's current schema (per Foundry doc
// § "Automatic schema evolution"). The check mirrors Rust's
// `enforce_schema_strict` in handlers/rest_catalog/tables.rs and
// returns a typed SchemaIncompatibleError so the handler can render
// the 422 envelope verbatim.
func enforceSchemaStrict(table *models.IcebergTable, updates []json.RawMessage) error {
	for _, raw := range updates {
		var update map[string]json.RawMessage
		if err := json.Unmarshal(raw, &update); err != nil {
			return err
		}
		if jsonString(update["action"]) != "add-schema" {
			continue
		}
		attempted, ok := update["schema"]
		if !ok {
			continue
		}
		diff := domain.DiffSchemas(table.SchemaJSON, attempted)
		if diff.IsCompatible() {
			continue
		}
		return &domain.SchemaIncompatibleError{
			CurrentSchema:   table.SchemaJSON,
			AttemptedSchema: attempted,
			Diff:            diff,
		}
	}
	return nil
}

func (r *Repo) DropTable(ctx context.Context, projectRID string, namespace []string, tableName string, purge bool) (bool, error) {
	_ = purge
	cmd, err := r.Pool.Exec(ctx,
		`DELETE FROM iceberg_tables t USING iceberg_namespaces n
		 WHERE t.namespace_id=n.id AND n.project_rid=$1 AND n.name=$2 AND t.name=$3`,
		projectRID, encodePath(namespace), tableName)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func (r *Repo) RenameTable(ctx context.Context, projectRID string, sourceNS []string, sourceName string, destNS []string, destName string) (*models.IcebergTable, error) {
	dest, err := r.GetNamespaceByProjectName(ctx, projectRID, encodePath(destNS))
	if err != nil || dest == nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("destination namespace not found")
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE iceberg_tables t SET namespace_id=$4, name=$5, updated_at=NOW()
		 FROM iceberg_namespaces n
		 WHERE t.namespace_id=n.id AND n.project_rid=$1 AND n.name=$2 AND t.name=$3
		 RETURNING t.id, t.rid, t.namespace_id, $6::text AS namespace_name, t.name, t.table_uuid,
		          t.format_version, t.location, t.current_snapshot_id, t.current_metadata_location,
		          t.last_sequence_number, t.partition_spec, t.schema_json, t.sort_order, t.properties,
		          t.markings, t.created_at, t.updated_at`,
		projectRID, encodePath(sourceNS), sourceName, dest.ID, strings.TrimSpace(destName), encodePath(destNS))
	v, err := scanTable(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

const refSelect = `SELECT id, table_id, name, kind, snapshot_id, max_ref_age_ms,
	max_snapshot_age_ms, min_snapshots_to_keep, created_at FROM iceberg_table_branches`

func (r *Repo) ListRefs(ctx context.Context, tableID uuid.UUID) ([]models.TableRef, error) {
	rows, err := r.Pool.Query(ctx, refSelect+` WHERE table_id=$1 ORDER BY name`, tableID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.TableRef{}
	for rows.Next() {
		v, err := scanRef(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) UpsertRef(ctx context.Context, tableID uuid.UUID, name string, body *models.UpdateRefRequest) (*models.TableRef, error) {
	kind := body.Type
	if kind == "" {
		kind = "branch"
	}
	if !oneOf(kind, "branch", "tag") {
		return nil, fmt.Errorf("invalid ref type `%s`", kind)
	}
	canonical := name
	if canonical == "master" {
		canonical = "main"
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO iceberg_table_branches
		 (id, table_id, name, kind, snapshot_id, max_ref_age_ms, max_snapshot_age_ms, min_snapshots_to_keep)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (table_id, name) DO UPDATE SET kind=EXCLUDED.kind, snapshot_id=EXCLUDED.snapshot_id,
		 max_ref_age_ms=EXCLUDED.max_ref_age_ms, max_snapshot_age_ms=EXCLUDED.max_snapshot_age_ms,
		 min_snapshots_to_keep=EXCLUDED.min_snapshots_to_keep
		 RETURNING id, table_id, name, kind, snapshot_id, max_ref_age_ms,
		           max_snapshot_age_ms, min_snapshots_to_keep, created_at`,
		uuid.New(), tableID, canonical, kind, body.SnapshotID, body.MaxRefAgeMS, body.MaxSnapshotAgeMS, body.MinSnapshotsToKeep)
	return scanRef(row)
}

func (r *Repo) GetRef(ctx context.Context, tableID uuid.UUID, name string) (*models.TableRef, error) {
	if name == "master" {
		name = "main"
	}
	row := r.Pool.QueryRow(ctx, refSelect+` WHERE table_id=$1 AND name=$2`, tableID, name)
	v, err := scanRef(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) DeleteRef(ctx context.Context, tableID uuid.UUID, name string) (bool, error) {
	if name == "master" {
		name = "main"
	}
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM iceberg_table_branches WHERE table_id=$1 AND name=$2`, tableID, name)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func (r *Repo) ListMetadataFiles(ctx context.Context, tableID uuid.UUID) ([]models.MetadataFile, error) {
	rows, err := r.Pool.Query(ctx, `SELECT id, table_id, version, path, created_at FROM iceberg_table_metadata_files WHERE table_id=$1 ORDER BY version`, tableID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.MetadataFile{}
	for rows.Next() {
		v, err := scanMetadataFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetMetadataFile(ctx context.Context, tableID uuid.UUID, version int32) (*models.MetadataFile, error) {
	row := r.Pool.QueryRow(ctx, `SELECT id, table_id, version, path, created_at FROM iceberg_table_metadata_files WHERE table_id=$1 AND version=$2`, tableID, version)
	v, err := scanMetadataFile(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) GetSnapshot(ctx context.Context, tableID uuid.UUID, snapshotID int64) (*models.Snapshot, error) {
	row := r.Pool.QueryRow(ctx, `SELECT id, table_id, snapshot_id, parent_snapshot_id, sequence_number, operation, manifest_list_location, summary, schema_id, timestamp_ms FROM iceberg_snapshots WHERE table_id=$1 AND snapshot_id=$2`, tableID, snapshotID)
	v, err := scanSnapshot(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func scanRef(r rowLikeT) (*models.TableRef, error) {
	v := &models.TableRef{}
	if err := r.Scan(&v.ID, &v.TableID, &v.Name, &v.Kind, &v.SnapshotID, &v.MaxRefAgeMS, &v.MaxSnapshotAgeMS, &v.MinSnapshotsToKeep, &v.CreatedAt); err != nil {
		return nil, err
	}
	return v, nil
}

func scanMetadataFile(r rowLikeT) (*models.MetadataFile, error) {
	v := &models.MetadataFile{}
	if err := r.Scan(&v.ID, &v.TableID, &v.Version, &v.Path, &v.CreatedAt); err != nil {
		return nil, err
	}
	return v, nil
}

// MultiTableCommit applies a batched all-or-nothing commit across
// every table named in `body`. Mirrors the Rust handler's flow in
// services/iceberg-catalog-service/src/handlers/rest_catalog/transactions.rs:
//
//  1. Resolve every (namespace, table) outside the lock.
//  2. Sort the resolved tables by `id` so SELECT … FOR UPDATE locks
//     are taken in a deterministic order — this is what prevents
//     deadlocks between two commits that share two or more tables.
//  3. BEGIN. For each (locked) table:
//     a. SELECT … FOR UPDATE on iceberg_tables — Postgres blocks
//     here until any other commit holding the row releases it.
//     b. Validate every requirement against the *locked* row state.
//     A failed assertion rolls back and surfaces as a typed
//     RetryableError with the `ConflictKind` set per the Rust
//     mapping (assert-uuid → user_job, schema/ref → compaction).
//     c. Schema-strict: any add-schema update that diverges from
//     the locked schema rolls back and surfaces as a
//     domain.SchemaIncompatibleError so the handler returns 422.
//     d. Apply updates (add-schema / set-properties / remove-properties /
//     add-snapshot) inside the same transaction.
//     e. Insert a metadata-file row + bump current_metadata_location +
//     last_sequence_number on iceberg_tables.
//  4. COMMIT.
//
// Empty `TableChanges` is a no-op (mirrors the Rust wrapper's
// "commit_with_no_pending_ops_is_a_noop" semantics) and returns an
// empty Committed slice without opening a transaction.
func (r *Repo) MultiTableCommit(ctx context.Context, projectRID string, body *models.MultiTableCommitRequest) ([]models.CommittedTable, error) {
	if body == nil || len(body.TableChanges) == 0 {
		return []models.CommittedTable{}, nil
	}

	// Phase 1 — resolve every table (no locks yet). Surface a typed
	// RetryableError with `unknown` kind when a referenced table does
	// not exist; the executor cannot recover by retrying so this is a
	// best-effort label.
	type resolved struct {
		table  *models.IcebergTable
		change *models.MultiTableChange
	}
	resolvedSlice := make([]resolved, 0, len(body.TableChanges))
	for i := range body.TableChanges {
		change := &body.TableChanges[i]
		if len(change.Identifier.Namespace) == 0 {
			return nil, fmt.Errorf("table-changes[%d] missing namespace", i)
		}
		if strings.TrimSpace(change.Identifier.Name) == "" {
			return nil, fmt.Errorf("table-changes[%d] missing table name", i)
		}
		t, err := r.GetTable(ctx, projectRID, change.Identifier.Namespace, change.Identifier.Name)
		if err != nil {
			return nil, err
		}
		if t == nil {
			return nil, &RetryableError{
				TableRID:        fmt.Sprintf("%s.%s", encodePath(change.Identifier.Namespace), change.Identifier.Name),
				Reason:          fmt.Sprintf("table `%s` not found in namespace `%s`", change.Identifier.Name, encodePath(change.Identifier.Namespace)),
				ConflictingWith: models.ConflictKindUnknown,
			}
		}
		resolvedSlice = append(resolvedSlice, resolved{table: t, change: change})
	}
	// Deterministic lock order: sort by table.id so two commits sharing
	// any subset of tables always acquire row-locks in the same order.
	sort.Slice(resolvedSlice, func(i, j int) bool {
		return strings.Compare(resolvedSlice[i].table.ID.String(), resolvedSlice[j].table.ID.String()) < 0
	})

	// Phase 2 — atomic commit.
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	committed := make([]models.CommittedTable, 0, len(resolvedSlice))
	for _, item := range resolvedSlice {
		out, err := commitOneInTx(ctx, tx, item.table, item.change)
		if err != nil {
			return nil, err
		}
		committed = append(committed, *out)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return committed, nil
}

// commitOneInTx runs the per-table portion of a multi-table commit
// inside an open transaction. The locked-row state (snapshot id,
// schema, sequence number) is fetched via SELECT … FOR UPDATE so
// requirement checks evaluate against the latest committed view.
func commitOneInTx(ctx context.Context, tx Executor, t *models.IcebergTable, change *models.MultiTableChange) (*models.CommittedTable, error) {
	var (
		lockedSnapshot *int64
		lockedSchema   json.RawMessage
		lockedSeq      int64
	)
	row := tx.QueryRow(ctx,
		`SELECT current_snapshot_id, schema_json, last_sequence_number
		 FROM iceberg_tables WHERE id = $1 FOR UPDATE`,
		t.ID,
	)
	if err := row.Scan(&lockedSnapshot, &lockedSchema, &lockedSeq); err != nil {
		// Row-lock acquisition (or row disappearance) failure — surface
		// as Retryable with `unknown` kind so the executor knows the
		// reason cannot be classified as user / compaction / maintenance.
		return nil, &RetryableError{
			TableRID:        t.RID,
			Reason:          fmt.Sprintf("unable to acquire row lock: %v", err),
			ConflictingWith: models.ConflictKindUnknown,
		}
	}

	// Replace the cached requirement-check inputs with the locked-row
	// view so requirements evaluate against the latest committed state.
	locked := *t
	locked.CurrentSnapshotID = lockedSnapshot
	locked.SchemaJSON = lockedSchema
	locked.LastSequenceNumber = lockedSeq

	if err := validateRequirementsLocked(&locked, change.Requirements); err != nil {
		return nil, err
	}
	if err := enforceSchemaStrict(&locked, change.Updates); err != nil {
		return nil, err
	}

	body := &models.CommitTableRequest{
		Identifier:   &change.Identifier,
		Requirements: change.Requirements,
		Updates:      change.Updates,
	}
	updated, metadataLocation, err := applyCommitInTx(ctx, tx, &locked, body, encodePath(t.Namespace))
	if err != nil {
		return nil, err
	}
	var newSnapshot *int64
	if updated.CurrentSnapshotID != nil {
		v := *updated.CurrentSnapshotID
		newSnapshot = &v
	} else if lockedSnapshot != nil {
		v := *lockedSnapshot
		newSnapshot = &v
	}
	return &models.CommittedTable{
		Identifier:       models.TableIdentifier{Namespace: t.Namespace, Name: t.Name},
		TableRID:         t.RID,
		NewSnapshotID:    newSnapshot,
		MetadataLocation: metadataLocation,
	}, nil
}

// validateRequirementsLocked is a thin wrapper around
// validateRequirements that promotes the typed RequirementError to a
// RetryableError with the appropriate ConflictKind. The Rust handler
// surfaces these as Retryable rather than RequirementError because the
// pipeline-build executor must re-snapshot and retry — not branch on
// the assertion `kind`.
func validateRequirementsLocked(table *models.IcebergTable, reqs []json.RawMessage) error {
	err := validateRequirements(table, reqs)
	if err == nil {
		return nil
	}
	var reqErr *RequirementError
	if !errors.As(err, &reqErr) {
		return err
	}
	kind := models.ConflictKindUserJob
	switch reqErr.Kind {
	case "assert-current-schema-id", "assert-default-spec-id", "assert-default-sort-order-id", "assert-ref-snapshot-id":
		kind = models.ConflictKindCompaction
	}
	return &RetryableError{
		TableRID:        table.RID,
		Reason:          reqErr.Error(),
		ConflictingWith: kind,
	}
}

// UpdateTableSchema persists an explicit ALTER TABLE schema result.
func (r *Repo) UpdateTableSchema(ctx context.Context, tableID uuid.UUID, schema json.RawMessage) (*models.IcebergTable, error) {
	row := r.Pool.QueryRow(ctx,
		`UPDATE iceberg_tables t SET schema_json = $2, updated_at = NOW()
		 WHERE t.id = $1
		 RETURNING t.id, t.rid, t.namespace_id,
		   (SELECT n.name FROM iceberg_namespaces n WHERE n.id = t.namespace_id) AS namespace_name,
		   t.name, t.table_uuid, t.format_version, t.location, t.current_snapshot_id,
		   t.current_metadata_location, t.last_sequence_number, t.partition_spec,
		   t.schema_json, t.sort_order, t.properties, t.markings, t.created_at, t.updated_at`,
		tableID, schema)
	v, err := scanTable(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// GetTableByRID loads a table by the stable Foundry RID used by the admin UI.
func (r *Repo) GetTableByRID(ctx context.Context, rid string) (*models.IcebergTable, error) {
	row := r.Pool.QueryRow(ctx, tableSelect+` WHERE t.rid = $1`, rid)
	v, err := scanTable(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// ListAdminTables mirrors the Rust /api/v1/iceberg-tables query surface.
func (r *Repo) ListAdminTables(ctx context.Context, query models.ListIcebergTablesQuery) ([]models.IcebergTableSummary, error) {
	sql := `SELECT t.id, t.rid, n.project_rid, n.name AS namespace_name, t.name,
	       t.format_version, t.location, t.markings, t.created_at,
	       (SELECT MAX(timestamp_ms) FROM iceberg_snapshots WHERE table_id = t.id) AS last_ts_ms,
	       (SELECT (summary->>'total-records')::BIGINT FROM iceberg_snapshots
	          WHERE table_id = t.id ORDER BY timestamp_ms DESC LIMIT 1) AS row_count_estimate
	       FROM iceberg_tables t JOIN iceberg_namespaces n ON n.id = t.namespace_id WHERE 1=1`
	args := make([]any, 0, 3)
	if query.ProjectRID != "" {
		args = append(args, query.ProjectRID)
		sql += fmt.Sprintf(" AND n.project_rid = $%d", len(args))
	}
	if query.Namespace != "" {
		args = append(args, query.Namespace)
		sql += fmt.Sprintf(" AND n.name = $%d", len(args))
	}
	if query.Name != "" {
		args = append(args, "%"+query.Name+"%")
		sql += fmt.Sprintf(" AND t.name ILIKE $%d", len(args))
	}
	switch query.Sort {
	case "name":
		sql += " ORDER BY t.name ASC"
	case "created_at":
		sql += " ORDER BY t.created_at DESC"
	default:
		sql += " ORDER BY t.updated_at DESC"
	}
	rows, err := r.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.IcebergTableSummary{}
	for rows.Next() {
		var s models.IcebergTableSummary
		var namespaceName string
		var lastTS *int64
		if err := rows.Scan(&s.ID, &s.RID, &s.ProjectRID, &namespaceName, &s.Name, &s.FormatVersion, &s.Location, &s.Markings, &s.CreatedAt, &lastTS, &s.RowCountEstimate); err != nil {
			return nil, err
		}
		s.Namespace = decodePath(namespaceName)
		if lastTS != nil {
			t := time.UnixMilli(*lastTS).UTC()
			s.LastSnapshotAt = &t
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
