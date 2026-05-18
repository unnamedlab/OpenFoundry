package runtime

import (
	"context"
	"errors"
	"log/slog"
	"time"

	databus "github.com/openfoundry/openfoundry-go/libs/event-bus-data"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/config"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/envelope"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/writer"
)

// Run is the Kafka → batch → Writer.Append → CommitMessages loop.
//
// Single target table: every successfully-decoded record goes into a
// flat pending slice; on flush the writer issues one Iceberg append
// and the offsets advance (good + poison together). On any writer or
// commit error the batch is preserved for replay — Kafka offsets do
// NOT advance.
//
// On ctx cancellation the loop drains the current buffer with one
// final flush so an orderly shutdown does not leak in-flight records.
func Run(ctx context.Context, cfg *config.Config, sub databus.Subscriber, w writer.Writer, m *Metrics, log *slog.Logger) error {
	pendingMsgs := make([]*databus.DataMessage, 0, cfg.BatchPolicy.MaxRecords)
	pendingRecs := make([]envelope.ActionEnvelope, 0, cfg.BatchPolicy.MaxRecords)
	batchStart := time.Now()

	flushAndReset := func() error {
		if len(pendingRecs) == 0 && len(pendingMsgs) == 0 {
			return nil
		}
		if err := flush(ctx, sub, w, m, log, pendingRecs, pendingMsgs, batchStart); err != nil {
			return err
		}
		pendingMsgs = pendingMsgs[:0]
		pendingRecs = pendingRecs[:0]
		batchStart = time.Now()
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			if err := flushAndReset(); err != nil {
				log.Error("final flush failed", slog.String("error", err.Error()))
				return err
			}
			return nil
		default:
		}

		remaining := cfg.BatchPolicy.MaxWait - time.Since(batchStart)
		if remaining <= 0 {
			remaining = 1 * time.Millisecond
		}
		pollCtx, cancel := context.WithTimeout(ctx, remaining)
		msg, err := sub.Poll(pollCtx)
		cancel()

		switch {
		case err == nil:
			env, decodeErr := envelope.Decode(msg.Value)
			if decodeErr != nil {
				log.Warn("skipping poison action record",
					slog.String("topic", msg.Topic),
					slog.Int("partition", msg.Partition),
					slog.Int64("offset", msg.Offset),
					slog.String("error", decodeErr.Error()))
				m.CommitsTotal.WithLabelValues(OutcomePoison).Inc()
				pendingMsgs = append(pendingMsgs, msg)
				continue
			}
			pendingRecs = append(pendingRecs, env)
			pendingMsgs = append(pendingMsgs, msg)

		case errors.Is(err, context.DeadlineExceeded):
			// poll timeout — fall through to flush check

		case errors.Is(err, context.Canceled):
			if err := flushAndReset(); err != nil {
				return err
			}
			return nil

		default:
			return err
		}

		if cfg.BatchPolicy.ShouldFlush(len(pendingRecs), time.Since(batchStart)) {
			if err := flushAndReset(); err != nil {
				return err
			}
		}
	}
}

// flush appends the buffered records to the writer, commits Kafka
// offsets only on success, and updates per-batch metrics. On writer
// or commit error the function returns without committing — the
// runtime keeps the batch and retries on the next iteration.
func flush(
	ctx context.Context,
	sub databus.Subscriber,
	w writer.Writer,
	m *Metrics,
	log *slog.Logger,
	recs []envelope.ActionEnvelope,
	msgs []*databus.DataMessage,
	batchStart time.Time,
) error {
	if len(recs) > 0 {
		if err := w.Append(ctx, recs); err != nil {
			m.CommitsTotal.WithLabelValues(OutcomeFailure).Inc()
			return err
		}
	}
	if err := sub.CommitMessages(ctx, msgs); err != nil {
		m.CommitsTotal.WithLabelValues(OutcomeFailure).Inc()
		return err
	}

	if len(recs) > 0 {
		oldestMs := recs[0].AppliedAtMs
		for _, r := range recs[1:] {
			if r.AppliedAtMs < oldestMs {
				oldestMs = r.AppliedAtMs
			}
		}
		lag := time.Since(time.UnixMilli(oldestMs)).Seconds()
		m.LagSeconds.Observe(lag)
		m.RecordsTotal.Add(float64(len(recs)))
		m.BatchSize.Observe(float64(len(recs)))
		m.CommitsTotal.WithLabelValues(OutcomeSuccess).Inc()
	}

	log.Info("flushed action-log batch",
		slog.Int("records", len(recs)),
		slog.Int("offsets_committed", len(msgs)),
		slog.Duration("elapsed", time.Since(batchStart)))
	return nil
}
