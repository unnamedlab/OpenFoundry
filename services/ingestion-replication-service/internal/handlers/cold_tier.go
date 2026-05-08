package handlers

// HTTP bridge that ships archived stream branches to the
// dataset-versioning-service cold-tier endpoint. Mirrors the inline
// reqwest call in
// services/ingestion-replication-service/src/event_streaming/handlers/branches.rs
// (`commit_cold` block of archive_branch).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/domain/streambranch"
)

// HTTPColdTierBridge POSTs archive metadata to dataset-versioning-service.
// BaseURL is the upstream root (e.g. http://dataset-versioning:50080) —
// trailing slashes are trimmed so the rendered path mirrors the Rust
// `state.dataset_service_url.trim_end_matches('/')` invocation.
type HTTPColdTierBridge struct {
	BaseURL string
	Client  *http.Client
}

// CommitCold mirrors the Rust archive endpoint's POST to
// `{base}/api/v1/datasets/{stream_id}/branches/{dataset_branch_id}:commit`.
// Returns (accepted, err) — implementations should treat any err as a
// transient failure (the local row is already archived).
func (b *HTTPColdTierBridge) CommitCold(ctx context.Context, branch *streambranch.StreamBranch, archivedAt time.Time) (bool, error) {
	if branch == nil {
		return false, errors.New("nil branch")
	}
	if b.BaseURL == "" {
		return false, errors.New("cold tier base URL not configured")
	}
	if branch.DatasetBranchID == nil || *branch.DatasetBranchID == "" {
		return false, errors.New("branch has no dataset_branch_id")
	}
	url := fmt.Sprintf(
		"%s/api/v1/datasets/%s/branches/%s:commit",
		strings.TrimRight(b.BaseURL, "/"),
		branch.StreamID.String(),
		*branch.DatasetBranchID,
	)
	payload, err := json.Marshal(map[string]any{
		"stream_id":        branch.StreamID,
		"branch_name":      branch.Name,
		"head_sequence_no": branch.HeadSequenceNo,
		"marking":          nil,
		"archived_at":      archivedAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		return false, fmt.Errorf("marshal cold payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := b.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}
	return false, fmt.Errorf("cold tier returned status %d", resp.StatusCode)
}
