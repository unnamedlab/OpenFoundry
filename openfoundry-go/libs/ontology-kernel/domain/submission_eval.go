// TASK C — Submission criteria evaluator.
//
// Pure-Go, side-effect-free evaluation of the AST defined in
// [models.SubmissionNode]. Called by `handlers/actions/{validate,
// execute}` after `EnsureActionActorPermission` and before
// `PlanAction` builds the mutation plan.
//
// Failure-message semantics (per
// `docs_original_palantir_foundry/foundry-docs/Ontology building/
// Define Ontologies/Action types/Submission criteria.md`):
//
//   - When a node fails AND owns a `failure_message`, that message
//     is surfaced and the messages of failing descendants are
//     suppressed.
//   - When a failing node has no message, the messages of its
//     failing children are surfaced instead.
//   - Leaves with no message synthesize a deterministic
//     "criterion failed" string so the user-facing list is never
//     empty.
//
// Mirrors `libs/ontology-kernel/src/domain/submission_eval.rs`.

package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// EvaluationContext mirrors `struct EvaluationContext<'a>`. Built
// once per request from the authored action, the materialized
// parameters and the calling JWT.
type EvaluationContext struct {
	Parameters map[string]json.RawMessage
	Claims     *authmw.Claims
}

// EvaluateSubmission mirrors `pub fn evaluate`. Returns nil on
// success; on failure returns the deduplicated user-facing failure
// messages (original order preserved). The synthetic
// "submission criteria failed" string lands when the failing
// subtree had no message of its own.
func EvaluateSubmission(node models.SubmissionNode, ctx *EvaluationContext) []string {
	var out []string
	if !evaluateNode(node, ctx, &out) {
		if len(out) == 0 {
			out = []string{"submission criteria failed"}
		}
		return dedupeStrings(out)
	}
	return nil
}

// evaluateNode mirrors `fn evaluate_node`. Returns true on success;
// appends failure messages produced by THIS subtree to `out`.
// Callers decide whether to keep them when a parent owns its own
// message.
func evaluateNode(node models.SubmissionNode, ctx *EvaluationContext, out *[]string) bool {
	switch node.Type {
	case models.SubmissionNodeTypeLeaf:
		lhs := resolveOperand(node.Left, ctx)
		rhs := resolveOperand(node.Right, ctx)
		ok := applyOperator(node.Op, lhs, rhs)
		if !ok {
			msg := ""
			if node.FailureMessage != nil {
				msg = *node.FailureMessage
			} else {
				msg = synthesizeLeafMessage(node.Left, node.Op, node.Right)
			}
			*out = append(*out, msg)
		}
		return ok

	case models.SubmissionNodeTypeAll:
		var childMsgs []string
		allOK := true
		for _, child := range node.Children {
			if !evaluateNode(child, ctx, &childMsgs) {
				allOK = false
			}
		}
		if allOK {
			return true
		}
		pushWithOverride(node.FailureMessage, childMsgs, out)
		return false

	case models.SubmissionNodeTypeAny:
		// Empty `any` is conventionally true (no constraint).
		if len(node.Children) == 0 {
			return true
		}
		var childMsgs []string
		for _, child := range node.Children {
			var local []string
			if evaluateNode(child, ctx, &local) {
				return true
			}
			childMsgs = append(childMsgs, local...)
		}
		pushWithOverride(node.FailureMessage, childMsgs, out)
		return false

	case models.SubmissionNodeTypeNot:
		var sink []string
		inner := false
		if node.Child != nil {
			inner = evaluateNode(*node.Child, ctx, &sink)
		}
		// NOT inverts truthiness; we discard the inner's failure
		// messages because the inner *succeeded* when we fail and
		// vice versa.
		if !inner {
			return true
		}
		msg := "negated criterion was satisfied"
		if node.FailureMessage != nil {
			msg = *node.FailureMessage
		}
		*out = append(*out, msg)
		return false
	}
	return true
}

func pushWithOverride(failureMessage *string, childMsgs []string, out *[]string) {
	if failureMessage != nil {
		*out = append(*out, *failureMessage)
		return
	}
	*out = append(*out, childMsgs...)
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, msg := range in {
		if seen[msg] {
			continue
		}
		seen[msg] = true
		out = append(out, msg)
	}
	return out
}

func synthesizeLeafMessage(left *models.Operand, op models.Operator, right *models.Operand) string {
	return fmt.Sprintf("criterion failed: %s %s %s",
		operandLabel(left), operatorLabel(op), operandLabel(right))
}

func operandLabel(op *models.Operand) string {
	if op == nil {
		return "<nil>"
	}
	switch op.Kind {
	case models.OperandKindParam:
		if op.Param != nil {
			return fmt.Sprintf("parameter '%s'", op.Param.Name)
		}
		return "parameter '?'"
	case models.OperandKindParamProperty:
		if op.ParamProp != nil {
			return fmt.Sprintf("parameter '%s.%s'", op.ParamProp.Param, op.ParamProp.Property)
		}
		return "parameter '?.?'"
	case models.OperandKindCurrentUser:
		if op.User != nil {
			return fmt.Sprintf("current user.%s", debugUserAttr(op.User.Attribute))
		}
		return "current user.?"
	case models.OperandKindStatic:
		if op.Static != nil {
			return string(op.Static.Value)
		}
		return "<no value>"
	}
	return "<unknown>"
}

// debugUserAttr mirrors Rust `format!("{:?}", attribute)` — Debug
// for an `enum UserAttr` variant is the variant name (PascalCase).
func debugUserAttr(a models.UserAttr) string {
	switch a {
	case models.UserAttrUserID:
		return "UserId"
	case models.UserAttrEmail:
		return "Email"
	case models.UserAttrOrganizationID:
		return "OrganizationId"
	case models.UserAttrRoles:
		return "Roles"
	case models.UserAttrPermissions:
		return "Permissions"
	case models.UserAttrAuthMethods:
		return "AuthMethods"
	}
	return string(a)
}

func operatorLabel(op models.Operator) string {
	switch op {
	case models.OperatorIs:
		return "is"
	case models.OperatorIsNot:
		return "is not"
	case models.OperatorMatches:
		return "matches"
	case models.OperatorLt:
		return "<"
	case models.OperatorLte:
		return "<="
	case models.OperatorGt:
		return ">"
	case models.OperatorGte:
		return ">="
	case models.OperatorIncludes:
		return "includes"
	case models.OperatorIncludesAny:
		return "includes any of"
	case models.OperatorIsIncludedIn:
		return "is included in"
	case models.OperatorEachIs:
		return "each is"
	case models.OperatorEachIsNot:
		return "each is not"
	case models.OperatorIsEmpty:
		return "is empty"
	case models.OperatorIsNotEmpty:
		return "is not empty"
	}
	return string(op)
}

// ---- Operand resolution ---------------------------------------------------

// resolveOperand mirrors `fn resolve`. Returns nil when the operand
// is missing (Rust `Option::None`).
func resolveOperand(op *models.Operand, ctx *EvaluationContext) json.RawMessage {
	if op == nil {
		return nil
	}
	switch op.Kind {
	case models.OperandKindParam:
		if op.Param == nil {
			return nil
		}
		v, ok := ctx.Parameters[op.Param.Name]
		if !ok {
			return nil
		}
		return v
	case models.OperandKindParamProperty:
		if op.ParamProp == nil {
			return nil
		}
		raw, ok := ctx.Parameters[op.ParamProp.Param]
		if !ok {
			return nil
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil
		}
		v, ok := obj[op.ParamProp.Property]
		if !ok {
			return nil
		}
		return v
	case models.OperandKindCurrentUser:
		if op.User == nil {
			return nil
		}
		return resolveUserAttr(op.User.Attribute, ctx.Claims)
	case models.OperandKindStatic:
		if op.Static == nil {
			return nil
		}
		return op.Static.Value
	}
	return nil
}

// resolveUserAttr mirrors `fn resolve_user_attr`. The Rust source
// emits `Value::Null` for absent org_id; we encode that as the
// JSON `null` raw bytes so downstream `is_empty` / `Is` operators
// see the same shape.
func resolveUserAttr(attr models.UserAttr, claims *authmw.Claims) json.RawMessage {
	if claims == nil {
		return json.RawMessage("null")
	}
	switch attr {
	case models.UserAttrUserID:
		return mustMarshal(claims.Sub.String())
	case models.UserAttrEmail:
		return mustMarshal(claims.Email)
	case models.UserAttrOrganizationID:
		if claims.OrgID == nil {
			return json.RawMessage("null")
		}
		return mustMarshal(claims.OrgID.String())
	case models.UserAttrRoles:
		return mustMarshal(claims.Roles)
	case models.UserAttrPermissions:
		return mustMarshal(claims.Permissions)
	case models.UserAttrAuthMethods:
		return mustMarshal(claims.AuthMethods)
	}
	return json.RawMessage("null")
}

func mustMarshal(value any) json.RawMessage {
	b, _ := json.Marshal(value)
	return b
}

// ---- Operator application -------------------------------------------------

func applyOperator(op models.Operator, lhs, rhs json.RawMessage) bool {
	switch op {
	case models.OperatorIsEmpty:
		return jsonIsEmpty(lhs)
	case models.OperatorIsNotEmpty:
		return !jsonIsEmpty(lhs)
	case models.OperatorIs:
		return jsonEqual(lhs, rhs)
	case models.OperatorIsNot:
		return !jsonEqual(lhs, rhs)
	case models.OperatorMatches:
		ls, lok := jsonString(lhs)
		rs, rok := jsonString(rhs)
		if !lok || !rok {
			return false
		}
		re, err := regexp.Compile(rs)
		if err != nil {
			return false
		}
		return re.MatchString(ls)
	case models.OperatorLt:
		c, ok := compareJSON(lhs, rhs)
		return ok && c < 0
	case models.OperatorLte:
		c, ok := compareJSON(lhs, rhs)
		return ok && c <= 0
	case models.OperatorGt:
		c, ok := compareJSON(lhs, rhs)
		return ok && c > 0
	case models.OperatorGte:
		c, ok := compareJSON(lhs, rhs)
		return ok && c >= 0
	case models.OperatorIncludes:
		// Array contains element OR string contains substring.
		if items, ok := jsonArray(lhs); ok && rhs != nil {
			return jsonArrayContains(items, rhs)
		}
		ls, lok := jsonString(lhs)
		rs, rok := jsonString(rhs)
		if lok && rok {
			return bytesContainsString(ls, rs)
		}
		return false
	case models.OperatorIncludesAny:
		items, lok := jsonArray(lhs)
		needles, rok := jsonArray(rhs)
		if !lok || !rok {
			return false
		}
		for _, n := range needles {
			if jsonArrayContains(items, n) {
				return true
			}
		}
		return false
	case models.OperatorIsIncludedIn:
		items, ok := jsonArray(rhs)
		if !ok || lhs == nil {
			return false
		}
		return jsonArrayContains(items, lhs)
	case models.OperatorEachIs:
		items, ok := jsonArray(lhs)
		if !ok || rhs == nil {
			return false
		}
		for _, it := range items {
			if !jsonEqual(it, rhs) {
				return false
			}
		}
		return true
	case models.OperatorEachIsNot:
		items, ok := jsonArray(lhs)
		if !ok || rhs == nil {
			return false
		}
		for _, it := range items {
			if jsonEqual(it, rhs) {
				return false
			}
		}
		return true
	}
	return false
}

// jsonIsEmpty mirrors `fn is_empty`. Treats nil / null / "" / [] /
// {} as empty; everything else as non-empty.
func jsonIsEmpty(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	trimmed := bytes.TrimSpace(raw)
	if string(trimmed) == "null" {
		return true
	}
	if s, ok := jsonString(raw); ok {
		return s == ""
	}
	if items, ok := jsonArray(raw); ok {
		return len(items) == 0
	}
	if obj, ok := jsonObject(raw); ok {
		return len(obj) == 0
	}
	return false
}

// jsonEqual mirrors `fn json_eq`. Both nil → equal. Otherwise the
// raw payloads are compared as decoded values so whitespace
// differences don't matter.
func jsonEqual(a, b json.RawMessage) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
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
	return bytes.Equal(ab, bb)
}

// compareJSON mirrors `fn compare`. Returns (cmp, true) when both
// sides are numbers or both are strings; (_, false) otherwise.
func compareJSON(a, b json.RawMessage) (int, bool) {
	if a == nil || b == nil {
		return 0, false
	}
	if af, aok := jsonNumber(a); aok {
		bf, bok := jsonNumber(b)
		if !bok {
			return 0, false
		}
		switch {
		case af < bf:
			return -1, true
		case af > bf:
			return 1, true
		default:
			return 0, true
		}
	}
	if as, aok := jsonString(a); aok {
		bs, bok := jsonString(b)
		if !bok {
			return 0, false
		}
		switch {
		case as < bs:
			return -1, true
		case as > bs:
			return 1, true
		default:
			return 0, true
		}
	}
	return 0, false
}

func jsonString(raw json.RawMessage) (string, bool) {
	if raw == nil {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func jsonNumber(raw json.RawMessage) (float64, bool) {
	if raw == nil {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0, false
	}
	return f, true
}

func jsonArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	if raw == nil {
		return nil, false
	}
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		return nil, false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, false
	}
	return items, true
}

func jsonObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if raw == nil {
		return nil, false
	}
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return nil, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, false
	}
	return obj, true
}

func jsonArrayContains(items []json.RawMessage, needle json.RawMessage) bool {
	for _, it := range items {
		if jsonEqual(it, needle) {
			return true
		}
	}
	return false
}

func bytesContainsString(haystack, needle string) bool {
	return bytes.Contains([]byte(haystack), []byte(needle))
}

// numberDebug renders a number raw as the Rust `Display` for f64
// would (used only by the synthesize path so call paths that don't
// hit the synth message don't pay any cost).
func numberDebug(raw json.RawMessage) string {
	f, ok := jsonNumber(raw)
	if !ok {
		return string(raw)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// (numberDebug is kept private — it's not used by the evaluator
// today but its companion `compareJSON` was the simplest place to
// share the underlying jsonNumber helper.)
var _ = numberDebug
