package automationoperations

import "github.com/google/uuid"

// AutomationOpsNamespace is the UUIDv5 namespace for everything
// emitted by this subsystem. Pinned forever.
var AutomationOpsNamespace = uuid.UUID{
	0x83, 0xa1, 0x71, 0x2c, 0x4f, 0x9d, 0x49, 0x46,
	0x9b, 0xc4, 0xb1, 0x6e, 0x6e, 0x57, 0x2c, 0x18,
}

// DeriveSagaID mirrors `event::derive_saga_id`. Producer retries that
// re-publish the same (task_type, correlation_id) pair collapse onto
// the same saga.state row.
func DeriveSagaID(taskType string, correlationID uuid.UUID) uuid.UUID {
	buf := make([]byte, 0, len(taskType)+1+16)
	buf = append(buf, []byte(taskType)...)
	buf = append(buf, '|')
	buf = append(buf, correlationID[:]...)
	return uuid.NewSHA1(AutomationOpsNamespace, buf)
}

// DeriveRequestEventID mirrors `event::derive_request_event_id`.
// Distinct namespace so the saga id and the dedup event id never
// collide.
func DeriveRequestEventID(taskType string, correlationID uuid.UUID) uuid.UUID {
	buf := make([]byte, 0, len(taskType)+2+16)
	buf = append(buf, []byte(taskType)...)
	buf = append(buf, '|')
	buf = append(buf, correlationID[:]...)
	buf = append(buf, 'R')
	return uuid.NewSHA1(AutomationOpsNamespace, buf)
}
