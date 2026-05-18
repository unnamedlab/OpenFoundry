package writer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/envelope"
)

// JSONLWriter is an explicit dev/staging fallback. Appends each batch
// row as a newline-delimited JSON record to `path`. A path of "-"
// writes to stdout.
type JSONLWriter struct {
	mu     sync.Mutex
	closer io.Closer
	out    io.Writer
}

// NewJSONLWriter opens `path` (O_APPEND|O_CREATE) or routes to stdout
// for path == "-".
func NewJSONLWriter(path string) (*JSONLWriter, error) {
	if path == "-" {
		return &JSONLWriter{out: os.Stdout}, nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open jsonl %s: %w", path, err)
	}
	return &JSONLWriter{out: f, closer: f}, nil
}

// Append serialises each envelope to JSON and writes one line per
// record. The same mutex guards every write so concurrent flushes do
// not interleave.
func (w *JSONLWriter) Append(_ context.Context, batch []envelope.ActionEnvelope) error {
	if len(batch) == 0 {
		return ErrEmptyBatch
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	enc := json.NewEncoder(w.out)
	for _, e := range batch {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("jsonl encode: %w", err)
		}
	}
	return nil
}

// Close releases the file handle (no-op for stdout).
func (w *JSONLWriter) Close() error {
	if w.closer == nil {
		return nil
	}
	return w.closer.Close()
}
