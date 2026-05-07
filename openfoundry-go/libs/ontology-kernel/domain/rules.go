// Rule engine: pure-evaluator + DB-backed catalog + machinery queue.
//
// Mirrors `libs/ontology-kernel/src/domain/rules.rs` 1:1 across the
// 14 public symbols. The pure-logic functions (DeriveChangedProperties,
// EvaluateRuleAgainstObject, BuildRuleEffectPreview,
// BuildRuleEvaluateResponse, derivedPriorityScore, dynamicPressureFromQueue)
// are byte-for-byte equivalent to the Rust impl and exhaustively
// unit-tested.
//
// One sub-phase deferral: ApplyRuleEffect wraps writeback::apply_object_with_outbox
// (Rust `domain/writeback.rs`, ~246 LOC) plus three object-handler
// helpers; the writeback port lands as a follow-up so the function
// returns a typed `ErrApplyEffectNotWired` while the catalog read
// paths and the rule evaluator are fully usable today.
package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

const ruleRunKind = "rule_run"

// ErrApplyEffectNotWired is returned by ApplyRuleEffect until the
// writeback.go port lands. The handler layer maps it to HTTP 501 so
// the client sees a clear "not yet implemented" response instead of
// a silent no-op.
var ErrApplyEffectNotWired = errors.New("apply_rule_effect: writeback.apply_object_with_outbox not yet ported (see migration plan)")

// ── Pure helpers (1:1 with Rust private functions) ──────────────────

func parsePayloadUUID(payload map[string]any, field string) *uuid.UUID {
	raw, ok := payload[field].(string)
	if !ok {
		return nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	return &id
}

func actionEntryToRuleRun(entry storage.ActionLogEntry) *models.OntologyRuleRun {
	if entry.Kind != ruleRunKind {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(entry.Payload, &payload); err != nil {
		return nil
	}
	id := parsePayloadUUID(payload, "id")
	if id == nil {
		id = parsePayloadUUID(payload, "rule_run_id")
	}
	if id == nil {
		if parsed, err := uuid.Parse(entry.ActionID); err == nil {
			id = &parsed
		}
	}
	if id == nil {
		return nil
	}
	objectID := parsePayloadUUID(payload, "object_id")
	if objectID == nil && entry.Object != nil {
		if parsed, err := uuid.Parse(string(*entry.Object)); err == nil {
			objectID = &parsed
		}
	}
	if objectID == nil {
		return nil
	}
	ruleID := parsePayloadUUID(payload, "rule_id")
	if ruleID == nil {
		return nil
	}
	matched, ok := payload["matched"].(bool)
	if !ok {
		return nil
	}
	simulated, ok := payload["simulated"].(bool)
	if !ok {
		return nil
	}

	triggerPayload := json.RawMessage(`null`)
	if raw, ok := payload["trigger_payload"]; ok {
		if b, err := json.Marshal(raw); err == nil {
			triggerPayload = b
		}
	}
	var effectPreview json.RawMessage
	if raw, ok := payload["effect_preview"]; ok && raw != nil {
		if b, err := json.Marshal(raw); err == nil {
			effectPreview = b
		}
	}

	createdBy := uuid.Nil
	if u := parsePayloadUUID(payload, "created_by"); u != nil {
		createdBy = *u
	} else if parsed, err := uuid.Parse(entry.Subject); err == nil {
		createdBy = parsed
	}

	createdAt := time.UnixMilli(entry.RecordedAtMs).UTC()
	if raw, ok := payload["created_at"].(string); ok {
		if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			createdAt = ts.UTC()
		}
	}

	return &models.OntologyRuleRun{
		ID:             *id,
		RuleID:         *ruleID,
		ObjectID:       *objectID,
		Matched:        matched,
		Simulated:      simulated,
		TriggerPayload: triggerPayload,
		EffectPreview:  effectPreview,
		CreatedBy:      createdBy,
		CreatedAt:      createdAt,
	}
}

func ensureObjectTypeExists(ctx context.Context, db *pgxpool.Pool, objectTypeID uuid.UUID) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM object_types WHERE id = $1)",
		objectTypeID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to validate object type: %w", err)
	}
	return exists, nil
}

func propertyTypeMap(ctx context.Context, db *pgxpool.Pool, objectTypeID uuid.UUID) (map[string]string, error) {
	defs, err := LoadEffectiveProperties(ctx, db, objectTypeID)
	if err != nil {
		return nil, fmt.Errorf("failed to load property definitions: %w", err)
	}
	out := map[string]string{}
	for _, p := range defs {
		out[p.Name] = p.PropertyType
	}
	return out, nil
}

// ── Public functions (1:1 with the Rust pub fn / pub async fn set) ──

// ValidateRuleDefinition mirrors `pub async fn validate_rule_definition`.
func ValidateRuleDefinition(
	ctx context.Context,
	state *ontologykernel.AppState,
	objectTypeID uuid.UUID,
	trigger models.RuleTriggerSpec,
	effect models.RuleEffectSpec,
) error {
	exists, err := ensureObjectTypeExists(ctx, state.DB, objectTypeID)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("referenced object type does not exist")
	}
	propertyTypes, err := propertyTypeMap(ctx, state.DB, objectTypeID)
	if err != nil {
		return err
	}

	for propertyName := range trigger.Equals {
		if _, ok := propertyTypes[propertyName]; !ok {
			return fmt.Errorf("unknown property '%s' in trigger equals", propertyName)
		}
	}

	for propertyName, threshold := range trigger.NumericGte {
		pType, ok := propertyTypes[propertyName]
		if !ok {
			return fmt.Errorf("unknown property '%s' in numeric_gte", propertyName)
		}
		if pType != "integer" && pType != "float" {
			return fmt.Errorf("property '%s' must be numeric for numeric_gte", propertyName)
		}
		if math.IsInf(threshold, 0) || math.IsNaN(threshold) {
			return fmt.Errorf("numeric_gte threshold for '%s' must be finite", propertyName)
		}
	}
	for propertyName, threshold := range trigger.NumericLte {
		pType, ok := propertyTypes[propertyName]
		if !ok {
			return fmt.Errorf("unknown property '%s' in numeric_lte", propertyName)
		}
		if pType != "integer" && pType != "float" {
			return fmt.Errorf("property '%s' must be numeric for numeric_lte", propertyName)
		}
		if math.IsInf(threshold, 0) || math.IsNaN(threshold) {
			return fmt.Errorf("numeric_lte threshold for '%s' must be finite", propertyName)
		}
	}

	for _, propertyName := range append(append([]string{}, trigger.Exists...), trigger.ChangedProperties...) {
		if _, ok := propertyTypes[propertyName]; !ok {
			return fmt.Errorf("unknown property '%s' in trigger specification", propertyName)
		}
	}

	if len(trigger.Markings) > 0 {
		for _, m := range trigger.Markings {
			if m != "public" && m != "confidential" && m != "pii" {
				return errors.New("markings must only contain public, confidential, or pii")
			}
		}
	}

	hasEffect := len(effect.ObjectPatch) > 0 || effect.Schedule != nil || effect.Alert != nil
	if !hasEffect {
		return errors.New("rule effect must define object_patch, schedule, or alert")
	}

	if len(effect.ObjectPatch) > 0 {
		var patch map[string]json.RawMessage
		if err := json.Unmarshal(effect.ObjectPatch, &patch); err != nil {
			return errors.New("object_patch must be a JSON object")
		}
		for propertyName, value := range patch {
			pType, ok := propertyTypes[propertyName]
			if !ok {
				return fmt.Errorf("unknown property '%s' in object_patch", propertyName)
			}
			if err := ValidatePropertyValue(pType, value); err != nil {
				return fmt.Errorf("%s: %s", propertyName, err)
			}
		}
	}

	if s := effect.Schedule; s != nil {
		pType, ok := propertyTypes[s.PropertyName]
		if !ok {
			return fmt.Errorf("unknown property '%s' in schedule", s.PropertyName)
		}
		if pType != "timestamp" && pType != "date" && pType != "string" {
			return fmt.Errorf("schedule property '%s' must be timestamp, date, or string", s.PropertyName)
		}
		if s.OffsetHours == 0 {
			return errors.New("schedule.offset_hours must not be zero so the schedule can move in time")
		}
		if s.PriorityScore != nil && (*s.PriorityScore < 0 || *s.PriorityScore > 100) {
			return errors.New("schedule.priority_score must be between 0 and 100")
		}
		if s.EstimatedDurationMinutes != nil && *s.EstimatedDurationMinutes <= 0 {
			return errors.New("schedule.estimated_duration_minutes must be greater than zero")
		}
		if s.RequiredCapability != nil && len(*s.RequiredCapability) == 0 {
			return errors.New("schedule.required_capability must not be empty when provided")
		}
		if s.HardDeadlineHours != nil && *s.HardDeadlineHours == 0 {
			return errors.New("schedule.hard_deadline_hours must not be zero when provided")
		}
	}

	if a := effect.Alert; a != nil {
		if len(a.Title) == 0 {
			return errors.New("alert.title is required when alert is configured")
		}
		switch a.Severity {
		case "low", "medium", "high", "critical":
		default:
			return errors.New("alert.severity must be one of low, medium, high, critical")
		}
	}
	return nil
}

// LoadRule mirrors `pub async fn load_rule`.
func LoadRule(ctx context.Context, state *ontologykernel.AppState, ruleID uuid.UUID) (*models.OntologyRule, error) {
	var row models.OntologyRuleRow
	err := state.DB.QueryRow(ctx,
		`SELECT id, name, display_name, description, object_type_id, evaluation_mode,
		        trigger_spec, effect_spec, owner_id, created_at, updated_at
		 FROM ontology_rules WHERE id = $1`,
		ruleID,
	).Scan(
		&row.ID, &row.Name, &row.DisplayName, &row.Description, &row.ObjectTypeID,
		&row.EvaluationMode, &row.TriggerSpec, &row.EffectSpec, &row.OwnerID,
		&row.CreatedAt, &row.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load rule: %w", err)
	}
	rule, err := row.IntoRule()
	if err != nil {
		return nil, fmt.Errorf("failed to decode rule: %w", err)
	}
	return &rule, nil
}

// LoadRulesForObjectType mirrors `pub async fn load_rules_for_object_type`.
func LoadRulesForObjectType(
	ctx context.Context,
	state *ontologykernel.AppState,
	objectTypeID uuid.UUID,
) ([]models.OntologyRule, error) {
	rows, err := state.DB.Query(ctx,
		`SELECT id, name, display_name, description, object_type_id, evaluation_mode,
		        trigger_spec, effect_spec, owner_id, created_at, updated_at
		 FROM ontology_rules
		 WHERE object_type_id = $1
		 ORDER BY created_at DESC`,
		objectTypeID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load rules: %w", err)
	}
	defer rows.Close()

	out := []models.OntologyRule{}
	for rows.Next() {
		var row models.OntologyRuleRow
		if err := rows.Scan(
			&row.ID, &row.Name, &row.DisplayName, &row.Description, &row.ObjectTypeID,
			&row.EvaluationMode, &row.TriggerSpec, &row.EffectSpec, &row.OwnerID,
			&row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to decode rules: %w", err)
		}
		rule, err := row.IntoRule()
		if err != nil {
			return nil, fmt.Errorf("failed to decode rules: %w", err)
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}

// mergedProperties mirrors `fn merged_properties`.
func mergedProperties(
	object *ObjectInstance,
	patch map[string]json.RawMessage,
) map[string]json.RawMessage {
	merged := map[string]json.RawMessage{}
	if len(object.Properties) > 0 {
		_ = json.Unmarshal(object.Properties, &merged)
	}
	for k, v := range patch {
		merged[k] = v
	}
	return merged
}

// DeriveChangedProperties mirrors `pub fn derive_changed_properties`.
// Returns a deterministic sorted slice (Rust's HashSet has no ordering
// guarantee but the consumer sorts before serialising).
func DeriveChangedProperties(
	before *ObjectInstance,
	afterProperties map[string]json.RawMessage,
) []string {
	beforeProps := map[string]json.RawMessage{}
	if before != nil && len(before.Properties) > 0 {
		_ = json.Unmarshal(before.Properties, &beforeProps)
	}
	changed := map[string]struct{}{}
	for key, value := range afterProperties {
		previous, ok := beforeProps[key]
		if !ok || !rawEqual(previous, value) {
			changed[key] = struct{}{}
		}
	}
	for key := range beforeProps {
		if _, ok := afterProperties[key]; !ok {
			changed[key] = struct{}{}
		}
	}
	out := make([]string, 0, len(changed))
	for k := range changed {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func rawEqual(a, b json.RawMessage) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	ab, _ := json.Marshal(av)
	bb, _ := json.Marshal(bv)
	return string(ab) == string(bb)
}

// matchesEquals mirrors `fn matches_equals`.
func matchesEquals(equals map[string]json.RawMessage, properties map[string]json.RawMessage) error {
	for key, expected := range equals {
		actual, ok := properties[key]
		if !ok || !rawEqual(actual, expected) {
			return fmt.Errorf("property '%s' does not match expected value", key)
		}
	}
	return nil
}

// matchesNumericThresholds mirrors `fn matches_numeric_thresholds`.
func matchesNumericThresholds(
	thresholds map[string]float64,
	properties map[string]json.RawMessage,
	comparator func(float64, float64) bool,
	label string,
) error {
	for key, threshold := range thresholds {
		raw, ok := properties[key]
		if !ok {
			return fmt.Errorf("property '%s' is missing for %s", key, label)
		}
		number, ok := numericValue(raw)
		if !ok {
			return fmt.Errorf("property '%s' is not numeric for %s", key, label)
		}
		if !comparator(number, threshold) {
			return fmt.Errorf("property '%s' does not satisfy %s %v", key, label, threshold)
		}
	}
	return nil
}

func numericValue(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var asFloat float64
	if err := json.Unmarshal(raw, &asFloat); err == nil {
		return asFloat, true
	}
	var asInt int64
	if err := json.Unmarshal(raw, &asInt); err == nil {
		return float64(asInt), true
	}
	return 0, false
}

// BuildRuleEffectPreview mirrors `fn build_rule_effect_preview`.
func BuildRuleEffectPreview(effect models.RuleEffectSpec, object *ObjectInstance) json.RawMessage {
	objectPatch := map[string]json.RawMessage{}
	if len(effect.ObjectPatch) > 0 {
		_ = json.Unmarshal(effect.ObjectPatch, &objectPatch)
	}

	var schedulePreview map[string]any
	if effect.Schedule != nil {
		s := effect.Schedule
		now := time.Now().UTC()
		scheduledAt := now.Add(time.Duration(s.OffsetHours) * time.Hour).Format(time.RFC3339Nano)
		var hardDeadlineAt *string
		if s.HardDeadlineHours != nil {
			d := now.Add(time.Duration(*s.HardDeadlineHours) * time.Hour).Format(time.RFC3339Nano)
			hardDeadlineAt = &d
		}
		// Mutate object_patch to embed the scheduled_at on the configured property.
		patched, _ := json.Marshal(scheduledAt)
		objectPatch[s.PropertyName] = patched

		priority := int32(50)
		if s.PriorityScore != nil {
			priority = *s.PriorityScore
		}
		duration := int32(30)
		if s.EstimatedDurationMinutes != nil {
			duration = *s.EstimatedDurationMinutes
		}
		constraintTags := s.ConstraintTags
		if constraintTags == nil {
			constraintTags = []string{}
		}
		schedulePreview = map[string]any{
			"property_name":              s.PropertyName,
			"scheduled_at":               scheduledAt,
			"offset_hours":               s.OffsetHours,
			"priority_score":             priority,
			"estimated_duration_minutes": duration,
			"required_capability":        s.RequiredCapability,
			"constraint_tags":            constraintTags,
			"hard_deadline_at":           hardDeadlineAt,
		}
	}

	var effectivePatch any
	if len(objectPatch) == 0 {
		effectivePatch = nil
	} else {
		effectivePatch = objectPatch
	}

	preview := map[string]any{
		"object_patch": effectivePatch,
		"schedule":     schedulePreview,
		"alert":        effect.Alert,
		"object_id":    object.ID,
	}
	out, _ := json.Marshal(preview)
	return out
}

// derivedPriorityScore mirrors `fn derived_priority_score`.
func derivedPriorityScore(object *ObjectInstance, effectPreview json.RawMessage) int32 {
	var preview map[string]any
	_ = json.Unmarshal(effectPreview, &preview)

	explicit := int32(50)
	if schedule, ok := preview["schedule"].(map[string]any); ok {
		if v, ok := schedule["priority_score"].(float64); ok {
			explicit = int32(v)
		}
	}

	riskBoost := int32(0)
	var props map[string]json.RawMessage
	_ = json.Unmarshal(object.Properties, &props)
	if rs, ok := props["risk_score"]; ok {
		var v float64
		if err := json.Unmarshal(rs, &v); err == nil {
			riskBoost = int32(math.Round(v * 20.0))
		}
	}

	severityBoost := int32(0)
	if alert, ok := preview["alert"].(map[string]any); ok {
		if sev, ok := alert["severity"].(string); ok {
			switch sev {
			case "critical":
				severityBoost = 25
			case "high":
				severityBoost = 18
			case "medium":
				severityBoost = 10
			case "low":
				severityBoost = 4
			}
		}
	}

	markingBoost := int32(0)
	switch object.Marking {
	case "pii":
		markingBoost = 18
	case "confidential":
		markingBoost = 10
	}

	total := explicit + riskBoost + severityBoost + markingBoost
	if total < 0 {
		total = 0
	}
	if total > 100 {
		total = 100
	}
	return total
}

// dynamicPressureFromQueue mirrors `fn dynamic_pressure_from_queue`.
func dynamicPressureFromQueue(queueDepth, overdueCount int) string {
	if overdueCount > 0 || queueDepth >= 8 {
		return "high"
	}
	if queueDepth >= 3 {
		return "medium"
	}
	return "low"
}

// EvaluateRuleAgainstObject mirrors `pub fn evaluate_rule_against_object`.
func EvaluateRuleAgainstObject(
	rule *models.OntologyRule,
	object *ObjectInstance,
	patch map[string]json.RawMessage,
) models.RuleMatchResponse {
	properties := mergedProperties(object, patch)
	changedProperties := DeriveChangedProperties(object, properties)
	var triggerReasons []string

	if err := matchesEquals(rule.TriggerSpec.Equals, properties); err != nil {
		triggerReasons = append(triggerReasons, err.Error())
	}
	if err := matchesNumericThresholds(rule.TriggerSpec.NumericGte, properties,
		func(v, t float64) bool { return v >= t }, "numeric_gte"); err != nil {
		triggerReasons = append(triggerReasons, err.Error())
	}
	if err := matchesNumericThresholds(rule.TriggerSpec.NumericLte, properties,
		func(v, t float64) bool { return v <= t }, "numeric_lte"); err != nil {
		triggerReasons = append(triggerReasons, err.Error())
	}

	for _, propertyName := range rule.TriggerSpec.Exists {
		if _, ok := properties[propertyName]; !ok {
			triggerReasons = append(triggerReasons, fmt.Sprintf("property '%s' is missing", propertyName))
		}
	}

	if len(rule.TriggerSpec.ChangedProperties) > 0 {
		seen := map[string]struct{}{}
		for _, p := range changedProperties {
			seen[p] = struct{}{}
		}
		matched := false
		for _, p := range rule.TriggerSpec.ChangedProperties {
			if _, ok := seen[p]; ok {
				matched = true
				break
			}
		}
		if !matched {
			triggerReasons = append(triggerReasons, "none of the configured changed_properties were updated")
		}
	}

	if len(rule.TriggerSpec.Markings) > 0 {
		matched := false
		for _, m := range rule.TriggerSpec.Markings {
			if m == object.Marking {
				matched = true
				break
			}
		}
		if !matched {
			triggerReasons = append(triggerReasons, "object marking does not match rule markings")
		}
	}

	matched := len(triggerReasons) == 0
	var effectPreview json.RawMessage = json.RawMessage("null")
	if matched {
		effectPreview = BuildRuleEffectPreview(rule.EffectSpec, object)
	}

	if triggerReasons == nil {
		triggerReasons = []string{}
	}
	if changedProperties == nil {
		changedProperties = []string{}
	}
	triggerPayload, _ := json.Marshal(map[string]any{
		"object_id":          object.ID,
		"changed_properties": changedProperties,
		"reasons":            triggerReasons,
	})

	return models.RuleMatchResponse{
		RuleID:         rule.ID,
		Matched:        matched,
		TriggerPayload: triggerPayload,
		EffectPreview:  effectPreview,
	}
}

// ApplyRuleEffect mirrors `pub async fn apply_rule_effect`.
//
// Sub-phase deferred: writeback.apply_object_with_outbox + the three
// object-handler helpers it composes (load_repo_object_from_store,
// apply_object_write, append_object_revision) need to land first.
// Until then this function returns ErrApplyEffectNotWired so the
// handler can map it to a clean 501 instead of pretending the patch
// was applied.
func ApplyRuleEffect(
	_ context.Context,
	_ *ontologykernel.AppState,
	_ *authmw.Claims,
	_ *ObjectInstance,
	_ json.RawMessage,
) (*ObjectInstance, error) {
	return nil, ErrApplyEffectNotWired
}

// RecordRuleRun mirrors `pub async fn record_rule_run`. Appends to
// the action log; idempotency follows the ActionLogStore contract.
func RecordRuleRun(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	ruleID, objectID uuid.UUID,
	matched, simulated bool,
	triggerPayload json.RawMessage,
	effectPreview json.RawMessage,
) (*models.OntologyRuleRun, error) {
	// Rust uses Uuid::now_v7() so rule run IDs sort by time. Go's
	// uuid.NewV7 mirrors that; the v4 alternative would break the
	// time-ordered sort the action log relies on for ListRecent.
	runID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to record rule run: %s", err)
	}
	run := &models.OntologyRuleRun{
		ID:             runID,
		RuleID:         ruleID,
		ObjectID:       objectID,
		Matched:        matched,
		Simulated:      simulated,
		TriggerPayload: triggerPayload,
		EffectPreview:  effectPreview,
		CreatedBy:      claims.Sub,
		CreatedAt:      time.Now().UTC(),
	}
	status := "not_matched"
	if matched {
		status = "matched"
	}
	payload, _ := json.Marshal(map[string]any{
		"id":              run.ID,
		"rule_id":         run.RuleID,
		"object_id":       run.ObjectID,
		"matched":         run.Matched,
		"simulated":       run.Simulated,
		"trigger_payload": json.RawMessage(run.TriggerPayload),
		"effect_preview":  effectPreviewOrNull(run.EffectPreview),
		"created_by":      run.CreatedBy,
		"created_at":      run.CreatedAt,
		"status":          status,
	})
	objectIDRef := storage.ObjectId(objectID.String())
	if err := state.Stores.Actions.Append(ctx, storage.ActionLogEntry{
		Tenant:       TenantFromClaims(claims),
		ActionID:     run.ID.String(),
		Kind:         ruleRunKind,
		Subject:      run.CreatedBy.String(),
		Object:       &objectIDRef,
		Payload:      payload,
		RecordedAtMs: run.CreatedAt.UnixMilli(),
	}); err != nil {
		return nil, fmt.Errorf("failed to record rule run: %w", err)
	}
	return run, nil
}

func effectPreviewOrNull(preview json.RawMessage) any {
	if len(preview) == 0 || string(preview) == "null" {
		return nil
	}
	return preview
}

// EnqueueRuleSchedule mirrors `pub async fn enqueue_rule_schedule`.
func EnqueueRuleSchedule(
	ctx context.Context,
	state *ontologykernel.AppState,
	rule *models.OntologyRule,
	object *ObjectInstance,
	ruleRunID uuid.UUID,
	effectPreview json.RawMessage,
	createdBy uuid.UUID,
) (*models.MachineryQueueItem, error) {
	var preview map[string]any
	if err := json.Unmarshal(effectPreview, &preview); err != nil {
		return nil, nil
	}
	scheduleRaw, ok := preview["schedule"].(map[string]any)
	if !ok || scheduleRaw == nil {
		return nil, nil
	}

	scheduledAtStr, ok := scheduleRaw["scheduled_at"].(string)
	if !ok {
		return nil, errors.New("rule schedule preview is missing a valid scheduled_at timestamp")
	}
	scheduledFor, err := time.Parse(time.RFC3339Nano, scheduledAtStr)
	if err != nil {
		return nil, errors.New("rule schedule preview is missing a valid scheduled_at timestamp")
	}
	scheduledFor = scheduledFor.UTC()

	priorityScore := derivedPriorityScore(object, effectPreview)
	// Rust: `.and_then(as_i64).map(as i32).unwrap_or(30).max(1)`.
	// `.max(1)` clamps negatives + zero up to 1; we mirror that exactly
	// so callers passing a -5 or 0 still get a 1-minute estimate
	// instead of the 30-minute default.
	estimatedDuration := int32(30)
	if v, ok := scheduleRaw["estimated_duration_minutes"].(float64); ok {
		estimatedDuration = int32(v)
	}
	if estimatedDuration < 1 {
		estimatedDuration = 1
	}
	// Rust: `.filter(|value| !value.trim().is_empty())` — trim before
	// the empty check, so whitespace-only strings drop to None.
	var requiredCapability *string
	if v, ok := scheduleRaw["required_capability"].(string); ok {
		trimmed := strings.TrimSpace(v)
		if trimmed != "" {
			requiredCapability = &v
		}
	}
	constraintTags := scheduleRaw["constraint_tags"]
	if constraintTags == nil {
		constraintTags = []any{}
	}
	hardDeadline := scheduleRaw["hard_deadline_at"]
	var riskScore any
	var props map[string]json.RawMessage
	_ = json.Unmarshal(object.Properties, &props)
	if rs, ok := props["risk_score"]; ok {
		var v any
		_ = json.Unmarshal(rs, &v)
		riskScore = v
	}
	constraintSnapshot, _ := json.Marshal(map[string]any{
		"marking":          object.Marking,
		"organization_id":  object.OrganizationID,
		"risk_score":       riskScore,
		"constraint_tags":  constraintTags,
		"hard_deadline_at": hardDeadline,
		"source":           "ontology-rule",
	})

	scheduleID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to enqueue machinery schedule: %s", err)
	}
	row := &models.MachineryQueueItem{}
	err = state.DB.QueryRow(ctx, `
		INSERT INTO ontology_rule_schedules (
			id, rule_id, rule_run_id, object_id, status, scheduled_for, priority_score,
			estimated_duration_minutes, required_capability, constraint_snapshot, created_by
		)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7, $8, $9::jsonb, $10)
		RETURNING
			id, rule_id, rule_run_id, object_id,
			$11::text AS rule_name, $12::text AS rule_display_name, $13 AS object_type_id,
			status, scheduled_for, priority_score, estimated_duration_minutes,
			required_capability, constraint_snapshot, created_by, created_at, updated_at,
			started_at, completed_at`,
		scheduleID, rule.ID, ruleRunID, object.ID, scheduledFor, priorityScore,
		estimatedDuration, requiredCapability, constraintSnapshot, createdBy,
		rule.Name, rule.DisplayName, object.ObjectTypeID,
	).Scan(
		&row.ID, &row.RuleID, &row.RuleRunID, &row.ObjectID,
		&row.RuleName, &row.RuleDisplayName, &row.ObjectTypeID,
		&row.Status, &row.ScheduledFor, &row.PriorityScore, &row.EstimatedDurationMinutes,
		&row.RequiredCapability, &row.ConstraintSnapshot, &row.CreatedBy,
		&row.CreatedAt, &row.UpdatedAt, &row.StartedAt, &row.CompletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to enqueue machinery schedule: %w", err)
	}
	return row, nil
}

// MachineryQueue mirrors `pub async fn machinery_queue`.
func MachineryQueue(
	ctx context.Context,
	state *ontologykernel.AppState,
	objectTypeFilter *uuid.UUID,
) (models.MachineryQueueResponse, error) {
	const baseSQL = `
		SELECT schedule.id, schedule.rule_id, schedule.rule_run_id, schedule.object_id,
		       rules.name AS rule_name, rules.display_name AS rule_display_name,
		       rules.object_type_id, schedule.status, schedule.scheduled_for,
		       schedule.priority_score, schedule.estimated_duration_minutes,
		       schedule.required_capability, schedule.constraint_snapshot,
		       schedule.created_by, schedule.created_at, schedule.updated_at,
		       schedule.started_at, schedule.completed_at
		FROM ontology_rule_schedules AS schedule
		INNER JOIN ontology_rules AS rules ON rules.id = schedule.rule_id`

	const orderBy = ` ORDER BY schedule.scheduled_for ASC, schedule.priority_score DESC, schedule.created_at ASC`
	var (
		rows pgx.Rows
		err  error
	)
	if objectTypeFilter != nil {
		rows, err = state.DB.Query(ctx, baseSQL+" WHERE rules.object_type_id = $1"+orderBy, *objectTypeFilter)
	} else {
		rows, err = state.DB.Query(ctx, baseSQL+orderBy)
	}
	if err != nil {
		return models.MachineryQueueResponse{}, fmt.Errorf("failed to load machinery queue: %w", err)
	}
	defer rows.Close()

	allRows := []models.MachineryQueueItem{}
	for rows.Next() {
		row, err := scanMachineryQueueItem(rows)
		if err != nil {
			return models.MachineryQueueResponse{}, fmt.Errorf("failed to load machinery queue: %w", err)
		}
		allRows = append(allRows, row)
	}

	now := time.Now().UTC()
	recommended := make([]models.MachineryQueueItem, 0, len(allRows))
	for _, row := range allRows {
		if row.Status == "pending" || row.Status == "in_progress" {
			recommended = append(recommended, row)
		}
	}
	sort.SliceStable(recommended, func(i, j int) bool {
		left, right := recommended[i], recommended[j]
		leftOverdue := !left.ScheduledFor.After(now) && left.Status == "pending"
		rightOverdue := !right.ScheduledFor.After(now) && right.Status == "pending"
		if leftOverdue != rightOverdue {
			return leftOverdue
		}
		if left.PriorityScore != right.PriorityScore {
			return left.PriorityScore > right.PriorityScore
		}
		if !left.ScheduledFor.Equal(right.ScheduledFor) {
			return left.ScheduledFor.Before(right.ScheduledFor)
		}
		return left.CreatedAt.Before(right.CreatedAt)
	})

	queueDepth := len(recommended)
	overdueCount := 0
	totalEstimated := 0
	var nextDueAt *time.Time
	for i := range recommended {
		row := &recommended[i]
		if !row.ScheduledFor.After(now) && row.Status == "pending" {
			overdueCount++
		}
		if row.EstimatedDurationMinutes > 0 {
			totalEstimated += int(row.EstimatedDurationMinutes)
		}
		if nextDueAt == nil || row.ScheduledFor.Before(*nextDueAt) {
			ts := row.ScheduledFor
			nextDueAt = &ts
		}
	}

	capability := map[string]*models.MachineryCapabilityLoad{}
	for _, row := range recommended {
		key := "general"
		if row.RequiredCapability != nil && *row.RequiredCapability != "" {
			key = *row.RequiredCapability
		}
		entry, ok := capability[key]
		if !ok {
			entry = &models.MachineryCapabilityLoad{Capability: key}
			capability[key] = entry
		}
		entry.PendingCount++
		if row.EstimatedDurationMinutes > 0 {
			entry.TotalEstimatedMinutes += int(row.EstimatedDurationMinutes)
		}
	}
	capabilityList := make([]models.MachineryCapabilityLoad, 0, len(capability))
	for _, entry := range capability {
		capabilityList = append(capabilityList, *entry)
	}
	sort.Slice(capabilityList, func(i, j int) bool {
		if capabilityList[i].PendingCount != capabilityList[j].PendingCount {
			return capabilityList[i].PendingCount > capabilityList[j].PendingCount
		}
		return capabilityList[i].TotalEstimatedMinutes > capabilityList[j].TotalEstimatedMinutes
	})

	recommendedOrder := make([]uuid.UUID, len(recommended))
	for i, row := range recommended {
		recommendedOrder[i] = row.ID
	}

	return models.MachineryQueueResponse{
		ObjectTypeID: objectTypeFilter,
		Data:         allRows,
		Recommendation: models.MachineryQueueRecommendation{
			GeneratedAt:           now,
			Strategy:              "priority+deadline+constraint-aware",
			QueueDepth:            queueDepth,
			OverdueCount:          overdueCount,
			TotalEstimatedMinutes: totalEstimated,
			NextDueAt:             nextDueAt,
			RecommendedOrder:      recommendedOrder,
			CapabilityLoad:        capabilityList,
		},
	}, nil
}

func scanMachineryQueueItem(rows pgx.Rows) (models.MachineryQueueItem, error) {
	var row models.MachineryQueueItem
	err := rows.Scan(
		&row.ID, &row.RuleID, &row.RuleRunID, &row.ObjectID,
		&row.RuleName, &row.RuleDisplayName, &row.ObjectTypeID,
		&row.Status, &row.ScheduledFor, &row.PriorityScore, &row.EstimatedDurationMinutes,
		&row.RequiredCapability, &row.ConstraintSnapshot, &row.CreatedBy,
		&row.CreatedAt, &row.UpdatedAt, &row.StartedAt, &row.CompletedAt,
	)
	return row, err
}

// TransitionMachineryQueueItem mirrors `pub async fn transition_machinery_queue_item`.
func TransitionMachineryQueueItem(
	ctx context.Context,
	state *ontologykernel.AppState,
	scheduleID uuid.UUID,
	status string,
) (*models.MachineryQueueItem, error) {
	switch status {
	case "pending", "in_progress", "completed", "cancelled":
	default:
		return nil, errors.New("unsupported machinery queue status")
	}
	const sql = `
		UPDATE ontology_rule_schedules AS schedule
		SET status = $2, updated_at = NOW(),
		    started_at = CASE
		        WHEN $2 = 'in_progress' AND schedule.started_at IS NULL THEN NOW()
		        ELSE schedule.started_at
		    END,
		    completed_at = CASE
		        WHEN $2 = 'completed' THEN NOW()
		        WHEN $2 IN ('pending', 'in_progress') THEN NULL
		        ELSE schedule.completed_at
		    END
		FROM ontology_rules AS rules
		WHERE schedule.id = $1 AND rules.id = schedule.rule_id
		RETURNING
		    schedule.id, schedule.rule_id, schedule.rule_run_id, schedule.object_id,
		    rules.name AS rule_name, rules.display_name AS rule_display_name,
		    rules.object_type_id, schedule.status, schedule.scheduled_for,
		    schedule.priority_score, schedule.estimated_duration_minutes,
		    schedule.required_capability, schedule.constraint_snapshot,
		    schedule.created_by, schedule.created_at, schedule.updated_at,
		    schedule.started_at, schedule.completed_at`

	var row models.MachineryQueueItem
	err := state.DB.QueryRow(ctx, sql, scheduleID, status).Scan(
		&row.ID, &row.RuleID, &row.RuleRunID, &row.ObjectID,
		&row.RuleName, &row.RuleDisplayName, &row.ObjectTypeID,
		&row.Status, &row.ScheduledFor, &row.PriorityScore, &row.EstimatedDurationMinutes,
		&row.RequiredCapability, &row.ConstraintSnapshot, &row.CreatedBy,
		&row.CreatedAt, &row.UpdatedAt, &row.StartedAt, &row.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to transition machinery queue item: %w", err)
	}
	return &row, nil
}

// EvaluateRulesForObject mirrors `pub async fn evaluate_rules_for_object`.
type RuleAndMatch struct {
	Rule    models.OntologyRule
	Matched models.RuleMatchResponse
}

func EvaluateRulesForObject(
	ctx context.Context,
	state *ontologykernel.AppState,
	object *ObjectInstance,
	patch map[string]json.RawMessage,
) ([]RuleAndMatch, error) {
	rules, err := LoadRulesForObjectType(ctx, state, object.ObjectTypeID)
	if err != nil {
		return nil, err
	}
	out := make([]RuleAndMatch, 0, len(rules))
	for i := range rules {
		r := rules[i]
		out = append(out, RuleAndMatch{Rule: r, Matched: EvaluateRuleAgainstObject(&r, object, patch)})
	}
	return out, nil
}

// LoadRecentRuleRuns mirrors `pub async fn load_recent_rule_runs`.
func LoadRecentRuleRuns(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	objectID uuid.UUID,
	limit int,
) ([]models.OntologyRuleRun, error) {
	if limit == 0 {
		return []models.OntologyRuleRun{}, nil
	}
	tenant := TenantFromClaims(claims)
	objID := storage.ObjectId(objectID.String())
	var token *string
	pageSize := uint32(50)
	if limit > 50 {
		pageSize = uint32(limit)
	}
	if pageSize > 500 {
		pageSize = 500
	}

	runs := []models.OntologyRuleRun{}
	for len(runs) < limit {
		page, err := state.Stores.Actions.ListForObject(
			ctx, tenant, objID,
			storage.Page{Size: pageSize, Token: token},
			storage.Strong(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to load recent rule runs: %w", err)
		}
		for _, entry := range page.Items {
			if run := actionEntryToRuleRun(entry); run != nil {
				runs = append(runs, *run)
				if len(runs) >= limit {
					break
				}
			}
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	sort.SliceStable(runs, func(i, j int) bool {
		return runs[i].CreatedAt.After(runs[j].CreatedAt)
	})
	if len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}

// MachineryInsights mirrors `pub async fn machinery_insights`.
func MachineryInsights(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	objectTypeFilter *uuid.UUID,
) ([]models.MachineryInsight, error) {
	rules, err := loadRulesForInsights(ctx, state.DB, objectTypeFilter)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return []models.MachineryInsight{}, nil
	}
	ruleIDSet := map[uuid.UUID]struct{}{}
	ruleIDs := make([]uuid.UUID, 0, len(rules))
	for _, r := range rules {
		ruleIDs = append(ruleIDs, r.ID)
		ruleIDSet[r.ID] = struct{}{}
	}

	tenant := TenantFromClaims(claims)
	var token *string
	var allRuns []models.OntologyRuleRun
	for {
		page, err := state.Stores.Actions.ListRecent(
			ctx, tenant,
			storage.Page{Size: 5_000, Token: token},
			storage.Strong(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to load rule runs: %w", err)
		}
		for _, entry := range page.Items {
			run := actionEntryToRuleRun(entry)
			if run != nil {
				if _, ok := ruleIDSet[run.RuleID]; ok {
					allRuns = append(allRuns, *run)
				}
			}
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	sort.SliceStable(allRuns, func(i, j int) bool {
		return allRuns[i].CreatedAt.After(allRuns[j].CreatedAt)
	})

	groupedRuns := map[uuid.UUID][]models.OntologyRuleRun{}
	for _, run := range allRuns {
		groupedRuns[run.RuleID] = append(groupedRuns[run.RuleID], run)
	}

	rows, err := state.DB.Query(ctx, `
		SELECT schedule.id, schedule.rule_id, schedule.rule_run_id, schedule.object_id,
		       rules.name AS rule_name, rules.display_name AS rule_display_name,
		       rules.object_type_id, schedule.status, schedule.scheduled_for,
		       schedule.priority_score, schedule.estimated_duration_minutes,
		       schedule.required_capability, schedule.constraint_snapshot,
		       schedule.created_by, schedule.created_at, schedule.updated_at,
		       schedule.started_at, schedule.completed_at
		FROM ontology_rule_schedules AS schedule
		INNER JOIN ontology_rules AS rules ON rules.id = schedule.rule_id
		WHERE schedule.rule_id = ANY($1)
		ORDER BY schedule.created_at DESC`, ruleIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to load machinery schedules: %w", err)
	}
	defer rows.Close()

	groupedSchedules := map[uuid.UUID][]models.MachineryQueueItem{}
	for rows.Next() {
		row, err := scanMachineryQueueItem(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to load machinery schedules: %w", err)
		}
		groupedSchedules[row.RuleID] = append(groupedSchedules[row.RuleID], row)
	}

	now := time.Now().UTC()
	insights := make([]models.MachineryInsight, 0, len(rules))
	for _, rule := range rules {
		runs := groupedRuns[rule.ID]
		schedules := groupedSchedules[rule.ID]
		matchedRuns := 0
		for _, r := range runs {
			if r.Matched {
				matchedRuns++
			}
		}
		pendingSchedules := 0
		overdueSchedules := 0
		for _, s := range schedules {
			if s.Status == "pending" || s.Status == "in_progress" {
				pendingSchedules++
			}
			if s.Status == "pending" && !s.ScheduledFor.After(now) {
				overdueSchedules++
			}
		}
		var avgLead *float64
		if len(schedules) > 0 {
			sum := 0.0
			for _, s := range schedules {
				sum += s.ScheduledFor.Sub(s.CreatedAt).Hours()
			}
			avg := sum / float64(len(schedules))
			avgLead = &avg
		}
		var lastMatchedAt *time.Time
		var lastObjectID *uuid.UUID
		for _, r := range runs {
			if r.Matched {
				ts := r.CreatedAt
				oid := r.ObjectID
				lastMatchedAt = &ts
				lastObjectID = &oid
				break
			}
		}
		insights = append(insights, models.MachineryInsight{
			RuleID:               rule.ID,
			Name:                 rule.Name,
			DisplayName:          rule.DisplayName,
			EvaluationMode:       rule.EvaluationMode,
			MatchedRuns:          matchedRuns,
			TotalRuns:            len(runs),
			PendingSchedules:     pendingSchedules,
			OverdueSchedules:     overdueSchedules,
			AvgScheduleLeadHours: avgLead,
			DynamicPressure:      dynamicPressureFromQueue(pendingSchedules, overdueSchedules),
			LastMatchedAt:        lastMatchedAt,
			LastObjectID:         lastObjectID,
		})
	}
	return insights, nil
}

func loadRulesForInsights(
	ctx context.Context,
	db *pgxpool.Pool,
	objectTypeFilter *uuid.UUID,
) ([]models.OntologyRule, error) {
	const cols = `id, name, display_name, description, object_type_id, evaluation_mode,
	              trigger_spec, effect_spec, owner_id, created_at, updated_at`
	var rows pgx.Rows
	var err error
	if objectTypeFilter != nil {
		rows, err = db.Query(ctx,
			`SELECT `+cols+` FROM ontology_rules WHERE object_type_id = $1 ORDER BY created_at DESC`,
			*objectTypeFilter,
		)
	} else {
		rows, err = db.Query(ctx, `SELECT `+cols+` FROM ontology_rules ORDER BY created_at DESC`)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load rules: %w", err)
	}
	defer rows.Close()

	out := []models.OntologyRule{}
	for rows.Next() {
		var row models.OntologyRuleRow
		if err := rows.Scan(
			&row.ID, &row.Name, &row.DisplayName, &row.Description, &row.ObjectTypeID,
			&row.EvaluationMode, &row.TriggerSpec, &row.EffectSpec, &row.OwnerID,
			&row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to decode rules: %w", err)
		}
		rule, err := row.IntoRule()
		if err != nil {
			return nil, fmt.Errorf("failed to decode rules: %w", err)
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}

// BuildRuleEvaluateResponse mirrors `pub fn build_rule_evaluate_response`.
func BuildRuleEvaluateResponse(
	rule models.OntologyRule,
	object *ObjectInstance,
	match models.RuleMatchResponse,
) models.RuleEvaluateResponse {
	objectJSON, _ := json.Marshal(object)
	return models.RuleEvaluateResponse{
		Rule:           rule,
		Matched:        match.Matched,
		TriggerPayload: match.TriggerPayload,
		EffectPreview:  match.EffectPreview,
		Object:         objectJSON,
	}
}
