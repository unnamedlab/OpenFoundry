// Package models holds wire types for iceberg-catalog-service.
//
// Foundation slice scope: iceberg_namespaces only. iceberg_tables,
// iceberg_snapshots, iceberg_branches, metadata files, REST Catalog
// OpenAPI surface (10k LOC of Rust handlers) all land in follow-up
// slices.
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ListResponse[T any] struct {
	Items []T `json:"items"`
}

// IcebergNamespace mirrors `iceberg_namespaces` rows.
type IcebergNamespace struct {
	ID                uuid.UUID       `json:"id"`
	ProjectRID        string          `json:"project_rid"`
	Name              string          `json:"name"`
	ParentNamespaceID *uuid.UUID      `json:"parent_namespace_id"`
	Properties        json.RawMessage `json:"properties"`
	CreatedAt         time.Time       `json:"created_at"`
	CreatedBy         uuid.UUID       `json:"created_by"`
}

// CreateNamespaceRequest is the body of POST /api/v1/namespaces.
type CreateNamespaceRequest struct {
	ProjectRID        string          `json:"project_rid"`
	Name              string          `json:"name"`
	ParentNamespaceID *uuid.UUID      `json:"parent_namespace_id,omitempty"`
	Properties        json.RawMessage `json:"properties,omitempty"`
}

// UpdateNamespaceRequest mirrors PATCH semantics — `properties` is the
// only mutable field (name/parent_namespace_id are immutable per the
// Iceberg REST spec).
type UpdateNamespaceRequest struct {
	Properties json.RawMessage `json:"properties,omitempty"`
}

type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    int    `json:"code"`
	// Kind, when set, carries the failing requirement assertion
	// (e.g. "assert-uuid", "assert-current-schema-id") so PyIceberg /
	// Spark callers can branch on the broken assertion without
	// parsing the free-form Message. Mirrors Rust's
	// TableError::RequirementsFailed envelope.
	Kind string `json:"kind,omitempty"`
}

// SchemaIncompatibleEnvelope is the 422 response shape returned when
// CommitTable refuses an `add-schema` update that diverges from the
// current schema (strict-mode rejection per Foundry doc
// § "Automatic schema evolution"). Mirrors Rust's
// ApiError::SchemaIncompatible body so the pipeline-authoring UI's
// "generate ALTER TABLE" CTA receives the structural diff verbatim.
type SchemaIncompatibleEnvelope struct {
	Error SchemaIncompatibleErrorBody `json:"error"`
}

// SchemaIncompatibleErrorBody carries the diff envelope used by the
// "generate ALTER TABLE" client surface. The `current_schema` and
// `attempted_schema` fields are returned verbatim so the client can
// render a side-by-side preview without re-fetching the table.
//
// `Diff` is typed as `domain.SchemaDiff` indirectly through
// json.RawMessage to avoid a models→domain import cycle; the
// CommitTable handler marshals the diff before constructing the
// envelope.
type SchemaIncompatibleErrorBody struct {
	Message         string          `json:"message"`
	Type            string          `json:"type"`
	Code            int             `json:"code"`
	CurrentSchema   json.RawMessage `json:"current_schema,omitempty"`
	AttemptedSchema json.RawMessage `json:"attempted_schema,omitempty"`
	Diff            any             `json:"diff"`
}

type TableIdentifier struct {
	Namespace []string `json:"namespace"`
	Name      string   `json:"name"`
}

type ListTablesResponse struct {
	Identifiers []TableIdentifier `json:"identifiers"`
}

type IcebergTable struct {
	ID                      uuid.UUID       `json:"id"`
	RID                     string          `json:"rid"`
	NamespaceID             uuid.UUID       `json:"namespace_id"`
	Namespace               []string        `json:"namespace"`
	Name                    string          `json:"name"`
	TableUUID               string          `json:"table_uuid"`
	FormatVersion           int32           `json:"format_version"`
	Location                string          `json:"location"`
	CurrentSnapshotID       *int64          `json:"current_snapshot_id"`
	CurrentMetadataLocation *string         `json:"current_metadata_location"`
	LastSequenceNumber      int64           `json:"last_sequence_number"`
	PartitionSpec           json.RawMessage `json:"partition_spec"`
	SchemaJSON              json.RawMessage `json:"schema_json"`
	SortOrder               json.RawMessage `json:"sort_order"`
	Properties              json.RawMessage `json:"properties"`
	Markings                []string        `json:"markings"`
	CreatedAt               time.Time       `json:"created_at"`
	UpdatedAt               time.Time       `json:"updated_at"`
}

type CreateTableRequest struct {
	Name          string            `json:"name"`
	Schema        json.RawMessage   `json:"schema"`
	PartitionSpec json.RawMessage   `json:"partition-spec,omitempty"`
	SortOrder     json.RawMessage   `json:"sort-order,omitempty"`
	Properties    map[string]string `json:"properties,omitempty"`
	Location      *string           `json:"location,omitempty"`
	FormatVersion *int32            `json:"format-version,omitempty"`
	StageCreate   *bool             `json:"stage-create,omitempty"`
	Markings      []string          `json:"markings,omitempty"`
}

type LoadTableResponse struct {
	Metadata         json.RawMessage   `json:"metadata"`
	MetadataLocation string            `json:"metadata-location"`
	Config           map[string]string `json:"config"`
}

type Snapshot struct {
	ID                   int64           `json:"id"`
	TableID              uuid.UUID       `json:"table_id"`
	SnapshotID           int64           `json:"snapshot_id"`
	ParentSnapshotID     *int64          `json:"parent_snapshot_id"`
	SequenceNumber       int64           `json:"sequence_number"`
	Operation            string          `json:"operation"`
	ManifestListLocation string          `json:"manifest_list_location"`
	Summary              json.RawMessage `json:"summary"`
	SchemaID             int32           `json:"schema_id"`
	TimestampMS          int64           `json:"timestamp_ms"`
}

type CommitTableRequest struct {
	Identifier   *TableIdentifier  `json:"identifier,omitempty"`
	Requirements []json.RawMessage `json:"requirements,omitempty"`
	Updates      []json.RawMessage `json:"updates,omitempty"`
}

type CommitTableResponse struct {
	Metadata         json.RawMessage `json:"metadata"`
	MetadataLocation string          `json:"metadata-location"`
}

type TableRef struct {
	ID                 uuid.UUID `json:"id,omitempty"`
	TableID            uuid.UUID `json:"table_id,omitempty"`
	Name               string    `json:"name"`
	Kind               string    `json:"type"`
	SnapshotID         int64     `json:"snapshot-id"`
	MaxRefAgeMS        *int64    `json:"max-ref-age-ms,omitempty"`
	MaxSnapshotAgeMS   *int64    `json:"max-snapshot-age-ms,omitempty"`
	MinSnapshotsToKeep *int32    `json:"min-snapshots-to-keep,omitempty"`
	CreatedAt          time.Time `json:"created_at,omitempty"`
}

type ListRefsResponse struct {
	Refs map[string]TableRef `json:"refs"`
}

type UpdateRefRequest struct {
	Type               string `json:"type"`
	SnapshotID         int64  `json:"snapshot-id"`
	MaxRefAgeMS        *int64 `json:"max-ref-age-ms,omitempty"`
	MaxSnapshotAgeMS   *int64 `json:"max-snapshot-age-ms,omitempty"`
	MinSnapshotsToKeep *int32 `json:"min-snapshots-to-keep,omitempty"`
}

type MetadataFile struct {
	ID        uuid.UUID `json:"id,omitempty"`
	TableID   uuid.UUID `json:"table_id,omitempty"`
	Version   int32     `json:"version"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

type ListMetadataFilesResponse struct {
	MetadataFiles []MetadataFile `json:"metadata-files"`
}

type MetadataFileResponse struct {
	Metadata         json.RawMessage `json:"metadata"`
	MetadataLocation string          `json:"metadata-location"`
	Version          int32           `json:"version"`
}

type ListSnapshotsResponse struct {
	Snapshots []Snapshot `json:"snapshots"`
}

type RenameTableRequest struct {
	Source      TableIdentifier `json:"source"`
	Destination TableIdentifier `json:"destination"`
}

// FieldSpec is the compact schema shape used by OpenFoundry sink writers when
// appending rows through the in-process table-writer adapter endpoint.
type FieldSpec struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// TableSpec identifies and validates an append target for
// POST /openfoundry/iceberg/v1/append.
type TableSpec struct {
	Catalog            string      `json:"catalog"`
	Warehouse          string      `json:"warehouse,omitempty"`
	Namespace          string      `json:"namespace"`
	Table              string      `json:"table"`
	PartitionTransform string      `json:"partition_transform"`
	SortOrder          string      `json:"sort_order"`
	Schema             []FieldSpec `json:"schema"`
}

// AppendBatch is the request body consumed by the OpenFoundry Iceberg HTTP
// table-writer adapter used by audit-sink and ai-sink.
type AppendBatch struct {
	Spec TableSpec        `json:"spec"`
	Rows []map[string]any `json:"rows"`
}

// AppendBatchResponse summarizes a committed append.
type AppendBatchResponse struct {
	Namespace        string `json:"namespace"`
	Table            string `json:"table"`
	Rows             int    `json:"rows"`
	MetadataLocation string `json:"metadata_location"`
}

// MultiTableCommitRequest is the body of POST /iceberg/v1/transactions/commit.
//
// Mirrors Rust's `MultiTableCommitRequest` in
// services/iceberg-catalog-service/src/handlers/rest_catalog/transactions.rs.
// Every entry in `TableChanges` either lands together or is rolled back —
// row-level locks are taken in deterministic table-id order so concurrent
// commits cannot deadlock.
type MultiTableCommitRequest struct {
	TableChanges []MultiTableChange `json:"table-changes"`
	// BuildRID correlates the commit with the originating Foundry build
	// (audit trail + metric labels). Optional: empty when the caller is
	// not a build executor.
	BuildRID string `json:"build_rid,omitempty"`
}

// MultiTableChange is one table's slice of a multi-table commit. Mirrors
// the per-table shape of Rust's `TableChange` (note: this is the
// handler-level wire shape, distinct from the domain-level
// `foundry_transaction::TableChange` used by the wrapper).
type MultiTableChange struct {
	Identifier   TableIdentifier   `json:"identifier"`
	Requirements []json.RawMessage `json:"requirements,omitempty"`
	Updates      []json.RawMessage `json:"updates,omitempty"`
}

// MultiTableCommitResponse is returned by a successful multi-table commit.
type MultiTableCommitResponse struct {
	Committed []CommittedTable `json:"committed"`
}

// CommittedTable describes one table's outcome inside a multi-table commit.
// `MetadataLocation` uses the kebab-case `metadata-location` JSON tag for
// parity with the Iceberg REST spec (and Rust's `#[serde(rename = …)]`).
type CommittedTable struct {
	Identifier       TableIdentifier `json:"identifier"`
	TableRID         string          `json:"table_rid"`
	NewSnapshotID    *int64          `json:"new_snapshot_id"`
	MetadataLocation string          `json:"metadata-location"`
}

// ConflictKind labels the source of a multi-table commit conflict so
// dashboards can split user-job conflicts from compaction / maintenance
// jobs. Mirrors Rust's `ConflictKind` enum (snake_case wire format).
//
// Mapping into the catalog:
//
//   - assert-uuid                  → ConflictKindUserJob
//   - assert-current-schema-id     → ConflictKindCompaction
//   - assert-ref-snapshot-id       → ConflictKindCompaction
//   - row lock acquisition failure → ConflictKindUnknown
type ConflictKind string

const (
	ConflictKindCompaction  ConflictKind = "compaction"
	ConflictKindMaintenance ConflictKind = "maintenance"
	ConflictKindUserJob     ConflictKind = "user_job"
	ConflictKindUnknown     ConflictKind = "unknown"
)

// RetryableErrorEnvelope is the 409 body returned when a multi-table
// commit hits a row-lock or requirement conflict. The pipeline-build
// executor branches on `Error.ConflictingWith` to decide whether to
// re-snapshot inputs and retry. Mirrors Rust's
// `ApiError::Retryable` JSON shape verbatim.
type RetryableErrorEnvelope struct {
	Error RetryableErrorBody `json:"error"`
}

// RetryableErrorBody mirrors Rust's CONFLICTING_CONCURRENT_UPDATE
// envelope: a structured body with `table_rid` and `conflicting_with`
// pulled out so clients can branch without parsing the message.
type RetryableErrorBody struct {
	Message         string       `json:"message"`
	Type            string       `json:"type"`
	Code            int          `json:"code"`
	TableRID        string       `json:"table_rid"`
	ConflictingWith ConflictKind `json:"conflicting_with"`
}

// ListIcebergTablesQuery captures Foundry admin table-list filters.
type ListIcebergTablesQuery struct {
	ProjectRID string
	Namespace  string
	Name       string
	Sort       string
}

// IcebergTableSummary is the UI-facing row returned by /api/v1/iceberg-tables.
type IcebergTableSummary struct {
	ID               uuid.UUID  `json:"id"`
	RID              string     `json:"rid"`
	ProjectRID       string     `json:"project_rid,omitempty"`
	Namespace        []string   `json:"namespace"`
	Name             string     `json:"name"`
	FormatVersion    int32      `json:"format_version"`
	Location         string     `json:"location"`
	Markings         []string   `json:"markings"`
	LastSnapshotAt   *time.Time `json:"last_snapshot_at,omitempty"`
	RowCountEstimate *int64     `json:"row_count_estimate,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type IcebergTableListResponse struct {
	Tables []IcebergTableSummary `json:"tables"`
}

type IcebergTableDetail struct {
	Summary                 IcebergTableSummary `json:"summary"`
	Schema                  json.RawMessage     `json:"schema"`
	Properties              json.RawMessage     `json:"properties"`
	PartitionSpec           json.RawMessage     `json:"partition_spec"`
	SortOrder               json.RawMessage     `json:"sort_order"`
	CurrentMetadataLocation *string             `json:"current_metadata_location"`
	CurrentSnapshotID       *int64              `json:"current_snapshot_id"`
	LastSequenceNumber      int64               `json:"last_sequence_number"`
}

type SnapshotEntry struct {
	SnapshotID       int64           `json:"snapshot_id"`
	ParentSnapshotID *int64          `json:"parent_snapshot_id"`
	Operation        string          `json:"operation"`
	Timestamp        time.Time       `json:"timestamp"`
	SequenceNumber   int64           `json:"sequence_number"`
	ManifestList     string          `json:"manifest_list"`
	SchemaID         int32           `json:"schema_id"`
	Summary          json.RawMessage `json:"summary"`
}

type SnapshotListResponse struct {
	Snapshots []SnapshotEntry `json:"snapshots"`
}

type MetadataResponse struct {
	Metadata         json.RawMessage        `json:"metadata"`
	MetadataLocation string                 `json:"metadata_location"`
	History          []MetadataHistoryEntry `json:"history"`
}

type MetadataHistoryEntry struct {
	Version   int32     `json:"version"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
}

type BranchEntry struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	SnapshotID int64  `json:"snapshot_id"`
}

type BranchListResponse struct {
	Branches []BranchEntry `json:"branches"`
}
