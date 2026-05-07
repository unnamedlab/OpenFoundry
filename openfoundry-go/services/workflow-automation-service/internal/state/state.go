// Package state holds the AppState struct shared by handlers + the
// background consumer tasks. Mirrors the Rust `workflow_automation_service::AppState`.
package state

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// AppState mirrors the Rust struct field-for-field. Only the fields
// the handlers + domain code actually consume are typed here; runtime
// sink scaffolding stays out of scope (same as lineage-service).
type AppState struct {
	DB                          *pgxpool.Pool
	HTTPClient                  *http.Client
	JWTConfig                   *authmw.JWTConfig
	NATSURL                     string
	PipelineServiceURL          string
	OntologyServiceURL          string
	AuditComplianceServiceURL   string
	AuditComplianceBearerToken  string
	ApprovalTTLHours            uint32
}
