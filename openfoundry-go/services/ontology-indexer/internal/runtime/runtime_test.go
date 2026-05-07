package runtime

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/openfoundry/openfoundry-go/services/ontology-indexer/internal/config"
)

func TestRedactedEndpoint(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "(unset)", redactedEndpoint(""))
	assert.Equal(t, "https://vespa.local:8080", redactedEndpoint("https://vespa.local:8080"))
	assert.Equal(t, "***@vespa.local:8080", redactedEndpoint("https://user:pass@vespa.local:8080"))
}

func TestRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Name = "ontology-indexer"
	cfg.BackendKind = config.BackendVespa

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, log) }()

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err, "Run should return nil on context.Canceled")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestTopicsAndConsumerGroup(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "ontology.object.changed.v1", TopicObjectChangedV1)
	assert.Equal(t, "ontology.link.changed.v1", TopicLinkChangedV1)
	assert.Equal(t, "ontology-indexer", ConsumerGroup)
}
