// Package conditionconsumer ports
// `services/workflow-automation-service/src/domain/condition_consumer.rs`
// 1:1.
//
// Per inbound `automate.condition.v1` the consumer:
//
//  1. Decodes the AutomateConditionV1 payload (poison-pill messages
//     are skipped without committing the offset).
//  2. Records the deterministic `derive_condition_event_id(definition_id,
//     correlation_id)` in the per-service idempotency table — a redelivery
//     short-circuits here without re-dispatching the effect.
//  3. Loads (or, on first delivery, observes) the automation_runs row.
//  4. Transitions Queued → Running via state-machine.PgStore.Apply.
//  5. Extracts the ontology-action invocation and dispatches it via
//     EffectDispatcher.DispatchWithRetries.
//  6. Persists the terminal Running → Completed/Failed transition AND
//     enqueues the outbox automate.outcome.v1 in the SAME tx (durability
//     join-point: row-in-terminal ⇔ outcome event published).
//  7. Belt-and-braces direct Kafka publish of the outcome.
//  8. Commits the Kafka offset.
package conditionconsumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	databus "github.com/openfoundry/openfoundry-go/libs/event-bus-data"
	"github.com/openfoundry/openfoundry-go/libs/idempotency"
	"github.com/openfoundry/openfoundry-go/libs/outbox"
	statemachine "github.com/openfoundry/openfoundry-go/libs/state-machine"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/domain/automationrun"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/domain/effectdispatcher"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/event"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/topics"
)

// ConsumerGroup is the Kafka consumer group used by every replica of
// the service.
const ConsumerGroup = "workflow-automation-service"

// SubscribeTopics enumerates the topics the consumer subscribes to.
var SubscribeTopics = []string{topics.AutomateConditionV1}

const defaultLineageNamespace = "openfoundry"

// Consumer is the long-lived state shared across the loop.
type Consumer struct {
	Pool             *pgxpool.Pool
	Runs             *statemachine.PgStore[*automationrun.AutomationRun, automationrun.Event]
	Idempotency      idempotency.Store
	Dispatcher       *effectdispatcher.EffectDispatcher
	Publisher        databus.Publisher
	RetryPolicy      effectdispatcher.RetryPolicy
	LineageNamespace string
}

// NewConsumer mirrors the Rust `ConditionConsumer::new`.
func NewConsumer(pool *pgxpool.Pool, idem idempotency.Store, dispatcher *effectdispatcher.EffectDispatcher, publisher databus.Publisher) *Consumer {
	runs := statemachine.NewPgStore[*automationrun.AutomationRun, automationrun.Event](
		pool, automationrun.TableName, func() *automationrun.AutomationRun { return &automationrun.AutomationRun{} },
	)
	ns := os.Getenv("OF_OPENLINEAGE_NAMESPACE")
	if ns == "" {
		ns = defaultLineageNamespace
	}
	return &Consumer{
		Pool:             pool,
		Runs:             runs,
		Idempotency:      idem,
		Dispatcher:       dispatcher,
		Publisher:        publisher,
		RetryPolicy:      effectdispatcher.DefaultRetryPolicy(),
		LineageNamespace: ns,
	}
}

// Process drives one inbound condition message. Returns a metric
// label so the caller can record the outcome.
func (c *Consumer) Process(ctx context.Context, condition event.AutomateConditionV1) (string, error) {
	eventID := event.DeriveConditionEventID(condition.DefinitionID, condition.CorrelationID)
	outcome, err := c.Idempotency.CheckAndRecord(ctx, eventID)
	if err != nil {
		return "", err
	}
	if outcome.IsAlreadyProcessed() {
		slog.Info("condition already processed; skipping",
			slog.String("event_id", eventID.String()),
			slog.String("definition_id", condition.DefinitionID.String()),
			slog.String("correlation_id", condition.CorrelationID.String()),
		)
		return "deduped", nil
	}

	runID := event.DeriveRunID(condition.DefinitionID, condition.CorrelationID)
	loaded, err := c.Runs.Load(ctx, runID)
	if err != nil {
		if statemachine.IsNotFound(err) {
			// HTTP handler is supposed to INSERT in the same tx as the
			// outbox publish. Missing row → synthetic creation path.
			slog.Warn("automation run row missing for known condition; creating on the fly",
				slog.String("run_id", runID.String()),
				slog.String("definition_id", condition.DefinitionID.String()),
			)
			run := automationrun.New(
				runID,
				event.TenantUUIDFromStr(condition.TenantID),
				condition.DefinitionID,
				condition.CorrelationID,
				nil,
			)
			loaded, err = c.Runs.Insert(ctx, run)
			if err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	}

	claimed, terminalState, claimErr := c.claimForDispatch(ctx, loaded)
	if claimErr != nil {
		return "", claimErr
	}
	if terminalState != "" {
		slog.Info("automation run already terminal; skipping dispatch",
			slog.String("run_id", runID.String()), slog.String("state", terminalState))
		return "already_terminal", nil
	}

	action, dErr := effectdispatcher.ExtractActionRequest(condition.TriggerPayload, condition.CorrelationID)
	if dErr != nil {
		if dispatchErr := effectdispatcher.AsDispatchError(dErr); dispatchErr != nil {
			if err := c.landTerminalFailed(ctx, runID, &condition, dispatchErr.Error(), claimed.Machine.Attempts); err != nil {
				return "", err
			}
			return "invalid_payload", nil
		}
		return "", dErr
	}

	disp, err := c.Dispatcher.DispatchWithRetries(ctx, action, c.RetryPolicy)
	if err == nil {
		response := append(json.RawMessage(nil), disp.Response...)
		if err := c.landTerminalCompleted(ctx, runID, &condition, response, disp.Attempts); err != nil {
			return "", err
		}
		c.publishOutcomeViaKafka(ctx, runID, &condition, "completed", response, nil, disp.Attempts)
		return "completed", nil
	}

	attempts := uint32(1)
	if de := effectdispatcher.AsDispatchError(err); de != nil && de.Kind == effectdispatcher.KindExhausted {
		attempts = de.Attempts
	}
	errMsg := err.Error()
	if err := c.landTerminalFailed(ctx, runID, &condition, errMsg, attempts); err != nil {
		return "", err
	}
	c.publishOutcomeViaKafka(ctx, runID, &condition, "failed", nil, &errMsg, attempts)
	return "failed", nil
}

// claimForDispatch ports the Rust helper. Returns (claimed, "", nil)
// when the row was successfully claimed; (claimed, "", nil) for a
// crash-recovery path on Running; ({}, terminal, nil) when the row is
// already terminal; ({}, "", err) on storage failure.
func (c *Consumer) claimForDispatch(ctx context.Context, loaded statemachine.Loaded[*automationrun.AutomationRun]) (statemachine.Loaded[*automationrun.AutomationRun], string, error) {
	switch loaded.Machine.State {
	case automationrun.StateQueued:
		next, err := c.Runs.Apply(ctx, loaded, automationrun.ClaimEvent())
		if err != nil {
			slog.Warn("automation run claim failed; surfacing", slog.String("error", err.Error()))
			return statemachine.Loaded[*automationrun.AutomationRun]{}, string(loaded.Machine.State), nil
		}
		return next, "", nil
	case automationrun.StateRunning:
		// Crash recovery: previous attempt died mid-dispatch. Re-issue
		// the effect call without bumping attempts via the SM (the row
		// stays in Running).
		return loaded, "", nil
	case automationrun.StateSuspended, automationrun.StateCompensating,
		automationrun.StateCompleted, automationrun.StateFailed:
		return statemachine.Loaded[*automationrun.AutomationRun]{}, string(loaded.Machine.State), nil
	default:
		return statemachine.Loaded[*automationrun.AutomationRun]{}, string(loaded.Machine.State), nil
	}
}

// landTerminalCompleted persists Running → Completed AND enqueues the
// outbox event in the same transaction. Mirrors the Rust helper.
func (c *Consumer) landTerminalCompleted(ctx context.Context, runID uuid.UUID, condition *event.AutomateConditionV1, response json.RawMessage, attempts uint32) error {
	tx, err := c.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	machine, version, err := loadInTx(ctx, tx, runID)
	if err != nil {
		return err
	}
	if err := machine.Apply(automationrun.EffectCompletedEvent(response)); err != nil {
		return err
	}
	if attempts > machine.Attempts {
		machine.Attempts = attempts
	}
	if err := writeInTx(ctx, tx, version, machine); err != nil {
		return err
	}

	outcome := event.AutomateOutcomeV1{
		RunID:          runID,
		DefinitionID:   condition.DefinitionID,
		TenantID:       condition.TenantID,
		CorrelationID:  condition.CorrelationID,
		Status:         "completed",
		EffectResponse: response,
		Attempts:       attempts,
	}
	if err := c.enqueueOutcome(ctx, tx, runID, condition, &outcome); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// landTerminalFailed persists either Queued → Failed (PreFlightFailed)
// or Running → Failed (EffectFailed) AND enqueues the outcome in the
// same transaction.
func (c *Consumer) landTerminalFailed(ctx context.Context, runID uuid.UUID, condition *event.AutomateConditionV1, errMsg string, attempts uint32) error {
	tx, err := c.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	machine, version, err := loadInTx(ctx, tx, runID)
	if err != nil {
		return err
	}
	var ev automationrun.Event
	if machine.State == automationrun.StateQueued {
		ev = automationrun.PreFlightFailedEvent(errMsg)
	} else {
		ev = automationrun.EffectFailedEvent(errMsg)
	}
	if err := machine.Apply(ev); err != nil {
		return err
	}
	if attempts > machine.Attempts {
		machine.Attempts = attempts
	}
	if err := writeInTx(ctx, tx, version, machine); err != nil {
		return err
	}

	errMsgCopy := errMsg
	outcome := event.AutomateOutcomeV1{
		RunID:         runID,
		DefinitionID:  condition.DefinitionID,
		TenantID:      condition.TenantID,
		CorrelationID: condition.CorrelationID,
		Status:        "failed",
		Error:         &errMsgCopy,
		Attempts:      attempts,
	}
	if err := c.enqueueOutcome(ctx, tx, runID, condition, &outcome); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// publishOutcomeViaKafka mirrors the belt-and-braces direct publish.
func (c *Consumer) publishOutcomeViaKafka(ctx context.Context, runID uuid.UUID, condition *event.AutomateConditionV1, status string, effectResponse json.RawMessage, errMsg *string, attempts uint32) {
	if c.Publisher == nil {
		return
	}
	outcome := event.AutomateOutcomeV1{
		RunID:          runID,
		DefinitionID:   condition.DefinitionID,
		TenantID:       condition.TenantID,
		CorrelationID:  condition.CorrelationID,
		Status:         status,
		EffectResponse: effectResponse,
		Error:          errMsg,
		Attempts:       attempts,
	}
	body, err := json.Marshal(outcome)
	if err != nil {
		slog.Warn("encode outcome event failed; outbox/Debezium will retry",
			slog.String("error", err.Error()))
		return
	}
	headers := databus.NewOpenLineageHeaders(
		c.LineageNamespace,
		fmt.Sprintf("automation_run/%s", condition.TenantID),
		runID.String(),
		ConsumerGroup,
	)
	key := runID[:]
	if err := c.Publisher.Publish(ctx, topics.AutomateOutcomeV1, key, body, &headers); err != nil {
		slog.Warn("direct Kafka outcome publish failed; outbox/Debezium will retry",
			slog.String("run_id", runID.String()),
			slog.String("status", status),
			slog.String("error", err.Error()))
	}
}

// enqueueOutcome appends the outcome event to outbox.events inside `tx`.
func (c *Consumer) enqueueOutcome(ctx context.Context, tx pgx.Tx, runID uuid.UUID, condition *event.AutomateConditionV1, outcome *event.AutomateOutcomeV1) error {
	payload, err := json.Marshal(outcome)
	if err != nil {
		return err
	}
	// Deterministic event id: collapses retries inside the outbox table
	// onto the same row. 33 bytes: run_id || correlation_id || status_byte.
	var bytes [33]byte
	copy(bytes[:16], runID[:])
	copy(bytes[16:32], condition.CorrelationID[:])
	switch outcome.Status {
	case "completed":
		bytes[32] = 'C'
	case "failed":
		bytes[32] = 'F'
	default:
		bytes[32] = 'X'
	}
	eventID := uuid.NewSHA1(event.WorkflowAutomationNamespace, bytes[:])
	out := outbox.New(eventID, "automation_run", runID.String(), topics.AutomateOutcomeV1, payload).
		WithHeader("ol-namespace", c.LineageNamespace).
		WithHeader("ol-job", fmt.Sprintf("automation_run/%s", condition.TenantID)).
		WithHeader("ol-run-id", runID.String()).
		WithHeader("ol-producer", ConsumerGroup).
		WithHeader("x-audit-correlation-id", condition.CorrelationID.String())
	return outbox.Enqueue(ctx, tx, out)
}

// loadInTx + writeInTx mirror the Rust helpers via raw pgx.Tx. Kept
// as package-internal helpers to keep the per-message function shape
// 1:1 with Rust.
func loadInTx(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (*automationrun.AutomationRun, int64, error) {
	row := tx.QueryRow(ctx,
		"SELECT state_data, version FROM workflow_automation.automation_runs WHERE id = $1 FOR UPDATE",
		runID,
	)
	var (
		payload []byte
		version int64
	)
	if err := row.Scan(&payload, &version); err != nil {
		return nil, 0, err
	}
	machine := &automationrun.AutomationRun{}
	if err := json.Unmarshal(payload, machine); err != nil {
		return nil, 0, fmt.Errorf("invalid persisted state for id=%s: %w", runID, err)
	}
	return machine, version, nil
}

func writeInTx(ctx context.Context, tx pgx.Tx, prevVersion int64, next *automationrun.AutomationRun) error {
	payload, err := json.Marshal(next)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx,
		"UPDATE workflow_automation.automation_runs "+
			"SET state = $1, state_data = $2, version = version + 1, expires_at = $3, updated_at = now() "+
			"WHERE id = $4 AND version = $5",
		next.CurrentState(), payload, next.ExpiresAt(), next.AggregateID(), prevVersion,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &statemachine.StaleError{ID: next.AggregateID(), ExpectedVersion: prevVersion}
	}
	return nil
}

// ─────────────────────── public Run loop ──────────────────────────────

// Run is the at-least-once consumer loop.
//
// One Kafka message ⇒ one full dispatch (Queued → Running →
// Completed/Failed). Offset is committed AFTER the terminal
// transition so a crash mid-dispatch replays the condition; the
// per-condition idempotency store + the row's state guarantee no
// duplicate side effects on the happy path.
func Run(ctx context.Context, c *Consumer, sub databus.Subscriber) error {
	for {
		msg, err := sub.Poll(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return ctx.Err()
			}
			return err
		}
		label, err := processMessage(ctx, c, msg)
		if err != nil {
			slog.Error("condition processing failed; offset uncommitted",
				slog.String("error", err.Error()))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
			continue
		}
		slog.Debug("condition processed",
			slog.String("label", label), slog.String("topic", msg.Topic))
		if err := sub.CommitMessages(ctx, []*databus.DataMessage{msg}); err != nil {
			return err
		}
	}
}

func processMessage(ctx context.Context, c *Consumer, msg *databus.DataMessage) (string, error) {
	if len(msg.Value) == 0 {
		slog.Warn("skipping condition without payload",
			slog.String("topic", msg.Topic),
			slog.Int("partition", msg.Partition),
			slog.Int64("offset", msg.Offset))
		return "empty_payload", nil
	}
	condition, err := DecodeCondition(msg.Value)
	if err != nil {
		slog.Warn("skipping malformed condition", slog.String("error", err.Error()))
		return "decode_error", nil
	}
	return c.Process(ctx, condition)
}

// DecodeCondition decodes a Kafka payload as AutomateConditionV1.
func DecodeCondition(payload []byte) (event.AutomateConditionV1, error) {
	var c event.AutomateConditionV1
	err := json.Unmarshal(payload, &c)
	return c, err
}
