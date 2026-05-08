package flink

// deployer ports event_streaming::runtime::flink::deployer.
//
// Materialises a topology as:
//
//  1. A ConfigMap/{deployment}-sql containing the rendered Flink SQL.
//  2. A FlinkDeployment/{deployment} running the sql-runner.jar image,
//     with args pointing at the ConfigMap.
//
// Both resources are upserted via server-side apply so the deployer is
// idempotent. The Rust source uses kube-rs directly; we keep the Go
// service free of the Kubernetes client dependency by abstracting the
// kube call behind a KubeApplier interface — same pattern HTTPApplier
// in internal/reconcile already uses for the ingest control plane.

import (
	"context"
	"fmt"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/domain"
)

// FieldManager mirrors deployer::FIELD_MANAGER.
const FieldManager = "event-streaming-service"

// DeploymentReport mirrors deployer::DeploymentReport. It carries the
// coordinates the caller persists back into streaming_topologies plus
// the rendered SQL the operator can preview.
type DeploymentReport struct {
	Coords FlinkJobCoords
	SQL    RenderedFlinkSQL
}

// DeployerErrorKind mirrors the Rust DeployerError thiserror enum.
type DeployerErrorKind int

const (
	DeployerErrUnknown DeployerErrorKind = iota
	DeployerErrKube
	DeployerErrDB
)

// DeployerError mirrors event_streaming::runtime::flink::deployer::DeployerError.
type DeployerError struct {
	Kind    DeployerErrorKind
	Message string
	Cause   error
}

func (e *DeployerError) Error() string {
	switch e.Kind {
	case DeployerErrKube:
		return "kube client: " + e.Message
	case DeployerErrDB:
		if e.Cause != nil {
			return fmt.Sprintf("database: %v", e.Cause)
		}
		return "database: " + e.Message
	default:
		if e.Message != "" {
			return e.Message
		}
		return "unknown deployer error"
	}
}

func (e *DeployerError) Unwrap() error { return e.Cause }

// KubeApplier abstracts the kube-rs server-side apply call. The
// production binding (when added) wraps a REST shim like the existing
// reconcile.HTTPApplier; tests inject a fake.
//
// Apply must be idempotent — multiple Apply calls with the same body
// must converge to the same cluster state. Delete is best-effort: a
// 404/missing-resource must NOT surface as an error.
type KubeApplier interface {
	Apply(ctx context.Context, namespace, kind, name string, manifest map[string]any) error
	Delete(ctx context.Context, namespace, kind, name string) error
}

// CoordsRecorder persists the deployment coordinates back into
// streaming_topologies. Mirrors the
//
//	UPDATE streaming_topologies
//	   SET flink_deployment_name = $2,
//	       flink_namespace       = $3,
//	       runtime_kind          = 'flink',
//	       updated_at            = now()
//	 WHERE id = $1
//
// statement the Rust source runs at the end of deploy_topology.
type CoordsRecorder interface {
	RecordTopologyDeployment(ctx context.Context, topologyID, deploymentName, namespace string) error
}

// Deployer wires the kube applier + DB recorder behind the public
// DeployTopology / DeleteTopology entry points that mirror the Rust
// helpers.
type Deployer struct {
	Applier  KubeApplier
	Recorder CoordsRecorder
}

// DeployTopology mirrors deployer::deploy_topology. It renders the
// topology to Flink SQL, applies the ConfigMap + FlinkDeployment, and
// records the coordinates back into streaming_topologies.
func (d *Deployer) DeployTopology(ctx context.Context, cfg FlinkRuntimeConfig, topology *domain.TopologyDefinition, streams []domain.DomainStreamDefinition) (DeploymentReport, error) {
	if d == nil || d.Applier == nil {
		return DeploymentReport{}, &DeployerError{Kind: DeployerErrKube, Message: "applier is not wired"}
	}
	if topology == nil {
		return DeploymentReport{}, &DeployerError{Kind: DeployerErrKube, Message: "topology is nil"}
	}
	namespace := cfg.DefaultNamespace
	if topology.FlinkNamespace != nil && *topology.FlinkNamespace != "" {
		namespace = *topology.FlinkNamespace
	}
	deployment := ""
	switch {
	case topology.FlinkDeploymentName != nil && *topology.FlinkDeploymentName != "":
		deployment = *topology.FlinkDeploymentName
	case topology.FlinkJobName != nil && *topology.FlinkJobName != "":
		deployment = *topology.FlinkJobName
	default:
		deployment = "topo-" + uuidSimple(topology.ID)
	}

	sql := RenderFlinkSQL(topology, streams)

	cm := RenderSQLConfigMap(namespace, deployment, sql.Script)
	if err := d.Applier.Apply(ctx, namespace, "ConfigMap", deployment+"-sql", cm); err != nil {
		return DeploymentReport{}, &DeployerError{Kind: DeployerErrKube, Message: fmt.Sprintf("apply ConfigMap/%s-sql: %v", deployment, err), Cause: err}
	}
	manifest := RenderFlinkDeploymentManifest(cfg, namespace, deployment, topology, streams)
	if err := d.Applier.Apply(ctx, namespace, "FlinkDeployment", deployment, manifest); err != nil {
		return DeploymentReport{}, &DeployerError{Kind: DeployerErrKube, Message: fmt.Sprintf("apply FlinkDeployment/%s: %v", deployment, err), Cause: err}
	}
	if d.Recorder != nil {
		if err := d.Recorder.RecordTopologyDeployment(ctx, topology.ID.String(), deployment, namespace); err != nil {
			return DeploymentReport{}, &DeployerError{Kind: DeployerErrDB, Cause: err}
		}
	}
	return DeploymentReport{
		Coords: FlinkJobCoords{
			DeploymentName: deployment,
			Namespace:      namespace,
		},
		SQL: sql,
	}, nil
}

// DeleteTopology mirrors deployer::delete_topology — best-effort
// teardown of the ConfigMap + FlinkDeployment owned by this topology.
// Errors from the applier are surfaced so callers can log them, but the
// applier itself is expected to swallow 404s.
func (d *Deployer) DeleteTopology(ctx context.Context, coords FlinkJobCoords) error {
	if d == nil || d.Applier == nil {
		return &DeployerError{Kind: DeployerErrKube, Message: "applier is not wired"}
	}
	if coords.DeploymentName == "" || coords.Namespace == "" {
		return &DeployerError{Kind: DeployerErrKube, Message: "deployment coords are incomplete"}
	}
	if err := d.Applier.Delete(ctx, coords.Namespace, "FlinkDeployment", coords.DeploymentName); err != nil {
		return &DeployerError{Kind: DeployerErrKube, Message: fmt.Sprintf("delete FlinkDeployment/%s: %v", coords.DeploymentName, err), Cause: err}
	}
	if err := d.Applier.Delete(ctx, coords.Namespace, "ConfigMap", coords.DeploymentName+"-sql"); err != nil {
		return &DeployerError{Kind: DeployerErrKube, Message: fmt.Sprintf("delete ConfigMap/%s-sql: %v", coords.DeploymentName, err), Cause: err}
	}
	return nil
}

// RenderSQLConfigMap builds the ConfigMap manifest as a JSON-shaped map.
// Pure helper exposed for unit tests and the kube applier shim.
func RenderSQLConfigMap(namespace, deployment, script string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      deployment + "-sql",
			"namespace": namespace,
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": FieldManager,
				"openfoundry.io/component":     "flink-sql",
			},
		},
		"data": map[string]any{
			"topology.sql": script,
		},
	}
}

// RenderFlinkDeploymentManifest mirrors deployer::render_flink_deployment_manifest.
// Pure function: builds the FlinkDeployment manifest as a JSON-shaped
// map. Exposed so the REST handler can preview the manifest without
// touching the cluster.
func RenderFlinkDeploymentManifest(cfg FlinkRuntimeConfig, namespace, deployment string, topology *domain.TopologyDefinition, streams []domain.DomainStreamDefinition) map[string]any {
	checkpointDir := strings.TrimRight(cfg.StateBucketURI, "/") + "/checkpoints/" + deployment
	savepointDir := strings.TrimRight(cfg.StateBucketURI, "/") + "/savepoints/" + deployment
	haDir := strings.TrimRight(cfg.StateBucketURI, "/") + "/ha/" + deployment
	checkpointingMode := "AT_LEAST_ONCE"
	if EffectiveExactlyOnce(topology, streams) {
		checkpointingMode = "EXACTLY_ONCE"
	}

	flinkConfig := map[string]any{
		"taskmanager.numberOfTaskSlots":       "2",
		"parallelism.default":                 "4",
		"high-availability.type":              "kubernetes",
		"high-availability.storageDir":        haDir,
		"state.backend.type":                  "rocksdb",
		"state.backend.incremental":           "true",
		"state.checkpoints.dir":               checkpointDir,
		"state.savepoints.dir":                savepointDir,
		"execution.checkpointing.mode":        checkpointingMode,
		"execution.checkpointing.interval":    fmt.Sprintf("%d", topology.CheckpointIntervalMS),
		"execution.checkpointing.timeout":     "600000",
		"metrics.reporter.prom.factory.class": "org.apache.flink.metrics.prometheus.PrometheusReporterFactory",
		"metrics.reporter.prom.port":          "9249",
	}

	return map[string]any{
		"apiVersion": "flink.apache.org/v1beta1",
		"kind":       "FlinkDeployment",
		"metadata": map[string]any{
			"name":      deployment,
			"namespace": namespace,
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": FieldManager,
				"openfoundry.io/topology-id":   topology.ID.String(),
				"openfoundry.io/topology-name": sanitizeLabel(topology.Name),
			},
		},
		"spec": map[string]any{
			"image":              cfg.SQLRunnerImage,
			"flinkVersion":       cfg.FlinkVersion,
			"mode":               "native",
			"serviceAccount":     "flink",
			"flinkConfiguration": flinkConfig,
			"jobManager": map[string]any{
				"replicas": 1,
				"resource": map[string]any{
					"memory": "1024m",
					"cpu":    1,
				},
			},
			"taskManager": map[string]any{
				"resource": map[string]any{
					"memory": "2048m",
					"cpu":    1,
				},
			},
			"podTemplate": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name": "flink-main-container",
							"volumeMounts": []any{
								map[string]any{
									"name":      "topology-sql",
									"mountPath": "/opt/flink/usrlib/sql",
								},
							},
						},
					},
					"volumes": []any{
						map[string]any{
							"name": "topology-sql",
							"configMap": map[string]any{
								"name": deployment + "-sql",
							},
						},
					},
				},
			},
			"job": map[string]any{
				"jarURI":      "local:///opt/flink/usrlib/sql-runner.jar",
				"args":        []any{"--script", "/opt/flink/usrlib/sql/topology.sql"},
				"parallelism": 2,
				"upgradeMode": "savepoint",
				"state":       "running",
			},
		},
	}
}

// sanitizeLabel mirrors deployer::sanitize_label. Truncates to 63 chars
// (the Kubernetes label-value limit) and replaces any non
// `[A-Za-z0-9-_.]` character with `-`.
func sanitizeLabel(s string) string {
	if len(s) > 63 {
		s = s[:63]
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'a' && ch <= 'z',
			ch >= 'A' && ch <= 'Z',
			ch >= '0' && ch <= '9',
			ch == '-', ch == '_', ch == '.':
			out = append(out, ch)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}
