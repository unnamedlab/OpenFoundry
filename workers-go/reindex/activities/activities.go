// Package activities holds Temporal activities for the reindex
// worker. Unlike the other workers in this workspace, reindex IS
// the platform's reindex tool — its job is to scan Cassandra and
// publish to Kafka `ontology.reindex.v1`, so it talks to those
// systems directly. This is the documented exception to the "no
// direct DB access" rule in workers-go/README.md, called out in
// ADR-0021 §Wire format.
package activities

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gocql/gocql"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

type Activities struct {
	logger *slog.Logger

	once      sync.Once
	initErr   error
	session   *gocql.Session
	publisher *kgo.Client
	keyspace  string
}

type scanInput struct {
	TenantID    string `json:"tenant_id"`
	TypeID      string `json:"type_id,omitempty"`
	PageSize    int    `json:"page_size"`
	ResumeToken string `json:"resume_token,omitempty"`
}

type publishInput struct {
	Topic   string           `json:"topic"`
	Records []map[string]any `json:"records"`
}

func NewActivities(logger *slog.Logger) *Activities {
	if logger == nil {
		logger = slog.Default()
	}
	return &Activities{logger: logger}
}

func (a *Activities) ScanCassandraObjects(ctx context.Context, in any) (map[string]any, error) {
	var req scanInput
	if err := decodeInput(in, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.TenantID) == "" {
		return nil, errors.New("tenant_id is required")
	}
	if req.PageSize <= 0 {
		req.PageSize = 1000
	}
	if err := a.ensureInitialized(); err != nil {
		return nil, err
	}

	records, nextToken, err := a.scan(ctx, req)
	if err != nil {
		return nil, err
	}
	a.logger.InfoContext(
		ctx,
		"reindex scan page",
		"tenant_id",
		req.TenantID,
		"type_id",
		req.TypeID,
		"records",
		len(records),
		"has_next_token",
		nextToken != "",
	)
	return map[string]any{
		"records":    records,
		"next_token": nextToken,
	}, nil
}

func (a *Activities) PublishReindexBatch(ctx context.Context, in any) (map[string]any, error) {
	var req publishInput
	if err := decodeInput(in, &req); err != nil {
		return nil, err
	}
	if req.Topic == "" {
		return nil, errors.New("topic is required")
	}
	if err := a.ensureInitialized(); err != nil {
		return nil, err
	}
	if err := a.publishBatch(ctx, req.Topic, req.Records); err != nil {
		return nil, err
	}
	a.logger.InfoContext(
		ctx,
		"reindex batch published",
		"topic",
		req.Topic,
		"published",
		len(req.Records),
	)
	return map[string]any{"published": int64(len(req.Records))}, nil
}

func (a *Activities) ensureInitialized() error {
	a.once.Do(func() {
		contactPoints := splitAndTrim(os.Getenv("CASSANDRA_CONTACT_POINTS"))
		if len(contactPoints) == 0 {
			a.initErr = errors.New("CASSANDRA_CONTACT_POINTS not set")
			return
		}

		cluster := gocql.NewCluster(contactPoints...)
		cluster.Keyspace = getenv("CASSANDRA_KEYSPACE", "ontology_objects")
		cluster.Consistency = gocql.LocalOne
		cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.RoundRobinHostPolicy())
		cluster.Timeout = 10 * time.Second
		cluster.ConnectTimeout = 10 * time.Second
		cluster.PageSize = 1000

		if localDC := strings.TrimSpace(os.Getenv("CASSANDRA_LOCAL_DC")); localDC != "" {
			cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(
				gocql.DCAwareRoundRobinPolicy(localDC),
			)
		}
		if username := strings.TrimSpace(os.Getenv("CASSANDRA_USERNAME")); username != "" {
			cluster.Authenticator = gocql.PasswordAuthenticator{
				Username: username,
				Password: os.Getenv("CASSANDRA_PASSWORD"),
			}
		}

		session, err := cluster.CreateSession()
		if err != nil {
			a.initErr = fmt.Errorf("create cassandra session: %w", err)
			return
		}
		a.session = session
		a.keyspace = cluster.Keyspace

		kafkaClient, err := newKafkaClient()
		if err != nil {
			a.initErr = err
			return
		}
		a.publisher = kafkaClient
	})
	return a.initErr
}

func (a *Activities) scan(ctx context.Context, req scanInput) ([]map[string]any, string, error) {
	if req.TypeID != "" {
		return a.scanByType(ctx, req)
	}
	return a.scanAllTypes(ctx, req)
}

func (a *Activities) scanByType(ctx context.Context, req scanInput) ([]map[string]any, string, error) {
	query := fmt.Sprintf(
		"SELECT object_id FROM %s.objects_by_type WHERE tenant = ? AND type_id = ?",
		a.keyspace,
	)
	iter := a.session.Query(query, req.TenantID, req.TypeID).
		WithContext(ctx).
		PageSize(req.PageSize).
		PageState(decodePageState(req.ResumeToken)).
		Iter()

	var ids []gocql.UUID
	var objectID gocql.UUID
	for iter.Scan(&objectID) {
		ids = append(ids, objectID)
	}
	nextToken := encodePageState(iter.PageState())
	if err := iter.Close(); err != nil {
		return nil, "", fmt.Errorf("scan objects_by_type: %w", err)
	}

	records := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		record, ok, err := a.fetchObject(ctx, req.TenantID, id)
		if err != nil {
			return nil, "", err
		}
		if ok {
			records = append(records, record)
		}
	}
	return records, nextToken, nil
}

func (a *Activities) scanAllTypes(ctx context.Context, req scanInput) ([]map[string]any, string, error) {
	query := fmt.Sprintf(
		"SELECT type_id, object_id FROM %s.objects_by_type WHERE tenant = ? ALLOW FILTERING",
		a.keyspace,
	)
	iter := a.session.Query(query, req.TenantID).
		WithContext(ctx).
		PageSize(req.PageSize).
		PageState(decodePageState(req.ResumeToken)).
		Iter()

	var records []map[string]any
	var typeID string
	var objectID gocql.UUID
	for iter.Scan(&typeID, &objectID) {
		record, ok, err := a.fetchObject(ctx, req.TenantID, objectID)
		if err != nil {
			return nil, "", err
		}
		if ok {
			record["type_id"] = typeID
			records = append(records, record)
		}
	}
	nextToken := encodePageState(iter.PageState())
	if err := iter.Close(); err != nil {
		return nil, "", fmt.Errorf("scan objects_by_type all-types: %w", err)
	}
	return records, nextToken, nil
}

func (a *Activities) fetchObject(ctx context.Context, tenantID string, objectID gocql.UUID) (map[string]any, bool, error) {
	query := fmt.Sprintf(
		"SELECT type_id, properties, revision_number, deleted FROM %s.objects_by_id WHERE tenant = ? AND object_id = ?",
		a.keyspace,
	)
	var typeID string
	var properties string
	var revision int64
	var deleted bool

	if err := a.session.Query(query, tenantID, objectID).
		WithContext(ctx).
		Consistency(gocql.LocalOne).
		Scan(&typeID, &properties, &revision, &deleted); err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load object %s: %w", objectID, err)
	}
	if deleted {
		return nil, false, nil
	}

	var payload any
	if err := json.Unmarshal([]byte(properties), &payload); err != nil {
		return nil, false, fmt.Errorf("decode object %s payload: %w", objectID, err)
	}

	record := map[string]any{
		"tenant":  tenantID,
		"id":      objectID.String(),
		"type_id": typeID,
		"version": revision,
		"payload": payload,
		"deleted": false,
	}
	if embedding := extractEmbedding(payload); embedding != nil {
		record["embedding"] = embedding
	}
	return record, true, nil
}

func (a *Activities) publishBatch(ctx context.Context, topic string, records []map[string]any) error {
	if len(records) == 0 {
		return nil
	}
	kafkaRecords := make([]*kgo.Record, 0, len(records))
	for _, record := range records {
		value, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("encode reindex record: %w", err)
		}
		key := ""
		if tenant, ok := record["tenant"].(string); ok {
			key += tenant
		}
		key += "/"
		if id, ok := record["id"].(string); ok {
			key += id
		}
		kafkaRecords = append(kafkaRecords, &kgo.Record{
			Topic: topic,
			Key:   []byte(key),
			Value: value,
		})
	}
	if err := a.publisher.ProduceSync(ctx, kafkaRecords...).FirstErr(); err != nil {
		return fmt.Errorf("publish reindex batch: %w", err)
	}
	return nil
}

func newKafkaClient() (*kgo.Client, error) {
	brokers := splitAndTrim(os.Getenv("KAFKA_BOOTSTRAP_SERVERS"))
	if len(brokers) == 0 {
		return nil, errors.New("KAFKA_BOOTSTRAP_SERVERS not set")
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.ClientID(getenv("KAFKA_CLIENT_ID", "workers-go-reindex")),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.AllowAutoTopicCreation(),
	}

	securityProtocol := strings.ToUpper(strings.TrimSpace(os.Getenv("KAFKA_SECURITY_PROTOCOL")))
	if strings.Contains(securityProtocol, "SSL") {
		opts = append(opts, kgo.DialTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
	}
	if username := strings.TrimSpace(os.Getenv("KAFKA_SASL_USERNAME")); username != "" {
		password := os.Getenv("KAFKA_SASL_PASSWORD")
		mechanism := strings.ToUpper(strings.TrimSpace(getenv("KAFKA_SASL_MECHANISM", "SCRAM-SHA-512")))
		switch mechanism {
		case "SCRAM-SHA-256":
			opts = append(opts, kgo.SASL(scram.Auth{
				User: username,
				Pass: password,
			}.AsSha256Mechanism()))
		default:
			opts = append(opts, kgo.SASL(scram.Auth{
				User: username,
				Pass: password,
			}.AsSha512Mechanism()))
		}
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("create kafka client: %w", err)
	}
	return client, nil
}

func decodeInput(in any, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("encode activity input: %w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode activity input: %w", err)
	}
	return nil
}

func decodePageState(token string) []byte {
	if token == "" {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return nil
	}
	return raw
}

func encodePageState(state []byte) string {
	if len(state) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(state)
}

func extractEmbedding(payload any) []float64 {
	object, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := object["embedding"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	embedding := make([]float64, 0, len(items))
	for _, item := range items {
		switch value := item.(type) {
		case float64:
			embedding = append(embedding, value)
		case float32:
			embedding = append(embedding, float64(value))
		}
	}
	if len(embedding) == 0 {
		return nil
	}
	return embedding
}

func splitAndTrim(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func getenv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
