package lineagedeletion

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// inMemoryDeps mirrors the Rust `InMemoryDeps` test fixture.
type inMemoryDeps struct {
	mu                sync.Mutex
	now               time.Time
	policies          []models.RetentionPolicySnapshot
	targets           map[uuid.UUID][]models.RetentionTarget
	markedAt          map[string]time.Time
	retired           []models.RetentionTarget
	physicallyDeleted []string
	published         []models.RetentionAppliedEvent
}

func key(rid string, txn uuid.UUID) string { return rid + "|" + txn.String() }

func (d *inMemoryDeps) ListActivePolicies(_ context.Context) ([]models.RetentionPolicySnapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]models.RetentionPolicySnapshot(nil), d.policies...), nil
}

func (d *inMemoryDeps) EnumerateTargets(_ context.Context, p *models.RetentionPolicySnapshot) ([]models.RetentionTarget, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]models.RetentionTarget(nil), d.targets[p.ID]...), nil
}

func (d *inMemoryDeps) OpenDeleteAndRetire(_ context.Context, target *models.RetentionTarget) (uuid.UUID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.retired = append(d.retired, *target)
	if _, ok := d.markedAt[key(target.DatasetRid, target.TransactionID)]; !ok {
		d.markedAt[key(target.DatasetRid, target.TransactionID)] = d.now
	}
	return uuid.New(), nil
}

func (d *inMemoryDeps) PhysicalDelete(_ context.Context, fileRefs []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.physicallyDeleted = append(d.physicallyDeleted, fileRefs...)
	return nil
}

func (d *inMemoryDeps) PublishApplied(_ context.Context, event *models.RetentionAppliedEvent) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.published = append(d.published, *event)
	return nil
}

func (d *inMemoryDeps) Now() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.now
}

func (d *inMemoryDeps) DeletionMarkedAt(_ context.Context, rid string, txn uuid.UUID) (*time.Time, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.markedAt[key(rid, txn)]
	if !ok {
		return nil, nil
	}
	return &t, nil
}

func fixture(grace int32, preMarked bool) *inMemoryDeps {
	policy := models.RetentionPolicySnapshot{
		ID:                 uuid.New(),
		Name:               "DELETE_ABORTED_TRANSACTIONS",
		IsSystem:           true,
		GracePeriodMinutes: grace,
	}
	rid := "ri.foundry.main.dataset.abc"
	target := models.RetentionTarget{
		DatasetRid:    rid,
		TransactionID: uuid.New(),
		FileRefs:      []string{"s3://b/a.parquet", "s3://b/b.parquet"},
		Bytes:         1024,
	}
	now := time.Now().UTC()
	marked := map[string]time.Time{}
	if preMarked {
		marked[key(target.DatasetRid, target.TransactionID)] = now.Add(-24 * time.Hour)
	}
	return &inMemoryDeps{
		now:      now,
		policies: []models.RetentionPolicySnapshot{policy},
		targets:  map[uuid.UUID][]models.RetentionTarget{policy.ID: {target}},
		markedAt: marked,
	}
}

func TestFirstPassMarksFilesButSkipsPhysicalDeleteWithinGrace(t *testing.T) {
	t.Parallel()
	deps := fixture(60, false)
	results, err := RunOnce(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results", len(results))
	}
	r := results[0]
	if r.TargetsProcessed != 1 || r.FilesMarked != 2 {
		t.Fatalf("targets/files: %+v", r)
	}
	if r.PhysicalDeletes != 0 {
		t.Fatal("must not delete within grace")
	}
	if r.PhysicalDeleteSkippedGrace != 1 {
		t.Fatalf("expected one skip, got %d", r.PhysicalDeleteSkippedGrace)
	}
	if len(deps.physicallyDeleted) != 0 {
		t.Fatal("storage should not be touched within grace")
	}
	if len(deps.published) != 1 || deps.published[0].PhysicallyDeleted {
		t.Fatalf("publish should fire with physically_deleted=false, got %+v", deps.published)
	}
}

func TestSecondPassAfterGracePhysicallyDeletesFiles(t *testing.T) {
	t.Parallel()
	deps := fixture(60, true)
	results, err := RunOnce(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	r := results[0]
	if r.PhysicalDeletes != 1 {
		t.Fatalf("expected 1 delete, got %d", r.PhysicalDeletes)
	}
	if r.PhysicalDeleteSkippedGrace != 0 {
		t.Fatalf("got %d skips", r.PhysicalDeleteSkippedGrace)
	}
	if len(deps.physicallyDeleted) != 2 {
		t.Fatalf("expected 2 physical deletes, got %d", len(deps.physicallyDeleted))
	}
	if !deps.published[0].PhysicallyDeleted {
		t.Fatal("publish must report physically_deleted=true")
	}
}

func TestZeroGraceDeletesInSamePass(t *testing.T) {
	t.Parallel()
	deps := fixture(0, true)
	results, err := RunOnce(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].PhysicalDeletes != 1 {
		t.Fatalf("expected 1 delete, got %d", results[0].PhysicalDeletes)
	}
}

func TestRunLoopWithZeroIntervalReturnsImmediately(t *testing.T) {
	t.Parallel()
	deps := fixture(60, false)
	// Should not block — zero interval is the disable signal.
	RunLoop(context.Background(), deps, 0)
}
