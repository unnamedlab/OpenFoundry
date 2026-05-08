package flink

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/domain"
)

func sampleTopology(t *testing.T) *domain.TopologyDefinition {
	t.Helper()
	id := uuid.New()
	return &domain.TopologyDefinition{
		ID:                   id,
		Name:                 "demo",
		Status:               "active",
		Nodes:                nil,
		Edges:                nil,
		BackpressurePolicy:   domain.DefaultBackpressurePolicy(),
		StateBackend:         "rocksdb",
		CheckpointIntervalMS: 60_000,
		RuntimeKind:          "flink",
		ConsistencyGuarantee: "exactly-once",
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	}
}

// TestManifestCarriesTopologyMetadataAndCheckpointing ports
// deployer::tests::manifest_carries_topology_metadata_and_checkpointing.
func TestManifestCarriesTopologyMetadataAndCheckpointing(t *testing.T) {
	cfg := FlinkRuntimeConfig{
		DefaultNamespace:      "flink",
		SQLRunnerImage:        "ghcr.io/x/sql-runner:1.19",
		FlinkVersion:          "v1_19",
		JobManagerURLTemplate: "http://{deployment}-rest.{namespace}.svc:8081",
		MetricsPollIntervalMS: 15_000,
		StateBucketURI:        "s3://bucket/flink",
	}
	topology := sampleTopology(t)
	manifest := RenderFlinkDeploymentManifest(cfg, "flink", "topo-demo", topology, nil)
	if manifest["kind"] != "FlinkDeployment" {
		t.Fatalf("kind = %v", manifest["kind"])
	}
	meta := manifest["metadata"].(map[string]any)
	labels := meta["labels"].(map[string]any)
	if labels["openfoundry.io/topology-id"] != topology.ID.String() {
		t.Fatalf("topology-id = %v", labels["openfoundry.io/topology-id"])
	}
	if labels["openfoundry.io/topology-name"] != "demo" {
		t.Fatalf("topology-name = %v", labels["openfoundry.io/topology-name"])
	}
	spec := manifest["spec"].(map[string]any)
	flinkConfig := spec["flinkConfiguration"].(map[string]any)
	if flinkConfig["execution.checkpointing.mode"] != "EXACTLY_ONCE" {
		t.Fatalf("checkpointing mode = %v", flinkConfig["execution.checkpointing.mode"])
	}
	if flinkConfig["state.checkpoints.dir"] != "s3://bucket/flink/checkpoints/topo-demo" {
		t.Fatalf("checkpoints dir = %v", flinkConfig["state.checkpoints.dir"])
	}
	if flinkConfig["execution.checkpointing.interval"] != "60000" {
		t.Fatalf("checkpointing interval = %v", flinkConfig["execution.checkpointing.interval"])
	}
	job := spec["job"].(map[string]any)
	if job["jarURI"] != "local:///opt/flink/usrlib/sql-runner.jar" {
		t.Fatalf("jarURI = %v", job["jarURI"])
	}
}

func TestManifestSwitchesToAtLeastOnceWhenNotExactlyOnce(t *testing.T) {
	cfg := FlinkRuntimeConfig{
		DefaultNamespace:      "flink",
		SQLRunnerImage:        "img",
		FlinkVersion:          "v1_19",
		JobManagerURLTemplate: "http://x",
		MetricsPollIntervalMS: 1000,
		StateBucketURI:        "s3://b",
	}
	topology := sampleTopology(t)
	topology.ConsistencyGuarantee = "at-least-once"
	manifest := RenderFlinkDeploymentManifest(cfg, "ns", "dep", topology, nil)
	mode := manifest["spec"].(map[string]any)["flinkConfiguration"].(map[string]any)["execution.checkpointing.mode"]
	if mode != "AT_LEAST_ONCE" {
		t.Fatalf("mode = %v, want AT_LEAST_ONCE", mode)
	}
}

func TestRenderSQLConfigMapShapesPayload(t *testing.T) {
	cm := RenderSQLConfigMap("flink", "topo-x", "SELECT 1;")
	if cm["kind"] != "ConfigMap" {
		t.Fatalf("kind = %v", cm["kind"])
	}
	meta := cm["metadata"].(map[string]any)
	if meta["name"] != "topo-x-sql" {
		t.Fatalf("name = %v", meta["name"])
	}
	if meta["namespace"] != "flink" {
		t.Fatalf("namespace = %v", meta["namespace"])
	}
	labels := meta["labels"].(map[string]any)
	if labels["app.kubernetes.io/managed-by"] != FieldManager {
		t.Fatalf("managed-by = %v", labels["app.kubernetes.io/managed-by"])
	}
	data := cm["data"].(map[string]any)
	if data["topology.sql"] != "SELECT 1;" {
		t.Fatalf("topology.sql = %v", data["topology.sql"])
	}
}

func TestSanitizeLabelReplacesIllegalCharsAndCapsAt63(t *testing.T) {
	long := strings.Repeat("a", 100)
	if got := sanitizeLabel(long); len(got) != 63 {
		t.Fatalf("length = %d, want 63", len(got))
	}
	if got := sanitizeLabel("ok name/with junk!"); got != "ok-name-with-junk-" {
		t.Fatalf("sanitizeLabel = %q", got)
	}
	allowed := "AbZ-_.09"
	if got := sanitizeLabel(allowed); got != allowed {
		t.Fatalf("expected %q to round-trip, got %q", allowed, got)
	}
}

type recordingApplier struct {
	mu       sync.Mutex
	applies  []applyCall
	deletes  []deleteCall
	applyErr error
}

type applyCall struct {
	namespace string
	kind      string
	name      string
	manifest  map[string]any
}

type deleteCall struct {
	namespace string
	kind      string
	name      string
}

func (r *recordingApplier) Apply(_ context.Context, namespace, kind, name string, manifest map[string]any) error {
	if r.applyErr != nil {
		return r.applyErr
	}
	r.mu.Lock()
	r.applies = append(r.applies, applyCall{namespace, kind, name, manifest})
	r.mu.Unlock()
	return nil
}

func (r *recordingApplier) Delete(_ context.Context, namespace, kind, name string) error {
	r.mu.Lock()
	r.deletes = append(r.deletes, deleteCall{namespace, kind, name})
	r.mu.Unlock()
	return nil
}

type recordingCoordsRecorder struct {
	mu        sync.Mutex
	calls     []recorderCall
	returnErr error
}

type recorderCall struct {
	topologyID, deployment, namespace string
}

func (r *recordingCoordsRecorder) RecordTopologyDeployment(_ context.Context, topologyID, deployment, namespace string) error {
	r.mu.Lock()
	r.calls = append(r.calls, recorderCall{topologyID, deployment, namespace})
	r.mu.Unlock()
	return r.returnErr
}

func TestDeployTopologyAppliesConfigMapAndFlinkDeploymentAndRecordsCoords(t *testing.T) {
	topology := sampleTopology(t)
	dep := "topo-demo"
	ns := "flink-runtime"
	topology.FlinkDeploymentName = &dep
	topology.FlinkNamespace = &ns
	cfg := FlinkRuntimeConfig{
		DefaultNamespace:      "default-ns",
		SQLRunnerImage:        "img",
		FlinkVersion:          "v1_19",
		JobManagerURLTemplate: "http://x",
		StateBucketURI:        "s3://b",
	}
	applier := &recordingApplier{}
	rec := &recordingCoordsRecorder{}
	d := &Deployer{Applier: applier, Recorder: rec}
	report, err := d.DeployTopology(context.Background(), cfg, topology, nil)
	if err != nil {
		t.Fatalf("DeployTopology: %v", err)
	}
	if report.Coords.DeploymentName != dep || report.Coords.Namespace != ns {
		t.Fatalf("coords = %+v", report.Coords)
	}
	if len(applier.applies) != 2 {
		t.Fatalf("expected 2 applies, got %d", len(applier.applies))
	}
	if applier.applies[0].kind != "ConfigMap" || applier.applies[0].name != dep+"-sql" {
		t.Fatalf("first apply = %+v", applier.applies[0])
	}
	if applier.applies[1].kind != "FlinkDeployment" || applier.applies[1].name != dep {
		t.Fatalf("second apply = %+v", applier.applies[1])
	}
	if len(rec.calls) != 1 || rec.calls[0].deployment != dep || rec.calls[0].namespace != ns {
		t.Fatalf("recorder calls = %+v", rec.calls)
	}
}

func TestDeployTopologyDefaultsNamespaceAndDeployment(t *testing.T) {
	topology := sampleTopology(t)
	cfg := FlinkRuntimeConfig{
		DefaultNamespace: "fallback",
		StateBucketURI:   "s3://b",
	}
	applier := &recordingApplier{}
	d := &Deployer{Applier: applier}
	report, err := d.DeployTopology(context.Background(), cfg, topology, nil)
	if err != nil {
		t.Fatalf("DeployTopology: %v", err)
	}
	if report.Coords.Namespace != "fallback" {
		t.Fatalf("namespace = %q, want fallback", report.Coords.Namespace)
	}
	if !strings.HasPrefix(report.Coords.DeploymentName, "topo-") {
		t.Fatalf("deployment fallback = %q", report.Coords.DeploymentName)
	}
}

func TestDeployTopologyFailsWhenApplierMissing(t *testing.T) {
	d := &Deployer{}
	_, err := d.DeployTopology(context.Background(), FlinkRuntimeConfig{}, sampleTopology(t), nil)
	var de *DeployerError
	if !errors.As(err, &de) || de.Kind != DeployerErrKube {
		t.Fatalf("expected kube DeployerError, got %v", err)
	}
}

func TestDeleteTopologyDeletesBothResources(t *testing.T) {
	applier := &recordingApplier{}
	d := &Deployer{Applier: applier}
	err := d.DeleteTopology(context.Background(), FlinkJobCoords{DeploymentName: "topo-x", Namespace: "flink"})
	if err != nil {
		t.Fatalf("DeleteTopology: %v", err)
	}
	if len(applier.deletes) != 2 {
		t.Fatalf("expected 2 deletes, got %d", len(applier.deletes))
	}
	if applier.deletes[0].kind != "FlinkDeployment" || applier.deletes[0].name != "topo-x" {
		t.Fatalf("first delete = %+v", applier.deletes[0])
	}
	if applier.deletes[1].kind != "ConfigMap" || applier.deletes[1].name != "topo-x-sql" {
		t.Fatalf("second delete = %+v", applier.deletes[1])
	}
}
