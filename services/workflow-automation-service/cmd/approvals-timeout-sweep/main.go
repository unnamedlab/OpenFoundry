// Command approvals-timeout-sweep is the single-shot CronJob binary
// that drives the `pending → expired` transition for every row in
// `audit_compliance.approval_requests` whose `expires_at <= now()`.
//
// Mirrors `services/workflow-automation-service/src/bin/approvals_timeout_sweep.rs`.
//
// Intended deployment: Kubernetes CronJob running every 5 min.
// Each pod boots, opens a Postgres pool, runs one
// state_machine.PgStore.TimeoutSweep, applies the Expire event to
// every claimed row + INSERTs an `approval.expired.v1` outbox row in
// the same transaction, and exits with code 0 on success.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/outbox"
	statemachine "github.com/openfoundry/openfoundry-go/libs/state-machine"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/approvals"
)

const serviceName = "approvals-timeout-sweep"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		slog.Error("DATABASE_URL must be set (Postgres connection URL)")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		slog.Error("parse DATABASE_URL failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		slog.Error("failed to connect to Postgres", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	store := statemachine.NewPgStore[*approvals.ApprovalRequest, approvals.Event](
		pool, approvals.TableName, func() *approvals.ApprovalRequest { return &approvals.ApprovalRequest{} },
	)
	now := time.Now().UTC()
	candidates, err := store.TimeoutSweep(ctx, now)
	if err != nil {
		slog.Error("timeout sweep failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	var (
		expired uint32
		skipped uint32
		failed  uint32
	)
	for _, loaded := range candidates {
		// The sweep returns every row whose `expires_at <= now()`,
		// including already-terminal rows that happen to keep an
		// `expires_at` value from before their decision. Skip
		// anything that is no longer pending — the row's state
		// machine refuses the Expire event for terminal states.
		if loaded.Machine.StateField != approvals.StatePending {
			skipped++
			continue
		}
		approvalID := loaded.Machine.ID
		deadline := now
		if loaded.Machine.ExpiresAtField != nil {
			deadline = *loaded.Machine.ExpiresAtField
		}
		if err := transitionAndPublish(ctx, pool, store, loaded, now, deadline); err != nil {
			failed++
			slog.Warn("approval expire failed; will retry on next sweep",
				slog.String("approval_id", approvalID.String()),
				slog.String("error", err.Error()))
			continue
		}
		expired++
		slog.Info("approval expired",
			slog.String("approval_id", approvalID.String()),
			slog.Time("deadline", deadline))
	}
	slog.Info("timeout sweep completed",
		slog.Int64("expired", int64(expired)),
		slog.Int64("skipped", int64(skipped)),
		slog.Int64("failed", int64(failed)),
		slog.Time("now", now))
	fmt.Println(expired)
}

func transitionAndPublish(ctx context.Context, pool *pgxpool.Pool, store *statemachine.PgStore[*approvals.ApprovalRequest, approvals.Event], loaded statemachine.Loaded[*approvals.ApprovalRequest], expiredAt, deadline time.Time) error {
	approvalID := loaded.Machine.ID
	tenantID := loaded.Machine.TenantID
	correlationID := loaded.Machine.CorrelationID

	// Step 1 — apply Expire via the helper (atomic UPDATE).
	if _, err := store.Apply(ctx, loaded, approvals.Event{Kind: approvals.EventExpire, ExpiredAt: expiredAt}); err != nil {
		return fmt.Errorf("apply Expire failed: %w", err)
	}

	// Step 2 — publish approval.expired.v1 via the outbox in a
	// separate transaction. Deterministic event_id collapses
	// duplicate emissions.
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	payload := approvals.ApprovalExpiredV1Payload{
		ApprovalID:    approvalID,
		TenantID:      tenantID,
		CorrelationID: correlationID,
		ExpiredAt:     expiredAt,
		Deadline:      deadline,
	}
	if err := enqueueExpired(ctx, tx, approvalID, &payload); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func enqueueExpired(ctx context.Context, tx pgx.Tx, approvalID uuid.UUID, payload *approvals.ApprovalExpiredV1Payload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("serialize payload: %w", err)
	}
	eventID := approvals.DeriveOutboxEventID(approvalID, "expired")
	out := outbox.New(eventID, "approval_request", approvalID.String(), approvals.ApprovalExpiredV1, body).
		WithHeader("x-audit-correlation-id", payload.CorrelationID.String()).
		WithHeader("ol-job", "approvals/timeout-sweep").
		WithHeader("ol-run-id", approvalID.String()).
		WithHeader("ol-producer", serviceName)
	if err := outbox.Enqueue(ctx, tx, out); err != nil {
		return fmt.Errorf("outbox enqueue: %w", err)
	}
	return nil
}
