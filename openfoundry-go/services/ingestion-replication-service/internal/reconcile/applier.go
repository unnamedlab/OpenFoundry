// HTTPApplier — the production JobApplier. It mirrors the Rust
// `services/ingestion-replication-service/src/control_plane.rs` layer:
//
//   - RenderResources is a pure function that validates an IngestJobSpec and
//     produces a KafkaConnector (Strimzi) plus an optional FlinkDeployment
//     (Apache Flink Kubernetes Operator) — same validation order, same error
//     messages and the same defaulting as Rust `render_resources`.
//   - HTTPApplier.Apply talks to a small REST shim in front of `kube::Client`:
//     it POSTs the rendered payload to {BaseURL}/v1/apply, mirroring the Rust
//     `apply_resources` server-side-apply call. Delete mirrors `delete_resources`
//     (best-effort, 404 treated as success).
package reconcile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/models"
)

// FieldManager is the value used as the field manager for server-side apply
// patches against the Kubernetes API. Mirrors Rust `FIELD_MANAGER`.
const FieldManager = "ingestion-replication-service"

// DefaultFlinkImage is used when IcebergSink.FlinkImage is empty.
const DefaultFlinkImage = "apache/flink:1.18-scala_2.12-java11"

// DefaultFlinkVersion is the default Flink version label sent to the operator.
const DefaultFlinkVersion = "v1_18"

// IngestJobSpec is the JSON-decode shape of `IngestJob.Spec` (json.RawMessage).
// Mirrors the Rust `proto::IngestJobSpec` struct — see
// `services/ingestion-replication-service/proto/ingestion_control_plane.proto`.
type IngestJobSpec struct {
	Name                string          `json:"name"`
	Namespace           string          `json:"namespace"`
	Source              string          `json:"source"`
	Postgres            *PostgresSource `json:"postgres,omitempty"`
	KafkaConnectCluster string          `json:"kafka_connect_cluster"`
	IcebergSink         *IcebergSink    `json:"iceberg_sink,omitempty"`
}

// PostgresSource mirrors Rust `proto::PostgresSource`.
type PostgresSource struct {
	Hostname        string   `json:"hostname"`
	Port            int32    `json:"port"`
	Database        string   `json:"database"`
	User            string   `json:"user"`
	PasswordSecret  string   `json:"password_secret"`
	SlotName        string   `json:"slot_name"`
	PublicationName string   `json:"publication_name"`
	Tables          []string `json:"tables,omitempty"`
	TopicPrefix     string   `json:"topic_prefix"`
}

// IcebergSink mirrors Rust `proto::IcebergSink`.
type IcebergSink struct {
	Warehouse    string `json:"warehouse"`
	CatalogName  string `json:"catalog_name"`
	Database     string `json:"database"`
	Table        string `json:"table"`
	FlinkImage   string `json:"flink_image"`
	FlinkVersion string `json:"flink_version"`
}

// ObjectMeta is the minimal Kubernetes ObjectMeta subset the control plane
// needs. Field names are camelCase to match k8s wire conventions.
type ObjectMeta struct {
	Name      string            `json:"name,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// orderedMap preserves the deterministic key ordering Rust BTreeMap provides
// when serialised to JSON. Marshalling iterates the underlying slice in order.
type orderedMap struct {
	keys   []string
	values map[string]any
}

func newOrderedMap() *orderedMap { return &orderedMap{values: map[string]any{}} }

func (m *orderedMap) set(key string, value any) {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

func (m *orderedMap) get(key string) (any, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m.values[key]
	return v, ok
}

// MarshalJSON renders keys in BTreeMap-equivalent (sorted insertion) order.
func (m *orderedMap) MarshalJSON() ([]byte, error) {
	if m == nil || len(m.keys) == 0 {
		return []byte("{}"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(m.values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON populates the ordered map preserving JSON key order. Useful
// when test helpers or callers want to round-trip the rendered payload.
func (m *orderedMap) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("orderedMap: expected '{', got %v", tok)
	}
	m.keys = nil
	m.values = map[string]any{}
	for dec.More() {
		ktok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := ktok.(string)
		if !ok {
			return fmt.Errorf("orderedMap: expected string key, got %v", ktok)
		}
		var v any
		if err := dec.Decode(&v); err != nil {
			return err
		}
		m.set(key, v)
	}
	return nil
}

// orderedStringMap is the string-valued counterpart used by FlinkDeployment.flinkConfiguration.
type orderedStringMap struct {
	keys   []string
	values map[string]string
}

func newOrderedStringMap() *orderedStringMap { return &orderedStringMap{values: map[string]string{}} }

func (m *orderedStringMap) set(key, value string) {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

func (m *orderedStringMap) MarshalJSON() ([]byte, error) {
	if m == nil || len(m.keys) == 0 {
		return []byte("{}"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(m.values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (m *orderedStringMap) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("orderedStringMap: expected '{', got %v", tok)
	}
	m.keys = nil
	m.values = map[string]string{}
	for dec.More() {
		ktok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := ktok.(string)
		if !ok {
			return fmt.Errorf("orderedStringMap: expected string key, got %v", ktok)
		}
		var v string
		if err := dec.Decode(&v); err != nil {
			return err
		}
		m.set(key, v)
	}
	return nil
}

// KafkaConnector mirrors the Strimzi KafkaConnector custom resource.
type KafkaConnector struct {
	Metadata ObjectMeta          `json:"metadata"`
	Spec     KafkaConnectorSpec  `json:"spec"`
}

// KafkaConnectorSpec mirrors Rust `crds::KafkaConnectorSpec`.
type KafkaConnectorSpec struct {
	Class    string      `json:"class"`
	TasksMax int32       `json:"tasksMax"`
	Config   *orderedMap `json:"config"`
	State    *string     `json:"state,omitempty"`
}

// FlinkDeployment mirrors the Apache Flink Kubernetes Operator FlinkDeployment.
type FlinkDeployment struct {
	Metadata ObjectMeta          `json:"metadata"`
	Spec     FlinkDeploymentSpec `json:"spec"`
}

// FlinkDeploymentSpec mirrors Rust `crds::FlinkDeploymentSpec`.
type FlinkDeploymentSpec struct {
	Image              string            `json:"image"`
	FlinkVersion       string            `json:"flinkVersion"`
	FlinkConfiguration *orderedStringMap `json:"flinkConfiguration,omitempty"`
	JobManager         ResourceSpec      `json:"jobManager"`
	TaskManager        ResourceSpec      `json:"taskManager"`
	Job                JobSpec           `json:"job"`
	ServiceAccount     *string           `json:"serviceAccount,omitempty"`
}

// ResourceSpec mirrors Rust `crds::ResourceSpec`.
type ResourceSpec struct {
	Resource ResourceLimits `json:"resource"`
	Replicas *int32         `json:"replicas,omitempty"`
}

// ResourceLimits mirrors Rust `crds::ResourceLimits`.
type ResourceLimits struct {
	Memory string  `json:"memory"`
	CPU    float32 `json:"cpu"`
}

// JobSpec mirrors Rust `crds::JobSpec`.
type JobSpec struct {
	JarURI      string   `json:"jarUri"`
	Args        []string `json:"args,omitempty"`
	Parallelism int32    `json:"parallelism"`
	UpgradeMode *string  `json:"upgradeMode,omitempty"`
}

// RenderedResources is the output of RenderResources: the resources the
// control plane will apply against the cluster. Mirrors Rust
// `control_plane::RenderedResources`.
type RenderedResources struct {
	KafkaConnector  KafkaConnector
	FlinkDeployment *FlinkDeployment
}

// RenderResources validates an IngestJobSpec and converts it into Kubernetes
// resources. Pure function — performs no I/O. Mirrors Rust `render_resources`
// (services/ingestion-replication-service/src/control_plane.rs).
func RenderResources(spec *IngestJobSpec) (*RenderedResources, error) {
	if spec == nil || strings.TrimSpace(spec.Name) == "" {
		return nil, fmt.Errorf("IngestJobSpec.name must not be empty")
	}
	if strings.TrimSpace(spec.KafkaConnectCluster) == "" {
		return nil, fmt.Errorf("IngestJobSpec.kafka_connect_cluster must not be empty")
	}
	namespace := spec.Namespace
	if strings.TrimSpace(namespace) == "" {
		namespace = "default"
	}

	var kafkaConnector KafkaConnector
	switch spec.Source {
	case "postgres":
		if spec.Postgres == nil {
			return nil, fmt.Errorf("postgres source selected but `postgres` block missing")
		}
		kc, err := renderPostgresKafkaConnector(spec, spec.Postgres, namespace)
		if err != nil {
			return nil, err
		}
		kafkaConnector = kc
	default:
		return nil, fmt.Errorf("unsupported source kind '%s'", spec.Source)
	}

	var flink *FlinkDeployment
	if spec.IcebergSink != nil {
		fd, err := renderIcebergFlinkDeployment(spec, spec.IcebergSink, namespace)
		if err != nil {
			return nil, err
		}
		flink = &fd
	}

	return &RenderedResources{KafkaConnector: kafkaConnector, FlinkDeployment: flink}, nil
}

// renderPostgresKafkaConnector mirrors Rust `render_postgres_kafka_connector`.
// Builds a Strimzi KafkaConnector running the Debezium PostgreSQL connector.
func renderPostgresKafkaConnector(spec *IngestJobSpec, pg *PostgresSource, namespace string) (KafkaConnector, error) {
	if strings.TrimSpace(pg.Hostname) == "" || strings.TrimSpace(pg.Database) == "" {
		return KafkaConnector{}, fmt.Errorf("postgres source requires hostname and database")
	}
	port := pg.Port
	if port == 0 {
		port = 5432
	}
	topicPrefix := pg.TopicPrefix
	if strings.TrimSpace(topicPrefix) == "" {
		topicPrefix = spec.Name
	}
	slot := pg.SlotName
	if strings.TrimSpace(slot) == "" {
		slot = strings.ReplaceAll(spec.Name, "-", "_") + "_slot"
	}
	publication := pg.PublicationName
	if strings.TrimSpace(publication) == "" {
		publication = strings.ReplaceAll(spec.Name, "-", "_") + "_pub"
	}

	cfg := newOrderedMap()
	// Insertion order here doesn't matter — orderedMap sorts on insertion via
	// the BTreeMap key insertion order. To match Rust BTreeMap (sorted by key),
	// we insert in alphabetical order.
	cfg.set("database.dbname", pg.Database)
	cfg.set("database.hostname", pg.Hostname)
	if strings.TrimSpace(pg.PasswordSecret) != "" {
		cfg.set("database.password", fmt.Sprintf("${secrets:%s/password}", pg.PasswordSecret))
	}
	cfg.set("database.port", strconv.FormatInt(int64(port), 10))
	cfg.set("database.user", pg.User)
	cfg.set("plugin.name", "pgoutput")
	cfg.set("publication.name", publication)
	cfg.set("slot.name", slot)
	if len(pg.Tables) > 0 {
		cfg.set("table.include.list", strings.Join(pg.Tables, ","))
	}
	cfg.set("topic.prefix", topicPrefix)

	labels := map[string]string{
		"app.kubernetes.io/managed-by":     FieldManager,
		"ingestion.openfoundry.io/job":     spec.Name,
		"strimzi.io/cluster":               spec.KafkaConnectCluster,
	}

	return KafkaConnector{
		Metadata: ObjectMeta{
			Name:      spec.Name + "-debezium-pg",
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: KafkaConnectorSpec{
			Class:    "io.debezium.connector.postgresql.PostgresConnector",
			TasksMax: 1,
			Config:   cfg,
		},
	}, nil
}

// renderIcebergFlinkDeployment mirrors Rust `render_iceberg_flink_deployment`.
func renderIcebergFlinkDeployment(spec *IngestJobSpec, sink *IcebergSink, namespace string) (FlinkDeployment, error) {
	if strings.TrimSpace(sink.Warehouse) == "" ||
		strings.TrimSpace(sink.CatalogName) == "" ||
		strings.TrimSpace(sink.Database) == "" ||
		strings.TrimSpace(sink.Table) == "" {
		return FlinkDeployment{}, fmt.Errorf("iceberg sink requires warehouse, catalog_name, database and table")
	}
	image := sink.FlinkImage
	if strings.TrimSpace(image) == "" {
		image = DefaultFlinkImage
	}
	flinkVersion := sink.FlinkVersion
	if strings.TrimSpace(flinkVersion) == "" {
		flinkVersion = DefaultFlinkVersion
	}
	topicPrefix := spec.Name
	if spec.Postgres != nil {
		if strings.TrimSpace(spec.Postgres.TopicPrefix) != "" {
			topicPrefix = spec.Postgres.TopicPrefix
		} else {
			topicPrefix = spec.Name
		}
	}

	labels := map[string]string{
		"app.kubernetes.io/managed-by": FieldManager,
		"ingestion.openfoundry.io/job": spec.Name,
	}

	flinkConfig := newOrderedStringMap()
	flinkConfig.set("taskmanager.numberOfTaskSlots", "2")

	one := int32(1)
	upgrade := "last-state"
	serviceAccount := "flink"
	return FlinkDeployment{
		Metadata: ObjectMeta{
			Name:      spec.Name + "-iceberg-sink",
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: FlinkDeploymentSpec{
			Image:              image,
			FlinkVersion:       flinkVersion,
			FlinkConfiguration: flinkConfig,
			ServiceAccount:     &serviceAccount,
			JobManager: ResourceSpec{
				Resource: ResourceLimits{Memory: "1024m", CPU: 1.0},
				Replicas: &one,
			},
			TaskManager: ResourceSpec{
				Resource: ResourceLimits{Memory: "2048m", CPU: 1.0},
			},
			Job: JobSpec{
				JarURI: "local:///opt/flink/usrlib/iceberg-sink.jar",
				Args: []string{
					"--source-topic-prefix",
					topicPrefix,
					"--iceberg-warehouse",
					sink.Warehouse,
					"--iceberg-catalog",
					sink.CatalogName,
					"--iceberg-database",
					sink.Database,
					"--iceberg-table",
					sink.Table,
				},
				Parallelism: 1,
				UpgradeMode: &upgrade,
			},
		},
	}, nil
}

// applyRequest is the body of POST {BaseURL}/v1/apply. The control-plane shim
// is responsible for translating these into kube server-side-apply patches.
type applyRequest struct {
	Namespace       string           `json:"namespace"`
	KafkaConnector  KafkaConnector   `json:"kafka_connector"`
	FlinkDeployment *FlinkDeployment `json:"flink_deployment"`
}

// HTTPApplier is the production JobApplier. It POSTs the rendered resources
// to a small REST shim in front of `kube::Client`, mirroring Rust
// `control_plane::apply_resources` / `delete_resources`.
type HTTPApplier struct {
	BaseURL    string
	HTTPClient *http.Client
	Logger     *slog.Logger
}

func (a *HTTPApplier) client() *http.Client {
	if a != nil && a.HTTPClient != nil {
		return a.HTTPClient
	}
	return http.DefaultClient
}

func (a *HTTPApplier) logger() *slog.Logger {
	if a != nil && a.Logger != nil {
		return a.Logger
	}
	return slog.Default()
}

// Apply implements JobApplier. It decodes job.Spec into IngestJobSpec, renders
// resources, POSTs them to the control plane and returns the resource names.
func (a *HTTPApplier) Apply(ctx context.Context, job *models.IngestJob) (string, string, error) {
	if job == nil {
		return "", "", fmt.Errorf("apply control-plane: job is nil")
	}
	var spec IngestJobSpec
	if len(job.Spec) == 0 {
		return "", "", fmt.Errorf("apply control-plane: job spec is empty")
	}
	if err := json.Unmarshal(job.Spec, &spec); err != nil {
		return "", "", fmt.Errorf("apply control-plane: decode spec: %w", err)
	}
	rendered, err := RenderResources(&spec)
	if err != nil {
		return "", "", err
	}
	body := applyRequest{
		Namespace:       rendered.KafkaConnector.Metadata.Namespace,
		KafkaConnector:  rendered.KafkaConnector,
		FlinkDeployment: rendered.FlinkDeployment,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", "", fmt.Errorf("apply control-plane: marshal request: %w", err)
	}
	endpoint := strings.TrimRight(a.BaseURL, "/") + "/v1/apply"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", "", fmt.Errorf("apply control-plane: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client().Do(req)
	if err != nil {
		return "", "", fmt.Errorf("apply control-plane: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf := make([]byte, 256)
		n, _ := io.ReadFull(resp.Body, buf)
		snippet := strings.TrimSpace(string(buf[:n]))
		return "", "", fmt.Errorf("apply control-plane: HTTP %d: %s", resp.StatusCode, snippet)
	}
	// Drain the response so the connection can be reused.
	_, _ = io.Copy(io.Discard, resp.Body)

	kc := rendered.KafkaConnector.Metadata.Name
	fl := ""
	if rendered.FlinkDeployment != nil {
		fl = rendered.FlinkDeployment.Metadata.Name
	}
	a.logger().DebugContext(ctx, "control-plane apply succeeded",
		slog.String("job_id", job.ID.String()),
		slog.String("kafka_connector", kc),
		slog.String("flink_deployment", fl),
	)
	return kc, fl, nil
}

// Delete mirrors Rust `control_plane::delete_resources`. It best-effort
// deletes the named resources by calling DELETE on the control-plane shim;
// 404 responses are treated as success.
func (a *HTTPApplier) Delete(ctx context.Context, namespace, kafkaConnectorName, flinkDeploymentName string) error {
	if strings.TrimSpace(kafkaConnectorName) != "" {
		if err := a.deleteOne(ctx, namespace, kafkaConnectorName); err != nil {
			return fmt.Errorf("delete KafkaConnector %s/%s: %w", namespace, kafkaConnectorName, err)
		}
	}
	if strings.TrimSpace(flinkDeploymentName) != "" {
		if err := a.deleteOne(ctx, namespace, flinkDeploymentName); err != nil {
			return fmt.Errorf("delete FlinkDeployment %s/%s: %w", namespace, flinkDeploymentName, err)
		}
	}
	return nil
}

func (a *HTTPApplier) deleteOne(ctx context.Context, namespace, name string) error {
	endpoint := fmt.Sprintf(
		"%s/v1/resources/%s/%s",
		strings.TrimRight(a.BaseURL, "/"),
		url.PathEscape(namespace),
		url.PathEscape(name),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := a.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// Compile-time assertion that HTTPApplier satisfies JobApplier.
var _ JobApplier = (*HTTPApplier)(nil)
