// Package storage holds the ObjectStore / LinkStore contract +
// in-memory test fakes that mirror libs/storage-abstraction in Rust.
//
// The Cassandra-backed implementation lives in a follow-up slice
// (libs/cassandra-kernel-go); the foundation port keeps the contract
// + InMemory fakes only, which already serves real reads/writes
// when CASSANDRA_CONTACT_POINTS is unset (matches Rust fallback).
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
