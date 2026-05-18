package repo

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/report-service/internal/handlers"
)

func TestRepoIntegrationCreateGenerateList(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	r := New(pool)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	def := handlers.ReportDefinition{
		ID:            uuid.NewString(),
		Name:          "Integration Report",
		Owner:         "qa",
		GeneratorKind: "pdf",
		DatasetName:   "integration_dataset",
		Template:      handlers.ReportTemplate{Title: "Integration Report"},
		Schedule:      handlers.ReportSchedule{Cadence: "manual", Timezone: "UTC"},
		Tags:          []string{},
		Parameters:    map[string]any{},
		Recipients:    []handlers.DistributionRecipient{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	created, err := r.CreateDefinition(ctx, def)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != def.ID {
		t.Fatalf("id mismatch: %s != %s", created.ID, def.ID)
	}
	exec := handlers.ReportExecution{
		ID:            uuid.NewString(),
		ReportID:      def.ID,
		ReportName:    def.Name,
		Status:        "succeeded",
		GeneratorKind: def.GeneratorKind,
		TriggeredBy:   "test",
		GeneratedAt:   now,
		CompletedAt:   &now,
		Preview:       handlers.ReportExecutionPreview{Headline: def.Name},
		Artifact:      handlers.ReportArtifact{FileName: "integration.pdf", MimeType: "application/pdf", StorageURL: "of://test"},
		Distributions: []handlers.DistributionResult{},
		Metrics:       handlers.ReportExecutionMetrics{DurationMS: 1, RowCount: 1, SectionCount: 1},
	}
	if err := r.SaveExecution(ctx, exec); err != nil {
		t.Fatal(err)
	}
	execs, err := r.ListExecutions(ctx, def.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(execs) == 0 || execs[0].ID != exec.ID {
		t.Fatalf("execution not listed: %+v", execs)
	}
}
