package reconcile

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/models"
)

// sampleSpec mirrors Rust `control_plane::tests::sample_spec`.
func sampleSpec() *IngestJobSpec {
	return &IngestJobSpec{
		Name:                "orders",
		Namespace:           "data",
		Source:              "postgres",
		KafkaConnectCluster: "main-connect",
		Postgres: &PostgresSource{
			Hostname:        "pg.example.com",
			Port:            0,
			Database:        "shop",
			User:            "debezium",
			PasswordSecret:  "pg-password",
			SlotName:        "",
			PublicationName: "",
			Tables:          []string{"public.orders", "public.line_items"},
			TopicPrefix:     "",
		},
		IcebergSink: &IcebergSink{
			Warehouse:    "s3://lake/warehouse",
			CatalogName:  "lake",
			Database:     "ops",
			Table:        "orders",
			FlinkImage:   "",
			FlinkVersion: "",
		},
	}
}

func TestRendersPostgresDebeziumConnector(t *testing.T) {
	rendered, err := RenderResources(sampleSpec())
	if err != nil {
		t.Fatalf("RenderResources returned error: %v", err)
	}
	kc := rendered.KafkaConnector
	if got, want := kc.Metadata.Name, "orders-debezium-pg"; got != want {
		t.Errorf("metadata.name = %q, want %q", got, want)
	}
	if got, want := kc.Metadata.Namespace, "data"; got != want {
		t.Errorf("metadata.namespace = %q, want %q", got, want)
	}
	if got, want := kc.Metadata.Labels["strimzi.io/cluster"], "main-connect"; got != want {
		t.Errorf("labels[strimzi.io/cluster] = %q, want %q", got, want)
	}
	if got, want := kc.Spec.Class, "io.debezium.connector.postgresql.PostgresConnector"; got != want {
		t.Errorf("spec.class = %q, want %q", got, want)
	}
	if kc.Spec.TasksMax != 1 {
		t.Errorf("spec.tasksMax = %d, want 1", kc.Spec.TasksMax)
	}
	cfg := kc.Spec.Config
	if v, _ := cfg.get("database.hostname"); v != "pg.example.com" {
		t.Errorf("config[database.hostname] = %v, want pg.example.com", v)
	}
	if v, _ := cfg.get("database.port"); v != "5432" {
		t.Errorf("config[database.port] = %v, want \"5432\" (default applied)", v)
	}
	if v, _ := cfg.get("plugin.name"); v != "pgoutput" {
		t.Errorf("config[plugin.name] = %v, want pgoutput", v)
	}
	if v, _ := cfg.get("table.include.list"); v != "public.orders,public.line_items" {
		t.Errorf("config[table.include.list] = %v, want public.orders,public.line_items", v)
	}
	pw, _ := cfg.get("database.password")
	pwStr, _ := pw.(string)
	if !strings.Contains(pwStr, "pg-password") {
		t.Errorf("config[database.password] = %q, expected to contain pg-password", pwStr)
	}
}

func TestRendersIcebergFlinkDeployment(t *testing.T) {
	rendered, err := RenderResources(sampleSpec())
	if err != nil {
		t.Fatalf("RenderResources returned error: %v", err)
	}
	flink := rendered.FlinkDeployment
	if flink == nil {
		t.Fatal("iceberg sink should produce a FlinkDeployment")
	}
	if got, want := flink.Metadata.Name, "orders-iceberg-sink"; got != want {
		t.Errorf("metadata.name = %q, want %q", got, want)
	}
	if got, want := flink.Spec.Image, DefaultFlinkImage; got != want {
		t.Errorf("spec.image = %q, want default %q", got, want)
	}
	if got, want := flink.Spec.FlinkVersion, DefaultFlinkVersion; got != want {
		t.Errorf("spec.flinkVersion = %q, want default %q", got, want)
	}
	hasArg := func(arg string) bool {
		for _, a := range flink.Spec.Job.Args {
			if a == arg {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"s3://lake/warehouse", "ops", "orders"} {
		if !hasArg(want) {
			t.Errorf("flink job args missing %q (got %v)", want, flink.Spec.Job.Args)
		}
	}
}

func TestNoFlinkWhenNoIcebergSink(t *testing.T) {
	spec := sampleSpec()
	spec.IcebergSink = nil
	rendered, err := RenderResources(spec)
	if err != nil {
		t.Fatalf("RenderResources returned error: %v", err)
	}
	if rendered.FlinkDeployment != nil {
		t.Errorf("FlinkDeployment = %+v, want nil", rendered.FlinkDeployment)
	}
}

func TestRejectsUnsupportedSource(t *testing.T) {
	spec := sampleSpec()
	spec.Source = "mysql"
	_, err := RenderResources(spec)
	if err == nil || !strings.Contains(err.Error(), "unsupported source") {
		t.Fatalf("expected error containing 'unsupported source', got %v", err)
	}
}

func TestRejectsPostgresWithoutBlock(t *testing.T) {
	spec := sampleSpec()
	spec.Postgres = nil
	_, err := RenderResources(spec)
	if err == nil || !strings.Contains(err.Error(), "postgres") {
		t.Fatalf("expected error containing 'postgres', got %v", err)
	}
}

func TestRejectsEmptyName(t *testing.T) {
	spec := sampleSpec()
	spec.Name = ""
	_, err := RenderResources(spec)
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("expected error containing 'name', got %v", err)
	}
}

func TestHTTPApplierApplyPostsRendered(t *testing.T) {
	var capturedPath, capturedMethod string
	var capturedBody applyRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &capturedBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	specBytes, err := json.Marshal(sampleSpec())
	if err != nil {
		t.Fatalf("marshal sample spec: %v", err)
	}
	job := &models.IngestJob{
		ID:        uuid.New(),
		Name:      "orders",
		Namespace: "data",
		Spec:      specBytes,
	}
	applier := &HTTPApplier{BaseURL: srv.URL}
	kc, fl, err := applier.Apply(context.Background(), job)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if kc != "orders-debezium-pg" {
		t.Errorf("kafkaConnector name = %q, want orders-debezium-pg", kc)
	}
	if fl != "orders-iceberg-sink" {
		t.Errorf("flinkDeployment name = %q, want orders-iceberg-sink", fl)
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/v1/apply" {
		t.Errorf("path = %q, want /v1/apply", capturedPath)
	}
	if capturedBody.Namespace != "data" {
		t.Errorf("body.namespace = %q, want data", capturedBody.Namespace)
	}
	if capturedBody.KafkaConnector.Metadata.Name != "orders-debezium-pg" {
		t.Errorf("body.kafka_connector.metadata.name = %q", capturedBody.KafkaConnector.Metadata.Name)
	}
	if capturedBody.FlinkDeployment == nil || capturedBody.FlinkDeployment.Metadata.Name != "orders-iceberg-sink" {
		t.Errorf("body.flink_deployment metadata mismatch: %+v", capturedBody.FlinkDeployment)
	}
}

func TestHTTPApplierApplyPropagatesNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	specBytes, _ := json.Marshal(sampleSpec())
	job := &models.IngestJob{
		ID:   uuid.New(),
		Name: "orders",
		Spec: specBytes,
	}
	applier := &HTTPApplier{BaseURL: srv.URL}
	_, _, err := applier.Apply(context.Background(), job)
	if err == nil {
		t.Fatal("expected error from non-2xx response, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error = %v, want it to mention HTTP 500", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %v, want it to wrap response body 'boom'", err)
	}
}

func TestHTTPApplierDeleteIgnores404(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	applier := &HTTPApplier{BaseURL: srv.URL}
	if err := applier.Delete(context.Background(), "data", "orders-debezium-pg", "orders-iceberg-sink"); err != nil {
		t.Fatalf("Delete returned error on 404 (want nil): %v", err)
	}
	if hits != 2 {
		t.Errorf("expected 2 DELETE hits (kc + flink), got %d", hits)
	}
}
