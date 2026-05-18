package envelope

import (
	"errors"
	"strings"
	"testing"
)

const validRecord = `{
	"event_id": "evt-1",
	"action_type_id": "atype-1",
	"action_name": "approve",
	"object_type_id": "otype-1",
	"object_id": "obj-1",
	"tenant": "default",
	"actor_sub": "auth0|abc",
	"actor_email": "a@b.com",
	"organization_id": "org-1",
	"status": "applied",
	"parameters": "{\"reason\":\"ok\"}",
	"previous_state": null,
	"new_state": "{\"k\":\"v\"}",
	"target_classification": null,
	"applied_at_ms": 1700000000000
}`

func TestDecode_happyPath(t *testing.T) {
	t.Parallel()
	env, err := Decode([]byte(validRecord))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.EventID != "evt-1" || env.ActionName != "approve" || env.Status != "applied" {
		t.Errorf("unexpected envelope: %+v", env)
	}
	if env.ObjectID == nil || *env.ObjectID != "obj-1" {
		t.Errorf("ObjectID = %v", env.ObjectID)
	}
	if env.PreviousState != nil {
		t.Errorf("PreviousState should be nil, got %v", env.PreviousState)
	}
	if env.AppliedAtMs != 1700000000000 {
		t.Errorf("AppliedAtMs = %d", env.AppliedAtMs)
	}
}

func TestDecode_invalidJSON(t *testing.T) {
	t.Parallel()
	_, err := Decode([]byte("{not valid"))
	if err == nil {
		t.Fatal("expected error")
	}
	var de *DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("expected *DecodeError, got %T", err)
	}
	if !IsPoison(err) {
		t.Errorf("IsPoison should be true for decode error")
	}
}

func TestDecode_missingRequired(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		patch string // field to set to empty in JSON
	}{
		{"event_id", `"event_id"`},
		{"action_type_id", `"action_type_id"`},
		{"action_name", `"action_name"`},
		{"object_type_id", `"object_type_id"`},
		{"tenant", `"tenant"`},
		{"actor_sub", `"actor_sub"`},
		{"status", `"status"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// replace the value after that key with "" — crude but
			// scoped to the test inputs.
			input := strings.Replace(validRecord, tc.patch+": \""+valueFor(tc.name)+"\"", tc.patch+": \"\"", 1)
			_, err := Decode([]byte(input))
			if err == nil {
				t.Fatalf("expected validate error for %s", tc.name)
			}
			var ve *ValidateError
			if !errors.As(err, &ve) {
				t.Fatalf("expected *ValidateError, got %T (%v)", err, err)
			}
			if ve.Field != tc.name {
				t.Errorf("ValidateError.Field = %q, want %q", ve.Field, tc.name)
			}
			if !IsPoison(err) {
				t.Errorf("IsPoison should be true for validate error")
			}
		})
	}
}

func TestDecode_appliedAtMsZeroIsRejected(t *testing.T) {
	t.Parallel()
	input := strings.Replace(validRecord, `"applied_at_ms": 1700000000000`, `"applied_at_ms": 0`, 1)
	_, err := Decode([]byte(input))
	var ve *ValidateError
	if !errors.As(err, &ve) || ve.Field != "applied_at_ms" {
		t.Fatalf("expected applied_at_ms validate error, got %v", err)
	}
}

func valueFor(field string) string {
	switch field {
	case "event_id":
		return "evt-1"
	case "action_type_id":
		return "atype-1"
	case "action_name":
		return "approve"
	case "object_type_id":
		return "otype-1"
	case "tenant":
		return "default"
	case "actor_sub":
		return "auth0|abc"
	case "status":
		return "applied"
	}
	return ""
}
