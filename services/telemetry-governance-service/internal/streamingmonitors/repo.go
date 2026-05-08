package streamingmonitors

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo wraps the SQL surface for the three streaming-monitor tables.
type Repo struct{ Pool *pgxpool.Pool }

// ─── Monitoring views ───────────────────────────────────────────────

func (r *Repo) ListViews(ctx context.Context) ([]MonitoringView, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id, name, description, project_rid, created_by, created_at, updated_at
		 FROM monitoring_views ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MonitoringView, 0)
	for rows.Next() {
		var v MonitoringView
		if err := rows.Scan(&v.ID, &v.Name, &v.Description, &v.ProjectRID,
			&v.CreatedBy, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repo) GetView(ctx context.Context, id uuid.UUID) (*MonitoringView, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT id, name, description, project_rid, created_by, created_at, updated_at
		 FROM monitoring_views WHERE id = $1`, id)
	v := &MonitoringView{}
	err := row.Scan(&v.ID, &v.Name, &v.Description, &v.ProjectRID,
		&v.CreatedBy, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (r *Repo) CreateView(ctx context.Context, id uuid.UUID, name, description, projectRID, createdBy string) (*MonitoringView, error) {
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO monitoring_views (id, name, description, project_rid, created_by)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, name, description, project_rid, created_by, created_at, updated_at`,
		id, strings.TrimSpace(name), description, strings.TrimSpace(projectRID), createdBy)
	v := &MonitoringView{}
	if err := row.Scan(&v.ID, &v.Name, &v.Description, &v.ProjectRID,
		&v.CreatedBy, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, nil
}

// ─── Monitor rules ──────────────────────────────────────────────────

const ruleSelect = `SELECT id, view_id, name, resource_type, resource_rid, monitor_kind,
	      window_seconds, comparator, threshold, severity, enabled,
	      created_by, created_at, updated_at`

func (r *Repo) ListRulesForView(ctx context.Context, viewID uuid.UUID) ([]MonitorRule, error) {
	rows, err := r.Pool.Query(ctx,
		ruleSelect+` FROM monitor_rules WHERE view_id = $1 ORDER BY created_at DESC`, viewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRules(rows)
}

// ListRulesQuery filters rules by optional resource_type, resource_rid,
// monitor_kind. Empty values mean "no filter".
type ListRulesQuery struct {
	ResourceType ResourceType
	ResourceRID  string
	MonitorKind  MonitorKind
}

func (r *Repo) ListRules(ctx context.Context, q ListRulesQuery) ([]MonitorRule, error) {
	var (
		clauses []string
		args    []any
	)
	if q.ResourceType != "" {
		clauses = append(clauses, "resource_type = $"+itoa(len(args)+1))
		args = append(args, string(q.ResourceType))
	}
	if q.ResourceRID != "" {
		clauses = append(clauses, "resource_rid = $"+itoa(len(args)+1))
		args = append(args, q.ResourceRID)
	}
	if q.MonitorKind != "" {
		clauses = append(clauses, "monitor_kind = $"+itoa(len(args)+1))
		args = append(args, string(q.MonitorKind))
	}
	sql := ruleSelect + ` FROM monitor_rules`
	if len(clauses) > 0 {
		sql += ` WHERE ` + strings.Join(clauses, " AND ")
	}
	sql += ` ORDER BY created_at DESC`
	rows, err := r.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRules(rows)
}

func (r *Repo) GetRule(ctx context.Context, id uuid.UUID) (*MonitorRule, error) {
	row := r.Pool.QueryRow(ctx, ruleSelect+` FROM monitor_rules WHERE id = $1`, id)
	rule, err := scanRule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return rule, err
}

func (r *Repo) CreateRule(ctx context.Context, id uuid.UUID, body *CreateMonitorRuleRequest, createdBy string) (*MonitorRule, error) {
	name := ""
	if body.Name != nil {
		name = *body.Name
	}
	severity := body.Severity
	if severity == "" {
		severity = SeverityWarn
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO monitor_rules (
		     id, view_id, name, resource_type, resource_rid, monitor_kind,
		     window_seconds, comparator, threshold, severity, created_by
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id, view_id, name, resource_type, resource_rid, monitor_kind,
		           window_seconds, comparator, threshold, severity, enabled,
		           created_by, created_at, updated_at`,
		id, body.ViewID, name, string(body.ResourceType), body.ResourceRID,
		string(body.MonitorKind), body.WindowSeconds, string(body.Comparator),
		body.Threshold, string(severity), createdBy,
	)
	return scanRule(row)
}

func (r *Repo) PatchRule(ctx context.Context, id uuid.UUID, body *PatchRuleRequest) (*MonitorRule, error) {
	current, err := r.GetRule(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, nil
	}
	name := current.Name
	if body.Name != nil {
		name = *body.Name
	}
	window := current.WindowSeconds
	if body.WindowSeconds != nil {
		window = *body.WindowSeconds
	}
	cmp := current.Comparator
	if body.Comparator != nil {
		cmp = *body.Comparator
	}
	threshold := current.Threshold
	if body.Threshold != nil {
		threshold = *body.Threshold
	}
	sev := current.Severity
	if body.Severity != nil {
		sev = *body.Severity
	}
	enabled := current.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE monitor_rules SET
		    name           = $2,
		    window_seconds = $3,
		    comparator     = $4,
		    threshold      = $5,
		    severity       = $6,
		    enabled        = $7,
		    updated_at     = $8
		  WHERE id = $1
		  RETURNING id, view_id, name, resource_type, resource_rid, monitor_kind,
		            window_seconds, comparator, threshold, severity, enabled,
		            created_by, created_at, updated_at`,
		id, name, window, string(cmp), threshold, string(sev), enabled, time.Now().UTC(),
	)
	return scanRule(row)
}

// DeleteRule returns false when no row was deleted (404 mapping).
func (r *Repo) DeleteRule(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM monitor_rules WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

// ─── Monitor evaluations ────────────────────────────────────────────

func (r *Repo) ListEvaluations(ctx context.Context, ruleID uuid.UUID, limit int) ([]MonitorEvaluation, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT id, rule_id, evaluated_at, observed_value, fired, alert_id
		 FROM monitor_evaluations WHERE rule_id = $1
		 ORDER BY evaluated_at DESC LIMIT $2`,
		ruleID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MonitorEvaluation, 0)
	for rows.Next() {
		var e MonitorEvaluation
		if err := rows.Scan(&e.ID, &e.RuleID, &e.EvaluatedAt, &e.ObservedValue,
			&e.Fired, &e.AlertID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ─── helpers ────────────────────────────────────────────────────────

type rowLikeT interface{ Scan(...any) error }

func scanRule(r rowLikeT) (*MonitorRule, error) {
	var (
		rule MonitorRule
		rt   string
		mk   string
		cmp  string
		sev  string
	)
	if err := r.Scan(&rule.ID, &rule.ViewID, &rule.Name, &rt, &rule.ResourceRID,
		&mk, &rule.WindowSeconds, &cmp, &rule.Threshold, &sev, &rule.Enabled,
		&rule.CreatedBy, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
		return nil, err
	}
	rule.ResourceType = ResourceType(rt)
	rule.MonitorKind = MonitorKind(mk)
	rule.Comparator = Comparator(cmp)
	rule.Severity = Severity(sev)
	return &rule, nil
}

func scanRules(rows pgx.Rows) ([]MonitorRule, error) {
	out := make([]MonitorRule, 0)
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rule)
	}
	return out, rows.Err()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
