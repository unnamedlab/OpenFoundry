// Writeback substrate for S1.4 (`ontology-actions-service`).
//
// Implements the canonical write pattern described in the migration
// plan §S1.4.c:
//
//  1. Compute a deterministic event_id so retries collapse to the
//     same row across both stores.
//  2. Issue the primary write to Cassandra with optimistic
//     concurrency. If Cassandra fails ⇒ the caller has nothing to
//     rollback (no PG transaction was opened); the helper returns an
//     error.
//  3. Open a PG transaction against pg-policy.
//  4. Append the domain event with [outbox.Enqueue].
//  5. COMMIT. If the commit fails *after* Cassandra succeeded, the
//     helper surfaces a [WritebackError] with `CommitAfterPrimary` set
//     that carries event_id and committed_version, so the caller can
//     retry the *same* call with the *same* input.
//
// Mirrors `libs/ontology-kernel/src/domain/writeback.rs`.

package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/outbox"
	storageabstraction "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// OntologyNamespace mirrors `pub const ONTOLOGY_NAMESPACE`. The
// literal value is the UUID-v5 derivation of
// `https://openfoundry.dev/ns/ontology/writeback` against
// `Uuid::NAMESPACE_URL`. Pinned here so the event_id derivation never
// depends on runtime URL parsing.
var OntologyNamespace = uuid.UUID{
	0x4a, 0x52, 0x0f, 0x9a, 0x6c, 0x9d, 0x5b, 0x18,
	0x9d, 0x4c, 0x88, 0x6f, 0x60, 0x9b, 0x16, 0x05,
}

// WritebackBudget mirrors `pub const WRITEBACK_BUDGET`. The helper
// itself does not enforce it — callers wrap the call in a
// `context.WithTimeout(WRITEBACK_BUDGET, …)` when they need a hard
// SLO. Pinned at 2 s so it composes with the 5 ms / 20 ms / 50 ms
// SLO targets in S1.8 without ever becoming the bottleneck.
const WritebackBudget = 2 * time.Second

// WritebackErrorKind tags the [WritebackError] variants. Mirrors
// `enum WritebackError` in the Rust source.
type WritebackErrorKind int

const (
	// WritebackPrimary — Cassandra rejected the write before a PG
	// transaction was opened.
	WritebackPrimary WritebackErrorKind = iota
	// WritebackCommitAfterPrimary — the PG transaction (or commit)
	// failed *after* Cassandra succeeded.
	WritebackCommitAfterPrimary
	// WritebackOpenTxAfterPrimary — opening the PG transaction
	// failed after Cassandra succeeded.
	WritebackOpenTxAfterPrimary
	// WritebackVersionConflict — Cassandra returned a real version
	// conflict (the stored version is not the version we tried).
	WritebackVersionConflict
)

// WritebackError mirrors `enum WritebackError`. Carries the retry
// metadata Rust attaches to `CommitAfterPrimary` /
// `OpenTxAfterPrimary` so callers can build a deterministic retry.
type WritebackError struct {
	Kind             WritebackErrorKind
	Source           error
	EventID          uuid.UUID
	CommittedVersion uint64
	ExpectedVersion  uint64
	ActualVersion    uint64
}

// Error mirrors the Display impl. Each variant emits the same
// human-readable string the Rust `thiserror` macro generates so log
// output stays byte-comparable.
func (e *WritebackError) Error() string {
	switch e.Kind {
	case WritebackPrimary:
		return fmt.Sprintf("primary store rejected the write: %s", e.Source)
	case WritebackCommitAfterPrimary:
		return fmt.Sprintf("commit after primary write failed (event_id=%s, version=%d): %s",
			e.EventID, e.CommittedVersion, e.Source)
	case WritebackOpenTxAfterPrimary:
		return fmt.Sprintf("could not open pg-policy tx after primary write (event_id=%s, version=%d): %s",
			e.EventID, e.CommittedVersion, e.Source)
	case WritebackVersionConflict:
		return fmt.Sprintf("version conflict: expected %d, found %d",
			e.ExpectedVersion, e.ActualVersion)
	}
	return "writeback error"
}

// Unwrap exposes the underlying source so errors.Is / errors.As
// reach into it for the non-VersionConflict variants.
func (e *WritebackError) Unwrap() error { return e.Source }

var _ error = (*WritebackError)(nil)

// WritebackOutcome mirrors `struct WritebackOutcome`.
type WritebackOutcome struct {
	EventID          uuid.UUID
	CommittedVersion uint64
	Created          bool
	IdempotentRetry  bool
}

// DeriveEventID mirrors `pub fn derive_event_id`. Format:
// `{tenant}/{aggregate}/{aggregate_id}@{version}`, hashed under
// [OntologyNamespace] as UUID-v5.
func DeriveEventID(tenant, aggregate, aggregateID string, version uint64) uuid.UUID {
	name := fmt.Sprintf("%s/%s/%s@%d", tenant, aggregate, aggregateID, version)
	return uuid.NewSHA1(OntologyNamespace, []byte(name))
}

// ApplyObjectWithOutbox mirrors `pub async fn apply_object_with_outbox`.
//
// Steps:
//
//  1. Compute target_version = expected_version+1 (or 1 when nil).
//  2. Issue the primary Put against the ObjectStore.
//  3. Map the PutOutcome:
//     - Inserted        → committed=1, created=true.
//     - Updated         → committed=NewVersion, created=false.
//     - VersionConflict whose ActualVersion == target_version
//     (idempotent retry) → committed=ActualVersion,
//     created=(expected_version is nil), idempotentRetry=true.
//     - Any other VersionConflict → return WritebackVersionConflict.
//  4. Open a PG transaction; enqueue the outbox event; commit.
//  5. Failures after step 2 surface a CommitAfterPrimary /
//     OpenTxAfterPrimary error with the deterministic event_id and
//     committed_version attached so callers can build idempotent
//     retries.
func ApplyObjectWithOutbox(
	ctx context.Context,
	pg *pgxpool.Pool,
	objects storageabstraction.ObjectStore,
	object storageabstraction.Object,
	expectedVersion *uint64,
	aggregate, topic string,
	payload json.RawMessage,
) (WritebackOutcome, error) {
	targetVersion := uint64(1)
	if expectedVersion != nil {
		targetVersion = *expectedVersion + 1
	}
	eventID := DeriveEventID(string(object.Tenant), aggregate, string(object.ID), targetVersion)

	// 1. Primary write.
	outcome, err := objects.Put(ctx, object, expectedVersion)
	if err != nil {
		return WritebackOutcome{}, &WritebackError{Kind: WritebackPrimary, Source: err}
	}
	var committedVersion uint64
	var created, idempotentRetry bool
	switch outcome.Kind {
	case storageabstraction.PutInserted:
		committedVersion = 1
		created = true
	case storageabstraction.PutUpdated:
		committedVersion = outcome.NewVersion
	case storageabstraction.PutVersionConflict:
		if pg != nil && outcome.ActualVersion == targetVersion {
			// Cassandra already accepted an identical prior attempt.
			// Treat as success and let the outbox enqueue collapse via
			// its own ON CONFLICT DO NOTHING. Without PG there is no
			// outbox idempotency row to prove, so dev/test nil-PG callers
			// retain the plain optimistic-lock conflict semantics.
			committedVersion = outcome.ActualVersion
			created = expectedVersion == nil
			idempotentRetry = true
		} else {
			return WritebackOutcome{}, &WritebackError{
				Kind:            WritebackVersionConflict,
				ExpectedVersion: outcome.ExpectedVersion,
				ActualVersion:   outcome.ActualVersion,
			}
		}
	}

	// 2. Outbox enqueue inside a pg-policy transaction.
	// Explicit local/test AppStates can run without PG. Keep the primary
	// ObjectStore write semantics identical but skip the outbox transaction
	// only when the caller deliberately supplied a nil pool; production
	// ontology-actions-service startup requires DATABASE_URL before these
	// handlers are served.
	if pg == nil {
		return WritebackOutcome{
			EventID:          eventID,
			CommittedVersion: committedVersion,
			Created:          created,
			IdempotentRetry:  idempotentRetry,
		}, nil
	}

	event := outbox.New(eventID, aggregate, string(object.ID), topic, payload)

	tx, err := pg.Begin(ctx)
	if err != nil {
		return WritebackOutcome{}, &WritebackError{
			Kind:             WritebackOpenTxAfterPrimary,
			Source:           err,
			EventID:          eventID,
			CommittedVersion: committedVersion,
		}
	}
	if err := outbox.Enqueue(ctx, tx, event); err != nil {
		// Surface the failure with full retry context. The helper
		// does not roll back the Cassandra write — the caller retries
		// the entire call with the same input (idempotent in both
		// stores). The transaction implicitly rolls back when tx is
		// closed without commit.
		_ = tx.Rollback(ctx)
		return WritebackOutcome{}, &WritebackError{
			Kind:             WritebackCommitAfterPrimary,
			Source:           err,
			EventID:          eventID,
			CommittedVersion: committedVersion,
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return WritebackOutcome{}, &WritebackError{
			Kind:             WritebackCommitAfterPrimary,
			Source:           err,
			EventID:          eventID,
			CommittedVersion: committedVersion,
		}
	}

	return WritebackOutcome{
		EventID:          eventID,
		CommittedVersion: committedVersion,
		Created:          created,
		IdempotentRetry:  idempotentRetry,
	}, nil
}

// IsVersionConflict is a convenience errors.Is helper: returns true
// when the error chain contains a WritebackError of kind
// VersionConflict. Mirrors `match err { WritebackError::VersionConflict {..} => ... }`.
func IsVersionConflict(err error) bool {
	var w *WritebackError
	if !errors.As(err, &w) {
		return false
	}
	return w.Kind == WritebackVersionConflict
}
