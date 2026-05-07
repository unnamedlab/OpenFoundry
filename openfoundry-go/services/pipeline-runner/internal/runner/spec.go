package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TransformSpec is the resolved transform plan returned by
// pipeline-build-service. The Scala port carried only `sql` and
// `format`; we keep the same minimal surface so the wire contract
// is unchanged. Future spec fields (input filters, output
// partitioning, pyspark/wasm transforms) extend this struct.
type TransformSpec struct {
	SQL    string `json:"sql"`
	Format string `json:"format"`
}

// specHTTPTimeout matches the 30-min start-to-close on the CR but
// is set to 30s here — the upstream is in-cluster and fast.
const specHTTPTimeout = 30 * time.Second

// ResolveSpec fetches the resolved transform from pipeline-build-service.
//
// On --smoke, on connection failure, on HTTP 404, or on a malformed
// body, it falls back to the smoke transform. Any other HTTP status
// is fatal. This matches the Scala behaviour byte-for-byte: callers
// rely on the smoke fallback to exercise the SparkApplication CR
// before pipeline-build-service has the spec endpoint wired up.
func ResolveSpec(ctx context.Context, args Args) (TransformSpec, error) {
	if args.Smoke {
		log(args, "smoke mode: using built-in 1-row transform")
		return SmokeSpec(args), nil
	}

	url := strings.TrimSuffix(args.PipelineBuildURL, "/") +
		"/api/v1/data-integration/pipelines/" + args.PipelineID +
		"/runs/" + args.RunID + "/spec"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return TransformSpec{}, fmt.Errorf("build spec request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: specHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		log(args, fmt.Sprintf("spec endpoint %s unreachable (%s); falling back to smoke", url, err))
		return SmokeSpec(args), nil
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			log(args, fmt.Sprintf("spec endpoint body read failed (%s); falling back to smoke", readErr))
			return SmokeSpec(args), nil
		}
		spec, parseErr := parseSpec(body)
		if parseErr != nil {
			log(args, "spec endpoint returned malformed body; falling back to smoke")
			return SmokeSpec(args), nil
		}
		return spec, nil
	case http.StatusNotFound:
		log(args, fmt.Sprintf("spec endpoint %s returned 404; falling back to smoke", url))
		return SmokeSpec(args), nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return TransformSpec{}, fmt.Errorf(
			"spec endpoint %s returned HTTP %d: %s",
			url, resp.StatusCode, strings.TrimSpace(string(body)),
		)
	}
}

// parseSpec accepts the same flat top-level object the Scala port
// did: `{"sql": "...", "format": "iceberg"}`. Unknown keys are
// ignored; a missing `sql` is a parse error.
func parseSpec(body []byte) (TransformSpec, error) {
	var spec TransformSpec
	if err := json.Unmarshal(body, &spec); err != nil {
		return TransformSpec{}, err
	}
	if strings.TrimSpace(spec.SQL) == "" {
		return TransformSpec{}, errors.New("spec missing required `sql` field")
	}
	if spec.Format == "" {
		spec.Format = "iceberg"
	}
	return spec, nil
}

// SmokeSpec is the built-in 1-row transform used when the spec
// endpoint is unreachable or `--smoke` is passed.
func SmokeSpec(args Args) TransformSpec {
	return TransformSpec{
		SQL: fmt.Sprintf(
			"SELECT CAST('%s' AS STRING) AS run_id, CAST(current_timestamp() AS TIMESTAMP) AS observed_at",
			escapeSQLLiteral(args.RunID),
		),
		Format: "iceberg",
	}
}

func escapeSQLLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
