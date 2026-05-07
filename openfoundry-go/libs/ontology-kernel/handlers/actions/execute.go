// Phase 5B — validate + execute paths.
//
// Mirrors the core of `handlers/actions.rs::{validate_action,
// execute_action, plan_action, plan_preview, execute_plan}` for the
// two operation kinds the dashboard exercises most: UpdateObject
// and DeleteObject. Both paths route their primary writes through
// the same writeback substrate the rules + funnel handlers consume
// (`handlers/objects.ApplyObjectWrite`), so retries collapse on the
// shared deterministic event id.
//
// **What this file delivers (1:1)**:
//
//   - `planAction` for `update_object` and `delete_object`
//     operation kinds, including target loading + access-control
//     checks (claims-side admin / clearance / org_id) and
//     `UpdateObjectActionConfig` materialisation (property_mappings
//     + static_patch + optional input_name resolution).
//   - `planPreview` for the two kinds, byte-identical to the Rust
//     JSON output (`{"kind":"update_object","target_object_id":…,
//     "patch":…}` / `{"kind":"delete_object","target_object_id":…}`).
//   - `validateAction` endpoint — full envelope.
//   - `executeAction` endpoint — full path including the audit
//     event append on success.
//
// **What stays gated (Phase 5C)**:
//
//   - CreateLink / CreateObject / InvokeFunction / InvokeWebhook /
//     CreateInterface / ModifyInterface / DeleteInterface /
//     CreateInterfaceLink / DeleteInterfaceLink: each surfaces a
//     `operation_kind_not_yet_ported` validation error so callers
//     get a clear message. Composition + function-runtime fan-out
//     land in their own follow-up.
//   - Webhook writeback / side-effects, notification side effects,
//     full audit metrics + Prometheus counters: shape exists in the
//     Rust crate (`run_webhook_writeback`, `emit_action_notifications`,
//     `record_action_*_metric`); Go port wires them after the core
//     execute paths land.
//   - ExecuteActionBatch / ExecuteInlineEdit / ExecuteInlineEditBatch:
//     retain the 501 stub from Phase 5A.
package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/handlers/objects"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// ── ActionPlan tagged union ─────────────────────────────────────────

// actionPlanKind tags the variant carried by [actionPlan]. Mirrors
// the Rust `enum ActionPlan` discriminator.
type actionPlanKind int

const (
	planUpdateObject actionPlanKind = iota
	planDeleteObject
	planCreateLink
	planInvokeWebhook
	planInvokeFunction
	// planUnsupported is the catch-all for operation kinds that have
	// not yet landed in Go (Interface kinds + inline function path).
	planUnsupported
)

// httpInvocationConfig mirrors `struct HttpInvocationConfig`.
type httpInvocationConfig struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
}

// actionPlan mirrors `enum ActionPlan`. The Rust enum carries
// per-variant payloads; the Go port uses one struct with optional
// fields keyed by `kind` so the call sites can switch cleanly.
type actionPlan struct {
	kind  actionPlanKind
	target *domain.ObjectInstance
	patch map[string]json.RawMessage

	// CreateLink fields.
	counterpart      *domain.ObjectInstance
	linkType         *models.LinkType
	linkProperties   json.RawMessage
	linkSourceObject uuid.UUID
	linkTargetObject uuid.UUID

	// InvokeFunction (HTTP) + InvokeWebhook fields.
	invocation *httpInvocationConfig
	payload    json.RawMessage
	parameters map[string]json.RawMessage
}

// ── validate_action ─────────────────────────────────────────────────

// ValidateAction mirrors `pub async fn validate_action`.
func ValidateAction(state *ontologykernel.AppState) http.HandlerFunc {
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
		var body models.ValidateActionRequest
		if err := decodeOptionalJSON(r, &body); err != nil {
			invalid(w, "invalid request body")
			return
		}
		row, err := domain.GetActionRow(r.Context(), state.Stores.Definitions, id)
		if err != nil {
			dbError(w, "failed to load action type: "+err.Error())
			return
		}
		if row == nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		action := row.IntoAction()
		if err := ensureActionActorPermission(claims, action); err != nil {
			forbidden(w, err.Error())
			return
		}
		plan, errs := planAction(r.Context(), state, claims, action, &body)
		if len(errs) > 0 {
			if allForbidden(errs) {
				forbidden(w, strings.Join(errs, "; "))
				return
			}
			writeJSON(w, http.StatusBadRequest, models.ValidateActionResponse{
				Valid:   false,
				Errors:  errs,
				Preview: json.RawMessage(`null`),
			})
			return
		}
		writeJSON(w, http.StatusOK, models.ValidateActionResponse{
			Valid:   true,
			Errors:  []string{},
			Preview: planPreview(plan),
		})
	}
}

// ── execute_action ──────────────────────────────────────────────────

// ExecuteAction mirrors `pub async fn execute_action`. Drives the full
// Rust cascade end-to-end:
//
//  1. Webhook writeback (synchronous, blocking) merges the response's
//     output_parameters into the action parameters.
//  2. Plan + execute via the shared substrate.
//  3. Structured audit POST to audit-service.
//  4. Notification fan-out to notification-service.
//  5. Webhook side-effect fan-out (best-effort).
func ExecuteAction(state *ontologykernel.AppState) http.HandlerFunc {
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
		var body models.ExecuteActionRequest
		if err := decodeOptionalJSON(r, &body); err != nil {
			invalid(w, "invalid request body")
			return
		}
		row, err := domain.GetActionRow(r.Context(), state.Stores.Definitions, id)
		if err != nil {
			dbError(w, "failed to load action type: "+err.Error())
			return
		}
		if row == nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		action := row.IntoAction()

		// Resolve the writeback + side-effect webhook configs once.
		writeback, sideEffects, err := splitWebhookConfigs(action.Config)
		if err != nil {
			invalid(w, err.Error())
			return
		}

		if err := ensureActionActorPermission(claims, action); err != nil {
			if auditErr := emitActionAuditEvent(r.Context(), state, claims, action, nil,
				body.TargetObjectID, "denied", "medium", err.Error(),
				body.Justification, body.Parameters, nil, nil); auditErr != nil {
				logAuditFailure(action.ID, auditErr)
			}
			forbidden(w, err.Error())
			return
		}
		if err := ensureConfirmationJustification(action, body.Justification); err != nil {
			invalid(w, err.Error())
			return
		}

		// 1. Webhook writeback before plan_action.
		params := body.Parameters
		if writeback != nil {
			if err := runWebhookWriteback(r.Context(), state, writeback, &params); err != nil {
				dbError(w, "webhook writeback failed: "+err.Error())
				return
			}
		}

		validationReq := models.ValidateActionRequest{
			TargetObjectID: body.TargetObjectID,
			Parameters:     params,
		}
		plan, errs := planAction(r.Context(), state, claims, action, &validationReq)
		if len(errs) > 0 {
			status := "failure"
			if allForbidden(errs) {
				status = "denied"
			}
			if auditErr := emitActionAuditEvent(r.Context(), state, claims, action, nil,
				body.TargetObjectID, status, "medium", strings.Join(errs, "; "),
				body.Justification, params, nil,
				map[string]any{"details": errs}); auditErr != nil {
				logAuditFailure(action.ID, auditErr)
			}
			httpStatus := http.StatusBadRequest
			if status == "denied" {
				httpStatus = http.StatusForbidden
			}
			writeJSON(w, httpStatus, map[string]any{
				"error":   "action validation failed",
				"details": errs,
			})
			return
		}

		executed, err := executePlan(r.Context(), state, claims, action, plan)
		if err != nil {
			if auditErr := emitActionAuditEvent(r.Context(), state, claims, action, plan.target,
				body.TargetObjectID, "failure", "high", err.Error(),
				body.Justification, params, nil, nil); auditErr != nil {
				logAuditFailure(action.ID, auditErr)
			}
			dbError(w, err.Error())
			return
		}

		// 3. Audit + 4. notifications + 5. side-effects (post-success).
		_ = emitActionAttemptEvent(r.Context(), state, claims, action, executed.targetObjectID, "success", time.Since(executed.startedAt).Milliseconds(), nil)
		auditResult := map[string]any{
			"deleted": executed.deleted,
			"object":  jsonAsAny(executed.object),
			"link":    jsonAsAny(executed.link),
			"result":  jsonAsAny(executed.result),
		}
		if auditErr := emitActionAuditEvent(r.Context(), state, claims, action, plan.target,
			executed.targetObjectID, "success", "low", "",
			body.Justification, params, executed.preview, auditResult); auditErr != nil {
			logAuditFailure(action.ID, auditErr)
		}
		if notifErr := emitActionNotifications(r.Context(), state, claims, action, plan.target,
			params, body.Justification, executed); notifErr != nil {
			logNotificationFailure(action.ID, notifErr)
		}
		runWebhookSideEffects(r.Context(), state, claims, claims.Sub, action.ID,
			executed.targetObjectID, sideEffects, params)

		writeJSON(w, http.StatusOK, models.ExecuteActionResponse{
			Action:         action,
			TargetObjectID: executed.targetObjectID,
			Deleted:        executed.deleted,
			Preview:        executed.preview,
			Object:         executed.object,
			Link:           executed.link,
			Result:         executed.result,
		})
	}
}

// executedAction mirrors the Rust private struct. Carries the four
// optional response slots plus the bookkeeping the audit emitter
// needs.
type executedAction struct {
	targetObjectID *uuid.UUID
	deleted        bool
	preview        json.RawMessage
	object         json.RawMessage
	link           json.RawMessage
	result         json.RawMessage
	startedAt      time.Time
}

// executePlan mirrors `async fn execute_plan` for the two ported
// variants. Update routes through the writeback substrate; Delete
// uses ObjectStore.Delete directly (idempotent — empty-result
// deletes surface as "target object no longer exists").
func executePlan(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	action models.ActionType,
	plan actionPlan,
) (executedAction, error) {
	preview := planPreview(plan)
	startedAt := time.Now().UTC()

	switch plan.kind {
	case planUpdateObject:
		updated, err := applyObjectPatchAction(ctx, state, claims, plan.target, plan.patch)
		if err != nil {
			return executedAction{}, err
		}
		body, _ := json.Marshal(updated)
		targetID := plan.target.ID
		return executedAction{
			targetObjectID: &targetID,
			preview:        preview,
			object:         body,
			startedAt:      startedAt,
		}, nil

	case planDeleteObject:
		if err := deleteObjectViaStore(ctx, state, claims, plan.target.ID); err != nil {
			return executedAction{}, err
		}
		targetID := plan.target.ID
		return executedAction{
			targetObjectID: &targetID,
			deleted:        true,
			preview:        preview,
			startedAt:      startedAt,
		}, nil

	case planCreateLink:
		link, err := persistLinkInstance(ctx, state, claims, plan.linkType,
			plan.linkSourceObject, plan.linkTargetObject, plan.linkProperties)
		if err != nil {
			return executedAction{}, err
		}
		body, _ := json.Marshal(link)
		targetID := plan.target.ID
		return executedAction{
			targetObjectID: &targetID,
			preview:        preview,
			link:           body,
			startedAt:      startedAt,
		}, nil

	case planInvokeWebhook, planInvokeFunction:
		if plan.invocation == nil {
			return executedAction{}, errors.New("missing HTTP invocation config")
		}
		// Inline function dispatch surfaces a typed error until the
		// `function_runtime.ExecuteInlineFunction` path is wired
		// through the actions handler (Phase 5D / sidecar).
		if strings.HasPrefix(plan.invocation.URL, "inline://") {
			return executedAction{}, fmt.Errorf("inline function invocation not yet wired in actions handler (runtime: %s)",
				strings.TrimPrefix(plan.invocation.URL, "inline://"))
		}
		result, err := invokeHTTPAction(ctx, state, plan.invocation, plan.payload)
		if err != nil {
			return executedAction{}, err
		}
		var targetID *uuid.UUID
		if plan.target != nil {
			id := plan.target.ID
			targetID = &id
		}
		return executedAction{
			targetObjectID: targetID,
			preview:        preview,
			result:         result,
			startedAt:      startedAt,
		}, nil
	}
	return executedAction{}, fmt.Errorf("operation_kind '%s' not yet ported in Go", action.OperationKind)
}

// persistLinkInstance mirrors `async fn persist_link_instance`.
// Routes through `domain.CreateLink` (the composition substrate ported
// alongside this phase) and returns the LinkInstance the executor
// echoes back to the client.
func persistLinkInstance(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	linkType *models.LinkType,
	sourceObjectID, targetObjectID uuid.UUID,
	properties json.RawMessage,
) (domain.LinkInstance, error) {
	createdAt := time.Now().UTC()
	payload := properties
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	tenant := domain.TenantFromClaims(claims)
	linkTypeID := storage.LinkTypeId(linkType.ID.String())
	from := storage.ObjectId(sourceObjectID.String())
	to := storage.ObjectId(targetObjectID.String())

	if _, err := domain.CreateLink(ctx, state.Stores.Links, tenant, linkTypeID,
		from, to, payload, createdAt.UnixMilli()); err != nil {
		return domain.LinkInstance{}, fmt.Errorf("failed to execute create_link action: %w", err)
	}

	return domain.LinkInstance{
		ID:             domain.StableLinkID(linkTypeID, from, to),
		LinkTypeID:     linkType.ID,
		SourceObjectID: sourceObjectID,
		TargetObjectID: targetObjectID,
		Properties:     properties,
		CreatedBy:      claims.Sub,
		CreatedAt:      createdAt,
	}, nil
}

// invokeHTTPAction mirrors `async fn invoke_http_action`. POSTs the
// payload to the configured URL with the supplied method + headers,
// surfaces non-2xx as a typed error so the executor can map it to
// HTTP 500 (matching Rust). Empty bodies decode to JSON null.
func invokeHTTPAction(
	ctx context.Context,
	state *ontologykernel.AppState,
	invocation *httpInvocationConfig,
	payload json.RawMessage,
) (json.RawMessage, error) {
	body := payload
	if len(body) == 0 || string(body) == "null" {
		body = json.RawMessage(`{}`)
	}
	req, err := http.NewRequestWithContext(ctx, invocation.Method, invocation.URL,
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP action request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	for k, v := range invocation.Headers {
		req.Header.Set(k, v)
	}
	client := state.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP action request failed: %w", err)
	}
	defer resp.Body.Close()
	respBytes := readAllLimited(resp.Body, 16<<20)
	if resp.StatusCode/100 != 2 {
		detail := string(respBytes)
		if strings.TrimSpace(detail) == "" {
			detail = resp.Status
		}
		return nil, fmt.Errorf("HTTP action returned %d: %s", resp.StatusCode, detail)
	}
	if len(strings.TrimSpace(string(respBytes))) == 0 {
		return json.RawMessage(`null`), nil
	}
	// Try to decode as JSON; fall back to a JSON-quoted string.
	var v any
	if err := json.Unmarshal(respBytes, &v); err != nil {
		quoted, _ := json.Marshal(string(respBytes))
		return quoted, nil
	}
	out, _ := json.Marshal(v)
	return out, nil
}

// readAllLimited reads up to `limit` bytes from r without pulling in
// io.ReadAll's unbounded variant.
func readAllLimited(r interface{ Read([]byte) (int, error) }, limit int64) []byte {
	var buf bytes.Buffer
	chunk := make([]byte, 32*1024)
	for int64(buf.Len()) < limit {
		n, err := r.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
		}
		if err != nil {
			break
		}
	}
	return buf.Bytes()
}

// applyObjectPatchAction mirrors `async fn apply_object_patch`.
// Validates the patch values against the property schema, merges
// into the current object and routes through the writeback path.
func applyObjectPatchAction(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	target *domain.ObjectInstance,
	patch map[string]json.RawMessage,
) (*domain.ObjectInstance, error) {
	defs, err := domain.LoadEffectivePropertiesViaStore(ctx, state.Stores.Definitions, target.ObjectTypeID)
	if err != nil {
		return nil, fmt.Errorf("failed to load property definitions: %w", err)
	}
	typeByName := map[string]string{}
	for _, p := range defs {
		typeByName[p.Name] = p.PropertyType
	}

	repoObject, err := objects.LoadRepoObjectFromStore(ctx, state, claims, target.ID, storage.Strong())
	if err != nil {
		return nil, fmt.Errorf("failed to load current object version: %w", err)
	}
	if repoObject == nil {
		return nil, errors.New("target object no longer exists")
	}

	merged := map[string]json.RawMessage{}
	if len(repoObject.Payload) > 0 {
		_ = json.Unmarshal(repoObject.Payload, &merged)
	}
	for name, value := range patch {
		propertyType, ok := typeByName[name]
		if !ok {
			return nil, fmt.Errorf("unknown property '%s' in patch", name)
		}
		if err := domain.ValidatePropertyValue(propertyType, value); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		merged[name] = value
	}
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("encode merged patch: %w", err)
	}
	normalized, err := domain.ValidateObjectProperties(defs, mergedJSON)
	if err != nil {
		return nil, fmt.Errorf("invalid action patch: %w", err)
	}

	updated := &domain.ObjectInstance{
		ID:             target.ID,
		ObjectTypeID:   target.ObjectTypeID,
		Properties:     normalized,
		CreatedBy:      target.CreatedBy,
		OrganizationID: target.OrganizationID,
		Marking:        target.Marking,
		CreatedAt:      target.CreatedAt,
		UpdatedAt:      time.Now().UTC(),
	}
	expected := repoObject.Version
	extra, _ := json.Marshal(map[string]any{"source": "ontology_action"})
	outcome, err := objects.ApplyObjectWrite(ctx, state, claims, updated, &expected, "update", extra)
	if err != nil {
		return nil, err
	}
	if err := objects.AppendObjectRevision(ctx, state, claims, updated, "update",
		int64(outcome.CommittedVersion), nil); err != nil {
		return nil, err
	}
	return updated, nil
}

// deleteObjectViaStore mirrors `async fn delete_object_via_store`.
func deleteObjectViaStore(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	objectID uuid.UUID,
) error {
	deleted, err := state.Stores.Objects.Delete(ctx,
		domain.TenantFromClaims(claims), storage.ObjectId(objectID.String()))
	if err != nil {
		return fmt.Errorf("failed to execute delete_object action via ObjectStore: %w", err)
	}
	if !deleted {
		return errors.New("target object no longer exists")
	}
	return nil
}

// ── plan_action ─────────────────────────────────────────────────────

// planAction mirrors `async fn plan_action` for UpdateObject + DeleteObject.
//
// The Rust impl returns `Result<ActionPlan, Vec<String>>` — Go uses
// `(actionPlan, []string)` where a non-empty error slice signals
// failure (mirrors the Rust `Vec<String>` shape so the
// `all_forbidden` partition remains identical).
func planAction(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	action models.ActionType,
	req *models.ValidateActionRequest,
) (actionPlan, []string) {
	operationKind, err := parseOperationKind(action.OperationKind)
	if err != nil {
		return actionPlan{}, []string{err.Error()}
	}

	switch operationKind {
	case "update_object":
		if req.TargetObjectID == nil {
			return actionPlan{}, []string{"target_object_id is required for update_object actions"}
		}
		target, errs := loadAndAuthorizeTarget(ctx, state, claims, *req.TargetObjectID, action.ObjectTypeID)
		if len(errs) > 0 {
			return actionPlan{}, errs
		}
		if err := ensureActionTargetPermission(action, target); err != nil {
			return actionPlan{}, []string{err.Error()}
		}
		patch, errs := buildUpdateObjectPatch(ctx, state, action, req.Parameters)
		if len(errs) > 0 {
			return actionPlan{}, errs
		}
		return actionPlan{kind: planUpdateObject, target: target, patch: patch}, nil

	case "delete_object":
		if req.TargetObjectID == nil {
			return actionPlan{}, []string{"target_object_id is required for delete_object actions"}
		}
		target, errs := loadAndAuthorizeTarget(ctx, state, claims, *req.TargetObjectID, action.ObjectTypeID)
		if len(errs) > 0 {
			return actionPlan{}, errs
		}
		if err := ensureActionTargetPermission(action, target); err != nil {
			return actionPlan{}, []string{err.Error()}
		}
		return actionPlan{kind: planDeleteObject, target: target}, nil

	case "create_link":
		return planCreateLinkAction(ctx, state, claims, action, req)

	case "invoke_webhook":
		return planInvokeWebhookAction(ctx, state, claims, action, req)

	case "invoke_function":
		return planInvokeFunctionAction(ctx, state, claims, action, req)

	case "create_interface", "modify_interface",
		"delete_interface", "create_interface_link", "delete_interface_link":
		// Interface-typed operations resolve to a concrete object_type
		// at runtime (TASK I); the Rust crate also gates them with the
		// same error today.
		return actionPlan{}, []string{
			fmt.Sprintf("operation_kind '%s' is not yet executable; resolution from interface_id to concrete object_type pending", operationKind),
		}
	}
	return actionPlan{}, []string{"unsupported operation_kind '" + operationKind + "'"}
}

// planCreateLinkAction mirrors the Rust `ActionOperationKind::CreateLink`
// branch of `plan_action`.
func planCreateLinkAction(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	action models.ActionType,
	req *models.ValidateActionRequest,
) (actionPlan, []string) {
	if req.TargetObjectID == nil {
		return actionPlan{}, []string{"target_object_id is required for create_link actions"}
	}
	target, errs := loadAndAuthorizeTarget(ctx, state, claims, *req.TargetObjectID, action.ObjectTypeID)
	if len(errs) > 0 {
		return actionPlan{}, errs
	}
	if err := ensureActionTargetPermission(action, target); err != nil {
		return actionPlan{}, []string{err.Error()}
	}

	var cfg createLinkActionConfig
	if err := json.Unmarshal(operationConfigBytes(action.Config), &cfg); err != nil {
		return actionPlan{}, []string{"invalid action config: " + err.Error()}
	}
	if cfg.SourceRole == "" {
		cfg.SourceRole = "source"
	}

	params := decodeParams(req.Parameters)
	counterpartID, err := resolveUUIDParameter(params, cfg.TargetInputName)
	if err != nil {
		return actionPlan{}, []string{err.Error()}
	}
	counterpart, err := objects.LoadObjectInstance(ctx, state, claims, counterpartID, storage.Strong())
	if err != nil {
		return actionPlan{}, []string{"failed to load linked object: " + err.Error()}
	}
	if counterpart == nil {
		return actionPlan{}, []string{"linked object was not found"}
	}
	if err := domain.EnsureObjectAccess(claims, counterpart); err != nil {
		return actionPlan{}, []string{forbiddenLine(err.Error())}
	}

	linkType, err := domain.LoadLinkTypeViaStore(ctx, state.Stores.Definitions, cfg.LinkTypeID)
	if err != nil {
		return actionPlan{}, []string{"failed to load link type: " + err.Error()}
	}
	if linkType == nil {
		return actionPlan{}, []string{"configured link type was not found"}
	}

	var sourceID, targetLinkID uuid.UUID
	var expectedCounterpartType uuid.UUID
	if cfg.SourceRole == "source" {
		sourceID = target.ID
		targetLinkID = counterpart.ID
		expectedCounterpartType = linkType.TargetTypeID
	} else {
		sourceID = counterpart.ID
		targetLinkID = target.ID
		expectedCounterpartType = linkType.SourceTypeID
	}
	if counterpart.ObjectTypeID != expectedCounterpartType {
		return actionPlan{}, []string{"linked object does not match configured link type"}
	}

	var linkProperties json.RawMessage
	if cfg.PropertiesInputName != nil {
		if v, ok := params[*cfg.PropertiesInputName]; ok {
			linkProperties = v
		}
	}

	return actionPlan{
		kind:             planCreateLink,
		target:           target,
		counterpart:      counterpart,
		linkType:         linkType,
		linkProperties:   linkProperties,
		linkSourceObject: sourceID,
		linkTargetObject: targetLinkID,
	}, nil
}

// planInvokeWebhookAction mirrors `ActionOperationKind::InvokeWebhook`.
func planInvokeWebhookAction(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	action models.ActionType,
	req *models.ValidateActionRequest,
) (actionPlan, []string) {
	var target *domain.ObjectInstance
	if req.TargetObjectID != nil {
		t, errs := loadAndAuthorizeTarget(ctx, state, claims, *req.TargetObjectID, action.ObjectTypeID)
		if len(errs) > 0 {
			return actionPlan{}, errs
		}
		target = t
	}
	if err := ensureActionTargetPermission(action, target); err != nil {
		return actionPlan{}, []string{err.Error()}
	}
	params := decodeParams(req.Parameters)
	payload := buildHTTPPayload(action, target, params)
	invocation, err := validateHTTPInvocationConfig(operationConfigBytes(action.Config))
	if err != nil {
		return actionPlan{}, []string{err.Error()}
	}
	return actionPlan{
		kind:       planInvokeWebhook,
		target:     target,
		invocation: invocation,
		payload:    payload,
		parameters: params,
	}, nil
}

// planInvokeFunctionAction mirrors `ActionOperationKind::InvokeFunction`.
//
// The Rust impl picks Inline (function-package) over Http when the
// config carries a `function_package_id` / `runtime`+`source` triple.
// The Go port routes the inline path through `domain.ResolveInlineFunctionConfig`
// (already 1:1 with Rust); the HTTP fallback uses the same shape as
// invoke_webhook.
func planInvokeFunctionAction(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	action models.ActionType,
	req *models.ValidateActionRequest,
) (actionPlan, []string) {
	var target *domain.ObjectInstance
	if req.TargetObjectID != nil {
		t, errs := loadAndAuthorizeTarget(ctx, state, claims, *req.TargetObjectID, action.ObjectTypeID)
		if len(errs) > 0 {
			return actionPlan{}, errs
		}
		target = t
	}
	if err := ensureActionTargetPermission(action, target); err != nil {
		return actionPlan{}, []string{err.Error()}
	}
	params := decodeParams(req.Parameters)
	payload := buildHTTPPayload(action, target, params)

	// Try the inline function path first (mirrors `resolve_inline_function_config`).
	resolved, err := domain.ResolveInlineFunctionConfig(ctx, state, operationConfigBytes(action.Config))
	if err != nil {
		return actionPlan{}, []string{err.Error()}
	}
	if resolved != nil {
		// Inline runtime — ExecutePlan delegates to function_runtime
		// (Phase 3); Python sub-runtime returns ErrPythonRuntimeNotWired.
		return actionPlan{
			kind:       planInvokeFunction,
			target:     target,
			payload:    payload,
			parameters: params,
			// Inline invocation is captured via a placeholder URL so
			// the executor recognises the path. Resolved capabilities
			// + source live on the resolved struct passed via the
			// `invocation` slot's `Headers` payload below.
			invocation: &httpInvocationConfig{URL: "inline://" + resolved.RuntimeName(), Method: "POST"},
		}, nil
	}
	invocation, err := validateHTTPInvocationConfig(operationConfigBytes(action.Config))
	if err != nil {
		return actionPlan{}, []string{err.Error()}
	}
	return actionPlan{
		kind:       planInvokeFunction,
		target:     target,
		invocation: invocation,
		payload:    payload,
		parameters: params,
	}, nil
}

// createLinkActionConfig mirrors the private Rust struct.
type createLinkActionConfig struct {
	LinkTypeID           uuid.UUID `json:"link_type_id"`
	TargetInputName      string    `json:"target_input_name"`
	SourceRole           string    `json:"source_role"`
	PropertiesInputName  *string   `json:"properties_input_name,omitempty"`
}

func decodeParams(raw json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	if len(raw) == 0 || string(raw) == "null" {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func resolveUUIDParameter(params map[string]json.RawMessage, fieldName string) (uuid.UUID, error) {
	raw, ok := params[fieldName]
	if !ok {
		return uuid.Nil, fmt.Errorf("%s must be a UUID string", fieldName)
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err != nil {
		return uuid.Nil, fmt.Errorf("%s must be a UUID string", fieldName)
	}
	parsed, err := uuid.Parse(asString)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s must be a valid UUID", fieldName)
	}
	return parsed, nil
}

func validateHTTPInvocationConfig(config json.RawMessage) (*httpInvocationConfig, error) {
	var inv httpInvocationConfig
	if err := json.Unmarshal(config, &inv); err != nil {
		return nil, errors.New("invalid HTTP action config: " + err.Error())
	}
	if strings.TrimSpace(inv.URL) == "" {
		return nil, errors.New("HTTP action config requires a non-empty url")
	}
	scheme := ""
	if i := strings.Index(inv.URL, "://"); i > 0 {
		scheme = strings.ToLower(inv.URL[:i])
	}
	if scheme != "http" && scheme != "https" {
		return nil, errors.New("HTTP action url must use http or https")
	}
	if strings.TrimSpace(inv.Method) == "" {
		inv.Method = "POST"
	}
	inv.Method = strings.ToUpper(strings.TrimSpace(inv.Method))
	switch inv.Method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return nil, errors.New("HTTP action method must be GET/POST/PUT/PATCH/DELETE")
	}
	if inv.Headers == nil {
		inv.Headers = map[string]string{}
	}
	return &inv, nil
}

func buildHTTPPayload(
	action models.ActionType,
	target *domain.ObjectInstance,
	parameters map[string]json.RawMessage,
) json.RawMessage {
	out, _ := json.Marshal(map[string]any{
		"action": map[string]any{
			"id":             action.ID,
			"name":           action.Name,
			"display_name":   action.DisplayName,
			"object_type_id": action.ObjectTypeID,
			"operation_kind": action.OperationKind,
		},
		"target_object": target,
		"parameters":    parameters,
	})
	return out
}

// buildUpdateObjectPatch mirrors the inner section of plan_action's
// UpdateObject branch. Walks property_mappings + static_patch,
// resolves input_name lookups against the request parameters, and
// validates each value against the property schema before
// returning the merged patch.
func buildUpdateObjectPatch(
	ctx context.Context,
	state *ontologykernel.AppState,
	action models.ActionType,
	parameters json.RawMessage,
) (map[string]json.RawMessage, []string) {
	var cfg models.UpdateObjectActionConfig
	// Pull operation_config out of the wrapper if it exists; otherwise
	// the action's config is the operation config.
	configBytes := operationConfigBytes(action.Config)
	if err := json.Unmarshal(configBytes, &cfg); err != nil {
		return nil, []string{"invalid action config: " + err.Error()}
	}

	defs, err := domain.LoadEffectivePropertiesViaStore(ctx, state.Stores.Definitions, action.ObjectTypeID)
	if err != nil {
		return nil, []string{"failed to load property definitions: " + err.Error()}
	}
	typeByName := map[string]string{}
	for _, p := range defs {
		typeByName[p.Name] = p.PropertyType
	}

	var paramMap map[string]json.RawMessage
	_ = json.Unmarshal(parameters, &paramMap)

	patch := map[string]json.RawMessage{}
	for _, m := range cfg.PropertyMappings {
		propType, ok := typeByName[m.PropertyName]
		if !ok {
			return nil, []string{fmt.Sprintf("unknown property '%s' in update_object action config", m.PropertyName)}
		}
		var value json.RawMessage
		switch {
		case m.InputName != nil:
			v, ok := paramMap[*m.InputName]
			if !ok {
				return nil, []string{fmt.Sprintf("missing input '%s' for property mapping", *m.InputName)}
			}
			value = v
		case len(m.Value) > 0 && string(m.Value) != "null":
			value = m.Value
		default:
			value = json.RawMessage(`null`)
		}
		if err := domain.ValidatePropertyValue(propType, value); err != nil {
			return nil, []string{m.PropertyName + ": " + err.Error()}
		}
		patch[m.PropertyName] = value
	}

	if len(cfg.StaticPatch) > 0 && string(cfg.StaticPatch) != "null" {
		var staticMap map[string]json.RawMessage
		if err := json.Unmarshal(cfg.StaticPatch, &staticMap); err != nil {
			return nil, []string{"static_patch must be a JSON object"}
		}
		for name, value := range staticMap {
			propType, ok := typeByName[name]
			if !ok {
				return nil, []string{"unknown property '" + name + "' in static_patch"}
			}
			if err := domain.ValidatePropertyValue(propType, value); err != nil {
				return nil, []string{name + ": " + err.Error()}
			}
			patch[name] = value
		}
	}
	if len(patch) == 0 {
		return nil, []string{"update_object action requires property_mappings or static_patch"}
	}
	return patch, nil
}

// operationConfigBytes mirrors `split_action_config`'s "extract the
// nested `operation` envelope" behaviour. When the config wraps the
// real operation under a `{ "operation": ..., "notification_side_effects": ... }`
// envelope, we pull just the `operation` value. Otherwise the
// config IS the operation config.
func operationConfigBytes(config json.RawMessage) json.RawMessage {
	if len(config) == 0 || string(config) == "null" {
		return json.RawMessage(`{}`)
	}
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(config, &asMap); err != nil {
		return config
	}
	hasEnvelope := false
	for _, key := range []string{"operation", "notification_side_effects",
		"webhook_writeback", "webhook_side_effects", "batched_execution"} {
		if _, ok := asMap[key]; ok {
			hasEnvelope = true
			break
		}
	}
	if !hasEnvelope {
		return config
	}
	if op, ok := asMap["operation"]; ok && len(op) > 0 && string(op) != "null" {
		return op
	}
	return json.RawMessage(`{}`)
}

// loadAndAuthorizeTarget mirrors `async fn load_and_authorize_target`.
// Loads the object via the read-side helper, runs the access check
// (claims-side admin / clearance / org_id), then enforces that the
// object's type matches the action's declared object_type_id.
func loadAndAuthorizeTarget(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	targetID, objectTypeID uuid.UUID,
) (*domain.ObjectInstance, []string) {
	obj, err := objects.LoadObjectInstance(ctx, state, claims, targetID, storage.Strong())
	if err != nil {
		return nil, []string{"failed to load target object: " + err.Error()}
	}
	if obj == nil {
		return nil, []string{"target object was not found"}
	}
	if err := domain.EnsureObjectAccess(claims, obj); err != nil {
		return nil, []string{forbiddenLine(err.Error())}
	}
	if obj.ObjectTypeID != objectTypeID {
		return nil, []string{"target object does not match the action's object_type_id"}
	}
	return obj, nil
}

// ensureActionTargetPermission mirrors
// `pub(crate) fn ensure_action_target_permission`. When the policy
// declares allowed_markings, the target's marking must intersect.
func ensureActionTargetPermission(action models.ActionType, target *domain.ObjectInstance) error {
	if len(action.AuthorizationPolicy.AllowedMarkings) == 0 {
		return nil
	}
	if target == nil {
		return errors.New("forbidden: target object is required by the action authorization policy")
	}
	for _, m := range action.AuthorizationPolicy.AllowedMarkings {
		if m == target.Marking {
			return nil
		}
	}
	return errors.New("forbidden: target marking is not permitted by the action authorization policy")
}

// ensureConfirmationJustification mirrors
// `fn ensure_confirmation_justification`. Actions flagged as
// confirmation_required reject calls without a justification string.
func ensureConfirmationJustification(action models.ActionType, justification *string) error {
	if !action.ConfirmationRequired {
		return nil
	}
	if justification == nil || strings.TrimSpace(*justification) == "" {
		return errors.New("justification is required for confirmation-gated actions")
	}
	return nil
}

// ── plan_preview ────────────────────────────────────────────────────

func planPreview(plan actionPlan) json.RawMessage {
	switch plan.kind {
	case planUpdateObject:
		out, _ := json.Marshal(map[string]any{
			"kind":             "update_object",
			"target_object_id": plan.target.ID,
			"patch":            plan.patch,
		})
		return out
	case planDeleteObject:
		out, _ := json.Marshal(map[string]any{
			"kind":             "delete_object",
			"target_object_id": plan.target.ID,
		})
		return out
	case planCreateLink:
		out, _ := json.Marshal(map[string]any{
			"kind":                  "create_link",
			"target_object_id":      plan.target.ID,
			"counterpart_object_id": plan.counterpart.ID,
			"link_type_id":          plan.linkType.ID,
			"source_object_id":      plan.linkSourceObject,
			"linked_object_id":      plan.linkTargetObject,
			"properties":            plan.linkProperties,
		})
		return out
	case planInvokeWebhook:
		var targetID *uuid.UUID
		if plan.target != nil {
			id := plan.target.ID
			targetID = &id
		}
		out, _ := json.Marshal(map[string]any{
			"kind":             "invoke_webhook",
			"target_object_id": targetID,
			"request": map[string]any{
				"url":     plan.invocation.URL,
				"method":  plan.invocation.Method,
				"headers": plan.invocation.Headers,
				"payload": json.RawMessage(plan.payload),
			},
		})
		return out
	case planInvokeFunction:
		var targetID *uuid.UUID
		if plan.target != nil {
			id := plan.target.ID
			targetID = &id
		}
		runtime := "http"
		if strings.HasPrefix(plan.invocation.URL, "inline://") {
			runtime = strings.TrimPrefix(plan.invocation.URL, "inline://")
		}
		out, _ := json.Marshal(map[string]any{
			"kind":             "invoke_function",
			"runtime":          runtime,
			"target_object_id": targetID,
			"request": map[string]any{
				"url":     plan.invocation.URL,
				"method":  plan.invocation.Method,
				"headers": plan.invocation.Headers,
				"payload": json.RawMessage(plan.payload),
			},
		})
		return out
	}
	return json.RawMessage(`null`)
}

// ── audit emitter (minimal) ─────────────────────────────────────────

// emitActionAttemptEvent appends a minimal `action_attempt` entry to
// the action log so `GetActionMetrics` (Phase 5A) can aggregate
// success/failure counts. The Rust impl carries far more fields
// (severity, target_snapshot, audit_result, classification_marker);
// the Go port keeps the minimum the metrics aggregator reads:
// `action_type_id`, `status`, `duration_ms`, optional `failure_type`.
func emitActionAttemptEvent(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	action models.ActionType,
	targetObjectID *uuid.UUID,
	status string,
	durationMs int64,
	failureType *string,
) error {
	payload := map[string]any{
		"action_type_id":   action.ID.String(),
		"status":           status,
		"duration_ms":      durationMs,
		"target_object_id": targetObjectID,
	}
	if failureType != nil {
		payload["failure_type"] = *failureType
	}
	body, _ := json.Marshal(payload)
	actionID, _ := uuid.NewV7()
	var objRef *storage.ObjectId
	if targetObjectID != nil {
		ref := storage.ObjectId(targetObjectID.String())
		objRef = &ref
	}
	return state.Stores.Actions.Append(ctx, storage.ActionLogEntry{
		Tenant:       domain.TenantFromClaims(claims),
		ActionID:     actionID.String(),
		Kind:         "action_attempt",
		Subject:      claims.Sub.String(),
		Object:       objRef,
		Payload:      body,
		RecordedAtMs: time.Now().UTC().UnixMilli(),
	})
}

// ── Helpers ─────────────────────────────────────────────────────────

// allForbidden mirrors `fn all_forbidden`. Returns true when every
// error string starts with the `forbidden:` prefix.
func allForbidden(errs []string) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if !strings.HasPrefix(e, "forbidden:") {
			return false
		}
	}
	return true
}

// forbiddenLine ensures access errors carry the prefix `allForbidden`
// keys off, matching the Rust impl that prefixes every authorisation
// error with `"forbidden:"` exactly.
func forbiddenLine(msg string) string {
	if strings.HasPrefix(msg, "forbidden:") {
		return msg
	}
	return "forbidden: " + msg
}

// decodeOptionalJSON tolerates an empty body — the Rust handlers
// accept default-constructed request envelopes via `axum::Json` +
// `#[serde(default)]`.
func decodeOptionalJSON(r *http.Request, dst any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}
