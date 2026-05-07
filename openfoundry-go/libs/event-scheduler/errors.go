package eventscheduler

import (
	"errors"
	"fmt"
)

// ErrorKind tags a [SchedulerError]. Mirrors the Rust thiserror enum.
type ErrorKind int

const (
	// ErrDB — underlying Postgres error.
	ErrDB ErrorKind = iota
	// ErrPublish — Kafka publish failed for the row identified by Name.
	ErrPublish
	// ErrInvalidCron — cron_expr could not be parsed.
	ErrInvalidCron
	// ErrUnknownFlavor — cron_flavor column held an unrecognised value.
	ErrUnknownFlavor
	// ErrInvalidTimeZone — time_zone column was not a valid IANA zone.
	ErrInvalidTimeZone
	// ErrNoFutureFire — the cron expression has no matching instant
	// within the evaluator's 10-year horizon.
	ErrNoFutureFire
)

// SchedulerError is the failure mode raised by [Scheduler.Tick] and
// helpers. Mirrors the Rust thiserror enum.
type SchedulerError struct {
	Kind ErrorKind
	// Name — schedule name, populated for every variant except ErrDB.
	Name string
	// Topic — Kafka topic, populated for ErrPublish.
	Topic string
	// Flavor — raw cron_flavor value, populated for ErrUnknownFlavor.
	Flavor string
	// TimeZone — raw time_zone value, populated for ErrInvalidTimeZone.
	TimeZone string
	// CronExpr — raw cron_expr value, populated for ErrNoFutureFire.
	CronExpr string
	// Cause — underlying error (DB / publish / cron parse).
	Cause error
}

// Error implements [error]. Mirrors the Rust thiserror messages
// byte-for-byte where the variants overlap.
func (e *SchedulerError) Error() string {
	switch e.Kind {
	case ErrDB:
		return fmt.Sprintf("database error: %s", e.Cause)
	case ErrPublish:
		return fmt.Sprintf("publish to topic `%s` failed for schedule `%s`: %s", e.Topic, e.Name, e.Cause)
	case ErrInvalidCron:
		return fmt.Sprintf("invalid cron expression for schedule `%s`: %s", e.Name, e.Cause)
	case ErrUnknownFlavor:
		return fmt.Sprintf("unknown cron flavor `%s` for schedule `%s` (expected `unix5` or `quartz6`)", e.Flavor, e.Name)
	case ErrInvalidTimeZone:
		return fmt.Sprintf("invalid time zone `%s` for schedule `%s`", e.TimeZone, e.Name)
	case ErrNoFutureFire:
		return fmt.Sprintf("schedule `%s` has no future fire within 10 years (cron: `%s`)", e.Name, e.CronExpr)
	}
	return "unknown scheduler error"
}

// Unwrap exposes Cause to errors.Is / errors.As.
func (e *SchedulerError) Unwrap() error { return e.Cause }

// IsKind is a convenience predicate for callers that want to switch
// on the underlying [ErrorKind] without unwrapping.
func IsKind(err error, kind ErrorKind) bool {
	var se *SchedulerError
	if !errors.As(err, &se) {
		return false
	}
	return se.Kind == kind
}
