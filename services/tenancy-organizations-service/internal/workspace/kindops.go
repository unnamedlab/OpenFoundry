package workspace

// kindops.go owns the trash registry — the per-ResourceKind dispatch
// table used by RestoreTrashed / PurgeTrashed in trash.go. Adding a new
// trashable kind means registering a ResourceHandler here; the HTTP
// layer maps an unregistered kind to 422 via ErrResourceKindUnsupported.

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrResourceKindUnsupported is returned when a restore/purge target
// has no registered handler. The HTTP layer maps it to 422 (the kind
// is syntactically valid but not actionable on this surface) — not 500.
var ErrResourceKindUnsupported = errors.New("resource_kind has no trash handler")

// ErrTrashedRowNotFound is returned when the UPDATE/DELETE affects
// zero rows: either the row never existed, was already restored, or
// was never trashed. The HTTP layer maps it to 404.
var ErrTrashedRowNotFound = errors.New("no trashed row matched")

// TrashExecutor is the slice of pgxpool.Pool the trash handlers need.
// Narrowed so unit tests can stub it without spinning up Postgres.
type TrashExecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// ResourceHandler restores or purges a single trashed row for one
// ResourceKind. Implementations are stateless and registered in
// trashRegistry from init().
type ResourceHandler interface {
	Restore(ctx context.Context, db TrashExecutor, rid uuid.UUID) error
	Purge(ctx context.Context, db TrashExecutor, rid uuid.UUID) error
}

// trashRegistry maps a wire-canonical ResourceKind to its handler.
// Populated exclusively from init() — runtime mutation would race with
// concurrent restore/purge dispatch.
var trashRegistry = map[ResourceKind]ResourceHandler{}

func registerTrashHandler(k ResourceKind, h ResourceHandler) {
	trashRegistry[k] = h
}

// IsTrashKindSupported reports whether a kind has a registered handler.
// Exposed so the HTTP handlers (and tests) can gate with a 422 before
// running the more expensive authz query.
func IsTrashKindSupported(k ResourceKind) bool {
	_, ok := trashRegistry[k]
	return ok
}

// lookupTrashHandler returns the handler for k or ErrResourceKindUnsupported.
func lookupTrashHandler(k ResourceKind) (ResourceHandler, error) {
	h, ok := trashRegistry[k]
	if !ok {
		return nil, ErrResourceKindUnsupported
	}
	return h, nil
}

// sqlKindHandler is the generic two-statement implementation used by
// every ontology trash row. Both statements are bound on $1 = rid and
// guarded by `is_deleted = TRUE` so live rows are never affected.
type sqlKindHandler struct {
	restoreSQL string
	purgeSQL   string
}

func (s sqlKindHandler) Restore(ctx context.Context, db TrashExecutor, rid uuid.UUID) error {
	ct, err := db.Exec(ctx, s.restoreSQL, rid)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrTrashedRowNotFound
	}
	return nil
}

func (s sqlKindHandler) Purge(ctx context.Context, db TrashExecutor, rid uuid.UUID) error {
	ct, err := db.Exec(ctx, s.purgeSQL, rid)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrTrashedRowNotFound
	}
	return nil
}

func init() {
	registerTrashHandler(ResourceOntologyProject, sqlKindHandler{
		restoreSQL: `UPDATE ontology_projects
		    SET is_deleted = FALSE, deleted_at = NULL, deleted_by = NULL,
		        purge_after = NULL,
		        original_project_id = NULL,
		        original_parent_folder_id = NULL,
		        updated_at = NOW()
		  WHERE id = $1 AND is_deleted = TRUE`,
		purgeSQL: `DELETE FROM ontology_projects WHERE id = $1 AND is_deleted = TRUE`,
	})
	registerTrashHandler(ResourceOntologyFolder, sqlKindHandler{
		restoreSQL: `UPDATE ontology_project_folders
		    SET is_deleted = FALSE, deleted_at = NULL, deleted_by = NULL,
		        purge_after = NULL,
		        original_project_id = NULL,
		        original_parent_folder_id = NULL,
		        updated_at = NOW()
		  WHERE id = $1 AND is_deleted = TRUE`,
		purgeSQL: `DELETE FROM ontology_project_folders WHERE id = $1 AND is_deleted = TRUE`,
	})
	registerTrashHandler(ResourceOntologyResourceBinding, sqlKindHandler{
		restoreSQL: `UPDATE ontology_project_resources
		    SET is_deleted = FALSE, deleted_at = NULL, deleted_by = NULL,
		        purge_after = NULL,
		        original_project_id = NULL,
		        original_parent_folder_id = NULL
		  WHERE resource_id = $1 AND is_deleted = TRUE`,
		purgeSQL: `DELETE FROM ontology_project_resources WHERE resource_id = $1 AND is_deleted = TRUE`,
	})
}
