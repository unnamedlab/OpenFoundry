// Package sessionscassandra is the Cassandra-backed session +
// refresh-token adapter used by identity-federation-service after
// slice 2 of the migration plan.
//
// Mirrors `services/identity-federation-service/src/sessions_cassandra.rs`
// but ports a smaller initial subset:
//
//   - user_session  — sliding TTL 30 min, partition by (user_id, hour_bucket)
//   - refresh_token — TTL 30 d, partition by token_hash_prefix (2-byte)
//
// Tables added in later slices (matching the slice plan in the inventory):
//
//   - oauth_state                — slice 5 (OAuth/OIDC)
//   - scoped_session_by_user/_by_id — slice 7 (scoped sessions UI)
//   - refresh_token_by_id        — slice 7
//   - repository_session_by_id   — when storage-abstraction lands
//
// Wiring note: the slice-1 Issuer still keeps refresh tokens in
// Postgres. Flipping the active backend to this Cassandra adapter is a
// one-line swap inside cmd/.../main.go and will be done in a follow-up
// once a Cassandra/Scylla instance is wired into the dev environment
// (the slice-1 dev workflow with only Postgres keeps working until then).
package sessionscassandra

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"

	cassandrakernel "github.com/openfoundry/openfoundry-go/libs/cassandra-kernel"
)

// Keyspace is the Cassandra keyspace identity-federation owns. Pinned
// here so a typo at wiring time is a compile error.
const Keyspace = "auth_runtime"

// TTL constants — match the Rust crate verbatim. Renaming here without
// updating the DDL re-creates rows with a different TTL.
const (
	UserSessionTTL  = 1800              // 30 min sliding
	RefreshTokenTTL = 30 * 24 * 3600    // 30 d absolute
)

// Migrations is the DDL ledger applied at startup. Mirrors the Rust
// MIGRATIONS array (sessions subset). Idempotent (CREATE TABLE IF NOT
// EXISTS) so re-running on a populated keyspace is safe.
var Migrations = []cassandrakernel.Migration{
	{
		Name: "0001_user_session",
		DDL: `CREATE TABLE IF NOT EXISTS auth_runtime.user_session (
		    user_id        text,
		    hour_bucket    timestamp,
		    session_id     uuid,
		    issued_at      timestamp,
		    last_seen_at   timestamp,
		    user_agent     text,
		    ip_address     inet,
		    mfa_level      text,
		    PRIMARY KEY ((user_id, hour_bucket), session_id)
		) WITH CLUSTERING ORDER BY (session_id ASC)
		  AND default_time_to_live = 1800
		  AND compaction = {'class': 'TimeWindowCompactionStrategy',
		                    'compaction_window_unit': 'MINUTES',
		                    'compaction_window_size': '30'}`,
	},
	{
		Name: "0002_refresh_token",
		DDL: `CREATE TABLE IF NOT EXISTS auth_runtime.refresh_token (
		    token_hash_prefix text,
		    token_hash        blob,
		    family_id         uuid,
		    user_id           text,
		    issued_at         timestamp,
		    expires_at        timestamp,
		    revoked_at        timestamp,
		    rotated_to        blob,
		    PRIMARY KEY ((token_hash_prefix), token_hash)
		) WITH default_time_to_live = 2592000
		  AND compaction = {'class': 'TimeWindowCompactionStrategy',
		                    'compaction_window_unit': 'DAYS',
		                    'compaction_window_size': '7'}`,
	},
}

// Adapter wraps the gocql session with the typed surface the Issuer +
// session handlers consume.
type Adapter struct {
	Session *gocql.Session
}

// New returns an Adapter with the given gocql session.
func New(session *gocql.Session) *Adapter { return &Adapter{Session: session} }

// Migrate applies the embedded Migrations ledger. Idempotent.
func (a *Adapter) Migrate(_ context.Context) error {
	return cassandrakernel.Apply(a.Session, Keyspace, Migrations)
}

// ─── Sessions ───────────────────────────────────────────────────────────

// SessionRecord is a single row in user_session.
type SessionRecord struct {
	UserID     string
	SessionID  uuid.UUID
	IssuedAt   time.Time
	LastSeenAt time.Time
	UserAgent  string
	IPAddress  string // empty when not known
	MFALevel   string
}

// RecordSession inserts a new session row with the sliding 30-min TTL.
//
// Partition key is `(user_id, hour_bucket)`; bounded partition size
// regardless of how many sessions a user opens.
func (a *Adapter) RecordSession(ctx context.Context, r SessionRecord) error {
	hourBucket := r.IssuedAt.Truncate(time.Hour)
	q := a.Session.Query(
		`INSERT INTO auth_runtime.user_session
		 (user_id, hour_bucket, session_id, issued_at, last_seen_at, user_agent, ip_address, mfa_level)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?) USING TTL ?`,
		r.UserID, hourBucket, r.SessionID, r.IssuedAt, r.LastSeenAt,
		r.UserAgent, r.IPAddress, r.MFALevel, UserSessionTTL,
	).WithContext(ctx)
	return q.Exec()
}

// TouchSession refreshes last_seen_at + extends the TTL another 30
// min. The Rust crate calls this on every authenticated request.
func (a *Adapter) TouchSession(ctx context.Context, userID string, sessionID uuid.UUID, at time.Time) error {
	hourBucket := at.Truncate(time.Hour)
	q := a.Session.Query(
		`UPDATE auth_runtime.user_session USING TTL ?
		 SET last_seen_at = ?
		 WHERE user_id = ? AND hour_bucket = ? AND session_id = ?`,
		UserSessionTTL, at, userID, hourBucket, sessionID,
	).WithContext(ctx)
	return q.Exec()
}

// ─── Refresh tokens ─────────────────────────────────────────────────────

// RefreshTokenRecord is a single row in refresh_token.
type RefreshTokenRecord struct {
	TokenHash []byte // raw SHA-256 (32 bytes)
	FamilyID  uuid.UUID
	UserID    string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// RecordRefreshToken inserts a refresh token. The 2-byte hex prefix of
// the hash is the partition key — gives 256 partitions of roughly
// equal size.
func (a *Adapter) RecordRefreshToken(ctx context.Context, r RefreshTokenRecord) error {
	prefix := hashPrefix(r.TokenHash)
	q := a.Session.Query(
		`INSERT INTO auth_runtime.refresh_token
		 (token_hash_prefix, token_hash, family_id, user_id, issued_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?) USING TTL ?`,
		prefix, r.TokenHash, r.FamilyID, r.UserID, r.IssuedAt, r.ExpiresAt, RefreshTokenTTL,
	).WithContext(ctx)
	return q.Exec()
}

// LookupRefreshToken returns the row for a hash, or (nil, nil) when absent.
func (a *Adapter) LookupRefreshToken(ctx context.Context, tokenHash []byte) (*RefreshTokenRecord, *time.Time, error) {
	prefix := hashPrefix(tokenHash)
	var rec RefreshTokenRecord
	var revoked *time.Time
	q := a.Session.Query(
		`SELECT family_id, user_id, issued_at, expires_at, revoked_at
		 FROM auth_runtime.refresh_token
		 WHERE token_hash_prefix = ? AND token_hash = ?`,
		prefix, tokenHash,
	).WithContext(ctx)
	rec.TokenHash = tokenHash
	if err := q.Scan(&rec.FamilyID, &rec.UserID, &rec.IssuedAt, &rec.ExpiresAt, &revoked); err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("lookup refresh token: %w", err)
	}
	return &rec, revoked, nil
}

// MarkRefreshUsed marks a single token as revoked (one-time use).
func (a *Adapter) MarkRefreshUsed(ctx context.Context, tokenHash []byte, at time.Time) error {
	prefix := hashPrefix(tokenHash)
	q := a.Session.Query(
		`UPDATE auth_runtime.refresh_token
		 SET revoked_at = ?
		 WHERE token_hash_prefix = ? AND token_hash = ?`,
		at, prefix, tokenHash,
	).WithContext(ctx)
	return q.Exec()
}

// HashRefreshToken returns the SHA-256 of `plaintext`. Mirrors the
// service-layer HashRefreshToken (kept here too so the Cassandra
// adapter is self-contained for tests).
func HashRefreshToken(plaintext string) []byte {
	sum := sha256.Sum256([]byte(plaintext))
	return sum[:]
}

// hashPrefix returns the 4-char hex of the first 2 bytes of the hash.
func hashPrefix(h []byte) string {
	if len(h) < 2 {
		return ""
	}
	return hex.EncodeToString(h[:2])
}
