// Package audit ports services/sql-bi-gateway-service/src/audit.rs:
// structured audit events for every SQL statement, plus the FNV-1a
// fingerprint used to log SQL identity without leaking content.
//
// We never log the full SQL payload (it can carry PII / secrets) —
// only the 16-hex-char FNV-1a-64 of the trimmed, case-folded
// statement, which is enough to correlate against client-side logs.
package audit

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Outcome is the executed-or-failed flag attached to every audit row.
type Outcome int

const (
	OutcomeOK Outcome = iota
	OutcomeError
)

func (o Outcome) String() string {
	switch o {
	case OutcomeOK:
		return "ok"
	case OutcomeError:
		return "error"
	default:
		return "unknown"
	}
}

// Backend mirrors routing.Backend without importing the package
// (avoids a cycle).
type Backend string

// SQLEvent is a single SQL execution audit record.
type SQLEvent struct {
	TenantID    string
	TenantTier  string
	UserEmail   string
	Backend     Backend
	Remote      bool
	SQLHash     string
	RowCount    int
	Duration    time.Duration
	Outcome     Outcome
}

// Emit writes the audit event at INFO level. The slog target name
// (`sql_bi_gateway.audit`) matches the Rust `tracing::info!` target.
func (e SQLEvent) Emit() {
	tenantID := e.TenantID
	if tenantID == "" {
		tenantID = "-"
	}
	userEmail := e.UserEmail
	if userEmail == "" {
		userEmail = "-"
	}
	slog.Info("sql_bi_gateway statement executed",
		slog.String("target", "sql_bi_gateway.audit"),
		slog.String("tenant_id", tenantID),
		slog.String("tenant_tier", e.TenantTier),
		slog.String("user_email", userEmail),
		slog.String("backend", string(e.Backend)),
		slog.Bool("remote", e.Remote),
		slog.String("sql_hash", e.SQLHash),
		slog.Int("row_count", e.RowCount),
		slog.Int64("duration_ms", e.Duration.Milliseconds()),
		slog.String("outcome", e.Outcome.String()),
	)
}

// Fingerprint returns a stable, short, non-reversible identifier for
// a SQL string, suitable for log correlation. Uses FNV-1a-64 of the
// trimmed, case-folded statement. Same algorithm + offset basis +
// prime as the Rust impl, byte-for-byte.
func Fingerprint(sql string) string {
	normalized := strings.ToLower(strings.TrimSpace(sql))
	const (
		offsetBasis uint64 = 0xcbf29ce484222325
		prime       uint64 = 0x100000001b3
	)
	hash := offsetBasis
	for _, b := range []byte(normalized) {
		hash ^= uint64(b)
		hash *= prime
	}
	return fmt.Sprintf("%016x", hash)
}
