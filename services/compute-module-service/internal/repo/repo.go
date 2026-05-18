// Package repo holds the persistence interface and implementations
// for Compute Module resources. The in-memory implementation drives
// the CM.1 unit tests and the smoke-mode deployment of
// compute-module-service. A Postgres implementation will land
// alongside the goose-style migrations in ./migrations (CM.3+).
package repo

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/domain/function"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/models"
)

// Sentinel errors returned by every Repository implementation. HTTP
// handlers map these to status codes; tests assert on them with
// errors.Is.
var (
	ErrNotFound        = errors.New("compute-module: not found")
	ErrNameConflict    = errors.New("compute-module: name already exists in target folder")
	ErrAlreadyArchived = errors.New("compute-module: already archived")
	ErrNotArchived     = errors.New("compute-module: not archived")
)

// ErrExecutionModeMismatch is returned when a mode-gated operation is
// attempted on a module of the wrong execution mode. Repository
// implementations wrap the executionmode-package sentinels under this
// umbrella so handlers can distinguish a mode mismatch from a
// not-found / conflict / validation failure with a single errors.Is.
var ErrExecutionModeMismatch = errors.New("compute-module: execution mode mismatch")

// ListFilter narrows a List request to a subset of modules. All
// fields are optional; the zero value lists every active module.
type ListFilter struct {
	ProjectID     *uuid.UUID
	FolderID      *uuid.UUID
	ExecutionMode *models.ExecutionMode
	State         *models.LifecycleState
	// IncludeArchived defaults to false: archived modules are excluded
	// unless explicitly opted in. State takes precedence when set.
	IncludeArchived bool
}

// Page captures cursor pagination input. Limit==0 falls back to the
// repository default (50). Cursor is an opaque token returned by the
// previous response; in the in-memory store it encodes the last
// module's UUID.
type Page struct {
	Cursor *string
	Limit  uint32
}

// ListResult bundles a single page of modules with a cursor for the
// next page (nil when exhausted).
type ListResult struct {
	Items      []*models.ComputeModule
	NextCursor *string
}

// Repository is the persistence contract.
type Repository interface {
	Create(ctx context.Context, params models.CreateParams) (*models.ComputeModule, error)
	Get(ctx context.Context, id uuid.UUID) (*models.ComputeModule, error)
	List(ctx context.Context, filter ListFilter, page Page) (ListResult, error)
	UpdateMetadata(ctx context.Context, id uuid.UUID, params models.UpdateMetadataParams) (*models.ComputeModule, error)
	Move(ctx context.Context, id uuid.UUID, params models.MoveParams) (*models.ComputeModule, error)
	Duplicate(ctx context.Context, id uuid.UUID, params models.DuplicateParams) (*models.ComputeModule, error)
	Archive(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*models.ComputeModule, error)
	Restore(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*models.ComputeModule, error)
	Delete(ctx context.Context, id uuid.UUID) error

	// SetPipelineIOConfig persists a pipeline I/O config on a
	// pipeline-mode module. Function-mode modules return
	// ErrExecutionModeMismatch.
	SetPipelineIOConfig(ctx context.Context, id uuid.UUID, cfg models.PipelineIOConfig, actor uuid.UUID) (*models.ComputeModule, error)

	// ClearPipelineIOConfig removes a previously-set pipeline I/O
	// config. Function-mode modules return ErrExecutionModeMismatch.
	ClearPipelineIOConfig(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*models.ComputeModule, error)

	// SetContainerImage attaches/replaces the container image
	// reference on a module. The image is stored verbatim — the
	// caller is expected to run the containerimage policy first and
	// pass the resulting findings inside img.Findings.
	SetContainerImage(ctx context.Context, id uuid.UUID, img models.ContainerImage, actor uuid.UUID) (*models.ComputeModule, error)

	// ClearContainerImage removes the image reference from the module.
	ClearContainerImage(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*models.ComputeModule, error)

	// SetRuntimeConfig persists the single-container runtime config on
	// the module. The caller is expected to run the runtime policy
	// (and any redaction) before invoking this method — the repo
	// stores the config verbatim.
	SetRuntimeConfig(ctx context.Context, id uuid.UUID, cfg models.RuntimeConfig, actor uuid.UUID) (*models.ComputeModule, error)

	// ClearRuntimeConfig removes the runtime configuration from the
	// module.
	ClearRuntimeConfig(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*models.ComputeModule, error)

	// CreateInvocation persists a freshly minted FunctionInvocation in
	// status=queued. The repo assigns the timestamps it owns
	// (ScheduledAt) and returns the stored row.
	CreateInvocation(ctx context.Context, inv function.FunctionInvocation) (*function.FunctionInvocation, error)

	// MarkInvocationRunning flips the row to status=running and stamps
	// StartedAt. Already-terminal rows return ErrInvocationTerminal.
	MarkInvocationRunning(ctx context.Context, id uuid.UUID) (*function.FunctionInvocation, error)

	// CompleteInvocation moves the row to a terminal status and
	// persists result/error/cost metadata.
	CompleteInvocation(ctx context.Context, id uuid.UUID, update InvocationCompletion) (*function.FunctionInvocation, error)

	// CancelInvocation flips a queued/running row to status=cancelled
	// and stamps FinishedAt. Terminal rows return ErrInvocationTerminal.
	CancelInvocation(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*function.FunctionInvocation, error)

	// GetInvocation returns the stored row by id.
	GetInvocation(ctx context.Context, id uuid.UUID) (*function.FunctionInvocation, error)

	// ListInvocations paginates invocations using cursor pagination,
	// optionally filtered by module / tenant / status.
	ListInvocations(ctx context.Context, filter InvocationFilter, page Page) (InvocationListResult, error)
}

// InvocationCompletion is the partial update applied to a row by
// CompleteInvocation.
type InvocationCompletion struct {
	Status       function.Status
	Result       []byte
	ErrorMessage string
	CostUnits    int64
}

// InvocationFilter narrows ListInvocations. Zero value lists every
// invocation across every tenant — callers are expected to scope by
// tenant from the request claims.
type InvocationFilter struct {
	ModuleID *uuid.UUID
	TenantID *uuid.UUID
	Status   *function.Status
}

// InvocationListResult bundles one page of invocations with a cursor
// for the next page (nil when exhausted).
type InvocationListResult struct {
	Items      []*function.FunctionInvocation
	NextCursor *string
}
