package flink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// JobGraphErrorKind mirrors the variants of JobGraphError in the Rust source.
type JobGraphErrorKind int

const (
	JobGraphErrUnknown JobGraphErrorKind = iota
	JobGraphErrNotDeployed
	JobGraphErrStatus
	JobGraphErrHTTP
	JobGraphErrBody
)

// JobGraphError mirrors event_streaming::runtime::flink::job_graph::JobGraphError.
//
// The Kind field discriminates between the Rust enum variants; Status
// carries the HTTP status code for `Status` errors; Cause carries the
// wrapped HTTP error for `HTTP` errors.
type JobGraphError struct {
	Kind    JobGraphErrorKind
	Status  int
	Message string
	Cause   error
}

func (e *JobGraphError) Error() string {
	switch e.Kind {
	case JobGraphErrNotDeployed:
		return "missing flink_deployment_name on topology"
	case JobGraphErrStatus:
		return fmt.Sprintf("flink jobmanager returned status %d", e.Status)
	case JobGraphErrHTTP:
		return fmt.Sprintf("http: %v", e.Cause)
	case JobGraphErrBody:
		return fmt.Sprintf("invalid response: %s", e.Message)
	default:
		if e.Message != "" {
			return e.Message
		}
		return "unknown job graph error"
	}
}

func (e *JobGraphError) Unwrap() error { return e.Cause }

// ErrJobGraphNotDeployed is the sentinel for the `NotDeployed` variant.
// Returned by the handler layer, kept here so callers can switch on it.
var ErrJobGraphNotDeployed = &JobGraphError{Kind: JobGraphErrNotDeployed}

// JobGraphVertex mirrors the cytoscape-friendly vertex projection
// produced by `normalise`.
type JobGraphVertex struct {
	ID          any `json:"id"`
	Name        any `json:"name"`
	Parallelism any `json:"parallelism"`
	Status      any `json:"status"`
}

// JobGraphEdge mirrors the cytoscape-friendly edge projection produced
// by `normalise`. Edges are derived from the Flink JSON `plan.nodes[].inputs`
// adjacency list.
type JobGraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// JobGraph is the normalised front-end-friendly job graph.
//
// Equivalent to the JSON object the Rust `normalise` returns:
//
//	{ "job_id": "...", "vertices": [...], "edges": [...], "raw": {...} }
//
// Raw is the verbatim Flink response, preserved for debugging.
type JobGraph struct {
	JobID    string                 `json:"job_id"`
	Vertices []JobGraphVertex       `json:"vertices"`
	Edges    []JobGraphEdge         `json:"edges"`
	Raw      map[string]any         `json:"raw"`
}

// HTTPDoer is the minimal HTTP client surface FetchJobGraph needs. Lets
// tests inject a fake without spinning up a real server.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DefaultHTTPClient is the production client used when callers pass nil.
// 10 s timeout matches the Rust `reqwest` builder.
func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// FetchJobGraph ports event_streaming::runtime::flink::job_graph::fetch_job_graph.
//
// It hits the JobManager REST API and returns the normalised cytoscape
// payload. When jobID is empty the function discovers the running job
// off `/jobs` (preferring `RUNNING`, falling back to the first entry).
func FetchJobGraph(ctx context.Context, client HTTPDoer, cfg FlinkRuntimeConfig, deployment, namespace, jobID string) (*JobGraph, error) {
	if client == nil {
		client = DefaultHTTPClient()
	}
	base := cfg.JobManagerURL(deployment, namespace)
	resolved := jobID
	if resolved == "" {
		discovered, err := discoverJob(ctx, client, base)
		if err != nil {
			return nil, err
		}
		resolved = discovered
	}
	url := base + "/jobs/" + resolved
	raw, err := getJSON(ctx, client, url)
	if err != nil {
		return nil, err
	}
	return Normalise(raw, resolved), nil
}

func discoverJob(ctx context.Context, client HTTPDoer, base string) (string, error) {
	body, err := getJSON(ctx, client, base+"/jobs")
	if err != nil {
		return "", err
	}
	rawJobs, ok := body["jobs"].([]any)
	if !ok {
		return "", &JobGraphError{Kind: JobGraphErrBody, Message: "no 'jobs' array"}
	}
	// Prefer the first RUNNING job, falling back to the first entry —
	// matches the Rust source's `find().or_else(first())` chain.
	var chosen map[string]any
	for _, entry := range rawJobs {
		obj, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if status, _ := obj["status"].(string); status == "RUNNING" {
			chosen = obj
			break
		}
	}
	if chosen == nil && len(rawJobs) > 0 {
		if obj, ok := rawJobs[0].(map[string]any); ok {
			chosen = obj
		}
	}
	if chosen == nil {
		return "", &JobGraphError{Kind: JobGraphErrBody, Message: "no jobs reported"}
	}
	id, ok := chosen["id"].(string)
	if !ok || id == "" {
		return "", &JobGraphError{Kind: JobGraphErrBody, Message: "no jobs reported"}
	}
	return id, nil
}

// Normalise plucks the cytoscape-friendly subset out of a Flink
// `/jobs/{id}` response. Pure, no I/O — exposed so callers (and tests)
// can reuse it without hitting the network.
func Normalise(raw map[string]any, jobID string) *JobGraph {
	vertices := make([]JobGraphVertex, 0)
	if rawVerts, ok := raw["vertices"].([]any); ok {
		for _, v := range rawVerts {
			obj, ok := v.(map[string]any)
			if !ok {
				continue
			}
			vertices = append(vertices, JobGraphVertex{
				ID:          obj["id"],
				Name:        obj["name"],
				Parallelism: obj["parallelism"],
				Status:      obj["status"],
			})
		}
	}
	edges := make([]JobGraphEdge, 0)
	if plan, ok := raw["plan"].(map[string]any); ok {
		if nodes, ok := plan["nodes"].([]any); ok {
			for _, n := range nodes {
				node, ok := n.(map[string]any)
				if !ok {
					continue
				}
				target, ok := node["id"].(string)
				if !ok {
					continue
				}
				inputs, ok := node["inputs"].([]any)
				if !ok {
					continue
				}
				for _, in := range inputs {
					inputObj, ok := in.(map[string]any)
					if !ok {
						continue
					}
					if src, ok := inputObj["id"].(string); ok {
						edges = append(edges, JobGraphEdge{Source: src, Target: target})
					}
				}
			}
		}
	}
	return &JobGraph{
		JobID:    jobID,
		Vertices: vertices,
		Edges:    edges,
		Raw:      raw,
	}
}

func getJSON(ctx context.Context, client HTTPDoer, url string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &JobGraphError{Kind: JobGraphErrHTTP, Cause: err}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &JobGraphError{Kind: JobGraphErrHTTP, Cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &JobGraphError{Kind: JobGraphErrStatus, Status: resp.StatusCode}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &JobGraphError{Kind: JobGraphErrHTTP, Cause: err}
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, &JobGraphError{Kind: JobGraphErrBody, Message: err.Error()}
	}
	return obj, nil
}

// IsNotDeployed reports whether err is the NotDeployed sentinel — used
// by the REST handler to map to a 409.
func IsNotDeployed(err error) bool {
	var jg *JobGraphError
	if errors.As(err, &jg) {
		return jg.Kind == JobGraphErrNotDeployed
	}
	return false
}
