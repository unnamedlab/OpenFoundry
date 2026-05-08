package cassandrakernel

import (
	"encoding/base64"
	"fmt"

	"github.com/gocql/gocql"
	"github.com/google/uuid"

	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// Shared helpers used by every Cassandra-backed Store implementation
// in this package. Mirrors the prelude of
// libs/cassandra-kernel/src/repos.rs.

// driverErr wraps a gocql/driver-level error in a RepoBackend
// RepoError. Mirrors fn driver_err.
func driverErr(err error) error {
	if err == nil {
		return nil
	}
	return repos.Backendf("%v", err)
}

// invalidArg builds a RepoInvalidArgument with the given message.
// Mirrors fn invalid.
func invalidArg(msg string) error { return repos.Invalid(msg) }

// invalidArgf is the formatted variant.
func invalidArgf(format string, args ...any) error {
	return repos.Invalidf(format, args...)
}

// cqlConsistency translates the abstract ReadConsistency into a CQL
// consistency level. Strong → LOCAL_QUORUM; everything else →
// LOCAL_ONE. Mirrors fn cql_consistency (per ADR-0020 §"opt-in
// strong reads").
func cqlConsistency(c repos.ReadConsistency) gocql.Consistency {
	switch c.Level {
	case repos.ConsistencyStrong:
		return gocql.LocalQuorum
	default:
		return gocql.LocalOne
	}
}

// encodePagingState wraps the Cassandra paging-state bytes into the
// opaque base64 token that callers see. Returns nil when there is no
// next page.
func encodePagingState(b []byte) *string {
	if len(b) == 0 {
		return nil
	}
	s := base64.StdEncoding.EncodeToString(b)
	return &s
}

// decodePagingState reverses encodePagingState. Returns
// (nil, nil) for an absent token. Mirrors fn decode_paging_state.
func decodePagingState(token *string) ([]byte, error) {
	if token == nil {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(*token)
	if err != nil {
		return nil, repos.Invalidf("malformed page token: %v", err)
	}
	return raw, nil
}

// parseUUID parses a UUID from a string field, surfacing a typed
// RepoInvalidArgument with the field name on failure. Mirrors fn
// parse_uuid.
func parseUUID(field, raw string) (gocql.UUID, error) {
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return gocql.UUID{}, repos.Invalidf("%s is not a valid UUID: %v", field, err)
	}
	return gocql.UUID(parsed), nil
}

// parseUUIDOpt is the optional-field variant. Returns (nil, nil)
// when the input is nil; otherwise either (UUID, nil) or
// (zero, err).
func parseUUIDOpt(field string, raw *string) (*gocql.UUID, error) {
	if raw == nil {
		return nil, nil
	}
	v, err := parseUUID(field, *raw)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// uuidString renders a gocql.UUID as the canonical 36-char form.
func uuidString(u gocql.UUID) string { return u.String() }

// tenantStr extracts the underlying string from a TenantId.
func tenantStr(t repos.TenantId) string { return string(t) }

// organizationIDFromTenant tries to parse the tenant id as a UUID
// and returns the canonical lower-case string when it succeeds.
// Mirrors fn organization_id_from_tenant — used when an index row
// did not project the organization_id and we want to surface a
// best-effort default to consumers.
func organizationIDFromTenant(t repos.TenantId) *string {
	parsed, err := uuid.Parse(string(t))
	if err != nil {
		return nil
	}
	s := parsed.String()
	return &s
}

// clampPageSize bounds the requested page size to [1, 5000].
// Mirrors fn clamp_page_size.
func clampPageSize(n uint32) int {
	if n == 0 {
		return 1
	}
	if n > 5000 {
		return 5000
	}
	return int(n)
}

// truncateSummary trims the canonical-JSON snapshot to 1024
// characters when it overflows. Mirrors fn truncate_summary.
func truncateSummary(s string) string {
	if len(s) <= 1024 {
		return s
	}
	return s[:1024]
}

// fmtErr returns a formatted error using the standard fmt package
// rules. Used in places where the Rust code calls format! → driver_err.
func fmtErr(format string, args ...any) error {
	return repos.Backend(fmt.Sprintf(format, args...))
}
