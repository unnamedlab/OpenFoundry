package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/domain/engine"
	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

// GetOverview mirrors `handlers::jobs::get_overview`.
func (h *Handlers) GetOverview(w http.ResponseWriter, r *http.Request) {
	ov, err := h.Overview.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

// ListJobs mirrors `handlers::jobs::list_jobs`.
func (h *Handlers) ListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.Jobs.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.FusionJob]{Data: jobs})
}

// CreateJob mirrors `handlers::jobs::create_job`.
func (h *Handlers) CreateJob(w http.ResponseWriter, r *http.Request) {
	var body models.CreateFusionJobRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "job name is required")
		return
	}
	rule, err := h.Rules.Get(r.Context(), body.MatchRuleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	strategy, err := h.MergeStrategies.Get(r.Context(), body.MergeStrategyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if rule == nil || strategy == nil {
		writeError(w, http.StatusBadRequest, "job requires an existing match rule and merge strategy")
		return
	}

	job, err := h.Jobs.Create(r.Context(), body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// RunJob mirrors `handlers::jobs::run_job` — the heart of the service.
//
// 1. load job + rule + strategy.
// 2. synthesize records, build candidate pairs, score with rules + ML.
// 3. cluster via union-find graph_resolution.
// 4. synthesize golden records per cluster.
// 5. persist (DELETE+INSERT clusters/golden records/review items).
// 6. update job metrics + status + summary.
func (h *Handlers) RunJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	job, err := h.Jobs.Get(r.Context(), jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if job == nil {
		writeError(w, http.StatusNotFound, "fusion job not found")
		return
	}
	rule, err := h.Rules.Get(r.Context(), job.MatchRuleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if rule == nil {
		writeError(w, http.StatusNotFound, "match rule not found")
		return
	}
	strategy, err := h.MergeStrategies.Get(r.Context(), job.MergeStrategyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if strategy == nil {
		writeError(w, http.StatusNotFound, "merge strategy not found")
		return
	}

	records := domain.SynthesizeEntityRecords(job.EntityType, job.Config)
	if len(records) < 2 {
		writeError(w, http.StatusBadRequest, "resolution job requires at least two records")
		return
	}

	blockingStrategy := rule.BlockingStrategy
	if job.Config.BlockingStrategyOverride != nil {
		blockingStrategy = *job.Config.BlockingStrategyOverride
	}
	candidatePairs := engine.BuildCandidatePairs(records, blockingStrategy)

	evidences := make([]models.MatchEvidence, 0, len(candidatePairs))
	for _, pair := range candidatePairs {
		evidence := engine.EvaluateCandidate(*rule, pair)
		mlScore := engine.ScoreCandidate(pair.Left, pair.Right, evidence)
		evidence.MLScore = mlScore
		evidence.FinalScore = engine.BlendScores(evidence.RuleScore, mlScore)
		evidence.RequiresReview = evidence.FinalScore >= rule.ReviewThreshold &&
			evidence.FinalScore < rule.AutoMergeThreshold
		if evidence.FinalScore >= rule.ReviewThreshold {
			evidences = append(evidences, evidence)
		}
	}

	resolution := engine.ResolveClusters(job.ID, records, evidences, rule.ReviewThreshold, rule.AutoMergeThreshold)
	clusters := resolution.Clusters
	reviewItems := resolution.ReviewItems
	goldenRecords := []models.GoldenRecord{}

	for i := range clusters {
		if clusters[i].Status == "rejected" {
			continue
		}
		gr := domain.SynthesizeGoldenRecord(clusters[i], *strategy)
		grID := gr.ID
		clusters[i].SuggestedGoldenRecordID = &grID
		goldenRecords = append(goldenRecords, gr)
	}

	if err := h.Clusters.DeleteByJob(r.Context(), job.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	for _, c := range clusters {
		if err := h.Clusters.Insert(r.Context(), c); err != nil {
			writeError(w, http.StatusInternalServerError, "database operation failed")
			return
		}
	}
	for _, gr := range goldenRecords {
		if err := h.Golden.Insert(r.Context(), gr); err != nil {
			writeError(w, http.StatusInternalServerError, "database operation failed")
			return
		}
	}
	for _, item := range reviewItems {
		if err := h.Review.Insert(r.Context(), item); err != nil {
			writeError(w, http.StatusInternalServerError, "database operation failed")
			return
		}
	}

	var avg float32
	if len(evidences) > 0 {
		total := float32(0)
		for _, e := range evidences {
			total += e.FinalScore
		}
		avg = total / float32(len(evidences))
	}
	matchedPairs := int32(0)
	reviewPairs := int32(0)
	for _, e := range evidences {
		if e.FinalScore >= rule.AutoMergeThreshold {
			matchedPairs++
		}
		if e.RequiresReview {
			reviewPairs++
		}
	}
	multiClusterCount := 0
	for _, c := range clusters {
		if len(c.Records) > 1 {
			multiClusterCount++
		}
	}
	denom := float32(len(records))
	if denom < 1 {
		denom = 1
	}
	recall := float32(multiClusterCount) / (denom / 2.0)
	switch {
	case recall < 0:
		recall = 0
	case recall > 1:
		recall = 1
	}
	precision := avg
	switch {
	case precision < 0:
		precision = 0
	case precision > 1:
		precision = 1
	}

	metrics := models.FusionJobMetrics{
		CandidatePairs:    int32(len(candidatePairs)),
		MatchedPairs:      matchedPairs,
		ReviewPairs:       reviewPairs,
		ClusterCount:      int32(len(clusters)),
		GoldenRecordCount: int32(len(goldenRecords)),
		PrecisionEstimate: precision,
		RecallEstimate:    recall,
	}

	status := "completed"
	if len(reviewItems) > 0 {
		status = "awaiting_review"
	}
	summary := fmt.Sprintf("Generated %d clusters, %d golden records, %d review items.",
		len(clusters), len(goldenRecords), len(reviewItems))

	updatedJob, err := h.Jobs.UpdateAfterRun(r.Context(), job.ID, status, summary, metrics)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}

	clusterIDs := make([]uuid.UUID, len(clusters))
	for i, c := range clusters {
		clusterIDs[i] = c.ID
	}
	goldenIDs := make([]uuid.UUID, len(goldenRecords))
	for i, gr := range goldenRecords {
		goldenIDs[i] = gr.ID
	}
	reviewIDs := make([]uuid.UUID, len(reviewItems))
	for i, item := range reviewItems {
		reviewIDs[i] = item.ID
	}

	writeJSON(w, http.StatusOK, models.RunResolutionJobResponse{
		Job:                updatedJob,
		ClusterIDs:         clusterIDs,
		GoldenRecordIDs:    goldenIDs,
		ReviewQueueItemIDs: reviewIDs,
		ExecutedAt:         time.Now().UTC(),
	})
}
