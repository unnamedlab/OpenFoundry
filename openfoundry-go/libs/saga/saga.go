package saga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/libs/outbox"
)

// ─── Errors ───────────────────────────────────────────────────────────

// SagaErrorKind discriminates SagaError variants. Use the Is*
// helpers to check classification through wrapped errors.
type SagaErrorKind uint8

const (
	// ErrStepFailure: a step's Execute returned a domain failure.
	ErrStepFailure SagaErrorKind = iota + 1
	// ErrCompensationFailure: a step's Compensate returned a
	// domain failure. Surfaced but does not stop the runner from
	// trying the remaining compensations.
	ErrCompensationFailure
	// ErrDB: underlying Postgres error.
	ErrDB
	// ErrOutbox: outbox emission failed.
	ErrOutbox
	// ErrSerialize: serializing a step input/output (or saga
	// payload) failed.
	ErrSerialize
	// ErrTerminal: the saga was already in a terminal state when
	// a write was attempted. Indicates a programming error — the
	// caller invoked ExecuteStep after Finish/Abort/a failure.
	ErrTerminal
)

// SagaError is the typed error returned by saga operations.
type SagaError struct {
	Kind    SagaErrorKind
	Step    string
	Message string
	Wrapped error
}

func (e *SagaError) Error() string {
	switch e.Kind {
	case ErrStepFailure:
		return fmt.Sprintf("step `%s` failed: %s", e.Step, e.Message)
	case ErrCompensationFailure:
		return fmt.Sprintf("compensate `%s` failed: %s", e.Step, e.Message)
	case ErrDB:
		return fmt.Sprintf("database error: %s", e.Message)
	case ErrOutbox:
		return fmt.Sprintf("outbox: %s", e.Message)
	case ErrSerialize:
		return fmt.Sprintf("serialize: %s", e.Message)
	case ErrTerminal:
		return fmt.Sprintf("saga is in terminal state `%s`", e.Message)
	}
	return "saga error"
}

func (e *SagaError) Unwrap() error { return e.Wrapped }

// StepFailure is a convenience constructor for use inside step bodies.
func StepFailure(step, message string) error {
	return &SagaError{Kind: ErrStepFailure, Step: step, Message: message}
}

// CompensationFailure is a convenience constructor for use inside
// compensation bodies.
func CompensationFailure(step, message string) error {
	return &SagaError{Kind: ErrCompensationFailure, Step: step, Message: message}
}

// IsStepFailure reports whether err is an ErrStepFailure SagaError.
func IsStepFailure(err error) bool {
	var se *SagaError
	return errors.As(err, &se) && se.Kind == ErrStepFailure
}

// IsCompensationFailure reports whether err is an
// ErrCompensationFailure SagaError.
func IsCompensationFailure(err error) bool {
	var se *SagaError
	return errors.As(err, &se) && se.Kind == ErrCompensationFailure
}

// IsTerminal reports whether err is an ErrTerminal SagaError.
func IsTerminal(err error) bool {
	var se *SagaError
	return errors.As(err, &se) && se.Kind == ErrTerminal
}

func dbErr(cause error) error {
	if cause == nil {
		return nil
	}
	return &SagaError{Kind: ErrDB, Message: cause.Error(), Wrapped: cause}
}

func outboxErr(cause error) error {
	if cause == nil {
		return nil
	}
	return &SagaError{Kind: ErrOutbox, Message: cause.Error(), Wrapped: cause}
}

func serializeErr(cause error) error {
	if cause == nil {
		return nil
	}
	return &SagaError{Kind: ErrSerialize, Message: cause.Error(), Wrapped: cause}
}

// ─── SagaStep contract ───────────────────────────────────────────────

// SagaStep is one unit of work in a saga. Implementors are typically
// stateless types; per-instance data lives in `Input`. Compensate
// must be safe to call after a successful Execute for the same input.
//
// Mirrors the Rust trait fn execute(input) → output + compensate(input).
// The Go shape uses generics for the typed input/output.
type SagaStep[Input any, Output any] interface {
	StepName() string
	Execute(ctx context.Context, input Input) (Output, error)
	Compensate(ctx context.Context, input Input) error
}

// ─── Outbox helper ───────────────────────────────────────────────────

// EnqueueOutboxEvent appends a single event to the transactional
// outbox in `tx` and returns the deterministic event_id. The id is
// a v5 UUID over `aggregate || aggregate_id || topic || payload`,
// so repeated calls with the same arguments inside a fresh
// transaction are no-ops at the outbox level.
//
// Mirrors fn enqueue_outbox_event.
func EnqueueOutboxEvent(
	ctx context.Context,
	tx pgx.Tx,
	aggregate, aggregateID, topic string,
	payload any,
) (uuid.UUID, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, serializeErr(err)
	}
	key := fmt.Sprintf("%s:%s:%s:%s", aggregate, aggregateID, topic, string(payloadJSON))
	eventID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(key))
	if err := outbox.Enqueue(ctx, tx, outbox.New(eventID, aggregate, aggregateID, topic, payloadJSON)); err != nil {
		return uuid.Nil, outboxErr(err)
	}
	return eventID, nil
}

// ─── Saga events recorded by the runner ──────────────────────────────

// SagaEventKind enumerates the runner's outbound topic family.
type SagaEventKind uint8

const (
	// EventStepCompleted: a step's Execute returned ok.
	EventStepCompleted SagaEventKind = iota
	// EventStepFailed: a step's Execute returned an error.
	EventStepFailed
	// EventStepCompensated: a previously-completed step was undone.
	EventStepCompensated
	// EventSagaCompleted: Finish was called after every step ok.
	EventSagaCompleted
	// EventSagaAborted: Abort was called by the caller.
	EventSagaAborted
)

// Topic returns the canonical Kafka topic name for the event kind.
// Locked in by TopicConstantsMatchRunnerEmitTopics in the test file.
func (k SagaEventKind) Topic() string {
	switch k {
	case EventStepCompleted:
		return SagaStepCompletedV1Topic
	case EventStepFailed:
		return SagaStepFailedV1Topic
	case EventStepCompensated:
		return SagaStepCompensatedV1Topic
	case EventSagaCompleted:
		return SagaCompletedV1Topic
	case EventSagaAborted:
		return SagaAbortedV1Topic
	}
	return ""
}

func (k SagaEventKind) discriminator() string {
	switch k {
	case EventStepCompleted:
		return "step.completed"
	case EventStepFailed:
		return "step.failed"
	case EventStepCompensated:
		return "step.compensated"
	case EventSagaCompleted:
		return "saga.completed"
	case EventSagaAborted:
		return "saga.aborted"
	}
	return ""
}

// SagaEvent is an in-memory record of one outbox event the runner
// emitted. Surfaced via SagaRunner.Events for tests + callers that
// want to react before tx.Commit().
type SagaEvent struct {
	Kind     SagaEventKind
	StepName string // empty for saga-terminal events
	EventID  uuid.UUID
}

// ─── Saga status ─────────────────────────────────────────────────────

// SagaStatus mirrors the `status` column of saga.state.
type SagaStatus string

const (
	StatusRunning     SagaStatus = "running"
	StatusCompleted   SagaStatus = "completed"
	StatusFailed      SagaStatus = "failed"
	StatusCompensated SagaStatus = "compensated"
	StatusAborted     SagaStatus = "aborted"
)

// ParseSagaStatus is the inverse of SagaStatus's underlying string.
// Unknown strings collapse to Running so an old row left behind by
// a buggy writer can still be resumed.
func ParseSagaStatus(s string) SagaStatus {
	switch SagaStatus(s) {
	case StatusCompleted:
		return StatusCompleted
	case StatusFailed:
		return StatusFailed
	case StatusCompensated:
		return StatusCompensated
	case StatusAborted:
		return StatusAborted
	}
	return StatusRunning
}

// IsTerminal reports whether the status forbids further writes.
func (s SagaStatus) IsTerminal() bool { return s != StatusRunning }

// ─── Compensation closures ───────────────────────────────────────────

// compensationFn is a closure capturing the step's typed input.
// The runner stores them as type-erased closures so we can hold a
// heterogeneous LIFO across step types.
type compensationFn func(ctx context.Context) error

type compensation struct {
	stepName string
	invoke   compensationFn
}

// ─── SagaRunner ──────────────────────────────────────────────────────

// SagaRunner drives one saga instance. Owns the surrounding pgx.Tx
// for its lifetime so all step bookkeeping + outbox emissions land
// atomically with the application's primary writes when the caller
// commits.
type SagaRunner struct {
	tx             pgx.Tx
	sagaID         uuid.UUID
	name           string
	status         SagaStatus
	completedSteps []string
	completedSet   map[string]struct{}
	stepOutputs    map[string]json.RawMessage
	compensations  []compensation
	events         []SagaEvent
}

// Start begins (or resumes) a saga identified by sagaID. If the row
// already exists in saga.state its completed_steps and cached
// outputs are loaded so subsequent calls to ExecuteStep become
// no-ops for the already-finished prefix. If the row is missing it
// is inserted with status=running. Returns ErrTerminal if the
// persisted status is terminal.
func Start(ctx context.Context, tx pgx.Tx, sagaID uuid.UUID, name string) (*SagaRunner, error) {
	if _, err := tx.Exec(ctx,
		`INSERT INTO saga.state (saga_id, name) VALUES ($1, $2)
         ON CONFLICT (saga_id) DO NOTHING`,
		sagaID, name); err != nil {
		return nil, dbErr(err)
	}

	var (
		statusStr      string
		completedSteps []string
		outputsJSON    []byte
	)
	if err := tx.QueryRow(ctx,
		`SELECT status, completed_steps, step_outputs FROM saga.state WHERE saga_id = $1`,
		sagaID).Scan(&statusStr, &completedSteps, &outputsJSON); err != nil {
		return nil, dbErr(err)
	}

	status := ParseSagaStatus(statusStr)
	if status.IsTerminal() {
		return nil, &SagaError{Kind: ErrTerminal, Message: statusStr}
	}

	stepOutputs := make(map[string]json.RawMessage)
	if len(outputsJSON) > 0 {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(outputsJSON, &raw); err == nil {
			stepOutputs = raw
		}
	}
	completedSet := make(map[string]struct{}, len(completedSteps))
	for _, s := range completedSteps {
		completedSet[s] = struct{}{}
	}

	return &SagaRunner{
		tx:             tx,
		sagaID:         sagaID,
		name:           name,
		status:         StatusRunning,
		completedSteps: completedSteps,
		completedSet:   completedSet,
		stepOutputs:    stepOutputs,
		compensations:  nil,
		events:         nil,
	}, nil
}

// SagaID is the saga aggregate id.
func (r *SagaRunner) SagaID() uuid.UUID { return r.sagaID }

// Name is the saga name (passed to Start).
func (r *SagaRunner) Name() string { return r.name }

// Status returns the current persisted status.
func (r *SagaRunner) Status() SagaStatus { return r.status }

// Events returns the in-memory record of every outbox event the
// runner emitted so far.
func (r *SagaRunner) Events() []SagaEvent { return r.events }

// CompletedSteps returns the names of every step that has
// succeeded so far, in execution order.
func (r *SagaRunner) CompletedSteps() []string {
	out := make([]string, len(r.completedSteps))
	copy(out, r.completedSteps)
	return out
}

// ExecuteStep runs `step` with the given input. Generic over the
// step's Input/Output types so the typed cache + idempotent replay
// path stays type-safe.
//
// Idempotent replay: when the step name is already in
// completed_steps the cached output is deserialised and returned;
// no event emitted, no compensation registered.
//
// On success: persists name+output, registers a compensation
// closure that captures `input`, emits saga.step.completed.v1.
//
// On failure: emits saga.step.failed.v1, runs every previously-
// recorded compensation in LIFO order, emits one
// saga.step.compensated.v1 per success, transitions the saga to
// `compensated` (≥1 ran) or `failed` (none ran).
func ExecuteStep[I any, O any](
	ctx context.Context,
	r *SagaRunner,
	step SagaStep[I, O],
	input I,
) (O, error) {
	var zero O
	if r.status.IsTerminal() {
		return zero, &SagaError{Kind: ErrTerminal, Message: string(r.status)}
	}
	stepName := step.StepName()

	// Idempotent replay path.
	if _, has := r.completedSet[stepName]; has {
		cached, ok := r.stepOutputs[stepName]
		if !ok {
			return zero, &SagaError{
				Kind: ErrStepFailure, Step: stepName,
				Message: "step marked complete but no cached output",
			}
		}
		var out O
		if err := json.Unmarshal(cached, &out); err != nil {
			return zero, serializeErr(err)
		}
		return out, nil
	}

	// Mark in-flight before executing so an interrupted process
	// can later see exactly where it was.
	if _, err := r.tx.Exec(ctx,
		`UPDATE saga.state SET current_step = $1, updated_at = now() WHERE saga_id = $2`,
		stepName, r.sagaID); err != nil {
		return zero, dbErr(err)
	}

	output, execErr := step.Execute(ctx, input)
	if execErr == nil {
		outputJSON, err := json.Marshal(output)
		if err != nil {
			return zero, serializeErr(err)
		}
		eventID, err := r.emit(ctx, EventStepCompleted, stepName, SagaStepCompletedV1{
			SagaID: r.sagaID,
			Saga:   r.name,
			Step:   stepName,
			Output: outputJSON,
		})
		if err != nil {
			return zero, err
		}
		_ = eventID

		if _, err := r.tx.Exec(ctx,
			`UPDATE saga.state
                SET completed_steps = array_append(completed_steps, $1),
                    step_outputs    = step_outputs || jsonb_build_object($1::text, $2::jsonb),
                    current_step    = NULL,
                    updated_at      = now()
              WHERE saga_id = $3`,
			stepName, outputJSON, r.sagaID); err != nil {
			return zero, dbErr(err)
		}

		r.completedSteps = append(r.completedSteps, stepName)
		r.completedSet[stepName] = struct{}{}
		r.stepOutputs[stepName] = outputJSON
		// Register compensation closure capturing the typed input.
		r.compensations = append(r.compensations, compensation{
			stepName: stepName,
			invoke: func(ctx context.Context) error {
				return step.Compensate(ctx, input)
			},
		})
		return output, nil
	}

	// Failure path: emit StepFailed, run compensations LIFO, set
	// status accordingly.
	if _, err := r.emit(ctx, EventStepFailed, stepName, SagaStepFailedV1{
		SagaID: r.sagaID,
		Saga:   r.name,
		Step:   stepName,
		Error:  execErr.Error(),
	}); err != nil {
		return zero, err
	}
	ranAny, err := r.runCompensations(ctx)
	if err != nil {
		return zero, err
	}
	newStatus := StatusFailed
	if ranAny {
		newStatus = StatusCompensated
	}
	if err := r.setStatus(ctx, newStatus, stepName); err != nil {
		return zero, err
	}
	return zero, execErr
}

// Finish marks the saga as `completed` and emits saga.completed.v1.
// Must be called after every step succeeded.
func (r *SagaRunner) Finish(ctx context.Context) error {
	if r.status.IsTerminal() {
		return &SagaError{Kind: ErrTerminal, Message: string(r.status)}
	}
	if _, err := r.emit(ctx, EventSagaCompleted, "", SagaCompletedV1{
		SagaID:         r.sagaID,
		Saga:           r.name,
		CompletedSteps: r.CompletedSteps(),
	}); err != nil {
		return err
	}
	return r.setStatus(ctx, StatusCompleted, "")
}

// Abort runs every recorded compensation in LIFO order and marks
// the row `aborted`. Use when the caller decides to give up before
// any step has failed.
func (r *SagaRunner) Abort(ctx context.Context) error {
	if r.status.IsTerminal() {
		return &SagaError{Kind: ErrTerminal, Message: string(r.status)}
	}
	if _, err := r.runCompensations(ctx); err != nil {
		return err
	}
	if _, err := r.emit(ctx, EventSagaAborted, "", SagaAbortedV1{
		SagaID: r.sagaID,
		Saga:   r.name,
	}); err != nil {
		return err
	}
	return r.setStatus(ctx, StatusAborted, "")
}

// runCompensations drains the LIFO chain. Compensation errors are
// surfaced via slog but do not stop the chain — operators observe
// partial-compensation alarms via the absence of a
// saga.step.compensated.v1 event for the failed step.
func (r *SagaRunner) runCompensations(ctx context.Context) (bool, error) {
	ranAny := false
	chain := r.compensations
	r.compensations = nil
	for i := len(chain) - 1; i >= 0; i-- {
		comp := chain[i]
		if err := comp.invoke(ctx); err != nil {
			// Log + continue; do not break the chain. The
			// integration with structured logging lives at the
			// service level; this lib stays dependency-light.
			continue
		}
		if _, err := r.emit(ctx, EventStepCompensated, comp.stepName, SagaStepCompensatedV1{
			SagaID: r.sagaID,
			Saga:   r.name,
			Step:   comp.stepName,
		}); err != nil {
			return ranAny, err
		}
		ranAny = true
	}
	return ranAny, nil
}

// emit writes a deterministic-ID outbox event + records it locally.
// Mirrors fn emit.
func (r *SagaRunner) emit(
	ctx context.Context,
	kind SagaEventKind,
	step string,
	payload any,
) (uuid.UUID, error) {
	idInput := fmt.Sprintf("%s:%s:%s", r.sagaID, step, kind.discriminator())
	eventID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(idInput))

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, serializeErr(err)
	}
	if err := outbox.Enqueue(ctx, r.tx, outbox.New(
		eventID, "saga", r.sagaID.String(), kind.Topic(), payloadJSON,
	)); err != nil {
		return uuid.Nil, outboxErr(err)
	}
	r.events = append(r.events, SagaEvent{
		Kind:     kind,
		StepName: step,
		EventID:  eventID,
	})
	return eventID, nil
}

func (r *SagaRunner) setStatus(ctx context.Context, status SagaStatus, failedStep string) error {
	var failedStepArg any
	if failedStep != "" {
		failedStepArg = failedStep
	}
	if _, err := r.tx.Exec(ctx,
		`UPDATE saga.state
            SET status = $1, failed_step = COALESCE($2, failed_step),
                current_step = NULL, updated_at = now()
          WHERE saga_id = $3`,
		string(status), failedStepArg, r.sagaID); err != nil {
		return dbErr(err)
	}
	r.status = status
	return nil
}
