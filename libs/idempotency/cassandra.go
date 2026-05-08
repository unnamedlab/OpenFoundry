package idempotency

// cassandra.go ports libs/idempotency/src/cassandra.rs.
//
// Cassandra-backed Store. Uses an LWT INSERT … IF NOT EXISTS at
// LOCAL_SERIAL — the only Cassandra primitive that gives a true
// atomic check-and-record. LWTs cost roughly 4× a regular write
// (Paxos round-trip), so reach for PgStore if Postgres is already
// on the consumer's data path; pick this one when the consumer is
// Cassandra-native (object-database-service, audit-trail,
// action-log, …).
//
// Rows expire via the table's `default_time_to_live = 2592000`
// (30 days). Operators MUST keep that TTL ≥ the source Kafka
// topic's retention or events delivered after the row TTLs out
// will be reprocessed.

import (
	"context"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
)

// CassandraStore is a Cassandra-backed Store.
//
// KsTable is the fully-qualified `keyspace.table` of the dedup
// table; it MUST already exist with the canonical shape declared in
// migrations/0001_processed_events.cql:
//
//	event_id     uuid PRIMARY KEY
//	processed_at timestamp
//	WITH default_time_to_live = 2592000
//
// The slot is operator-controlled (never user input) — Cassandra,
// like Postgres, will not bind table or keyspace names as
// parameters.
type CassandraStore struct {
	session *gocql.Session
	ksTable string
}

// NewCassandraStore wires a CassandraStore to ksTable (e.g.
// `idem.processed_events`). Panics on an empty table name to make
// the operator-set constraint explicit at boot.
func NewCassandraStore(session *gocql.Session, ksTable string) *CassandraStore {
	if session == nil {
		panic("idempotency: session must not be nil")
	}
	if ksTable == "" {
		panic("idempotency: ksTable must not be empty")
	}
	return &CassandraStore{session: session, ksTable: ksTable}
}

// Session returns the underlying gocql session (tests, advanced use).
func (s *CassandraStore) Session() *gocql.Session { return s.session }

// KsTable returns the fully-qualified keyspace.table this store
// writes to.
func (s *CassandraStore) KsTable() string { return s.ksTable }

// cqlInsert is split into a method so the unit test can assert the
// `IF NOT EXISTS` shape without owning a live session — same pattern
// link_store.go uses.
func (s *CassandraStore) cqlInsert() string {
	return fmt.Sprintf(
		"INSERT INTO %s (event_id, processed_at) VALUES (?, ?) IF NOT EXISTS",
		s.ksTable,
	)
}

// CheckAndRecord implements Store via an LWT INSERT … IF NOT
// EXISTS. The first return of gocql.MapScanCAS is the `[applied]`
// boolean, which is true when the row was newly inserted and false
// when an existing row already held this event_id.
//
// processed_at is bound to the consumer's wall-clock so the value
// tracks the consumer that won the race — this lines up with
// PgStore which uses the consumer's session clock via DEFAULT now().
func (s *CassandraStore) CheckAndRecord(
	ctx context.Context,
	eventID uuid.UUID,
) (Outcome, error) {
	now := time.Now().UTC()
	q := s.session.Query(s.cqlInsert(), gocql.UUID(eventID), now).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum).
		// LocalSerial keeps the Paxos round inside the local DC.
		// Global SERIAL is unsafe under multi-DC active-active per
		// ADR-0020 / cassandra-kernel docs.
		SerialConsistency(gocql.LocalSerial)

	// MapScanCAS returns (applied, err). When applied=true the row
	// map is empty (the LWT applied, no prior state). When
	// applied=false the row map carries the existing columns; we
	// don't need them.
	prev := map[string]any{}
	applied, err := q.MapScanCAS(prev)
	if err != nil {
		return OutcomeAlreadyProcessed, &ErrBackend{Cause: err}
	}
	if applied {
		return OutcomeFirstSeen, nil
	}
	return OutcomeAlreadyProcessed, nil
}
