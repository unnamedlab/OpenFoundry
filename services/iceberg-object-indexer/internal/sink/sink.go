// Package sink defines the per-row writer contract the indexer hands
// every JSON-serialised row to. The production implementation PUTs to
// object-database-service; tests pass in a fake.
package sink

import (
	"context"
	"fmt"
)

// Sink writes a single object payload to its destination.
type Sink interface {
	// Put writes `body` for (tenant, id). Implementations must return
	// *HTTPError for non-2xx responses so the runner can classify them
	// as client / server failures and keep going on 4xx-per-row; any
	// other error type is treated as fatal and aborts the run.
	Put(ctx context.Context, tenant, id string, body []byte) error
}

// HTTPError carries a non-2xx response from the sink. The runner uses
// it to decide whether a single row failure is recoverable (continue)
// or whether the run should abort (fatal err).
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("object-database returned HTTP %d: %s", e.StatusCode, e.Body)
}
