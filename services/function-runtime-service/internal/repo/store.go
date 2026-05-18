// Package repo defines the persistence contract for
// function-runtime-service and ships both a pgx-backed (Postgres) and
// an in-memory implementation. Handlers only ever depend on the Store
// interface so the HTTP layer is testable without a live database.
package repo

import (
	"context"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
)

// ListFunctionsFilter narrows the result of Store.ListFunctions.
// Empty fields are ignored.
type ListFunctionsFilter struct {
	TenantID  uuid.UUID
	Namespace string
	Status    models.Status
	Runtime   models.Runtime
	Limit     int
}

// ListRunsFilter narrows the result of Store.ListRuns. Empty fields
// are ignored.
type ListRunsFilter struct {
	TenantID   uuid.UUID
	FunctionID uuid.UUID
	Status     models.RunStatus
	Limit      int
}

// RunUpdate carries the mutable fields a Store.FinishRun call applies.
type RunUpdate struct {
	Status     models.RunStatus
	Output     []byte
	Error      string
	DurationMs int64
}

// Store is the persistence contract. Implementations must be
// goroutine-safe — handlers may serve concurrent requests.
type Store interface {
	// Functions
	CreateFunction(ctx context.Context, fn *models.FunctionDefinition) error
	GetFunction(ctx context.Context, tenantID, id uuid.UUID) (*models.FunctionDefinition, error)
	ListFunctions(ctx context.Context, f ListFunctionsFilter) ([]models.FunctionDefinition, error)
	UpdateFunctionStatus(ctx context.Context, tenantID, id uuid.UUID, status models.Status, activeVersion *int) (*models.FunctionDefinition, error)

	// Versions
	AppendVersion(ctx context.Context, tenantID, fnID uuid.UUID, sourceURI, entryPoint string) (*models.FunctionVersion, error)
	GetVersion(ctx context.Context, fnID uuid.UUID, version int) (*models.FunctionVersion, error)
	ListVersions(ctx context.Context, fnID uuid.UUID) ([]models.FunctionVersion, error)

	// Runs
	CreateRun(ctx context.Context, run *models.FunctionRun) error
	GetRun(ctx context.Context, id uuid.UUID) (*models.FunctionRun, error)
	ListRuns(ctx context.Context, f ListRunsFilter) ([]models.FunctionRun, error)
	FinishRun(ctx context.Context, id uuid.UUID, upd RunUpdate) (*models.FunctionRun, error)
}
