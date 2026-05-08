package handlers

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

// OverviewHandlers exposes the ML-platform landing summary. Mirrors
// libs/ml-kernel/src/handlers/overview.rs.
type OverviewHandlers struct {
	Pool *pgxpool.Pool
}

// GetOverview handles `GET /api/v1/overview`. Aggregates 10 counters
// across ml_experiments / ml_runs / ml_models / ml_model_versions /
// ml_features / ml_deployments / ml_training_jobs.
func (h *OverviewHandlers) GetOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	count := func(sql string) (int64, error) {
		var n int64
		err := h.Pool.QueryRow(ctx, sql).Scan(&n)
		return n, err
	}

	queries := []struct {
		name string
		sql  string
	}{
		{"experiments", `SELECT COUNT(*) FROM ml_experiments`},
		{"activeRuns", `SELECT COUNT(*) FROM ml_runs WHERE status IN ('queued', 'running', 'completed')`},
		{"models", `SELECT COUNT(*) FROM ml_models`},
		{"production", `SELECT COUNT(*) FROM ml_model_versions WHERE stage = 'production'`},
		{"features", `SELECT COUNT(*) FROM ml_features`},
		{"online", `SELECT COUNT(*) FROM ml_features WHERE online_enabled = TRUE`},
		{"deployments", `SELECT COUNT(*) FROM ml_deployments`},
		{"abTests", `SELECT COUNT(*) FROM ml_deployments WHERE strategy_type = 'ab_test'`},
		{"drift", `SELECT COUNT(*) FROM ml_deployments WHERE COALESCE((drift_report->>'recommend_retraining')::boolean, FALSE)`},
		{"queuedTraining", `SELECT COUNT(*) FROM ml_training_jobs WHERE status IN ('queued', 'running')`},
	}
	values := make(map[string]int64, len(queries))
	for _, q := range queries {
		v, err := count(q.sql)
		if err != nil {
			dbError(w, err)
			return
		}
		values[q.name] = v
	}

	writeJSON(w, http.StatusOK, models.MlStudioOverview{
		ExperimentCount:        values["experiments"],
		ActiveRunCount:         values["activeRuns"],
		ModelCount:             values["models"],
		ProductionModelCount:   values["production"],
		FeatureCount:           values["features"],
		OnlineFeatureCount:     values["online"],
		DeploymentCount:        values["deployments"],
		ABTestCount:        values["abTests"],
		DriftAlertCount:    values["drift"],
		QueuedTrainingJobs: values["queuedTraining"],
	})
}
