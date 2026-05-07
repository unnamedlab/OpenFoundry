package models

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Package-level note: this file ports
// `libs/ontology-kernel/src/models/submission_criteria.rs` (TASK C —
// Submission criteria AST). The Rust types use `#[serde(tag = "kind",
// rename_all = "snake_case")]` for `Operand` and `#[serde(tag =
// "type", rename_all = "snake_case")]` for `SubmissionNode`. Rust
// emits enum variants using the discriminant key + the variant
// fields side-by-side at the top level of the JSON object. Custom
// MarshalJSON/UnmarshalJSON below preserve byte-for-byte output.

// Operand mirrors `enum Operand` with `#[serde(tag = "kind",
// rename_all = "snake_case")]`.
type Operand struct {
	Kind     OperandKind     `json:"-"`
	Param    *OperandParam    `json:"-"`
	ParamProp *OperandParamProperty `json:"-"`
	User     *OperandCurrentUser `json:"-"`
	Static   *OperandStatic     `json:"-"`
}

type OperandKind string

const (
	OperandKindParam         OperandKind = "param"
	OperandKindParamProperty OperandKind = "param_property"
	OperandKindCurrentUser   OperandKind = "current_user"
	OperandKindStatic        OperandKind = "static"
)

// OperandParam: { "kind": "param", "name": ... }
type OperandParam struct {
	Name string `json:"name"`
}

// OperandParamProperty: { "kind": "param_property", "param": ...,
// "property": ... }
type OperandParamProperty struct {
	Param    string `json:"param"`
	Property string `json:"property"`
}

// OperandCurrentUser: { "kind": "current_user", "attribute": ... }
type OperandCurrentUser struct {
	Attribute UserAttr `json:"attribute"`
}

// OperandStatic: { "kind": "static", "value": ... }
type OperandStatic struct {
	Value json.RawMessage `json:"value"`
}

// MarshalJSON emits the operand with the `kind` discriminant inlined
// at the top level — same shape Rust serde produces.
func (o Operand) MarshalJSON() ([]byte, error) {
	switch o.Kind {
	case OperandKindParam:
		if o.Param == nil {
			return nil, errors.New("Operand.Param is nil for kind=param")
		}
		return json.Marshal(struct {
			Kind OperandKind `json:"kind"`
			*OperandParam
		}{Kind: o.Kind, OperandParam: o.Param})
	case OperandKindParamProperty:
		if o.ParamProp == nil {
			return nil, errors.New("Operand.ParamProp is nil for kind=param_property")
		}
		return json.Marshal(struct {
			Kind OperandKind `json:"kind"`
			*OperandParamProperty
		}{Kind: o.Kind, OperandParamProperty: o.ParamProp})
	case OperandKindCurrentUser:
		if o.User == nil {
			return nil, errors.New("Operand.User is nil for kind=current_user")
		}
		return json.Marshal(struct {
			Kind OperandKind `json:"kind"`
			*OperandCurrentUser
		}{Kind: o.Kind, OperandCurrentUser: o.User})
	case OperandKindStatic:
		if o.Static == nil {
			return nil, errors.New("Operand.Static is nil for kind=static")
		}
		return json.Marshal(struct {
			Kind  OperandKind     `json:"kind"`
			Value json.RawMessage `json:"value"`
		}{Kind: o.Kind, Value: o.Static.Value})
	default:
		return nil, fmt.Errorf("unknown operand kind %q", o.Kind)
	}
}

// UnmarshalJSON dispatches on the `kind` discriminant. Mirrors Rust
// serde with `tag = "kind"`.
func (o *Operand) UnmarshalJSON(b []byte) error {
	var head struct {
		Kind OperandKind `json:"kind"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return err
	}
	o.Kind = head.Kind
	switch head.Kind {
	case OperandKindParam:
		var v OperandParam
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		o.Param = &v
	case OperandKindParamProperty:
		var v OperandParamProperty
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		o.ParamProp = &v
	case OperandKindCurrentUser:
		var v OperandCurrentUser
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		o.User = &v
	case OperandKindStatic:
		var v OperandStatic
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		o.Static = &v
	default:
		return fmt.Errorf("unknown operand kind %q", head.Kind)
	}
	return nil
}

// UserAttr mirrors `enum UserAttr` (`rename_all = "snake_case"`).
type UserAttr string

const (
	UserAttrUserID         UserAttr = "user_id"
	UserAttrEmail          UserAttr = "email"
	UserAttrOrganizationID UserAttr = "organization_id"
	UserAttrRoles          UserAttr = "roles"
	UserAttrPermissions    UserAttr = "permissions"
	UserAttrAuthMethods    UserAttr = "auth_methods"
)

// Operator mirrors `enum Operator` (`rename_all = "snake_case"`). 14
// variants — keep this list verbatim to match the Rust definition.
type Operator string

const (
	OperatorIs           Operator = "is"
	OperatorIsNot        Operator = "is_not"
	OperatorMatches      Operator = "matches"
	OperatorLt           Operator = "lt"
	OperatorLte          Operator = "lte"
	OperatorGt           Operator = "gt"
	OperatorGte          Operator = "gte"
	OperatorIncludes     Operator = "includes"
	OperatorIncludesAny  Operator = "includes_any"
	OperatorIsIncludedIn Operator = "is_included_in"
	OperatorEachIs       Operator = "each_is"
	OperatorEachIsNot    Operator = "each_is_not"
	OperatorIsEmpty      Operator = "is_empty"
	OperatorIsNotEmpty   Operator = "is_not_empty"
)

// SubmissionNodeType mirrors `enum SubmissionNode` discriminant `type`.
type SubmissionNodeType string

const (
	SubmissionNodeTypeLeaf SubmissionNodeType = "leaf"
	SubmissionNodeTypeAll  SubmissionNodeType = "all"
	SubmissionNodeTypeAny  SubmissionNodeType = "any"
	SubmissionNodeTypeNot  SubmissionNodeType = "not"
)

// SubmissionNode mirrors `enum SubmissionNode` with `#[serde(tag =
// "type", rename_all = "snake_case")]`. The four variants and their
// fields are flattened with `type` at the top level, matching Rust
// serde output.
//
// All four variants carry an optional `failure_message` with
// `#[serde(default, skip_serializing_if = "Option::is_none")]`.
type SubmissionNode struct {
	Type           SubmissionNodeType `json:"-"`
	Left           *Operand           `json:"-"`
	Op             Operator           `json:"-"`
	Right          *Operand           `json:"-"`
	Children       []SubmissionNode   `json:"-"`
	Child          *SubmissionNode    `json:"-"`
	FailureMessage *string            `json:"-"`
}

// NewLeaf mirrors `fn leaf(left, op, right)`.
func NewLeaf(left Operand, op Operator, right Operand) SubmissionNode {
	return SubmissionNode{
		Type:  SubmissionNodeTypeLeaf,
		Left:  &left,
		Op:    op,
		Right: &right,
	}
}

// FailureMessageStr mirrors `fn failure_message(&self) -> Option<&str>`.
func (n SubmissionNode) FailureMessageStr() *string { return n.FailureMessage }

// MarshalJSON emits each variant in the Rust serde shape.
func (n SubmissionNode) MarshalJSON() ([]byte, error) {
	switch n.Type {
	case SubmissionNodeTypeLeaf:
		return json.Marshal(struct {
			Type           SubmissionNodeType `json:"type"`
			Left           Operand            `json:"left"`
			Op             Operator           `json:"op"`
			Right          Operand            `json:"right"`
			FailureMessage *string            `json:"failure_message,omitempty"`
		}{
			Type:           n.Type,
			Left:           *n.Left,
			Op:             n.Op,
			Right:          *n.Right,
			FailureMessage: n.FailureMessage,
		})
	case SubmissionNodeTypeAll, SubmissionNodeTypeAny:
		return json.Marshal(struct {
			Type           SubmissionNodeType `json:"type"`
			Children       []SubmissionNode   `json:"children"`
			FailureMessage *string            `json:"failure_message,omitempty"`
		}{
			Type:           n.Type,
			Children:       n.Children,
			FailureMessage: n.FailureMessage,
		})
	case SubmissionNodeTypeNot:
		return json.Marshal(struct {
			Type           SubmissionNodeType `json:"type"`
			Child          *SubmissionNode    `json:"child"`
			FailureMessage *string            `json:"failure_message,omitempty"`
		}{
			Type:           n.Type,
			Child:          n.Child,
			FailureMessage: n.FailureMessage,
		})
	default:
		return nil, fmt.Errorf("unknown submission node type %q", n.Type)
	}
}

// UnmarshalJSON dispatches on the `type` discriminant.
func (n *SubmissionNode) UnmarshalJSON(b []byte) error {
	var head struct {
		Type SubmissionNodeType `json:"type"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return err
	}
	n.Type = head.Type
	switch head.Type {
	case SubmissionNodeTypeLeaf:
		var v struct {
			Left           Operand  `json:"left"`
			Op             Operator `json:"op"`
			Right          Operand  `json:"right"`
			FailureMessage *string  `json:"failure_message"`
		}
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		n.Left = &v.Left
		n.Op = v.Op
		n.Right = &v.Right
		n.FailureMessage = v.FailureMessage
	case SubmissionNodeTypeAll, SubmissionNodeTypeAny:
		var v struct {
			Children       []SubmissionNode `json:"children"`
			FailureMessage *string          `json:"failure_message"`
		}
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		n.Children = v.Children
		n.FailureMessage = v.FailureMessage
	case SubmissionNodeTypeNot:
		var v struct {
			Child          *SubmissionNode `json:"child"`
			FailureMessage *string         `json:"failure_message"`
		}
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		n.Child = v.Child
		n.FailureMessage = v.FailureMessage
	default:
		return fmt.Errorf("unknown submission node type %q", head.Type)
	}
	return nil
}
