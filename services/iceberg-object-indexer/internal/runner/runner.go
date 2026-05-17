// Package runner orchestrates a single indexing pass: open the
// iceberg source, stream rows, PUT each row into object-database-service.
// The package itself depends only on the Source/Sink interfaces so it
// is exercised by unit tests without Iceberg or HTTP at hand.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/openfoundry/openfoundry-go/services/iceberg-object-indexer/internal/sink"
	"github.com/openfoundry/openfoundry-go/services/iceberg-object-indexer/internal/source"
)

// Metrics is the narrow surface the runner needs from the
// observability layer. server.Metrics satisfies it; tests pass nil
// or a no-op.
type Metrics interface {
	RecordRow(outcome string)
	RecordBatch(rows int)
	RecordDuration(d time.Duration)
}

// Deps bundles every collaborator the runner needs. Source and Sink
// are mandatory; Log defaults to slog.Default(), Now to time.Now,
// Metrics to a no-op.
type Deps struct {
	Source  source.Source
	Sink    sink.Sink
	Log     *slog.Logger
	Now     func() time.Time
	Metrics Metrics
}

type noopMetrics struct{}

func (noopMetrics) RecordRow(string)            {}
func (noopMetrics) RecordBatch(int)             {}
func (noopMetrics) RecordDuration(time.Duration) {}

// Run executes one indexing pass. Returns nil on success (even when
// some rows produced 4xx — those count as `client_error` and the run
// keeps going, matching the Scala implementation). Returns an error
// only when the source fails or a non-HTTP sink error occurs.
func Run(ctx context.Context, args Args, deps Deps) error {
	if deps.Log == nil {
		deps.Log = slog.Default()
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Metrics == nil {
		deps.Metrics = noopMetrics{}
	}
	log := deps.Log.With(
		slog.String("source_table", args.SourceTable),
		slog.String("target_type_id", args.TargetTypeID),
		slog.String("id_column", args.IDColumn),
	)
	if args.Smoke {
		log.Info("smoke mode: arguments validated, skipping iceberg read and object-database writes")
		return nil
	}
	if deps.Source == nil {
		return errors.New("runner: source is required")
	}
	if deps.Sink == nil {
		return errors.New("runner: sink is required")
	}

	start := deps.Now()
	rows, err := deps.Source.Scan(ctx, args.Limit)
	if err != nil {
		return fmt.Errorf("scan source: %w", err)
	}

	var written, skipped, clientErrors, serverErrors int64
	for row, scanErr := range rows {
		if scanErr != nil {
			return fmt.Errorf("iterate source: %w", scanErr)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		id, ok := stringifyID(row, args.IDColumn)
		if !ok {
			deps.Metrics.RecordRow("skipped")
			skipped++
			log.Warn("row skipped: id column missing or empty")
			continue
		}
		body, err := buildPutBody(args.TargetTypeID, row, deps.Now)
		if err != nil {
			return fmt.Errorf("build put body id=%s: %w", id, err)
		}
		putErr := deps.Sink.Put(ctx, args.TargetTenant, id, body)
		if putErr == nil {
			deps.Metrics.RecordRow("success")
			written++
			continue
		}
		var httpErr *sink.HTTPError
		if errors.As(putErr, &httpErr) {
			outcome := "server_error"
			if httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 {
				outcome = "client_error"
				clientErrors++
			} else {
				serverErrors++
			}
			deps.Metrics.RecordRow(outcome)
			log.Error("object put failed",
				slog.String("id", id),
				slog.Int("status", httpErr.StatusCode),
				slog.String("body", httpErr.Body),
			)
			continue
		}
		return fmt.Errorf("put id=%s: %w", id, putErr)
	}

	duration := deps.Now().Sub(start)
	deps.Metrics.RecordDuration(duration)
	log.Info("indexing complete",
		slog.Int64("rows_written", written),
		slog.Int64("rows_skipped", skipped),
		slog.Int64("client_errors", clientErrors),
		slog.Int64("server_errors", serverErrors),
		slog.Duration("duration", duration),
	)
	return nil
}

// stringifyID renders row[col] as a non-empty string suitable for the
// URL path. Returns ("", false) when the column is missing, nil, or
// empty after string conversion.
func stringifyID(row source.Row, col string) (string, bool) {
	v, ok := row[col]
	if !ok || v == nil {
		return "", false
	}
	switch s := v.(type) {
	case string:
		if s == "" {
			return "", false
		}
		return s, true
	case []byte:
		if len(s) == 0 {
			return "", false
		}
		return string(s), true
	case json.Number:
		return s.String(), true
	case int:
		return strconv.FormatInt(int64(s), 10), true
	case int8:
		return strconv.FormatInt(int64(s), 10), true
	case int16:
		return strconv.FormatInt(int64(s), 10), true
	case int32:
		return strconv.FormatInt(int64(s), 10), true
	case int64:
		return strconv.FormatInt(s, 10), true
	case uint:
		return strconv.FormatUint(uint64(s), 10), true
	case uint8:
		return strconv.FormatUint(uint64(s), 10), true
	case uint16:
		return strconv.FormatUint(uint64(s), 10), true
	case uint32:
		return strconv.FormatUint(uint64(s), 10), true
	case uint64:
		return strconv.FormatUint(s, 10), true
	case float32:
		return strconv.FormatFloat(float64(s), 'f', -1, 32), true
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(s), true
	default:
		b, err := json.Marshal(v)
		if err != nil || len(b) == 0 || string(b) == `""` {
			return "", false
		}
		// Strip surrounding quotes when the JSON encoding is a bare
		// string (e.g. uuid types that fall through to default).
		if b[0] == '"' && b[len(b)-1] == '"' && len(b) >= 2 {
			return string(b[1 : len(b)-1]), true
		}
		return string(b), true
	}
}

// buildPutBody wraps the row payload in the writeObjectRequest shape
// object-database-service expects. Byte-for-byte equivalent to the
// Scala helper of the same name: {type_id, version, payload,
// updated_at_ms, markings=[]}. version+updated_at_ms share the same
// timestamp (Scala used System.currentTimeMillis() once and reused
// it; we do the same via deps.Now).
func buildPutBody(typeID string, row source.Row, now func() time.Time) ([]byte, error) {
	ts := now().UnixMilli()
	payload, err := json.Marshal(row)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		TypeID      string          `json:"type_id"`
		Version     int64           `json:"version"`
		Payload     json.RawMessage `json:"payload"`
		UpdatedAtMs int64           `json:"updated_at_ms"`
		Markings    []string        `json:"markings"`
	}{
		TypeID:      typeID,
		Version:     ts,
		Payload:     payload,
		UpdatedAtMs: ts,
		Markings:    []string{},
	})
}
