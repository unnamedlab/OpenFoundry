package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/repo"
)

func (h *Handlers) resolveDatasetForQuality(w http.ResponseWriter, r *http.Request) (*models.Dataset, bool) {
	datasetID, ok := h.resolveDatasetForCatalog(w, r)
	if !ok {
		return nil, false
	}
	dataset, err := h.Repo.GetDataset(r.Context(), datasetID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to load dataset")
		return nil, false
	}
	if dataset == nil {
		writeJSONErr(w, http.StatusNotFound, "dataset not found")
		return nil, false
	}
	return dataset, true
}

func (h *Handlers) GetDatasetQuality(w http.ResponseWriter, r *http.Request) {
	dataset, ok := h.resolveDatasetForQuality(w, r)
	if !ok {
		return
	}
	out, err := h.Repo.GetDatasetQuality(r.Context(), dataset.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to load dataset quality")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) RefreshDatasetQuality(w http.ResponseWriter, r *http.Request) {
	dataset, ok := h.resolveDatasetForQuality(w, r)
	if !ok {
		return
	}
	files, err := h.Repo.ListFiles(r.Context(), dataset.ID, "", "")
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to inspect dataset files")
		return
	}
	hasUploadedData := dataset.SizeBytes > 0 || dataset.RowCount > 0
	for _, file := range files {
		if file.DeletedAt == nil && (file.Status == "" || strings.EqualFold(file.Status, "active")) {
			hasUploadedData = true
			break
		}
	}
	if !hasUploadedData {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload data before generating a quality profile"})
		return
	}
	out, err := h.Repo.GetDatasetQuality(r.Context(), dataset.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to load dataset quality")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) CreateQualityRule(w http.ResponseWriter, r *http.Request) {
	dataset, ok := h.resolveDatasetForQuality(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireDatasetWrite(w, r, dataset.ID); !ok {
		return
	}
	var body models.CreateQualityRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.RuleType) == "" {
		writeJSONErr(w, http.StatusBadRequest, "name and rule_type required")
		return
	}
	if _, err := h.Repo.UpsertQualityRule(r.Context(), dataset.ID, &body); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to create quality rule")
		return
	}
	out, err := h.Repo.GetDatasetQuality(r.Context(), dataset.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to load dataset quality")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *Handlers) UpdateQualityRule(w http.ResponseWriter, r *http.Request) {
	dataset, ok := h.resolveDatasetForQuality(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireDatasetWrite(w, r, dataset.ID); !ok {
		return
	}
	ruleID, err := uuid.Parse(chi.URLParam(r, "rule_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid rule_id")
		return
	}
	var body models.UpdateQualityRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.Repo.UpdateQualityRule(r.Context(), dataset.ID, ruleID, &body); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeJSONErr(w, http.StatusNotFound, "quality rule not found")
			return
		}
		writeJSONErr(w, http.StatusInternalServerError, "failed to update quality rule")
		return
	}
	out, err := h.Repo.GetDatasetQuality(r.Context(), dataset.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to load dataset quality")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) DeleteQualityRule(w http.ResponseWriter, r *http.Request) {
	dataset, ok := h.resolveDatasetForQuality(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireDatasetWrite(w, r, dataset.ID); !ok {
		return
	}
	ruleID, err := uuid.Parse(chi.URLParam(r, "rule_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid rule_id")
		return
	}
	if err := h.Repo.DeleteQualityRule(r.Context(), dataset.ID, ruleID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeJSONErr(w, http.StatusNotFound, "quality rule not found")
			return
		}
		writeJSONErr(w, http.StatusInternalServerError, "failed to delete quality rule")
		return
	}
	out, err := h.Repo.GetDatasetQuality(r.Context(), dataset.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to load dataset quality")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) GetDatasetLint(w http.ResponseWriter, r *http.Request) {
	dataset, ok := h.resolveDatasetForQuality(w, r)
	if !ok {
		return
	}
	summary, err := h.Repo.DatasetLintSummary(r.Context(), dataset.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to compute dataset lint")
		return
	}
	findings, recommendations := lintFindingsFromSummary(*summary)
	summary.TotalFindings = len(findings)
	for _, finding := range findings {
		switch finding.Severity {
		case "high":
			summary.HighSeverity++
		case "medium":
			summary.MediumSeverity++
		case "low":
			summary.LowSeverity++
		}
	}
	writeJSON(w, http.StatusOK, models.DatasetLintResponse{DatasetID: dataset.ID, DatasetName: dataset.Name, AnalyzedAt: time.Now().UTC(), Summary: *summary, Findings: findings, Recommendations: recommendations})
}

func (h *Handlers) GetDatasetHealth(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveDatasetForCatalog(w, r); !ok {
		return
	}
	rid := datasetIDParam(r)
	if id, err := uuid.Parse(rid); err == nil {
		rid = "ri.foundry.main.dataset." + id.String()
	}
	out, err := h.Repo.GetDatasetHealth(r.Context(), rid)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to load dataset health")
		return
	}
	if out == nil {
		writeJSONErr(w, http.StatusNotFound, "dataset health not found")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func lintFindingsFromSummary(summary models.DatasetLintSummary) ([]models.DatasetLintFinding, []models.DatasetLintRecommendation) {
	findings := []models.DatasetLintFinding{}
	recommendations := []models.DatasetLintRecommendation{}
	add := func(code, title, severity, category, description, impact, recommendation string, evidence []string) {
		findings = append(findings, models.DatasetLintFinding{Code: code, Title: title, Severity: severity, Category: category, Description: description, Evidence: evidence, Impact: impact, Recommendation: recommendation})
		recommendations = append(recommendations, models.DatasetLintRecommendation{Code: code, Priority: severity, Title: title, Rationale: impact, Actions: []string{recommendation}})
	}
	if summary.TrackedVersions >= 12 {
		severity := "medium"
		if summary.TrackedVersions >= 24 {
			severity = "high"
		}
		add("version-sprawl", "Version sprawl is increasing storage overhead", severity, "lifecycle", "The dataset is retaining many tracked versions.", "Storage footprint and branch maintenance can grow unexpectedly.", "Prune cold versions, archive historical snapshots, or promote a retention policy.", []string{"tracked_versions=" + strconv.Itoa(summary.TrackedVersions)})
	}
	if summary.StaleBranchCount >= 2 || summary.BranchCount >= 6 {
		add("stale-branches", "Stale branches need cleanup", "medium", "lifecycle", "Several branches have not had recent activity.", "Review and merge workflows become harder to reason about.", "Archive or delete stale branches after validation.", []string{"branch_count=" + strconv.Itoa(summary.BranchCount), "stale_branch_count=" + strconv.Itoa(summary.StaleBranchCount)})
	}
	if summary.ActiveAlertCount > 0 {
		add("active-quality-alerts", "Active quality alerts are open", "high", "quality", "The quality subsystem has unresolved active alerts.", "Consumers may be reading data that failed quality expectations.", "Review and resolve quality alerts or adjust rules.", []string{"active_alert_count=" + strconv.Itoa(summary.ActiveAlertCount)})
	}
	if summary.SmallFileCount >= 50 {
		add("small-files", "Small file count is high", "medium", "storage", "Many small backing files can slow scans and metadata operations.", "Read planning and object-store request volume can increase.", "Compact small files into larger columnar objects.", []string{"small_file_count=" + strconv.Itoa(summary.SmallFileCount)})
	}
	return findings, recommendations
}
