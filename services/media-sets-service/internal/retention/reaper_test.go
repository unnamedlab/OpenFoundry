package retention

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/mediapath"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/metrics"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/storage"
)

// fakeBackend records the keys handleExpired asks it to delete.
type fakeBackend struct {
	deletes []string
	failNth int
	calls   int
}

func (b *fakeBackend) Bucket() string { return "media" }
func (b *fakeBackend) PresignUpload(_ context.Context, _ mediapath.Key, _ string, _ time.Duration) (*storage.PresignedURL, error) {
	return nil, nil
}
func (b *fakeBackend) PresignDownload(_ context.Context, _ mediapath.Key, _ time.Duration) (*storage.PresignedURL, error) {
	return nil, nil
}
func (b *fakeBackend) Delete(_ context.Context, k mediapath.Key) error {
	b.calls++
	if b.failNth > 0 && b.calls == b.failNth {
		return errors.New("fake delete error")
	}
	b.deletes = append(b.deletes, k.ObjectKey())
	return nil
}

func newReaper(t *testing.T, be storage.Backend) (*Reaper, *metrics.Metrics) {
	t.Helper()
	m := metrics.New(observability.NewMetrics())
	// Discarded logger so test output stays clean.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(nil, be, m, log, time.Minute), m
}

func readCounter(t *testing.T, c interface{ Write(*dto.Metric) error }) float64 {
	t.Helper()
	var pb dto.Metric
	require.NoError(t, c.Write(&pb))
	return pb.GetCounter().GetValue()
}

func TestHandleExpiredBumpsCounterAndDeletesBytes(t *testing.T) {
	t.Parallel()
	be := &fakeBackend{}
	r, m := newReaper(t, be)

	expired := []repo.ExpiredItem{
		{RID: "ri.item.1", MediaSetRID: "ri.set.1", Branch: "main", SHA256: "deadbeef", SizeBytes: 1024},
		{RID: "ri.item.2", MediaSetRID: "ri.set.1", Branch: "main", SHA256: "cafebabe", SizeBytes: 2048},
	}
	r.handleExpired(context.Background(), expired)

	assert.Equal(t, 2, len(be.deletes))
	assert.InDelta(t, 2.0, readCounter(t, m.MediaRetentionPurgesTotal), 0.0001)
}

func TestHandleExpiredSkipsEmptySha256(t *testing.T) {
	t.Parallel()
	be := &fakeBackend{}
	r, m := newReaper(t, be)

	expired := []repo.ExpiredItem{
		{RID: "ri.item.1", MediaSetRID: "ri.set.1", Branch: "main", SHA256: ""},
		{RID: "ri.item.2", MediaSetRID: "ri.set.1", Branch: "main", SHA256: "abc"},
	}
	r.handleExpired(context.Background(), expired)

	// Only the row with sha256 hits the storage backend; both rows
	// still bump the counter (they were soft-deleted regardless).
	assert.Equal(t, 1, len(be.deletes))
	assert.InDelta(t, 2.0, readCounter(t, m.MediaRetentionPurgesTotal), 0.0001)
}

func TestHandleExpiredTolerantOfDeleteFailure(t *testing.T) {
	t.Parallel()
	be := &fakeBackend{failNth: 1}
	r, m := newReaper(t, be)

	expired := []repo.ExpiredItem{
		{RID: "ri.item.1", MediaSetRID: "ri.set.1", Branch: "main", SHA256: "abc"},
		{RID: "ri.item.2", MediaSetRID: "ri.set.1", Branch: "main", SHA256: "def"},
	}
	// First Delete returns an error — we still process item 2 and
	// bump the counter to 2. The fakeBackend records only the
	// successful delete (item 2).
	r.handleExpired(context.Background(), expired)
	assert.Equal(t, 1, len(be.deletes))
	assert.InDelta(t, 2.0, readCounter(t, m.MediaRetentionPurgesTotal), 0.0001)
}

func TestHandleExpiredEmptyBatchIsNoOp(t *testing.T) {
	t.Parallel()
	be := &fakeBackend{}
	r, m := newReaper(t, be)
	r.handleExpired(context.Background(), nil)
	assert.Equal(t, 0, be.calls)
	assert.InDelta(t, 0.0, readCounter(t, m.MediaRetentionPurgesTotal), 0.0001)
}

func TestNewAppliesDefaultsForIntervalAndLogger(t *testing.T) {
	t.Parallel()
	m := metrics.New(observability.NewMetrics())
	r := New(nil, &fakeBackend{}, m, nil, 0)
	assert.Equal(t, time.Minute, r.Interval)
	assert.NotNil(t, r.Logger)
}
