// Package rules ports `libs/ontology-kernel/src/handlers/rules.rs`
// 1:1: the 12 endpoints that drive the ontology rule catalog and
// machinery queue under `/api/v1/ontology/rules/*`,
// `/types/{type_id}/rules` and `/objects/{obj_id}/rule-runs`.
//
// All response shapes (envelope + status codes) are byte-identical to
// the Rust source so existing dashboards, frontends and integration
// tests round-trip unchanged. apply_rule routes through the evaluator,
// DB-backed object writeback/outbox helper, rule-run recorder and
// machinery queue path.
package rules

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/handlers/objects"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// Mount registers every rules-handler endpoint on the chi router
// under the same path / verb shape as `lib.rs::build_router::rules_routes`.
func Mount(r chi.Router, state *ontologykernel.AppState) {
	r.Get("/rules", ListRules(state))
	r.Post("/rules", CreateRule(state))
	r.Get("/rules/insights", GetMachineryInsights(state))
	r.Get("/rules/machinery/queue", GetMachineryQueue(state))
	r.Patch("/rules/machinery/queue/{id}", UpdateMachineryQueueItem(state))
	r.Get("/rules/{id}", GetRule(state))
	r.Patch("/rules/{id}", UpdateRule(state))
	r.Delete("/rules/{id}", DeleteRule(state))
	r.Post("/rules/{id}/simulate", SimulateRule(state))
	r.Post("/rules/{id}/apply", ApplyRule(state))
	r.Get("/types/{type_id}/rules", ListRulesForObjectType(state))
	r.Get("/objects/{obj_id}/rule-runs", ListObjectRuleRuns(state))
}

// ── Endpoints (1:1 with the Rust pub async fn set) ──────────────────

// ListRules mirrors `pub async fn list_rules`. Pagination is post-load
// (matches Rust: it loads everything then `skip().take()`). The text
// of the SQL query is identical to the Rust source.
func ListRules(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := parseListRulesQuery(r)
		page := defaultPage(query.Page)
		perPage := defaultPerPage(query.PerPage)
		search := strDeref(query.Search)
		ctx := r.Context()

		const cols = `id, name, display_name, description, object_type_id, evaluation_mode,
		              trigger_spec, effect_spec, owner_id, created_at, updated_at`
		var rows pgx.Rows
		var err error
		if query.ObjectTypeID != nil {
			rows, err = state.DB.Query(ctx,
				`SELECT `+cols+` FROM ontology_rules
				 WHERE object_type_id = $1
				   AND ($2 = '' OR name ILIKE '%' || $2 || '%' OR display_name ILIKE '%' || $2 || '%')
				 ORDER BY created_at DESC`,
				*query.ObjectTypeID, search,
			)
		} else {
			rows, err = state.DB.Query(ctx,
				`SELECT `+cols+` FROM ontology_rules
				 WHERE ($1 = '' OR name ILIKE '%' || $1 || '%' OR display_name ILIKE '%' || $1 || '%')
				 ORDER BY created_at DESC`,
				search,
			)
		}
		if err != nil {
			dbError(w, "failed to list rules: "+err.Error())
			return
		}
		defer rows.Close()

		all := []models.OntologyRule{}
		for rows.Next() {
			var row models.OntologyRuleRow
			if err := rows.Scan(
				&row.ID, &row.Name, &row.DisplayName, &row.Description, &row.ObjectTypeID,
				&row.EvaluationMode, &row.TriggerSpec, &row.EffectSpec, &row.OwnerID,
				&row.CreatedAt, &row.UpdatedAt,
			); err != nil {
				dbError(w, "failed to decode rules: "+err.Error())
				return
			}
			rule, err := row.IntoRule()
			if err != nil {
				dbError(w, "failed to decode rules: "+err.Error())
				return
			}
			all = append(all, rule)
		}

		total := int64(len(all))
		offset := int((page - 1) * perPage)
		end := offset + int(perPage)
		if offset > len(all) {
			offset = len(all)
		}
		if end > len(all) {
			end = len(all)
		}
		writeJSON(w, http.StatusOK, models.ListRulesResponse{
			Data:    all[offset:end],
			Total:   total,
			Page:    page,
			PerPage: perPage,
		})
	}
}

// CreateRule mirrors `pub async fn create_rule`.
func CreateRule(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errBody("missing claims"))
			return
		}
		var body models.CreateRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			invalid(w, "invalid request body")
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			invalid(w, "rule name is required")
			return
		}
		displayName := body.Name
		if body.DisplayName != nil {
			displayName = *body.DisplayName
		}
		description := ""
		if body.Description != nil {
			description = *body.Description
		}
		evaluationMode := models.RuleEvaluationModeAdvisory
		if body.EvaluationMode != nil {
			evaluationMode = *body.EvaluationMode
		}
		var trigger models.RuleTriggerSpec
		if body.TriggerSpec != nil {
			trigger = *body.TriggerSpec
		}
		var effect models.RuleEffectSpec
		if body.EffectSpec != nil {
			effect = *body.EffectSpec
		}

		ctx := r.Context()
		if err := domain.ValidateRuleDefinition(ctx, state, body.ObjectTypeID, trigger, effect); err != nil {
			invalid(w, err.Error())
			return
		}

		triggerJSON, _ := json.Marshal(trigger)
		effectJSON, _ := json.Marshal(effect)

		// Rust uses Uuid::now_v7() so rule IDs are time-sortable; the
		// catalog read paths rely on that for stable iteration.
		ruleID, err := uuid.NewV7()
		if err != nil {
			dbError(w, "failed to allocate rule id: "+err.Error())
			return
		}
		var row models.OntologyRuleRow
		err = state.DB.QueryRow(ctx, `
			INSERT INTO ontology_rules (
				id, name, display_name, description, object_type_id, evaluation_mode,
				trigger_spec, effect_spec, owner_id
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9)
			RETURNING id, name, display_name, description, object_type_id, evaluation_mode,
			          trigger_spec, effect_spec, owner_id, created_at, updated_at`,
			ruleID, strings.TrimSpace(body.Name), displayName, description,
			body.ObjectTypeID, string(evaluationMode), triggerJSON, effectJSON, claims.Sub,
		).Scan(
			&row.ID, &row.Name, &row.DisplayName, &row.Description, &row.ObjectTypeID,
			&row.EvaluationMode, &row.TriggerSpec, &row.EffectSpec, &row.OwnerID,
			&row.CreatedAt, &row.UpdatedAt,
		)
		if err != nil {
			dbError(w, "failed to create rule: "+err.Error())
			return
		}
		rule, err := row.IntoRule()
		if err != nil {
			dbError(w, "failed to decode rule: "+err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, rule)
	}
}

// GetRule mirrors `pub async fn get_rule`.
func GetRule(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathUUID(r, "id")
		if err != nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		rule, err := domain.LoadRule(r.Context(), state, id)
		if err != nil {
			dbError(w, err.Error())
			return
		}
		if rule == nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		writeJSON(w, http.StatusOK, rule)
	}
}

// UpdateRule mirrors `pub async fn update_rule`.
func UpdateRule(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathUUID(r, "id")
		if err != nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		var body models.UpdateRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			invalid(w, "invalid request body")
			return
		}
		ctx := r.Context()
		existing, err := domain.LoadRule(ctx, state, id)
		if err != nil {
			dbError(w, err.Error())
			return
		}
		if existing == nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		evaluationMode := existing.EvaluationMode
		if body.EvaluationMode != nil {
			evaluationMode = *body.EvaluationMode
		}
		trigger := existing.TriggerSpec
		if body.TriggerSpec != nil {
			trigger = *body.TriggerSpec
		}
		effect := existing.EffectSpec
		if body.EffectSpec != nil {
			effect = *body.EffectSpec
		}
		if err := domain.ValidateRuleDefinition(ctx, state, existing.ObjectTypeID, trigger, effect); err != nil {
			invalid(w, err.Error())
			return
		}

		triggerJSON, _ := json.Marshal(trigger)
		effectJSON, _ := json.Marshal(effect)
		var row models.OntologyRuleRow
		err = state.DB.QueryRow(ctx, `
			UPDATE ontology_rules
			SET display_name = COALESCE($2, display_name),
			    description  = COALESCE($3, description),
			    evaluation_mode = $4,
			    trigger_spec = $5::jsonb,
			    effect_spec  = $6::jsonb,
			    updated_at   = NOW()
			WHERE id = $1
			RETURNING id, name, display_name, description, object_type_id, evaluation_mode,
			          trigger_spec, effect_spec, owner_id, created_at, updated_at`,
			id, body.DisplayName, body.Description, string(evaluationMode), triggerJSON, effectJSON,
		).Scan(
			&row.ID, &row.Name, &row.DisplayName, &row.Description, &row.ObjectTypeID,
			&row.EvaluationMode, &row.TriggerSpec, &row.EffectSpec, &row.OwnerID,
			&row.CreatedAt, &row.UpdatedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		if err != nil {
			dbError(w, "failed to update rule: "+err.Error())
			return
		}
		rule, err := row.IntoRule()
		if err != nil {
			dbError(w, "failed to decode rule: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, rule)
	}
}

// DeleteRule mirrors `pub async fn delete_rule`.
func DeleteRule(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathUUID(r, "id")
		if err != nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		ct, err := state.DB.Exec(r.Context(), "DELETE FROM ontology_rules WHERE id = $1", id)
		if err != nil {
			dbError(w, "failed to delete rule: "+err.Error())
			return
		}
		if ct.RowsAffected() == 0 {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// SimulateRule mirrors `pub async fn simulate_rule`.
func SimulateRule(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errBody("missing claims"))
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		var body models.RuleEvaluateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			invalid(w, "invalid request body")
			return
		}
		ctx := r.Context()

		rule, err := domain.LoadRule(ctx, state, id)
		if err != nil {
			dbError(w, err.Error())
			return
		}
		if rule == nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		object, err := objects.LoadObjectInstance(ctx, state, claims, body.ObjectID, storage.Strong())
		if err != nil {
			dbError(w, "failed to load object: "+err.Error())
			return
		}
		if object == nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		if object.ObjectTypeID != rule.ObjectTypeID {
			invalid(w, "rule object_type_id does not match the target object")
			return
		}
		if err := domain.EnsureObjectAccess(claims, object); err != nil {
			writeJSON(w, http.StatusForbidden, errBody(err.Error()))
			return
		}
		patch, err := parsePropertiesPatch(body.PropertiesPatch)
		if err != nil {
			invalid(w, err.Error())
			return
		}
		match := domain.EvaluateRuleAgainstObject(rule, object, patch)
		if _, err := domain.RecordRuleRun(ctx, state, claims, rule.ID, object.ID,
			match.Matched, true, match.TriggerPayload, match.EffectPreview); err != nil {
			// log only — Rust uses `tracing::warn!` and continues.
			_ = err
		}
		writeJSON(w, http.StatusOK, domain.BuildRuleEvaluateResponse(*rule, object, match))
	}
}

// ApplyRule mirrors `pub async fn apply_rule`. It evaluates the rule,
// applies matched object patches through the DB-backed writeback/outbox
// helper, records the rule run and preserves Rust's response envelope.
func ApplyRule(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errBody("missing claims"))
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		var body models.RuleEvaluateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			invalid(w, "invalid request body")
			return
		}
		ctx := r.Context()

		rule, err := domain.LoadRule(ctx, state, id)
		if err != nil {
			dbError(w, err.Error())
			return
		}
		if rule == nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		object, err := objects.LoadObjectInstance(ctx, state, claims, body.ObjectID, storage.Strong())
		if err != nil {
			dbError(w, "failed to load object: "+err.Error())
			return
		}
		if object == nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		if object.ObjectTypeID != rule.ObjectTypeID {
			invalid(w, "rule object_type_id does not match the target object")
			return
		}
		if err := domain.EnsureObjectAccess(claims, object); err != nil {
			writeJSON(w, http.StatusForbidden, errBody(err.Error()))
			return
		}
		patch, err := parsePropertiesPatch(body.PropertiesPatch)
		if err != nil {
			invalid(w, err.Error())
			return
		}
		match := domain.EvaluateRuleAgainstObject(rule, object, patch)

		updated := object
		if match.Matched {
			// Real writeback path (sub-phase complete):
			// `ApplyRuleEffectReal` validates the effect_preview's
			// object_patch, merges it into the existing properties,
			// runs the schema validator, and routes the resulting
			// object through `objects.ApplyObjectWrite` (writeback +
			// outbox enqueue). On version conflicts we surface the
			// typed error so the client can refresh and retry.
			next, err := ApplyRuleEffectReal(ctx, state, claims, object, match.EffectPreview)
			if err != nil {
				if domain.IsVersionConflict(err) {
					writeJSON(w, http.StatusConflict, map[string]any{
						"error":          "version_conflict",
						"detail":         err.Error(),
						"rule":           rule,
						"effect_preview": match.EffectPreview,
					})
					return
				}
				dbError(w, err.Error())
				return
			}
			if next != nil {
				updated = next
			}
		}

		recorded, err := domain.RecordRuleRun(ctx, state, claims, rule.ID, object.ID,
			match.Matched, false, match.TriggerPayload, match.EffectPreview)
		if err != nil {
			recorded = nil
		}
		if recorded != nil && match.Matched {
			if _, err := domain.EnqueueRuleSchedule(ctx, state, rule, updated, recorded.ID,
				match.EffectPreview, claims.Sub); err != nil {
				_ = err // Rust logs warn and continues.
			}
		}

		response := domain.BuildRuleEvaluateResponse(*rule, object, match)
		updatedJSON, _ := json.Marshal(updated)
		response.Object = updatedJSON
		writeJSON(w, http.StatusOK, response)
	}
}

// GetMachineryInsights mirrors `pub async fn get_machinery_insights`.
func GetMachineryInsights(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errBody("missing claims"))
			return
		}
		query := parseListRulesQuery(r)
		insights, err := domain.MachineryInsights(r.Context(), state, claims, query.ObjectTypeID)
		if err != nil {
			dbError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, models.MachineryInsightsResponse{
			ObjectTypeID: query.ObjectTypeID,
			Data:         insights,
		})
	}
}

// GetMachineryQueue mirrors `pub async fn get_machinery_queue`.
func GetMachineryQueue(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := parseListRulesQuery(r)
		response, err := domain.MachineryQueue(r.Context(), state, query.ObjectTypeID)
		if err != nil {
			dbError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, response)
	}
}

// UpdateMachineryQueueItem mirrors `pub async fn update_machinery_queue_item`.
func UpdateMachineryQueueItem(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathUUID(r, "id")
		if err != nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		var body models.UpdateMachineryQueueItemRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			invalid(w, "invalid request body")
			return
		}
		row, err := domain.TransitionMachineryQueueItem(r.Context(), state, id, body.Status)
		if err != nil {
			if err.Error() == "unsupported machinery queue status" {
				invalid(w, err.Error())
				return
			}
			dbError(w, err.Error())
			return
		}
		if row == nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		writeJSON(w, http.StatusOK, row)
	}
}

// ListObjectRuleRuns mirrors `pub async fn list_object_rule_runs`.
func ListObjectRuleRuns(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errBody("missing claims"))
			return
		}
		objectID, err := pathUUID(r, "obj_id")
		if err != nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		ctx := r.Context()
		object, err := objects.LoadObjectInstance(ctx, state, claims, objectID, storage.Strong())
		if err != nil {
			dbError(w, "failed to load object: "+err.Error())
			return
		}
		if object == nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		if err := domain.EnsureObjectAccess(claims, object); err != nil {
			writeJSON(w, http.StatusForbidden, errBody(err.Error()))
			return
		}
		runs, err := domain.LoadRecentRuleRuns(ctx, state, claims, objectID, 20)
		if err != nil {
			dbError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": runs})
	}
}

// ListRulesForObjectType mirrors `pub async fn list_rules_for_object_type`.
func ListRulesForObjectType(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		objectTypeID, err := pathUUID(r, "type_id")
		if err != nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		rules, err := domain.LoadRulesForObjectType(r.Context(), state, objectTypeID)
		if err != nil {
			dbError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": rules})
	}
}

// ── Helpers (HTTP plumbing + parameter parsing) ─────────────────────

func parseListRulesQuery(r *http.Request) models.ListRulesQuery {
	q := r.URL.Query()
	out := models.ListRulesQuery{}
	if raw := q.Get("object_type_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			out.ObjectTypeID = &id
		}
	}
	if raw := q.Get("page"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			out.Page = &v
		}
	}
	if raw := q.Get("per_page"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			out.PerPage = &v
		}
	}
	if raw := q.Get("search"); raw != "" {
		out.Search = &raw
	}
	return out
}

func defaultPage(p *int64) int64 {
	if p == nil || *p < 1 {
		return 1
	}
	return *p
}

func defaultPerPage(p *int64) int64 {
	if p == nil {
		return 20
	}
	if *p < 1 {
		return 1
	}
	if *p > 100 {
		return 100
	}
	return *p
}

func strDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func pathUUID(r *http.Request, key string) (uuid.UUID, error) {
	raw := chi.URLParam(r, key)
	if raw == "" {
		return uuid.Nil, errors.New("missing path parameter " + key)
	}
	return uuid.Parse(strings.TrimSpace(raw))
}

func parsePropertiesPatch(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asMap); err != nil {
		return nil, errors.New("properties_patch must be a JSON object")
	}
	return asMap, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func errBody(msg string) map[string]string { return map[string]string{"error": msg} }

func invalid(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, errBody(msg))
}

func dbError(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusInternalServerError, errBody(msg))
}
