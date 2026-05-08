package cassandrakernel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"

	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// SessionStore (P2.5.5) is the Cassandra-backed implementation of
// repos.SessionStore mirroring libs/cassandra-kernel/src/repos.rs::
// CassandraSessionStore. Single table `auth_runtime.sessions_by_id`
// keyed by (tenant, session_id) with a server-side TTL set to
// `expires_at_ms - now_ms` so expired rows tombstone automatically.
//
// Identity-specific refresh-token, OAuth-state and revocation tables
// remain owned by the identity/session services in the same
// `auth_runtime` keyspace; SessionStore is a point-lookup repository
// surface for the storage-abstraction trait.
type SessionStore struct {
	session  *gocql.Session
	keyspace string
}

// NewSessionStore builds a store bound to the standard
// `auth_runtime` keyspace.
func NewSessionStore(session *gocql.Session) *SessionStore {
	return &SessionStore{session: session, keyspace: "auth_runtime"}
}

// NewSessionStoreWithKeyspace allows a custom keyspace.
func NewSessionStoreWithKeyspace(session *gocql.Session, keyspace string) *SessionStore {
	return &SessionStore{session: session, keyspace: keyspace}
}

// Compile-time interface assertion.
var _ repos.SessionStore = (*SessionStore)(nil)

func (s *SessionStore) cqlInsertSession() string {
	return fmt.Sprintf(
		`INSERT INTO %s.sessions_by_id
            (tenant, session_id, subject, attributes, issued_at, expires_at)
         VALUES (?, ?, ?, ?, ?, ?) USING TTL ?`, s.keyspace)
}

func (s *SessionStore) cqlSelectSession() string {
	return fmt.Sprintf(
		`SELECT subject, attributes, issued_at, expires_at
           FROM %s.sessions_by_id WHERE tenant = ? AND session_id = ?`,
		s.keyspace)
}

func (s *SessionStore) cqlDeleteSession() string {
	return fmt.Sprintf(
		`DELETE FROM %s.sessions_by_id WHERE tenant = ? AND session_id = ?`,
		s.keyspace)
}

// ttlSecondsUntil computes the TTL we set on the INSERT. Returns
// (0, false) when the session is already expired (caller should
// short-circuit to a delete). Mirrors fn ttl_seconds_until: rounds
// up to the next whole second, clamps to MaxInt32.
func ttlSecondsUntil(expiresAtMs, nowMs int64) (int32, bool) {
	ttlMs := expiresAtMs - nowMs
	if ttlMs <= 0 {
		return 0, false
	}
	ttlSecs := (ttlMs + 999) / 1000
	if ttlSecs > maxInt32 {
		return maxInt32, true
	}
	return int32(ttlSecs), true
}

// Get fetches by session id. Returns (nil, nil) for a genuine miss
// or for an expired row (the storage TTL handles passive cleanup,
// but a freshly-just-expired row is filtered here too).
func (s *SessionStore) Get(
	ctx context.Context,
	tenant repos.TenantId,
	id string,
	consistency repos.ReadConsistency,
) (*repos.Session, error) {
	q := s.session.Query(s.cqlSelectSession(), tenantStr(tenant), id).
		WithContext(ctx).
		Consistency(cqlConsistency(consistency))

	var (
		subject    string
		attributes map[string]string
		issuedAt   time.Time
		expiresAt  time.Time
	)
	if err := q.Scan(&subject, &attributes, &issuedAt, &expiresAt); err != nil {
		if err == gocql.ErrNotFound {
			return nil, nil
		}
		return nil, driverErr(err)
	}
	expiresAtMs := expiresAt.UnixMilli()
	if expiresAtMs <= time.Now().UnixMilli() {
		return nil, nil
	}
	if attributes == nil {
		attributes = map[string]string{}
	}
	return &repos.Session{
		Tenant:      tenant,
		ID:          id,
		Subject:     subject,
		Attributes:  attributes,
		IssuedAtMs:  issuedAt.UnixMilli(),
		ExpiresAtMs: expiresAtMs,
	}, nil
}

// Put persists a session with a TTL of `expires_at_ms - now`.
// Already-expired sessions short-circuit to a DELETE so the row is
// actively tombstoned rather than left lingering.
func (s *SessionStore) Put(ctx context.Context, session repos.Session) error {
	if strings.TrimSpace(session.ID) == "" {
		return invalidArg("session id must not be empty")
	}
	ttl, ok := ttlSecondsUntil(session.ExpiresAtMs, time.Now().UnixMilli())
	if !ok {
		return s.session.Query(s.cqlDeleteSession(),
			tenantStr(session.Tenant), session.ID).
			WithContext(ctx).
			Consistency(gocql.LocalQuorum).Exec()
	}

	attrs := session.Attributes
	if attrs == nil {
		attrs = map[string]string{}
	}
	q := s.session.Query(s.cqlInsertSession(),
		tenantStr(session.Tenant), session.ID, session.Subject,
		attrs,
		time.UnixMilli(session.IssuedAtMs).UTC(),
		time.UnixMilli(session.ExpiresAtMs).UTC(),
		ttl).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum)
	if err := q.Exec(); err != nil {
		return driverErr(err)
	}
	return nil
}

// Revoke removes a session immediately. Returns true when the row
// existed at probe-time. Same probe-then-delete pattern as
// LinkStore.Delete (Cassandra non-LWT DELETE doesn't surface
// rows_affected).
func (s *SessionStore) Revoke(
	ctx context.Context,
	tenant repos.TenantId,
	id string,
) (bool, error) {
	probe := s.session.Query(s.cqlSelectSession(), tenantStr(tenant), id).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum)
	var (
		subject    string
		attributes map[string]string
		issuedAt   time.Time
		expiresAt  time.Time
	)
	existed := true
	if err := probe.Scan(&subject, &attributes, &issuedAt, &expiresAt); err != nil {
		if err == gocql.ErrNotFound {
			existed = false
		} else {
			return false, driverErr(err)
		}
	}

	if err := s.session.Query(s.cqlDeleteSession(), tenantStr(tenant), id).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum).Exec(); err != nil {
		return false, driverErr(err)
	}
	return existed, nil
}
