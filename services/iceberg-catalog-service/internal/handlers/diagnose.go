package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// ConfigResponse mirrors `handlers::rest_catalog::config::ConfigResponse`:
// `{defaults: {...}, overrides: {...}}`. Returned by `GET /iceberg/v1/config`
// so PyIceberg / Spark clients learn which Foundry warehouse the catalog
// routes to.
type ConfigResponse struct {
	Defaults  map[string]string `json:"defaults"`
	Overrides map[string]string `json:"overrides"`
}

// GetConfig serves `GET /iceberg/v1/config`. Mirrors the Rust handler:
// requires an authenticated principal, populates `defaults.warehouse` from
// the configured warehouse URI, and leaves `overrides` empty.
func (h *Handlers) GetConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	defaults := map[string]string{}
	if h.WarehouseURI != "" {
		defaults["warehouse"] = h.WarehouseURI
	}
	writeJSON(w, http.StatusOK, ConfigResponse{
		Defaults:  defaults,
		Overrides: map[string]string{},
	})
}

// DiagnoseRequest mirrors `handlers::diagnose::DiagnoseRequest`. `client`
// labels the catalog client implementation (PyIceberg/Spark/etc) so audit
// dashboards can break failures down by client. `project_rid` is optional
// and falls back to the default Foundry project on the catalog.
type DiagnoseRequest struct {
	Client     string  `json:"client"`
	ProjectRID *string `json:"project_rid,omitempty"`
}

// DiagnoseStep mirrors `handlers::diagnose::DiagnoseStep`. The shape and
// field names are byte-exact with the Rust serde output: `latency_ms` is a
// number of milliseconds, `detail` may be null.
type DiagnoseStep struct {
	Name      string  `json:"name"`
	Ok        bool    `json:"ok"`
	LatencyMS int64   `json:"latency_ms"`
	Detail    *string `json:"detail"`
}

// DiagnoseResponse mirrors `handlers::diagnose::DiagnoseResponse`.
type DiagnoseResponse struct {
	Client         string         `json:"client"`
	Success        bool           `json:"success"`
	Steps          []DiagnoseStep `json:"steps"`
	TotalLatencyMS int64          `json:"total_latency_ms"`
}

const (
	diagnoseDefaultProjectRID = "ri.foundry.main.project.default"
	diagnoseProbeNamespace    = "_diagnostic"
	diagnoseProbeMissingMsg   = "no probe namespace; create `_diagnostic` to enable load probe"
	diagnoseProbeReachableMsg = "probe namespace reachable"
)

// RunDiagnose serves `POST /iceberg/v1/diagnose`. Runs the same two-step
// probe as the Rust handler — `list_namespaces` then `load_probe_namespace`
// against the `_diagnostic` namespace — and reports per-step ok/latency
// plus the overall success flag.
func (h *Handlers) RunDiagnose(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body DiagnoseRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}

	projectRID := diagnoseDefaultProjectRID
	if body.ProjectRID != nil && *body.ProjectRID != "" {
		projectRID = *body.ProjectRID
	}

	started := time.Now()
	steps := make([]DiagnoseStep, 0, 2)

	// Step 1 — ListNamespaces: a hard error fails the probe.
	t := time.Now()
	list, err := h.Repo.ListTopLevelNamespaces(r.Context(), projectRID)
	elapsed := time.Since(t).Milliseconds()
	step1 := DiagnoseStep{Name: "list_namespaces", LatencyMS: elapsed}
	if err != nil {
		detail := err.Error()
		step1.Ok = false
		step1.Detail = &detail
	} else {
		detail := fmt.Sprintf("%d namespaces", len(list))
		step1.Ok = true
		step1.Detail = &detail
	}
	steps = append(steps, step1)

	// Step 2 — Resolve the `_diagnostic` probe namespace. Always reported as
	// `ok=true` so a missing probe namespace is a soft-warn rather than a
	// hard failure (matches Rust).
	t = time.Now()
	probe, _ := h.Repo.FetchNamespaceByName(r.Context(), projectRID, []string{diagnoseProbeNamespace})
	elapsed = time.Since(t).Milliseconds()
	var probeDetail string
	if probe != nil {
		probeDetail = diagnoseProbeReachableMsg
	} else {
		probeDetail = diagnoseProbeMissingMsg
	}
	steps = append(steps, DiagnoseStep{
		Name:      "load_probe_namespace",
		Ok:        true,
		LatencyMS: elapsed,
		Detail:    &probeDetail,
	})

	total := time.Since(started).Milliseconds()
	success := true
	for _, s := range steps {
		if !s.Ok {
			success = false
			break
		}
	}
	writeJSON(w, http.StatusOK, DiagnoseResponse{
		Client:         body.Client,
		Success:        success,
		Steps:          steps,
		TotalLatencyMS: total,
	})
}
