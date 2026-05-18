package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	cedarauthz "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/domain"
)

const (
	cipherOpEncrypt = "encrypt"
	cipherOpDecrypt = "decrypt"
	cipherOpManage  = "manage"
)

func authorizeKeyOperation(ctx context.Context, claims *authmw.Claims, key *domain.CipherKey, op string) error {
	if claims == nil || key == nil {
		return domain.ErrAccessDenied
	}
	policy := operationPolicy(key.AccessPolicy, op)
	if isEmptyOperationPolicy(policy) {
		return domain.ErrAccessDenied
	}
	records := make([]cedarauthz.PolicyRecord, 0, 1)
	if src := cedarPolicyForOperation(key.ID, op, policy); src != "" {
		records = append(records, cedarauthz.PolicyRecord{ID: "cipher-key-" + key.ID.String() + "-" + op, Source: src})
	}
	store, err := cedarauthz.NewWithPolicies(records)
	if err != nil {
		return fmt.Errorf("cipher: build cedar policy: %w", err)
	}
	engine := cedarauthz.NewEngineNoopAudit(store)
	principal := types.NewEntityUID("User", types.String(claims.Sub.String()))
	resource := types.NewEntityUID("CipherKey", types.String(key.ID.String()))
	action := types.NewEntityUID("Action", types.String("cipher::"+op))
	out, err := engine.Authorize(ctx, principal, action, resource, cedar.NewRecord(cedar.RecordMap{}), cedarEntities(claims, key))
	if err != nil {
		return err
	}
	if !out.IsAllow() {
		return domain.ErrAccessDenied
	}
	return nil
}

func operationPolicy(policy domain.AccessPolicy, op string) domain.OperationPolicy {
	switch op {
	case cipherOpEncrypt:
		return policy.Encrypt
	case cipherOpDecrypt:
		return policy.Decrypt
	case cipherOpManage:
		return policy.Manage
	default:
		return domain.OperationPolicy{}
	}
}

func isEmptyOperationPolicy(policy domain.OperationPolicy) bool {
	return len(policy.Roles) == 0 && len(policy.Groups) == 0 && len(policy.Projects) == 0
}

func cedarPolicyForOperation(keyID uuid.UUID, op string, policy domain.OperationPolicy) string {
	conditions := make([]string, 0)
	for _, role := range cleanStrings(policy.Roles) {
		conditions = append(conditions, `principal.roles.contains("`+escapeCedarString(role)+`")`)
	}
	for _, group := range cleanStrings(policy.Groups) {
		conditions = append(conditions, `principal in Group::"`+escapeCedarString(group)+`"`)
	}
	for _, project := range cleanStrings(policy.Projects) {
		conditions = append(conditions, `principal has project_scope_rids && principal.project_scope_rids.contains("`+escapeCedarString(project)+`")`)
	}
	if len(conditions) == 0 {
		return ""
	}
	return `permit(
  principal,
  action == Action::"cipher::` + op + `",
  resource == CipherKey::"` + keyID.String() + `"
) when {
  ` + strings.Join(conditions, " ||\n  ") + `
};`
}

func cedarEntities(claims *authmw.Claims, key *domain.CipherKey) cedar.EntityMap {
	entities := cedar.EntityMap{}
	principal := types.NewEntityUID("User", types.String(claims.Sub.String()))
	roleValues := make([]cedar.Value, 0, len(claims.Roles))
	for _, role := range cleanStrings(claims.Roles) {
		roleValues = append(roleValues, cedar.String(role))
	}
	clearanceIDs := markingUIDs(claims.AllowedMarkings())
	clearanceValues := make([]cedar.Value, 0, len(clearanceIDs))
	for _, id := range clearanceIDs {
		clearanceValues = append(clearanceValues, id)
	}
	projectValues := make([]cedar.Value, 0)
	for _, project := range claimStringList(claims, "project_scope_rids", "project_rids", "projects") {
		projectValues = append(projectValues, cedar.String(project))
	}
	attrs := cedar.RecordMap{
		"tenant":     cedar.String(tenantString(claims)),
		"clearances": cedar.NewSet(clearanceValues...),
		"roles":      cedar.NewSet(roleValues...),
	}
	if len(projectValues) > 0 {
		attrs["project_scope_rids"] = cedar.NewSet(projectValues...)
	}
	entities[principal] = cedar.Entity{UID: principal, Attributes: cedar.NewRecord(attrs), Parents: types.NewEntityUIDSet(groupUIDs(claims)...)}
	for _, group := range groupUIDs(claims) {
		entities[group] = cedar.Entity{UID: group, Attributes: cedar.NewRecord(cedar.RecordMap{"id": cedar.String(string(group.ID))})}
	}
	for _, marking := range clearanceIDs {
		entities[marking] = cedar.Entity{UID: marking, Attributes: cedar.NewRecord(cedar.RecordMap{"name": cedar.String(string(marking.ID))})}
	}
	resource := types.NewEntityUID("CipherKey", types.String(key.ID.String()))
	resourceMarkings := markingUIDs(key.Markings)
	markingValues := make([]cedar.Value, 0, len(resourceMarkings))
	for _, marking := range resourceMarkings {
		markingValues = append(markingValues, marking)
		entities[marking] = cedar.Entity{UID: marking, Attributes: cedar.NewRecord(cedar.RecordMap{"name": cedar.String(string(marking.ID))})}
	}
	entities[resource] = cedar.Entity{UID: resource, Attributes: cedar.NewRecord(cedar.RecordMap{
		"rid":      cedar.String("ri.cipher.main.key." + key.ID.String()),
		"tenant":   cedar.String(key.TenantID.String()),
		"markings": cedar.NewSet(markingValues...),
	})}
	return entities
}

func tenantString(claims *authmw.Claims) string {
	if claims != nil && claims.OrgID != nil {
		return claims.OrgID.String()
	}
	return ""
}

func groupUIDs(claims *authmw.Claims) []cedar.EntityUID {
	groups := claimStringList(claims, "groups", "group_ids")
	out := make([]cedar.EntityUID, 0, len(groups))
	for _, group := range groups {
		out = append(out, types.NewEntityUID("Group", types.String(group)))
	}
	return out
}

func markingUIDs(markings []string) []cedar.EntityUID {
	cleaned := cleanStrings(markings)
	out := make([]cedar.EntityUID, 0, len(cleaned))
	for _, marking := range cleaned {
		out = append(out, types.NewEntityUID("Marking", types.String(strings.ToLower(marking))))
	}
	return out
}

func claimStringList(claims *authmw.Claims, keys ...string) []string {
	if claims == nil || len(claims.Attributes) == 0 {
		return nil
	}
	var attrs map[string]any
	if err := json.Unmarshal(claims.Attributes, &attrs); err != nil {
		return nil
	}
	out := make([]string, 0)
	for _, key := range keys {
		value, ok := attrs[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				if s, ok := item.(string); ok {
					out = append(out, s)
				}
			}
		case []string:
			out = append(out, typed...)
		case string:
			out = append(out, typed)
		}
	}
	return cleanStrings(out)
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func escapeCedarString(value string) string {
	return strings.Trim(strconv.Quote(value), "\"")
}
