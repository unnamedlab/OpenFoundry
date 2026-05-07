package steps

import (
	"context"
	"encoding/json"
)

var _ = context.Canceled // keep context import alive after dropping the saga import

// RetentionSweepInput mirrors the Rust struct.
type RetentionSweepInput struct {
	TenantID       string `json:"tenant_id"`
	OlderThanDays  uint32 `json:"older_than_days,omitempty"`
	DryRun         bool   `json:"dry_run,omitempty"`
}

// UnmarshalJSON applies `default = 90` for older_than_days when the
// caller omits it (mirrors the Rust `#[serde(default = "default_days")]`).
func (in *RetentionSweepInput) UnmarshalJSON(data []byte) error {
	type alias RetentionSweepInput
	out := alias{OlderThanDays: 90}
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	if out.OlderThanDays == 0 {
		out.OlderThanDays = 90
	}
	*in = RetentionSweepInput(out)
	return nil
}

// RetentionSweepOutput mirrors the Rust struct.
type RetentionSweepOutput struct {
	Evicted        uint64 `json:"evicted"`
	OlderThanDays  uint32 `json:"older_than_days"`
	DryRun         bool   `json:"dry_run"`
}

// EvictRetentionEligible is the single step of the `retention.sweep`
// saga.
type EvictRetentionEligible struct{}

// StepName satisfies SagaStep.
func (EvictRetentionEligible) StepName() string { return "evict_retention_eligible" }

// Execute satisfies SagaStep — pure stub that echoes the input.
// TODO: replace with HTTP call to audit-compliance-service.
func (EvictRetentionEligible) Execute(_ context.Context, in RetentionSweepInput) (RetentionSweepOutput, error) {
	return RetentionSweepOutput{
		Evicted:       0,
		OlderThanDays: in.OlderThanDays,
		DryRun:        in.DryRun,
	}, nil
}

// Compensate satisfies SagaStep — nothing to compensate.
func (EvictRetentionEligible) Compensate(_ context.Context, _ RetentionSweepInput) error { return nil }
