package reconcile

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/models"
)

// stubApplier is a JobApplier double for tests that want to assert wiring
// without driving the real HTTP control plane.
type stubApplier struct {
	calls          int
	kafkaConnector string
	flinkDeploy    string
	err            error
}

func (s *stubApplier) Apply(ctx context.Context, job *models.IngestJob) (string, string, error) {
	s.calls++
	return s.kafkaConnector, s.flinkDeploy, s.err
}

func TestReconcilerUsesConfiguredApplier(t *testing.T) {
	stub := &stubApplier{kafkaConnector: "kc-1", flinkDeploy: "fd-1"}
	r := &Reconciler{Applier: stub}
	job := &models.IngestJob{ID: uuid.New(), Name: "orders"}
	kc, fl, err := r.Apply(context.Background(), job)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if stub.calls != 1 {
		t.Errorf("stub calls = %d, want 1", stub.calls)
	}
	if kc != "kc-1" || fl != "fd-1" {
		t.Errorf("Apply returned (%q, %q), want (kc-1, fd-1)", kc, fl)
	}
}

func TestReconcilerRequiresConfiguredApplier(t *testing.T) {
	r := &Reconciler{}
	job := &models.IngestJob{ID: uuid.New(), Name: "orders"}
	_, _, err := r.Apply(context.Background(), job)
	if err == nil {
		t.Fatal("expected missing applier error, got nil")
	}
}

func TestReconcilerSurfacesApplierError(t *testing.T) {
	want := errors.New("boom")
	r := &Reconciler{Applier: &stubApplier{err: want}}
	job := &models.IngestJob{ID: uuid.New(), Name: "orders"}
	_, _, err := r.Apply(context.Background(), job)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
