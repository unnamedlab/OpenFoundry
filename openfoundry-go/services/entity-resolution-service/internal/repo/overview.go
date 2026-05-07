package repo

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

// OverviewRepo runs the dashboard count SQL from `handlers::jobs::get_overview`.
type OverviewRepo struct {
	Pool *pgxpool.Pool
}

func (r *OverviewRepo) Get(ctx context.Context) (models.FusionOverview, error) {
	var ov models.FusionOverview
	queries := []struct {
		dst *int64
		sql string
	}{
		{&ov.RuleCount, "SELECT COUNT(*) FROM fusion_match_rules"},
		{&ov.ActiveJobCount, "SELECT COUNT(*) FROM fusion_jobs WHERE status IN ('draft', 'running', 'awaiting_review')"},
		{&ov.CompletedJobCount, "SELECT COUNT(*) FROM fusion_jobs WHERE status IN ('completed', 'awaiting_review')"},
		{&ov.ClusterCount, "SELECT COUNT(*) FROM fusion_clusters"},
		{&ov.PendingReviewCount, "SELECT COUNT(*) FROM fusion_review_queue WHERE status = 'pending'"},
		{&ov.GoldenRecordCount, "SELECT COUNT(*) FROM fusion_golden_records"},
		{&ov.AutoMergedClusterCount, "SELECT COUNT(*) FROM fusion_clusters WHERE status = 'resolved' AND requires_review = FALSE"},
	}
	for _, q := range queries {
		if err := r.Pool.QueryRow(ctx, q.sql).Scan(q.dst); err != nil {
			return models.FusionOverview{}, err
		}
	}
	return ov, nil
}
