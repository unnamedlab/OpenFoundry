// Package envelope owns the wire format of an
// `ontology.actions.applied.v1` Kafka record.
//
// Shape mirrors the publisher contract in
// libs/ontology-kernel/handlers/actions/side_effects.go::publishActionAuditToKafka
// byte-for-byte. The Scala port (ActionLogStreamSink) consumed the
// same envelope; the Iceberg target table is
// `lakekeeper.default.action_log` with the column set listed in
// FieldSpecs.
package envelope

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ActionEnvelope is the JSON record landing on
// `ontology.actions.applied.v1`. Required fields surface as
// ErrMissingField in Validate; optional fields are pointer-typed when
// the producer omits them.
type ActionEnvelope struct {
	EventID              string  `json:"event_id"`
	ActionTypeID         string  `json:"action_type_id"`
	ActionName           string  `json:"action_name"`
	ObjectTypeID         string  `json:"object_type_id"`
	ObjectID             *string `json:"object_id,omitempty"`
	Tenant               string  `json:"tenant"`
	ActorSub             string  `json:"actor_sub"`
	ActorEmail           *string `json:"actor_email,omitempty"`
	OrganizationID       *string `json:"organization_id,omitempty"`
	Status               string  `json:"status"`
	Parameters           *string `json:"parameters,omitempty"`
	PreviousState        *string `json:"previous_state,omitempty"`
	NewState             *string `json:"new_state,omitempty"`
	TargetClassification *string `json:"target_classification,omitempty"`
	AppliedAtMs          int64   `json:"applied_at_ms"`
}

// Decode is the canonical JSON decoder. Returns *DecodeError on JSON
// failure, *ValidateError on missing required fields.
func Decode(b []byte) (ActionEnvelope, error) {
	var env ActionEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		return ActionEnvelope{}, &DecodeError{Cause: err}
	}
	if err := env.Validate(); err != nil {
		return ActionEnvelope{}, err
	}
	return env, nil
}

// Validate enforces the required-field set the Iceberg table declares
// NOT NULL. A field that fails here is a poison record — the runtime
// counts it under outcome=poison and moves on.
func (e ActionEnvelope) Validate() error {
	required := []struct {
		name  string
		value string
	}{
		{"event_id", e.EventID},
		{"action_type_id", e.ActionTypeID},
		{"action_name", e.ActionName},
		{"object_type_id", e.ObjectTypeID},
		{"tenant", e.Tenant},
		{"actor_sub", e.ActorSub},
		{"status", e.Status},
	}
	for _, f := range required {
		if f.value == "" {
			return &ValidateError{Field: f.name}
		}
	}
	if e.AppliedAtMs <= 0 {
		return &ValidateError{Field: "applied_at_ms"}
	}
	return nil
}

// DecodeError wraps a JSON decode failure.
type DecodeError struct{ Cause error }

func (e *DecodeError) Error() string { return fmt.Sprintf("invalid action envelope JSON: %s", e.Cause) }
func (e *DecodeError) Unwrap() error { return e.Cause }

// ValidateError carries the name of the missing/empty required field.
type ValidateError struct{ Field string }

func (e *ValidateError) Error() string {
	return fmt.Sprintf("action envelope missing required field %q", e.Field)
}

// ErrPoison is the umbrella error wrapped by both Decode failures.
// Use errors.Is(err, ErrPoison) in the runtime to classify metrics.
var ErrPoison = errors.New("poison action record")

// IsPoison reports whether err originates from Decode or Validate.
func IsPoison(err error) bool {
	var de *DecodeError
	var ve *ValidateError
	return errors.As(err, &de) || errors.As(err, &ve)
}
