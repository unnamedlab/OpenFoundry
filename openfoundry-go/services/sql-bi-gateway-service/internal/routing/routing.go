// Package routing ports services/sql-bi-gateway-service/src/routing.rs.
//
// Every SQL statement that arrives over Flight SQL is classified into
// one of the [Backend] variants defined in ADR-0014. Classification is
// purely syntactic (cheap, deterministic, no external metadata fetch)
// and is based on the catalog prefix of the first table reference in
// the statement:
//
//	SELECT * FROM iceberg.sales.orders   -> BackendIceberg
//	SELECT * FROM vespa.documents        -> BackendVespa
//	SELECT * FROM postgres.public.users  -> BackendPostgres
//	SELECT * FROM trino.of_lineage.runs  -> BackendTrino
//	SELECT 1                             -> BackendIceberg (default / DataFusion local)
//
// The heuristic is intentionally conservative: anything that does not
// start with a recognised catalog prefix falls back to the local
// DataFusion SessionContext, which is the path used by SELECT 1-style
// probes that Tableau / Superset send during connection bring-up.
package routing

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/config"
)

// Backend is the logical backend that owns the data referenced by a
// SQL statement. Mirrors the Rust enum 1:1.
type Backend string

const (
	// BackendIceberg is the lakehouse backend served by the local
	// DataFusion session or by sql-warehousing-service over Flight
	// SQL when configured.
	BackendIceberg Backend = "iceberg"
	// BackendVespa is search / hybrid retrieval.
	BackendVespa Backend = "vespa"
	// BackendPostgres is OLTP reference data (PostgreSQL via CloudNativePG).
	BackendPostgres Backend = "postgres"
	// BackendTrino is the Iceberg analytical engine (ADR-0029, S5.6).
	BackendTrino Backend = "trino"
)

// AllBackends returns every backend in the order they are advertised
// to BI clients. Mirrors `Backend::all()`.
func AllBackends() []Backend {
	return []Backend{BackendIceberg, BackendVespa, BackendPostgres, BackendTrino}
}

// Decision is what BackendRouter.Route returns: which backend owns the
// statement and, when the backend is fronted by a remote Flight SQL
// endpoint, the URL to delegate to. Empty RemoteURL ⇒ run against the
// local DataFusion SessionContext.
type Decision struct {
	Backend   Backend
	RemoteURL string
}

// ErrBackendUnavailable is returned when a statement targets a backend
// that has not been configured. Mirrors `RoutingError::BackendUnavailable`.
type ErrBackendUnavailable struct {
	Backend Backend
}

func (e *ErrBackendUnavailable) Error() string {
	return fmt.Sprintf(
		"backend %q is not configured on this gateway; set the corresponding `*_FLIGHT_SQL_URL` environment variable (see services/sql-bi-gateway-service/k8s/README.md)",
		e.Backend,
	)
}

// IsBackendUnavailable reports whether err is a typed
// ErrBackendUnavailable, similar to errors.Is on a sentinel.
func IsBackendUnavailable(err error) bool {
	var typed *ErrBackendUnavailable
	return errors.As(err, &typed)
}

// BackendRouter decides which Backend to use for a given SQL statement
// based on the configured endpoints in [config.Config].
type BackendRouter struct {
	WarehousingFlightSQLURL string
	VespaFlightSQLURL       string
	PostgresFlightSQLURL    string
	TrinoFlightSQLURL       string
}

// FromConfig builds a router from cfg. Mirrors `BackendRouter::from_config`.
func FromConfig(cfg *config.Config) *BackendRouter {
	return &BackendRouter{
		WarehousingFlightSQLURL: normalize(cfg.WarehousingFlightSQLURL),
		VespaFlightSQLURL:       normalize(cfg.VespaFlightSQLURL),
		PostgresFlightSQLURL:    normalize(cfg.PostgresFlightSQLURL),
		TrinoFlightSQLURL:       normalize(cfg.TrinoFlightSQLURL),
	}
}

// Route classifies sql and returns the routing decision. Returns
// ErrBackendUnavailable when the chosen backend is one of the
// federated ones but no endpoint was configured.
func (r *BackendRouter) Route(sql string) (Decision, error) {
	backend := Classify(sql)
	switch backend {
	case BackendIceberg:
		// Optional warehousing endpoint; empty ⇒ local DataFusion.
		return Decision{Backend: BackendIceberg, RemoteURL: r.WarehousingFlightSQLURL}, nil
	case BackendVespa:
		if r.VespaFlightSQLURL == "" {
			return Decision{}, &ErrBackendUnavailable{Backend: BackendVespa}
		}
		return Decision{Backend: BackendVespa, RemoteURL: r.VespaFlightSQLURL}, nil
	case BackendPostgres:
		if r.PostgresFlightSQLURL == "" {
			return Decision{}, &ErrBackendUnavailable{Backend: BackendPostgres}
		}
		return Decision{Backend: BackendPostgres, RemoteURL: r.PostgresFlightSQLURL}, nil
	case BackendTrino:
		if r.TrinoFlightSQLURL == "" {
			return Decision{}, &ErrBackendUnavailable{Backend: BackendTrino}
		}
		return Decision{Backend: BackendTrino, RemoteURL: r.TrinoFlightSQLURL}, nil
	default:
		return Decision{}, fmt.Errorf("routing: unknown backend %q", backend)
	}
}

// Classify inspects the first identifier that follows the first FROM
// (or INTO/UPDATE/JOIN) keyword. Anything without a recognised catalog
// prefix is treated as BackendIceberg, which routes to the local
// DataFusion session by default — exactly what `SELECT 1` and similar
// BI-client probes need. Mirrors the Rust `classify` function 1:1.
func Classify(sql string) Backend {
	lowered := strings.ToLower(sql)
	tokens := splitTokens(lowered)
	nextIsTable := false
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		if nextIsTable {
			catalog := tok
			if i := strings.IndexByte(tok, '.'); i >= 0 {
				catalog = tok[:i]
			}
			switch catalog {
			case "vespa":
				return BackendVespa
			case "postgres", "postgresql":
				return BackendPostgres
			case "trino":
				return BackendTrino
			default:
				return BackendIceberg
			}
		}
		switch tok {
		case "from", "into", "update", "join":
			nextIsTable = true
		default:
			nextIsTable = false
		}
	}
	return BackendIceberg
}

// splitTokens mirrors Rust's split on
// `|c| !c.is_ascii_alphanumeric() && c != '_' && c != '.'`.
func splitTokens(s string) []string {
	out := make([]string, 0, 16)
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isTokenByte(c) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			out = append(out, s[start:i])
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

func isTokenByte(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '.'
}

func normalize(url string) string {
	t := strings.TrimSpace(url)
	if t == "" {
		return ""
	}
	return t
}
