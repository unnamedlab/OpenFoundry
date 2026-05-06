package ontologykernel

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/stores"
)

// AppState mirrors `pub struct AppState` in
// `libs/ontology-kernel/src/lib.rs`. Each ontology-* binary builds an
// AppState from environment configuration and threads it through the
// HTTP router; handlers consume it as a function argument.
//
// Field semantics are 1:1 with the Rust source; field names use Go
// idiomatic casing but the role of each is identical.
type AppState struct {
	// DB is the PostgreSQL pool retained for control-plane schema
	// lookups, outbox, and residual warm handlers that have not
	// migrated off direct PG access yet. The object/link/action hot
	// path routes through Stores.
	//
	// Maps to Rust `pub db: PgPool`.
	DB *pgxpool.Pool

	// Stores is the trait bag of repository implementations. Handlers
	// migrated as part of S1.4–S1.7 of the Cassandra-Foundry parity
	// plan route their I/O through this field; legacy handlers still
	// use DB directly. Both fields coexist while the migration is in
	// flight.
	//
	// Maps to Rust `pub stores: stores::Stores`.
	Stores stores.Stores

	HTTPClient *http.Client
	JWTConfig  *authmw.JWTConfig

	AuditServiceURL         string
	DatasetServiceURL       string
	OntologyServiceURL      string
	PipelineServiceURL      string
	AIServiceURL            string
	NotificationServiceURL  string
	SearchEmbeddingProvider string
	NodeRuntimeCommand      string

	// ConnectorManagementServiceURL is the base URL of
	// `connector-management-service`. Used by TASK G to invoke
	// registered webhooks (writeback + side effects). When empty,
	// the kernel logs a warning and skips the call.
	//
	// Maps to Rust `pub connector_management_service_url: String`.
	ConnectorManagementServiceURL string
}
