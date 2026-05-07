// Package handler hosts the HTTP handlers for pipeline-build-service.
//
// Status: every URL the Rust crate exposes is mounted with the same
// path / verb. Handler bodies are stubbed to the empty-envelope or
// 501 shape because the bulk of pipeline-build-service is the
// resolver / DAG executor / Spark runner / Iceberg client (~30 KLOC of
// Rust). Those domain modules are tracked as separate Go ports —
// see the README for the breakdown.
//
// What IS ported 1:1 in this binary:
//
//   - Models (build, job, pipeline, run) — `internal/models`
//   - Job lifecycle state machine — `internal/domain/joblifecycle`
//   - Marking propagation SQL — `internal/domain/markings`
package handler

import (
	"encoding/json"
	"net/http"
)

// Builds (v1).
func ListBuilds(w http.ResponseWriter, _ *http.Request)  { writeEmptyList(w) }
func CreateBuild(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func GetBuild(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, nil)
}
func AbortBuild(w http.ResponseWriter, r *http.Request)  { notImplemented(w, r) }

// Jobs and job logs.
func ListJobs(w http.ResponseWriter, _ *http.Request)     { writeEmptyList(w) }
func GetJob(w http.ResponseWriter, _ *http.Request)       { writeJSON(w, http.StatusNotFound, nil) }
func ListJobLogs(w http.ResponseWriter, _ *http.Request)  { writeEmptyList(w) }
func StreamJobLogs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	// SSE not implemented yet — emit a single event explaining the gap.
	_, _ = w.Write([]byte("data: {\"event\":\"unimplemented\",\"message\":\"log streaming not ported\"}\n\n"))
}

// Dry-run + execute (resolution preview / immediate trigger).
func DryRunResolve(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func DryRunValidate(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func ExecutePipeline(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }

// Pipeline CRUD (legacy surface that still owns the cron schedule rows).
func ListPipelines(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}, "total": 0, "page": 1, "per_page": 20})
}
func CreatePipeline(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func GetPipeline(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, nil)
}
func UpdatePipeline(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func DeletePipeline(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// Pipeline runs (legacy run table; coexists with the new builds table).
func ListPipelineRuns(w http.ResponseWriter, _ *http.Request) { writeEmptyList(w) }
func TriggerPipelineRun(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func GetPipelineRun(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, nil)
}
func RetryPipelineRun(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func CancelPipelineRun(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }

// SparkApplication-backed runs (FASE 3 / Tarea 3.4). When kube_client
// is unavailable the Rust crate returns 503; we mirror that shape.
func ListSparkRuns(w http.ResponseWriter, _ *http.Request) { writeEmptyList(w) }
func SubmitSparkRun(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{
		"error":  "kube_client_unavailable",
		"detail": "SparkApplication endpoints require an in-cluster kubeconfig (Go port: pending)",
	})
}
func GetSparkRun(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, nil)
}
func GetSpecForRun(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, nil)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func writeEmptyList(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}, "total": 0})
}

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error":  "not_implemented",
		"detail": "build resolver / DAG executor / Iceberg client not yet ported (see service README)",
	})
}
