package retention

import (
	"context"
	"time"

	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/repo"
)

// loadCandidatesPageSize keeps a single LoadRetentionRows call from
// pulling unbounded rows. Tuned by env in production wiring.
const loadCandidatesPageSize = 10000

// RepoStore adapts *repo.Repo to the worker's Store interface. It is
// the production implementation injected from main.go.
type RepoStore struct {
	Repo *repo.Repo
	// PageSize is the per-tick row cap on LoadRetentionRows. Defaults
	// to loadCandidatesPageSize when zero.
	PageSize int
}

// NewRepoStore constructs the production adapter.
func NewRepoStore(r *repo.Repo) *RepoStore { return &RepoStore{Repo: r} }

// LoadRetentionRows returns the active branches eligible for retention
// scanning. Mirrors the load step of Rust retention_worker::run_once
// (minus the DISTINCT branch_id open-transaction subquery, which the
// repo's SQL inlines into the candidate query for the same effect).
func (s *RepoStore) LoadRetentionRows(ctx context.Context) ([]models.RetentionRow, error) {
	limit := s.PageSize
	if limit <= 0 {
		limit = loadCandidatesPageSize
	}
	return s.Repo.ListRetentionCandidates(ctx, time.Now().UTC(), limit)
}

// ArchiveBranch delegates to the repo's transactional archive +
// reparent + outbox emit path.
func (s *RepoStore) ArchiveBranch(ctx context.Context, row models.RetentionRow, graceUntil time.Time, payload models.JSONValue) (bool, error) {
	return s.Repo.ArchiveBranchForRetentionWithOutbox(ctx, row, graceUntil, payload)
}
