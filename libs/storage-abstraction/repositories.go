// Package storageabstraction is the Go counterpart of
// libs/storage-abstraction. This file (`repositories.go`) ports the
// trait surface from `libs/storage-abstraction/src/repositories.rs`
// — the storage-agnostic repository interfaces consumed by every
// service that persists ontology-shaped data, links, sessions or
// action history.
//
// The concrete implementations live elsewhere: Cassandra-backed
// stores in `libs/cassandra-kernel`, Vespa/OpenSearch-backed stores
// in `libs/search-abstraction`. Putting the interfaces next to the
// `StorageBackend` (object storage) keeps the two abstractions in
// the same module so services depend on a single
// `storage-abstraction` and pick the wiring at composition time.
//
// Scope of this slice (P2.5.1):
//   - All ID newtypes (ObjectId, TypeId, LinkTypeId, TenantId,
//     OwnerId, MarkingId, ObjectSetId).
//   - Domain payloads (Object, Link, Schema, Session,
//     ActionLogEntry).
//   - Pagination + read-consistency hint + put-outcome union +
//     RepoError tree.
//   - Five interfaces consumed by cassandra-kernel: ObjectStore,
//     LinkStore, SchemaStore, SessionStore, ActionLogStore.
//
// Out of scope (lands with their own ports):
//   - ObjectSet* materialization, DefinitionStore, ReadModelStore,
//     SearchBackend, IndexDoc, VectorQuery, BulkOutcome — all
//     search-abstraction territory.
//   - StorageBackend (object storage), backing_fs / s3 / iceberg,
//     signed URLs — separate sub-package.
package storageabstraction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ----------------------------------------------------------------------
// IDs
// ----------------------------------------------------------------------

// ObjectId is a stable identifier for a stored object. Backends
// decide the lexical shape (UUIDv7, TimeUUID, …); the trait surface
// is opaque.
type ObjectId string

// TypeId is a stable identifier for an object type (a node label in
// ontology terms, e.g. "aircraft", "flight_event", "customer").
type TypeId string

// ObjectSetId is a stable identifier for a saved object set definition.
type ObjectSetId string

// LinkTypeId is a stable identifier for a link type between two
// object types.
type LinkTypeId string

// TenantId is the tenant scope. Every read and write is implicitly
// tenant-scoped; backends MUST never serve cross-tenant data.
type TenantId string

// OwnerId is the owner of an object (the principal that created it).
// Maps to the `objects_by_owner` Cassandra access pattern.
type OwnerId string

// MarkingId is a classification / marking label that gates access
// to an object. Maps to `objects_by_marking` Cassandra access
// pattern.
type MarkingId string

// ----------------------------------------------------------------------
// Domain payloads
// ----------------------------------------------------------------------

// Object is a persisted ontology object. The Payload is opaque
// (json.RawMessage) because the schema is per-type and lives in
// SchemaStore; this trait surface is intentionally schema-free.
type Object struct {
	Tenant         TenantId        `json:"tenant"`
	ID             ObjectId        `json:"id"`
	TypeID         TypeId          `json:"type_id"`
	Version        uint64          `json:"version"`
	Payload        json.RawMessage `json:"payload"`
	OrganizationID *string         `json:"organization_id,omitempty"`
	CreatedAtMs    *int64          `json:"created_at_ms,omitempty"`
	UpdatedAtMs    int64           `json:"updated_at_ms"`
	Owner          *OwnerId        `json:"owner,omitempty"`
	Markings       []MarkingId     `json:"markings,omitempty"`
}

// Link is a directed edge between two objects.
type Link struct {
	Tenant      TenantId        `json:"tenant"`
	LinkType    LinkTypeId      `json:"link_type"`
	From        ObjectId        `json:"from"`
	To          ObjectId        `json:"to"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	CreatedAtMs int64           `json:"created_at_ms"`
}

// Schema is a versioned JSON-Schema for a TypeId.
type Schema struct {
	TypeID      TypeId          `json:"type_id"`
	Version     uint32          `json:"version"`
	JsonSchema  json.RawMessage `json:"json_schema"`
	CreatedAtMs int64           `json:"created_at_ms"`
}

// Session is a user / service-account session.
type Session struct {
	Tenant       TenantId          `json:"tenant"`
	ID           string            `json:"id"`
	Subject      string            `json:"subject"`
	Attributes   map[string]string `json:"attributes"`
	IssuedAtMs   int64             `json:"issued_at_ms"`
	ExpiresAtMs  int64             `json:"expires_at_ms"`
}

// ActionLogEntry is a single ontology action log entry.
type ActionLogEntry struct {
	Tenant       TenantId        `json:"tenant"`
	EventID      *string         `json:"event_id,omitempty"`
	ActionID     string          `json:"action_id"`
	Kind         string          `json:"kind"`
	Subject      string          `json:"subject"`
	Object       *ObjectId       `json:"object,omitempty"`
	Payload      json.RawMessage `json:"payload"`
	RecordedAtMs int64           `json:"recorded_at_ms"`
}

// ----------------------------------------------------------------------
// Pagination
// ----------------------------------------------------------------------

// Page is a token-based paging hint. Token is opaque to callers and
// is exactly what each backend needs (Cassandra paging state, Vespa
// continuation, OpenSearch search_after).
type Page struct {
	Size  uint32  `json:"size"`
	Token *string `json:"token,omitempty"`
}

// PagedResult is the shape returned by every paged list call.
type PagedResult[T any] struct {
	Items     []T     `json:"items"`
	NextToken *string `json:"next_token,omitempty"`
}

// ----------------------------------------------------------------------
// Consistency hints
// ----------------------------------------------------------------------

// ConsistencyLevel discriminates the ReadConsistency variants.
type ConsistencyLevel uint8

const (
	// ConsistencyStrong → LOCAL_QUORUM on Cassandra; "wait for
	// indexing" on search backends.
	ConsistencyStrong ConsistencyLevel = iota
	// ConsistencyEventual → LOCAL_ONE on Cassandra; immediate /
	// cached on search.
	ConsistencyEventual
	// ConsistencyBoundedStaleness → eventual but with an upper
	// bound on staleness. Backends that cannot honour the bound
	// fall back to Eventual.
	ConsistencyBoundedStaleness
)

// ReadConsistency is the consistency hint passed to read-side calls.
// Mirrors the Rust enum (Strong | Eventual | BoundedStaleness(d)).
type ReadConsistency struct {
	Level    ConsistencyLevel
	Staleness time.Duration // valid only when Level == BoundedStaleness
}

// Strong is the default consistency. Mirrors Default::default in the
// Rust impl.
func Strong() ReadConsistency { return ReadConsistency{Level: ConsistencyStrong} }

// Eventual constructs the LOCAL_ONE / immediate-read variant.
func Eventual() ReadConsistency { return ReadConsistency{Level: ConsistencyEventual} }

// BoundedStaleness constructs a BoundedStaleness(d) hint.
func BoundedStaleness(d time.Duration) ReadConsistency {
	return ReadConsistency{Level: ConsistencyBoundedStaleness, Staleness: d}
}

// ----------------------------------------------------------------------
// Put outcome (tagged union)
// ----------------------------------------------------------------------

// PutOutcomeKind discriminates the three PutOutcome variants.
type PutOutcomeKind uint8

const (
	// PutInserted → first insert; previous version did not exist.
	PutInserted PutOutcomeKind = iota
	// PutUpdated → update applied; the persisted row now has
	// NewVersion (the row had PreviousVersion before the update).
	PutUpdated
	// PutVersionConflict → optimistic lock failed. Caller must
	// re-read and retry. ExpectedVersion is what the caller
	// declared, ActualVersion is what the row actually held.
	PutVersionConflict
)

// PutOutcome is the result of an optimistic-concurrency put.
// Mirrors the Rust enum: Inserted | Updated{prev,new} |
// VersionConflict{expected,actual}.
type PutOutcome struct {
	Kind            PutOutcomeKind
	PreviousVersion uint64
	NewVersion      uint64
	ExpectedVersion uint64
	ActualVersion   uint64
}

// Inserted is the canonical Inserted outcome.
func Inserted() PutOutcome { return PutOutcome{Kind: PutInserted} }

// Updated builds an Updated outcome.
func Updated(previous, next uint64) PutOutcome {
	return PutOutcome{Kind: PutUpdated, PreviousVersion: previous, NewVersion: next}
}

// VersionConflict builds a VersionConflict outcome.
func VersionConflict(expected, actual uint64) PutOutcome {
	return PutOutcome{Kind: PutVersionConflict, ExpectedVersion: expected, ActualVersion: actual}
}

// ----------------------------------------------------------------------
// Errors
// ----------------------------------------------------------------------

// RepoErrorKind tags the RepoError variants. The Rust side uses an
// enum with thiserror::Error; Go uses sentinel-typed errors so
// callers can `errors.As` or `errors.Is` selectively.
type RepoErrorKind uint8

const (
	// RepoNotFound → object / link / session was not found.
	RepoNotFound RepoErrorKind = iota
	// RepoInvalidArgument → caller passed an argument the backend
	// cannot satisfy (malformed token, payload too large, …).
	RepoInvalidArgument
	// RepoTenantScope → tenant scope violation. Backends raise
	// this rather than returning empty results so callers do not
	// silently confuse a missing object with a missing tenant.
	RepoTenantScope
	// RepoBackend → backend-level failure (network, timeout,
	// decode, …). The repository contract does not promise to
	// expose backend-typed errors.
	RepoBackend
)

// RepoError is the typed error returned by every repository call.
// Use errors.As or the ErrorKind helper to discriminate.
type RepoError struct {
	Kind    RepoErrorKind
	Message string
}

func (e *RepoError) Error() string {
	switch e.Kind {
	case RepoNotFound:
		return "not found: " + e.Message
	case RepoInvalidArgument:
		return "invalid argument: " + e.Message
	case RepoTenantScope:
		return "tenant scope violation: " + e.Message
	case RepoBackend:
		return "backend error: " + e.Message
	default:
		return "repo error: " + e.Message
	}
}

// NotFound builds a RepoNotFound error.
func NotFound(msg string) error { return &RepoError{Kind: RepoNotFound, Message: msg} }

// Invalid builds a RepoInvalidArgument error.
func Invalid(msg string) error { return &RepoError{Kind: RepoInvalidArgument, Message: msg} }

// Invalidf is the formatted variant of Invalid.
func Invalidf(format string, args ...any) error {
	return &RepoError{Kind: RepoInvalidArgument, Message: fmt.Sprintf(format, args...)}
}

// TenantScope builds a RepoTenantScope error.
func TenantScope(msg string) error { return &RepoError{Kind: RepoTenantScope, Message: msg} }

// Backend builds a RepoBackend error.
func Backend(msg string) error { return &RepoError{Kind: RepoBackend, Message: msg} }

// Backendf is the formatted variant of Backend.
func Backendf(format string, args ...any) error {
	return &RepoError{Kind: RepoBackend, Message: fmt.Sprintf(format, args...)}
}

// IsNotFound reports whether err (or any wrapped err) is a
// RepoNotFound RepoError.
func IsNotFound(err error) bool {
	var re *RepoError
	return errors.As(err, &re) && re.Kind == RepoNotFound
}

// IsInvalidArgument reports whether err (or any wrapped err) is a
// RepoInvalidArgument RepoError.
func IsInvalidArgument(err error) bool {
	var re *RepoError
	return errors.As(err, &re) && re.Kind == RepoInvalidArgument
}

// IsTenantScope reports whether err (or any wrapped err) is a
// RepoTenantScope RepoError.
func IsTenantScope(err error) bool {
	var re *RepoError
	return errors.As(err, &re) && re.Kind == RepoTenantScope
}

// IsBackendError reports whether err (or any wrapped err) is a
// RepoBackend RepoError.
func IsBackendError(err error) bool {
	var re *RepoError
	return errors.As(err, &re) && re.Kind == RepoBackend
}

// ----------------------------------------------------------------------
// Store interfaces
// ----------------------------------------------------------------------

// ObjectStore exposes CRUD over Object with optimistic concurrency.
// Mirrors trait ObjectStore in repositories.rs.
type ObjectStore interface {
	// Get fetches one object by (tenant, id). Returns (nil, nil)
	// for a genuine miss; reserves NotFound for the case where the
	// type itself is unknown.
	Get(ctx context.Context, tenant TenantId, id ObjectId, consistency ReadConsistency) (*Object, error)

	// Put inserts or updates with optimistic concurrency.
	// expectedVersion = nil → insert-only (fails on conflict).
	Put(ctx context.Context, obj Object, expectedVersion *uint64) (PutOutcome, error)

	// Delete by (tenant, id). Returns (false, nil) if the object
	// did not exist — deletes are idempotent.
	Delete(ctx context.Context, tenant TenantId, id ObjectId) (bool, error)

	// ListByType pages through every object of a given type within
	// a tenant. Backends MUST enforce stable ordering across pages.
	ListByType(ctx context.Context, tenant TenantId, typeID TypeId, page Page, consistency ReadConsistency) (PagedResult[Object], error)

	// ListByOwner pages through every object owned by `owner`
	// within a tenant. Maps to objects_by_owner Cassandra access
	// pattern. Production backends MUST override the default
	// (non-existent in Go since interfaces have no default impls)
	// — surfaces NotImplemented when the backend has not
	// specialised it.
	ListByOwner(ctx context.Context, tenant TenantId, owner OwnerId, page Page, consistency ReadConsistency) (PagedResult[Object], error)

	// ListByMarking pages through every object bearing `marking`
	// within a tenant. Maps to objects_by_marking access pattern.
	ListByMarking(ctx context.Context, tenant TenantId, marking MarkingId, page Page, consistency ReadConsistency) (PagedResult[Object], error)
}

// LinkStore exposes CRUD over Link.
type LinkStore interface {
	// Put persists a link. Links are immutable: a second Put of
	// the same (tenant, linkType, from, to) triple is a no-op.
	Put(ctx context.Context, link Link) error

	// Delete a link triple. Returns (false, nil) if absent.
	Delete(ctx context.Context, tenant TenantId, linkType LinkTypeId, from, to ObjectId) (bool, error)

	// ListOutgoing yields every outgoing link of the given type
	// from `from`.
	ListOutgoing(ctx context.Context, tenant TenantId, linkType LinkTypeId, from ObjectId, page Page, consistency ReadConsistency) (PagedResult[Link], error)

	// ListIncoming yields every incoming link of the given type
	// into `to`.
	ListIncoming(ctx context.Context, tenant TenantId, linkType LinkTypeId, to ObjectId, page Page, consistency ReadConsistency) (PagedResult[Link], error)
}

// SchemaStore is the per-type schema registry.
type SchemaStore interface {
	// GetLatest returns the latest schema for a type, or nil if
	// unknown.
	GetLatest(ctx context.Context, typeID TypeId, consistency ReadConsistency) (*Schema, error)

	// GetVersion returns a specific version, or nil if absent.
	GetVersion(ctx context.Context, typeID TypeId, version uint32, consistency ReadConsistency) (*Schema, error)

	// Put appends a new schema version. Implementations MUST
	// reject any version ≤ the latest known one.
	Put(ctx context.Context, schema Schema) error
}

// SessionStore is the session storage. Backed by the auth_runtime
// keyspace in production; sessions carry a TTL enforced at the
// storage layer.
type SessionStore interface {
	// Get fetches by session id. Expired sessions return
	// (nil, nil).
	Get(ctx context.Context, tenant TenantId, id string, consistency ReadConsistency) (*Session, error)

	// Put persists a session. Implementations MUST set the storage
	// TTL to expires_at_ms - now.
	Put(ctx context.Context, session Session) error

	// Revoke a session immediately, regardless of its TTL.
	Revoke(ctx context.Context, tenant TenantId, id string) (bool, error)
}

// ActionLogStore is the append-only action log.
type ActionLogStore interface {
	// Append one entry. The append is atomic and idempotent on
	// (tenant, event_id); implementations MAY derive a
	// deterministic event_id when the field is empty.
	Append(ctx context.Context, entry ActionLogEntry) error

	// ListRecent pages through actions for a tenant in time-DESC
	// order.
	ListRecent(ctx context.Context, tenant TenantId, page Page, consistency ReadConsistency) (PagedResult[ActionLogEntry], error)

	// ListForObject pages through actions touching a specific
	// object.
	ListForObject(ctx context.Context, tenant TenantId, object ObjectId, page Page, consistency ReadConsistency) (PagedResult[ActionLogEntry], error)

	// ListForAction pages through actions for a specific
	// action_id.
	ListForAction(ctx context.Context, tenant TenantId, actionID string, page Page, consistency ReadConsistency) (PagedResult[ActionLogEntry], error)
}
