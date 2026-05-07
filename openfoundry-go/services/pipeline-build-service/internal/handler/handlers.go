// Package handler hosts the HTTP handlers for pipeline-build-service.
//
// Status: every URL the Rust crate exposes is mounted with the same
// path / verb. Most handler bodies still use the empty-envelope or 501 shape while
// Iceberg and remaining legacy CRUD surfaces finish migrating. The critical
// resolver-backed CreateBuild / DryRunResolve paths and executor-backed
// ExecutePipeline / TriggerPipelineRun paths are wired through injectable
// ports; see the README for the full port-status breakdown.
//
// What IS ported 1:1 in this binary:
//
//   - Models (build, job, pipeline, run) — `internal/models`
//   - Job lifecycle state machine — `internal/domain/joblifecycle`
//   - Marking propagation SQL — `internal/domain/markings`
//   - Build resolver — `internal/domain/resolver` plus HTTP wiring
//   - DAG executor — `internal/domain/executor` plus HTTP wiring
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	livellogs "github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/logs"
	sparkpkg "github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/spark"
)

const defaultSSEInitialDelay = 10 * time.Second
const defaultSSEHeartbeatInterval = time.Second

type jobLogStreamConfig struct {
	InitialDelay      time.Duration
	HeartbeatInterval time.Duration
}

var jobLogService atomic.Value    // stores *livellogs.Service
var streamConfig atomic.Value     // stores jobLogStreamConfig
var sparkClientValue atomic.Value // stores *sparkClientSlot

type sparkClientSlot struct {
	client sparkpkg.SparkClient
}

func init() {
	streamConfig.Store(jobLogStreamConfig{InitialDelay: defaultSSEInitialDelay, HeartbeatInterval: defaultSSEHeartbeatInterval})
}

// SetJobLogService injects the live-log ports used by StreamJobLogs. It returns
// a restore function so tests can isolate global handler state.
func SetJobLogService(service *livellogs.Service) func() {
	previous, _ := jobLogService.Load().(*livellogs.Service)
	jobLogService.Store(service)
	return func() { jobLogService.Store(previous) }
}

// SetJobLogStreamConfig adjusts SSE timing. Production defaults mirror Rust's
// 10-second live-log delay; tests can set zero delay for immediate history.
func SetJobLogStreamConfig(initialDelay, heartbeatInterval time.Duration) func() {
	previous := streamConfig.Load().(jobLogStreamConfig)
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultSSEHeartbeatInterval
	}
	streamConfig.Store(jobLogStreamConfig{InitialDelay: initialDelay, HeartbeatInterval: heartbeatInterval})
	return func() { streamConfig.Store(previous) }
}

// SetSparkClient injects the Kubernetes/Spark client used by SubmitSparkRun and
// GetSparkRun. It returns a restore function for tests.
func SetSparkClient(client sparkpkg.SparkClient) func() {
	previous, _ := sparkClientValue.Load().(*sparkClientSlot)
	if previous == nil {
		previous = &sparkClientSlot{}
	}
	sparkClientValue.Store(&sparkClientSlot{client: client})
	return func() { sparkClientValue.Store(previous) }
}

// Builds (v1).
func ListBuilds(w http.ResponseWriter, _ *http.Request) { writeEmptyList(w) }
func GetBuild(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, nil)
}

// Jobs and job logs.
func ListJobs(w http.ResponseWriter, _ *http.Request)    { writeEmptyList(w) }
func GetJob(w http.ResponseWriter, _ *http.Request)      { writeJSON(w, http.StatusNotFound, nil) }
func ListJobLogs(w http.ResponseWriter, _ *http.Request) { writeEmptyList(w) }

// StreamJobLogs streams Rust-compatible Server-Sent Events for job logs.
func StreamJobLogs(w http.ResponseWriter, r *http.Request) {
	service, _ := jobLogService.Load().(*livellogs.Service)
	if service == nil || service.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "live_logs_not_configured", "detail": "live logs not configured"})
		return
	}
	jobRID := jobRIDFromRequest(r)
	if jobRID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_job_id"})
		return
	}
	query, err := parseLogsQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_logs_query", "detail": err.Error()})
		return
	}

	history, err := service.Store.History(r.Context(), jobRID, query)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "log_store_unavailable", "detail": err.Error()})
		return
	}

	var live <-chan livellogs.LogEntry
	var unsubscribe func()
	if query.Follow {
		if service.Subscriber == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "live_logs_not_configured", "detail": "log subscriber not configured"})
			return
		}
		live, unsubscribe, err = service.Subscriber.Subscribe(r.Context(), jobRID)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "log_subscriber_unavailable", "detail": err.Error()})
			return
		}
		defer unsubscribe()
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming_not_supported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	cfg := streamConfig.Load().(jobLogStreamConfig)
	writeHeartbeat(w, flusher, cfg.InitialDelay, "Live logs are streamed in real-time. Time range filters do not apply.")
	if !waitInitialDelay(r.Context(), w, flusher, cfg) {
		return
	}
	for _, entry := range history {
		if !writeLogEvent(w, flusher, entry) {
			return
		}
	}
	if !query.Follow {
		return
	}

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			_, _ = w.Write([]byte(": keep-alive\n\n"))
			flusher.Flush()
		case entry, ok := <-live:
			if !ok {
				return
			}
			if entry.JobRID != "" && entry.JobRID != jobRID {
				continue
			}
			if !queryAllowsLive(query, entry) {
				continue
			}
			if !writeLogEvent(w, flusher, entry) {
				return
			}
		}
	}
}

// Dry-run + execute (resolution preview / immediate trigger).
func DryRunValidate(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }

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

func GetPipelineRun(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, nil)
}
func RetryPipelineRun(w http.ResponseWriter, r *http.Request)  { notImplemented(w, r) }
func CancelPipelineRun(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }

// SparkApplication-backed runs (FASE 3 / Tarea 3.4). When kube_client
// is unavailable the Rust crate returns 503; we mirror that shape.
func ListSparkRuns(w http.ResponseWriter, _ *http.Request) { writeEmptyList(w) }

type submitSparkRunRequest struct {
	PipelineRunID       *uuid.UUID                      `json:"pipeline_run_id,omitempty"`
	PipelineID          string                          `json:"pipeline_id"`
	RunID               string                          `json:"run_id,omitempty"`
	InputDatasetRID     string                          `json:"input_dataset_rid"`
	OutputDatasetRID    string                          `json:"output_dataset_rid"`
	ApplicationType     *sparkpkg.SparkApplicationType  `json:"application_type,omitempty"`
	PipelineRunnerImage string                          `json:"pipeline_runner_image,omitempty"`
	Namespace           string                          `json:"namespace,omitempty"`
	Resources           sparkpkg.SparkResourceOverrides `json:"resources,omitempty"`
}

func SubmitSparkRun(w http.ResponseWriter, r *http.Request) {
	client, ok := currentSparkClient()
	if !ok {
		writeKubeUnavailable(w)
		return
	}
	var body submitSparkRunRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json", "detail": err.Error()})
		return
	}
	runID := uuid.New()
	if body.PipelineRunID != nil {
		runID = *body.PipelineRunID
	}
	shortRunID := body.RunID
	if shortRunID == "" {
		shortRunID = strings.ReplaceAll(runID.String(), "-", "")[:12]
	}
	appType := sparkpkg.SparkApplicationScala
	if body.ApplicationType != nil {
		appType = *body.ApplicationType
	}
	image := body.PipelineRunnerImage
	if image == "" {
		image = "openfoundry/pipeline-runner:dev"
	}
	namespace := body.Namespace
	if namespace == "" {
		namespace = "openfoundry-spark"
	}
	input := sparkpkg.PipelineRunInput{PipelineID: body.PipelineID, RunID: shortRunID, Namespace: namespace, ApplicationType: appType, PipelineRunnerImage: image, InputDatasetRID: body.InputDatasetRID, OutputDatasetRID: body.OutputDatasetRID, Resources: body.Resources}
	name, err := client.SubmitPipelineRun(r.Context(), input)
	if err != nil {
		writeSparkError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"pipeline_run_id": runID, "namespace": namespace, "spark_app_name": name, "status": string(sparkpkg.SparkRunSubmitted)})
}

func GetSparkRun(w http.ResponseWriter, r *http.Request) {
	client, ok := currentSparkClient()
	if !ok {
		writeKubeUnavailable(w)
		return
	}
	name := chi.URLParam(r, "id")
	if name == "" {
		name = strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/"):], "/")
	}
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "openfoundry-spark"
	}
	report, err := client.GetPipelineRunStatus(r.Context(), namespace, name)
	if err != nil {
		writeSparkError(w, err)
		return
	}
	if report == nil {
		writeJSON(w, http.StatusNotFound, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespace": namespace, "spark_app_name": name, "status": string(report.Status), "error_message": report.ErrorMessage})
}

func GetSpecForRun(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, nil)
}

func parseLogsQuery(r *http.Request) (livellogs.Query, error) {
	values := r.URL.Query()
	query := livellogs.Query{Follow: true, Limit: 5000}
	if raw := values.Get("from_sequence"); raw != "" {
		seq, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return query, fmt.Errorf("from_sequence: %w", err)
		}
		query.FromSequence = seq
	} else if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		seq, err := strconv.ParseInt(raw, 10, 64)
		if err == nil {
			query.FromSequence = seq + 1
		}
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return query, fmt.Errorf("limit: %w", err)
		}
		if limit < 1 {
			limit = 1
		}
		if limit > 5000 {
			limit = 5000
		}
		query.Limit = limit
	}
	if raw := values.Get("follow"); raw != "" {
		follow, err := strconv.ParseBool(raw)
		if err != nil {
			return query, fmt.Errorf("follow: %w", err)
		}
		query.Follow = follow
	}
	if raw := values.Get("levels"); raw != "" {
		for _, item := range strings.Split(raw, ",") {
			level, ok := livellogs.ParseLogLevel(item)
			if ok {
				query.Levels = append(query.Levels, level)
			}
		}
	}
	if raw := values.Get("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return query, fmt.Errorf("since: %w", err)
		}
		query.Since = &t
	}
	if raw := values.Get("until"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return query, fmt.Errorf("until: %w", err)
		}
		query.Until = &t
	}
	return query, nil
}

func waitInitialDelay(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, cfg jobLogStreamConfig) bool {
	if cfg.InitialDelay <= 0 {
		return true
	}
	remainingTicks := int(cfg.InitialDelay / cfg.HeartbeatInterval)
	if cfg.InitialDelay%cfg.HeartbeatInterval != 0 {
		remainingTicks++
	}
	for remainingTicks > 0 {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(cfg.HeartbeatInterval):
			remainingTicks--
			writeHeartbeat(w, flusher, time.Duration(remainingTicks)*cfg.HeartbeatInterval, "")
		}
	}
	return true
}

func writeHeartbeat(w http.ResponseWriter, flusher http.Flusher, remaining time.Duration, message string) {
	payload := map[string]any{"phase": "initializing", "delay_remaining_seconds": int64(remaining.Seconds())}
	if message != "" {
		payload["message"] = message
	}
	_ = writeSSE(w, "heartbeat", "", payload)
	flusher.Flush()
}

func writeLogEvent(w http.ResponseWriter, flusher http.Flusher, entry livellogs.LogEntry) bool {
	id := ""
	if entry.Sequence > 0 {
		id = strconv.FormatInt(entry.Sequence, 10)
	}
	if err := writeSSE(w, "log", id, entry.RowDTO()); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func writeSSE(w http.ResponseWriter, eventName, id string, payload any) error {
	if id != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", id); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
		return err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return err
		}
	}
	_, err = w.Write([]byte("\n"))
	return err
}

func jobRIDFromRequest(r *http.Request) string {
	if rid := chi.URLParam(r, "id"); rid != "" {
		return rid
	}
	if rid := chi.URLParam(r, "rid"); rid != "" {
		return rid
	}
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "jobs" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func queryAllowsLive(query livellogs.Query, entry livellogs.LogEntry) bool {
	if entry.Sequence < query.FromSequence {
		return false
	}
	if len(query.Levels) == 0 {
		return true
	}
	for _, level := range query.Levels {
		if entry.Level == level {
			return true
		}
	}
	return false
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

func currentSparkClient() (sparkpkg.SparkClient, bool) {
	if slot, ok := sparkClientValue.Load().(*sparkClientSlot); ok && slot != nil && slot.client != nil {
		if _, disabled := slot.client.(noSparkClient); disabled {
			return nil, false
		}
		return slot.client, true
	}
	client, err := sparkpkg.NewKubernetesClientFromEnv()
	if err != nil {
		return nil, false
	}
	return client, true
}

func writeKubeUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{
		"error":  "kube_client_unavailable",
		"detail": "SparkApplication endpoints require an in-cluster kubeconfig (Go port: pending)",
	})
}

func writeSparkError(w http.ResponseWriter, err error) {
	var invalid *sparkpkg.InvalidInputError
	var render *sparkpkg.RenderError
	var kube *sparkpkg.KubeError
	switch {
	case errors.As(err, &invalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_spark_spec", "detail": err.Error()})
	case errors.As(err, &render):
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "spark_render_failed", "detail": err.Error()})
	case errors.As(err, &kube):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "kubernetes_api_error", "detail": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "spark_error", "detail": err.Error()})
	}
}

type noSparkClient struct{}

func (noSparkClient) SubmitPipelineRun(context.Context, sparkpkg.PipelineRunInput) (string, error) {
	return "", &sparkpkg.UnavailableError{}
}
func (noSparkClient) GetPipelineRunStatus(context.Context, string, string) (*sparkpkg.SparkRunStatusReport, error) {
	return nil, &sparkpkg.UnavailableError{}
}
