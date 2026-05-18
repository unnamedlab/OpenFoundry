//go:build integration

package repo_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/repo"
)

func TestPgStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	h := testingx.BootPostgres(ctx, t)
	require.NoError(t, repo.Migrate(ctx, h.Pool))

	store := repo.NewPgStore(h.Pool)

	// Create
	j := models.Job{
		ID:        uuid.New(),
		SourceURI: "s3://docs/contract.pdf",
		Pipeline:  "pdf-ocr",
		Status:    models.JobStatusQueued,
		Options:   json.RawMessage(`{"lang":"en"}`),
	}
	created, err := store.CreateJob(ctx, j)
	require.NoError(t, err)
	require.Equal(t, j.ID, created.ID)
	require.False(t, created.CreatedAt.IsZero())

	// Get
	got, err := store.GetJob(ctx, j.ID)
	require.NoError(t, err)
	assert.Equal(t, j.Pipeline, got.Pipeline)

	// List filters
	items, _, err := store.ListJobs(ctx, repo.ListJobsFilter{Status: models.JobStatusQueued})
	require.NoError(t, err)
	require.Len(t, items, 1)

	items, _, err = store.ListJobs(ctx, repo.ListJobsFilter{Status: models.JobStatusFailed})
	require.NoError(t, err)
	assert.Empty(t, items)

	// Update
	running := models.JobStatusRunning
	updated, err := store.UpdateJob(ctx, j.ID, models.UpdateJobRequest{Status: &running})
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusRunning, updated.Status)

	// Append event
	ev, err := store.AppendEvent(ctx, models.StatusEvent{
		ID: uuid.New(), JobID: j.ID, Status: models.JobStatusRunning,
	})
	require.NoError(t, err)
	require.False(t, ev.CreatedAt.IsZero())

	events, err := store.ListEvents(ctx, j.ID)
	require.NoError(t, err)
	require.Len(t, events, 1)

	// Record extraction
	conf := float32(0.85)
	ex, err := store.RecordExtraction(ctx, models.Extraction{
		ID: uuid.New(), JobID: j.ID,
		ExtractionKind: "text",
		Payload:        json.RawMessage(`{"chunks":4}`),
		Confidence:     &conf,
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, ex.ID)

	extractions, err := store.ListExtractions(ctx, j.ID)
	require.NoError(t, err)
	require.Len(t, extractions, 1)

	// AppendEvent FK violation when job missing.
	_, err = store.AppendEvent(ctx, models.StatusEvent{
		ID: uuid.New(), JobID: uuid.New(), Status: models.JobStatusRunning,
	})
	require.ErrorIs(t, err, domain.ErrNotFound)

	// DeleteJob cascades events + extractions.
	require.NoError(t, store.DeleteJob(ctx, j.ID))
	_, err = store.GetJob(ctx, j.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestPgStoreListJobsCursor(t *testing.T) {
	ctx := context.Background()
	h := testingx.BootPostgres(ctx, t)
	require.NoError(t, repo.Migrate(ctx, h.Pool))

	store := repo.NewPgStore(h.Pool)
	for i := 0; i < 7; i++ {
		_, err := store.CreateJob(ctx, models.Job{
			ID:        uuid.New(),
			SourceURI: "s3://docs/", Pipeline: "ocr", Status: models.JobStatusQueued,
			Options: json.RawMessage("{}"),
		})
		require.NoError(t, err)
	}

	page1, next, err := store.ListJobs(ctx, repo.ListJobsFilter{Limit: 3})
	require.NoError(t, err)
	require.Len(t, page1, 3)
	require.NotNil(t, next)

	page2, next2, err := store.ListJobs(ctx, repo.ListJobsFilter{Limit: 3, Cursor: next})
	require.NoError(t, err)
	require.Len(t, page2, 3)
	require.NotNil(t, next2)

	page3, next3, err := store.ListJobs(ctx, repo.ListJobsFilter{Limit: 3, Cursor: next2})
	require.NoError(t, err)
	require.Len(t, page3, 1)
	require.Nil(t, next3)
}
