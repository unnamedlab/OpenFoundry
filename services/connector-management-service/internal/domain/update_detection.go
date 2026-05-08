package domain

// UpdateState mirrors Rust's update_detection::UpdateState JSON spelling.
type UpdateState string

const (
	UpdateStateFirstSeen UpdateState = "first_seen"
	UpdateStateUnknown   UpdateState = "unknown"
	UpdateStateUnchanged UpdateState = "unchanged"
	UpdateStateChanged   UpdateState = "changed"
)

type UpdateOutcome struct {
	Selector          string      `json:"selector"`
	State             UpdateState `json:"state"`
	PreviousSignature *string     `json:"previous_signature,omitempty"`
	CurrentSignature  *string     `json:"current_signature,omitempty"`
}

func EvaluateUpdate(selector string, previous, current *string) UpdateOutcome {
	state := UpdateStateChanged
	switch {
	case previous == nil:
		state = UpdateStateFirstSeen
	case current == nil:
		state = UpdateStateUnknown
	case *previous == *current:
		state = UpdateStateUnchanged
	}
	return UpdateOutcome{Selector: selector, State: state, PreviousSignature: previous, CurrentSignature: current}
}
