// Package storage holds the ObjectStore / LinkStore contract plus the
// service-local adapters for in-memory test state and production Cassandra
// stores backed by libs/cassandra-kernel.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Newtype string wrappers (mirror Rust serde transparent encoding —
// JSON uses the inner string directly).
type (
	TenantId   string
	ObjectId   string
	TypeId     string
	OwnerId    string
	MarkingId  string
	LinkTypeId string
)

// Object is one persisted ontology object. Payload is JSON-opaque.
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

// Link is a directed link between two objects.
type Link struct {
	Tenant      TenantId         `json:"tenant"`
	LinkType    LinkTypeId       `json:"link_type"`
	From        ObjectId         `json:"from"`
	To          ObjectId         `json:"to"`
	Payload     *json.RawMessage `json:"payload,omitempty"`
	CreatedAtMs int64            `json:"created_at_ms"`
}

// Page captures pagination state on the request side.
type Page struct {
	Size  uint32  `json:"size"`
	Token *string `json:"token,omitempty"`
}

// PagedResult wraps a slice + the next-page token.
type PagedResult[T any] struct {
	Items     []T     `json:"items"`
	NextToken *string `json:"next_token"`
}

// ReadConsistency mirrors the Rust enum (BoundedStaleness omitted
// from the Go foundation: not needed by handlers yet).
type ReadConsistency uint8

const (
	ReadStrong   ReadConsistency = iota // LOCAL_QUORUM / wait for index
	ReadEventual                        // LOCAL_ONE / cached
)

// PutOutcomeKind discriminates the put outcome.
type PutOutcomeKind string

const (
	PutInserted        PutOutcomeKind = "inserted"
	PutUpdated         PutOutcomeKind = "updated"
	PutVersionConflict PutOutcomeKind = "version_conflict"
)

// PutOutcome is the result of an optimistic-concurrency put.
type PutOutcome struct {
	Kind            PutOutcomeKind
	PreviousVersion uint64 // Updated
	NewVersion      uint64 // Updated
	ExpectedVersion uint64 // VersionConflict
	ActualVersion   uint64 // VersionConflict
}

// RepoErrorKind classifies repo errors. HTTP layer maps these to
// 404/400/403/500 matching Rust.
type RepoErrorKind uint8

const (
	ErrNotFound RepoErrorKind = iota
	ErrInvalidArgument
	ErrTenantScope
	ErrBackend
)

// RepoError carries a kind + message; HTTP handler unwraps via errors.As.
type RepoError struct {
	Kind RepoErrorKind
	Msg  string
}

func (e *RepoError) Error() string {
	switch e.Kind {
	case ErrNotFound:
		return fmt.Sprintf("not found: %s", e.Msg)
	case ErrInvalidArgument:
		return fmt.Sprintf("invalid argument: %s", e.Msg)
	case ErrTenantScope:
		return fmt.Sprintf("tenant scope violation: %s", e.Msg)
	default:
		return fmt.Sprintf("backend error: %s", e.Msg)
	}
}

// As helper for errors.As.
func AsRepoError(err error) (*RepoError, bool) {
	var re *RepoError
	if errors.As(err, &re) {
		return re, true
	}
	return nil, false
}

// PointReadStore is implemented by stores that can fetch by object type plus
// primary key/RID directly, matching OSV2.6 Get(type, primary_key).
type PointReadStore interface {
	GetByTypeAndPrimaryKey(ctx context.Context, tenant TenantId, typeID TypeId, primaryKey string, c ReadConsistency) (*Object, error)
}

// PropertyPredicate is the OSV2.4 property-index query contract used by
// object-database-service query pushdown.
type PropertyPredicate struct {
	PropertyName string
	Operator     string
	Value        any
}

// PropertyQueryStore is implemented by stores that can push simple predicates
// into the OSV2 property index instead of scanning the full object type.
type PropertyQueryStore interface {
	QueryByProperty(ctx context.Context, tenant TenantId, typeID TypeId, predicate PropertyPredicate, page Page, c ReadConsistency) (PagedResult[Object], error)
}

// ObjectStore is the contract used by handlers. Cassandra impl follows.
type ObjectStore interface {
	Get(ctx context.Context, tenant TenantId, id ObjectId, c ReadConsistency) (*Object, error)
	Put(ctx context.Context, obj Object, expectedVersion *uint64) (PutOutcome, error)
	Delete(ctx context.Context, tenant TenantId, id ObjectId) (bool, error)
	ListByType(ctx context.Context, tenant TenantId, typeID TypeId, page Page, c ReadConsistency) (PagedResult[Object], error)
	ListByOwner(ctx context.Context, tenant TenantId, owner OwnerId, page Page, c ReadConsistency) (PagedResult[Object], error)
	ListByMarking(ctx context.Context, tenant TenantId, marking MarkingId, page Page, c ReadConsistency) (PagedResult[Object], error)
}

// LinkStore is the link-side contract.
type LinkStore interface {
	Put(ctx context.Context, link Link) error
	Delete(ctx context.Context, tenant TenantId, lt LinkTypeId, from, to ObjectId) (bool, error)
	ListOutgoing(ctx context.Context, tenant TenantId, lt LinkTypeId, from ObjectId, page Page, c ReadConsistency) (PagedResult[Link], error)
	ListIncoming(ctx context.Context, tenant TenantId, lt LinkTypeId, to ObjectId, page Page, c ReadConsistency) (PagedResult[Link], error)
}

// IncidentLinkDeleter is the optional cascade-delete contract used by
// the ontology DeleteObject handler. Stores that can scan their full
// link surface (in-memory) implement this directly; the Cassandra
// adapter wires it as a no-op (returns 0 + a guidance comment) because
// the production cassandra-kernel link tables are keyed on
// `(link_type_rid, src/dst)` and a scan across all link types is
// degenerate without a separate index. The handler degrades to a soft
// "best-effort" cascade in that case and the FK-equivalent cleanup is
// expected to come from the indexer / outbox.
type IncidentLinkDeleter interface {
	DeleteIncident(ctx context.Context, tenant TenantId, id ObjectId) (int, error)
}
