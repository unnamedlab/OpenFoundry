// Package runtime hosts the ontology-indexer worker loop.
//
// Foundation slice: stub. The real consumer reads from Kafka topic
// `ontology.object.changed.v1` and applies index actions to a search
// backend (Vespa / OpenSearch). Both pieces (kafka-go consumer +
// libs/search-abstraction-go SearchBackend trait port) land in
// follow-up slices.
package runtime

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/openfoundry/openfoundry-go/services/ontology-indexer/internal/config"
)

// Topics the indexer subscribes to on startup.
const (
	TopicObjectChangedV1 = "ontology.object.changed.v1"
	TopicLinkChangedV1   = "ontology.link.changed.v1"
)

// ConsumerGroup pinned here so replicas don't fork rebalance state.
const ConsumerGroup = "ontology-indexer"

// Run is the worker loop. Foundation slice: blocks on ctx; logs the
// configured backend + endpoints; emits a heartbeat every 30s so
// /metrics shows the binary is alive even without the consumer wired.
func Run(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	log.Info("ontology-indexer starting (stub runtime)",
		slog.String("backend", string(cfg.BackendKind)),
		slog.String("search_endpoint", redactedEndpoint(cfg.SearchEndpoint)),
		slog.String("kafka_bootstrap", cfg.KafkaBootstrap),
		slog.String("consumer_group", cfg.ConsumerGroup),
	)

	if cfg.KafkaBootstrap == "" {
		log.Warn("KAFKA_BOOTSTRAP_SERVERS unset — no consumption will happen")
	}
	if cfg.SearchEndpoint == "" {
		log.Warn("SEARCH_ENDPOINT unset — no indexing will happen")
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("ontology-indexer stopping")
			if !errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err()
			}
			return nil
		case <-ticker.C:
			log.Debug("ontology-indexer heartbeat")
		}
	}
}

// redactedEndpoint hides query strings / passwords from logs.
func redactedEndpoint(ep string) string {
	if ep == "" {
		return "(unset)"
	}
	if i := indexAt(ep); i >= 0 {
		return "***" + ep[i:]
	}
	return ep
}

func indexAt(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '@' {
			return i
		}
	}
	return -1
}
