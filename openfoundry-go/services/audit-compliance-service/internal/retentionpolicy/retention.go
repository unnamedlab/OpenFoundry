// Package retentionpolicy ports the retention-policy subsystem 1:1
// from `services/audit-compliance-service/src/retention_policy/`.
//
// The Rust source is split into:
//
//   - models/retention.rs   — wire types (now in internal/models)
//   - domain/retention.rs   — filter / apply_update / resolve_applicable / run_preview
//   - handlers/retention.rs — CRUD + applicable-policies + retention-preview
//   - metrics.rs            — Prometheus counters
//
// The Go package consolidates the domain layer here; handlers live in
// `handlers.go`.
package retentionpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

const policySelect = `SELECT id, name, scope, target_kind, retention_days,
        legal_hold, purge_mode, rules, updated_by, active, is_system, selector,
        criteria, grace_period_minutes, last_applied_at, next_run_at,
        created_at, updated_at
        FROM retention_policies`

// LoadPolicies mirrors `domain::retention::load_policies`.
func LoadPolicies(ctx context.Context, db *pgxpool.Pool) ([]models.RetentionPolicy, error) {
	rows, err := db.Query(ctx, policySelect+` ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.RetentionPolicy, 0)
	for rows.Next() {
		v, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// LoadPolicy returns a single policy or nil.
func LoadPolicy(ctx context.Context, db *pgxpool.Pool, id uuid.UUID) (*models.RetentionPolicy, error) {
	row := db.QueryRow(ctx, policySelect+` WHERE id = $1`, id)
	v, err := scanPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

type rowLikeT interface {
	Scan(dest ...any) error
}

func scanPolicy(r rowLikeT) (*models.RetentionPolicy, error) {
	v := &models.RetentionPolicy{}
	if err := r.Scan(&v.ID, &v.Name, &v.Scope, &v.TargetKind, &v.RetentionDays,
		&v.LegalHold, &v.PurgeMode, &v.Rules, &v.UpdatedBy, &v.Active,
		&v.IsSystem, &v.Selector, &v.Criteria, &v.GracePeriodMinutes,
		&v.LastAppliedAt, &v.NextRunAt, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, nil
}

// FilterPolicies mirrors `domain::retention::filter_policies`. AND-
// combines selector / project_id / marking_id / active / system_only.
func FilterPolicies(policies []models.RetentionPolicy, query *models.ListRetentionPoliciesQuery) []models.RetentionPolicy {
	out := make([]models.RetentionPolicy, 0, len(policies))
	for i := range policies {
		if matchesQuery(&policies[i], query) {
			out = append(out, policies[i])
		}
	}
	return out
}

func matchesQuery(policy *models.RetentionPolicy, query *models.ListRetentionPoliciesQuery) bool {
	if query.Active != nil && policy.Active != *query.Active {
		return false
	}
	if query.SystemOnly != nil && *query.SystemOnly && !policy.IsSystem {
		return false
	}
	anySelector := query.DatasetRid != nil || query.ProjectID != nil || query.MarkingID != nil
	if !anySelector {
		return true
	}
	selector, err := models.SelectorFromRaw(policy.Selector)
	if err != nil {
		return false
	}
	if selector.AllDatasets {
		return true
	}
	if query.DatasetRid != nil && selector.DatasetRid != nil && *selector.DatasetRid == *query.DatasetRid {
		return true
	}
	if query.ProjectID != nil && selector.ProjectID != nil && *selector.ProjectID == *query.ProjectID {
		return true
	}
	if query.MarkingID != nil && selector.MarkingID != nil && *selector.MarkingID == *query.MarkingID {
		return true
	}
	return false
}

// ResolvedPolicies mirrors `domain::retention::ResolvedPolicies`.
type ResolvedPolicies struct {
	Inherited models.InheritedPolicies
	Explicit  []models.RetentionPolicy
	Effective *models.RetentionPolicy
	Conflicts []models.PolicyConflict
}

// ResolveApplicable mirrors `domain::retention::resolve_applicable`.
//
// Iterates the (active) policies and assigns each to the first
// matching bucket: explicit > project > space (marking_id ==
// ctx.marking_id || ctx.space_id) > org (all_datasets).
func ResolveApplicable(policies []models.RetentionPolicy, rid string, ctx *models.ResolutionContext) ResolvedPolicies {
	out := ResolvedPolicies{}
	for i := range policies {
		policy := policies[i]
		if !policy.Active {
			continue
		}
		selector, err := models.SelectorFromRaw(policy.Selector)
		if err != nil {
			continue
		}
		if selector.DatasetRid != nil && *selector.DatasetRid == rid {
			out.Explicit = append(out.Explicit, policy)
			continue
		}
		if ctx.ProjectID != nil && selector.ProjectID != nil && *selector.ProjectID == *ctx.ProjectID {
			out.Inherited.Project = append(out.Inherited.Project, policy)
			continue
		}
		if selector.MarkingID != nil {
			if (ctx.MarkingID != nil && *selector.MarkingID == *ctx.MarkingID) ||
				(ctx.SpaceID != nil && *selector.MarkingID == *ctx.SpaceID) {
				out.Inherited.Space = append(out.Inherited.Space, policy)
				continue
			}
		}
		if selector.AllDatasets {
			out.Inherited.Org = append(out.Inherited.Org, policy)
		}
	}
	out.Effective, out.Conflicts = pickEffective(&out)
	return out
}

func pickEffective(resolved *ResolvedPolicies) (*models.RetentionPolicy, []models.PolicyConflict) {
	type candidate struct {
		level  uint8
		policy models.RetentionPolicy
	}
	candidates := make([]candidate, 0)
	for _, p := range resolved.Explicit {
		candidates = append(candidates, candidate{0, p})
	}
	for _, p := range resolved.Inherited.Project {
		candidates = append(candidates, candidate{1, p})
	}
	for _, p := range resolved.Inherited.Space {
		candidates = append(candidates, candidate{2, p})
	}
	for _, p := range resolved.Inherited.Org {
		candidates = append(candidates, candidate{3, p})
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	score := func(c candidate) (uint8, int32, time.Time) {
		legalHold := uint8(1)
		if c.policy.LegalHold {
			legalHold = 0
		}
		bias := int32(c.level) << 24
		return legalHold, c.policy.RetentionDays + bias, c.policy.CreatedAt
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b, c := score(candidates[i])
		x, y, z := score(candidates[j])
		if a != x {
			return a < x
		}
		if b != y {
			return b < y
		}
		return c.Before(z)
	})
	winner := candidates[0].policy
	conflicts := make([]models.PolicyConflict, 0, len(candidates)-1)
	for _, c := range candidates[1:] {
		reason := "winner_has_higher_specificity"
		if winner.LegalHold && !c.policy.LegalHold {
			reason = "winner_has_legal_hold"
		} else if winner.RetentionDays < c.policy.RetentionDays {
			reason = "winner_has_lower_retention_days"
		}
		conflicts = append(conflicts, models.PolicyConflict{
			WinnerID: winner.ID,
			LoserID:  c.policy.ID,
			Reason:   reason,
		})
	}
	return &winner, conflicts
}

// ApplyUpdate mirrors `domain::retention::apply_update`. Mutates the
// passed policy in place.
func ApplyUpdate(current *models.RetentionPolicy, update *UpdateRetentionPolicyRequest) {
	if update.Name != nil {
		current.Name = *update.Name
	}
	if update.Scope != nil {
		current.Scope = *update.Scope
	}
	if update.TargetKind != nil {
		current.TargetKind = *update.TargetKind
	}
	if update.RetentionDays != nil {
		current.RetentionDays = *update.RetentionDays
	}
	if update.LegalHold != nil {
		current.LegalHold = *update.LegalHold
	}
	if update.PurgeMode != nil {
		current.PurgeMode = *update.PurgeMode
	}
	if update.Rules != nil {
		raw, _ := json.Marshal(*update.Rules)
		current.Rules = raw
	}
	if update.UpdatedBy != nil {
		current.UpdatedBy = *update.UpdatedBy
	}
	if update.Active != nil {
		current.Active = *update.Active
	}
	if update.Selector != nil {
		raw, _ := json.Marshal(update.Selector)
		current.Selector = raw
	}
	if update.Criteria != nil {
		raw, _ := json.Marshal(update.Criteria)
		current.Criteria = raw
	}
	if update.GracePeriodMinutes != nil {
		current.GracePeriodMinutes = *update.GracePeriodMinutes
	}
}

// CreateRetentionPolicyRequest mirrors `models::retention::CreateRetentionPolicyRequest`.
type CreateRetentionPolicyRequest struct {
	Name               string                    `json:"name"`
	Scope              string                    `json:"scope,omitempty"`
	TargetKind         string                    `json:"target_kind"`
	RetentionDays      int32                     `json:"retention_days"`
	LegalHold          bool                      `json:"legal_hold,omitempty"`
	PurgeMode          string                    `json:"purge_mode"`
	Rules              []string                  `json:"rules,omitempty"`
	UpdatedBy          string                    `json:"updated_by"`
	Active             *bool                     `json:"active,omitempty"`
	Selector           *models.RetentionSelector `json:"selector,omitempty"`
	Criteria           *models.RetentionCriteria `json:"criteria,omitempty"`
	GracePeriodMinutes *int32                    `json:"grace_period_minutes,omitempty"`
}

// EffectiveActive applies the Rust default of true.
func (r *CreateRetentionPolicyRequest) EffectiveActive() bool {
	if r.Active == nil {
		return true
	}
	return *r.Active
}

// EffectiveGrace applies the Rust default of 60 minutes.
func (r *CreateRetentionPolicyRequest) EffectiveGrace() int32 {
	if r.GracePeriodMinutes == nil {
		return 60
	}
	return *r.GracePeriodMinutes
}

// UpdateRetentionPolicyRequest mirrors the Rust struct of the same name.
type UpdateRetentionPolicyRequest struct {
	Name               *string                   `json:"name,omitempty"`
	Scope              *string                   `json:"scope,omitempty"`
	TargetKind         *string                   `json:"target_kind,omitempty"`
	RetentionDays      *int32                    `json:"retention_days,omitempty"`
	LegalHold          *bool                     `json:"legal_hold,omitempty"`
	PurgeMode          *string                   `json:"purge_mode,omitempty"`
	Rules              *[]string                 `json:"rules,omitempty"`
	UpdatedBy          *string                   `json:"updated_by,omitempty"`
	Active             *bool                     `json:"active,omitempty"`
	Selector           *models.RetentionSelector `json:"selector,omitempty"`
	Criteria           *models.RetentionCriteria `json:"criteria,omitempty"`
	GracePeriodMinutes *int32                    `json:"grace_period_minutes,omitempty"`
}

// CreatePolicy mirrors `handlers::retention::create_policy`.
func CreatePolicy(ctx context.Context, db *pgxpool.Pool, request *CreateRetentionPolicyRequest) (*models.RetentionPolicy, error) {
	if strings.TrimSpace(request.Name) == "" {
		return nil, errors.New("name is required")
	}
	if strings.TrimSpace(request.TargetKind) == "" {
		return nil, errors.New("target_kind is required")
	}
	if strings.TrimSpace(request.UpdatedBy) == "" {
		return nil, errors.New("updated_by is required")
	}
	if request.Rules == nil {
		request.Rules = []string{}
	}
	rulesJSON, err := json.Marshal(request.Rules)
	if err != nil {
		return nil, err
	}
	selectorJSON, err := json.Marshal(orZeroSelector(request.Selector))
	if err != nil {
		return nil, err
	}
	criteriaJSON, err := json.Marshal(orZeroCriteria(request.Criteria))
	if err != nil {
		return nil, err
	}

	row := db.QueryRow(ctx,
		`INSERT INTO retention_policies (id, name, scope, target_kind, retention_days,
		       legal_hold, purge_mode, rules, updated_by, active, selector, criteria,
		       grace_period_minutes)
		    VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11::jsonb, $12::jsonb, $13)
		    RETURNING id, name, scope, target_kind, retention_days, legal_hold,
		              purge_mode, rules, updated_by, active, is_system, selector,
		              criteria, grace_period_minutes, last_applied_at, next_run_at,
		              created_at, updated_at`,
		uuid.New(), request.Name, request.Scope, request.TargetKind, request.RetentionDays,
		request.LegalHold, request.PurgeMode, rulesJSON, request.UpdatedBy,
		request.EffectiveActive(), selectorJSON, criteriaJSON, request.EffectiveGrace(),
	)
	return scanPolicy(row)
}

// UpdatePolicy mirrors `handlers::retention::update_policy`. Returns
// nil + nil error when the policy id does not exist.
func UpdatePolicy(ctx context.Context, db *pgxpool.Pool, id uuid.UUID, request *UpdateRetentionPolicyRequest) (*models.RetentionPolicy, error) {
	current, err := LoadPolicy(ctx, db, id)
	if err != nil || current == nil {
		return current, err
	}
	ApplyUpdate(current, request)
	rules, err := MaterialiseRules(current.Rules)
	if err != nil {
		return nil, err
	}
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return nil, err
	}
	row := db.QueryRow(ctx,
		`UPDATE retention_policies
		    SET name = $2, scope = $3, target_kind = $4, retention_days = $5,
		        legal_hold = $6, purge_mode = $7, rules = $8::jsonb,
		        updated_by = $9, active = $10, selector = $11::jsonb,
		        criteria = $12::jsonb, grace_period_minutes = $13, updated_at = NOW()
		  WHERE id = $1
		  RETURNING id, name, scope, target_kind, retention_days, legal_hold,
		            purge_mode, rules, updated_by, active, is_system, selector,
		            criteria, grace_period_minutes, last_applied_at, next_run_at,
		            created_at, updated_at`,
		id, current.Name, current.Scope, current.TargetKind, current.RetentionDays,
		current.LegalHold, current.PurgeMode, rulesJSON, current.UpdatedBy, current.Active,
		current.Selector, current.Criteria, current.GracePeriodMinutes,
	)
	return scanPolicy(row)
}

// DeletePolicy mirrors `handlers::retention::delete_policy`. Returns
// `ErrPolicyIsSystem` when the row carries `is_system = true`.
func DeletePolicy(ctx context.Context, db *pgxpool.Pool, id uuid.UUID) error {
	current, err := LoadPolicy(ctx, db, id)
	if err != nil {
		return err
	}
	if current == nil {
		return ErrPolicyNotFound
	}
	if current.IsSystem {
		return ErrPolicyIsSystem
	}
	if _, err := db.Exec(ctx, `DELETE FROM retention_policies WHERE id = $1`, id); err != nil {
		return err
	}
	return nil
}

// ErrPolicyNotFound is returned by helpers when the row id is missing.
var ErrPolicyNotFound = errors.New("retention policy not found")

// ErrPolicyIsSystem is returned by DeletePolicy when the row is built-in.
var ErrPolicyIsSystem = errors.New("system policies cannot be deleted")

// MaterialiseRules decodes the JSONB rules column into a string slice.
func MaterialiseRules(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []string{}, nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RunJob mirrors `domain::retention::run_job`.
func RunJob(ctx context.Context, db *pgxpool.Pool, request *models.RunRetentionJobRequest) (*models.RetentionJob, error) {
	policy, err := LoadPolicy(ctx, db, request.PolicyID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, ErrPolicyNotFound
	}
	target := "policy scope"
	if request.TargetDatasetID != nil {
		target = fmt.Sprintf("dataset %s", *request.TargetDatasetID)
	} else if request.TargetTransactionID != nil {
		target = fmt.Sprintf("transaction %s", *request.TargetTransactionID)
	}
	actionSummary := fmt.Sprintf(
		"Applied %s retention (%d days, purge mode %s) to %s",
		policy.TargetKind, policy.RetentionDays, policy.PurgeMode, target)
	affected := int32(3)
	if request.TargetTransactionID != nil {
		affected = 1
	}
	now := time.Now().UTC()
	row := db.QueryRow(ctx,
		`INSERT INTO retention_jobs (id, policy_id, target_dataset_id,
		       target_transaction_id, status, action_summary, affected_record_count,
		       created_at, completed_at)
		    VALUES ($1, $2, $3, $4, 'completed', $5, $6, $7, $8)
		    RETURNING id, policy_id, target_dataset_id, target_transaction_id, status,
		              action_summary, affected_record_count, created_at, completed_at`,
		uuid.New(), request.PolicyID, request.TargetDatasetID, request.TargetTransactionID,
		actionSummary, affected, now, now,
	)
	out := models.RetentionJob{}
	if err := row.Scan(&out.ID, &out.PolicyID, &out.TargetDatasetID,
		&out.TargetTransactionID, &out.Status, &out.ActionSummary,
		&out.AffectedRecordCount, &out.CreatedAt, &out.CompletedAt); err != nil {
		return nil, err
	}
	return &out, nil
}

func orZeroSelector(s *models.RetentionSelector) models.RetentionSelector {
	if s == nil {
		return models.RetentionSelector{}
	}
	return *s
}

func orZeroCriteria(c *models.RetentionCriteria) models.RetentionCriteria {
	if c == nil {
		return models.RetentionCriteria{}
	}
	return *c
}
