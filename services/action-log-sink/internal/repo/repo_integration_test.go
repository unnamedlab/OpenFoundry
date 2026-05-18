//go:build integration

// Integration tests for the action-log-sink Postgres path.
//
// Boots an ephemeral postgres:16-alpine via libs/testing.BootPostgres
// and exercises the repo + handlers end-to-end.
package repo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/envelope"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/repo"
)

func mkEnvelope(eventID, tenant, actor, action string, appliedAtMs int64) envelope.ActionEnvelope {
	params := `{"reason":"ok"}`
	objID := "obj-" + eventID
	return envelope.ActionEnvelope{
		EventID:      eventID,
		ActionTypeID: "atype-1",
		ActionName:   action,
		ObjectTypeID: "otype-1",
		ObjectID:     &objID,
		Tenant:       tenant,
		ActorSub:     actor,
		Status:       "applied",
		Parameters:   &params,
		AppliedAtMs:  appliedAtMs,
	}
}

func TestRepo_InsertBatchAndQuery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pg := testingx.BootPostgres(ctx, t)
	require.NoError(t, repo.Migrate(ctx, pg.Pool))
	store := &repo.Repo{Pool: pg.Pool}

	base := time.Now().UTC().Add(-time.Hour).UnixMilli()
	batch := make([]envelope.ActionEnvelope, 0, 30)
	for i := 0; i < 20; i++ {
		batch = append(batch, mkEnvelope(fmt.Sprintf("tgt-%02d", i), "tenant-a", "actor-1", "approve", base+int64(i*1000)))
	}
	for i := 0; i < 10; i++ {
		batch = append(batch, mkEnvelope(fmt.Sprintf("noise-%02d", i), "tenant-b", "actor-2", "reject", base+int64(i*1000)))
	}

	inserted, err := store.InsertBatch(ctx, batch)
	require.NoError(t, err)
	require.Equal(t, 30, inserted)

	// Re-insert is idempotent — ON CONFLICT DO NOTHING.
	inserted, err = store.InsertBatch(ctx, batch)
	require.NoError(t, err)
	require.Equal(t, 0, inserted)

	// Filter by tenant + actor — must return exactly the 20 tgt-* rows.
	rows, _, err := store.Query(ctx, repo.QueryFilter{Tenant: "tenant-a", ActorSub: "actor-1"}, 100, nil)
	require.NoError(t, err)
	assert.Equal(t, 20, len(rows))
	for _, r := range rows {
		assert.Equal(t, "tenant-a", r.Tenant)
		assert.Equal(t, "actor-1", r.ActorSub)
	}

	// Get by event_id.
	row, err := store.Get(ctx, "tgt-05")
	require.NoError(t, err)
	assert.Equal(t, "tgt-05", row.EventID)
	assert.Equal(t, "approve", row.ActionName)

	// Not-found path.
	_, err = store.Get(ctx, "does-not-exist")
	assert.ErrorIs(t, err, repo.ErrNotFound)
}

func TestRepo_QueryPagination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pg := testingx.BootPostgres(ctx, t)
	require.NoError(t, repo.Migrate(ctx, pg.Pool))
	store := &repo.Repo{Pool: pg.Pool}

	base := time.Now().UTC().Add(-time.Hour).UnixMilli()
	batch := make([]envelope.ActionEnvelope, 0, 50)
	for i := 0; i < 50; i++ {
		batch = append(batch, mkEnvelope(fmt.Sprintf("e-%03d", i), "tenant-a", "actor-1", "approve", base+int64(i*1000)))
	}
	_, err := store.InsertBatch(ctx, batch)
	require.NoError(t, err)

	h := &handlers.Handlers{Repo: store}

	seen := make(map[string]struct{}, 50)
	cursor := ""
	for pages := 1; pages <= 5; pages++ {
		url := "/api/v1/action-log/events?tenant=tenant-a&page_size=20"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		h.QueryEvents(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "page %d body: %s", pages, rec.Body.String())

		var resp handlers.QueryEventsResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.NotEmpty(t, resp.Events, "page %d returned 0 events", pages)
		for _, e := range resp.Events {
			_, dup := seen[e.EventID]
			assert.False(t, dup, "duplicate event id across pages: %s", e.EventID)
			seen[e.EventID] = struct{}{}
		}
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	assert.Equal(t, 50, len(seen))
}

func TestRepo_RecordEventWriteThrough(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pg := testingx.BootPostgres(ctx, t)
	require.NoError(t, repo.Migrate(ctx, pg.Pool))
	store := &repo.Repo{Pool: pg.Pool}
	h := &handlers.Handlers{Repo: store}

	envBytes, err := json.Marshal(mkEnvelope("rt-1", "tenant-a", "actor-1", "approve", time.Now().UTC().UnixMilli()))
	require.NoError(t, err)
	body, err := json.Marshal(handlers.RecordEventRequest{Envelope: envBytes})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/action-log/events", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.RecordEvent(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp handlers.RecordEventResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "rt-1", resp.EventID)

	row, err := store.Get(ctx, "rt-1")
	require.NoError(t, err)
	assert.Equal(t, "approve", row.ActionName)
}
