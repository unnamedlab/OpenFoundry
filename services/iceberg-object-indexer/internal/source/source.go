// Package source defines the row-stream contract that the indexer
// reads from. The production implementation wraps apache/iceberg-go;
// tests pass in a fake.
package source

import (
	"context"
	"iter"
)

// Row is a single record yielded by a Source. Values follow the
// apache/arrow GetOneForMarshal contract — every concrete type
// json.Marshal can encode.
type Row = map[string]any

// Source produces rows from an Iceberg table (or test fake).
type Source interface {
	// Scan plans the table read and returns a row iterator. The
	// implementation must respect `limit > 0` and yield at most that
	// many rows; `limit == 0` means unbounded.
	Scan(ctx context.Context, limit int64) (iter.Seq2[Row, error], error)
	// Close releases any catalog/HTTP/file handles. Safe to call
	// multiple times.
	Close() error
}
