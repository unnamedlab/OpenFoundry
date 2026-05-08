package scim

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

// ApplyGroupPatch dispatches one PatchOp against `group` +
// `store` (the membership writes go through the store). Mirrors
// fn apply_group_patch verbatim — supports add/replace/remove
// over displayName / externalId / members.
//
// Handler-level responsibilities (caller's job): wrap the entire
// patch sequence in a transaction (Postgres impl), persist the
// mutated record at the end, reload + serialise.
//
// The in-memory store doesn't expose a Begin handle so each call
// is its own atomic mutation; the Postgres impl will need to
// thread tx through (lands with 3.7b.3.4).
func ApplyGroupPatch(ctx context.Context, store GroupStore, group *GroupRecord, op ScimPatchOperation) *ScimError {
	switch strings.ToLower(op.Op) {
	case "add":
		return applyGroupAdd(ctx, store, group, op)
	case "replace":
		return applyGroupReplace(ctx, store, group, op)
	case "remove":
		return applyGroupRemove(ctx, store, group, op)
	}
	t := "invalidSyntax"
	err := NewScimError(400, "unsupported PATCH op "+op.Op, &t)
	return &err
}

// applyGroupAdd handles `op: add` — only the `members` path is
// supported (matches Rust verbatim).
func applyGroupAdd(ctx context.Context, store GroupStore, group *GroupRecord, op ScimPatchOperation) *ScimError {
	if op.Path == nil || *op.Path != "members" {
		return scimErr(400, "only members add is supported for Group", "mutability")
	}
	members, scimErr := MembersFromValue(op.Value)
	if scimErr != nil {
		return scimErr
	}
	ids, scimErr := memberValuesToUUIDs(members)
	if scimErr != nil {
		return scimErr
	}
	if err := store.AddMembers(ctx, group.ID, ids); err != nil {
		if IsMemberNotFound(err) {
			return invalidValueErr("group member does not reference an existing user")
		}
		return scimErr500("failed to patch group")
	}
	return nil
}

// applyGroupReplace handles `op: replace` for both path-bearing
// (displayName / externalId / members) and pathless (whole-group
// object) shapes.
func applyGroupReplace(ctx context.Context, store GroupStore, group *GroupRecord, op ScimPatchOperation) *ScimError {
	if op.Path != nil {
		switch *op.Path {
		case "displayName":
			var name string
			if err := json.Unmarshal(op.Value, &name); err != nil {
				return invalidValueErr("displayName must be a string")
			}
			group.Name = strings.TrimSpace(name)
			return nil
		case "externalId":
			var ext string
			if err := json.Unmarshal(op.Value, &ext); err != nil {
				return invalidValueErr("externalId must be a string")
			}
			trimmed := strings.TrimSpace(ext)
			if trimmed == "" {
				group.ScimExternalID = nil
			} else {
				group.ScimExternalID = &trimmed
			}
			return nil
		case "members":
			members, scimErr := MembersFromValue(op.Value)
			if scimErr != nil {
				return scimErr
			}
			ids, scimErr := memberValuesToUUIDs(members)
			if scimErr != nil {
				return scimErr
			}
			if err := store.ReplaceMembers(ctx, group.ID, ids); err != nil {
				if IsMemberNotFound(err) {
					return invalidValueErr("group member does not reference an existing user")
				}
				return scimErr500("failed to replace group members")
			}
			return nil
		}
		return scimErrMutability("unsupported Group PATCH path " + *op.Path)
	}

	// Pathless: object-bodied replace covering displayName /
	// externalId / members.
	if len(op.Value) == 0 {
		return invalidValueErr("pathless Group PATCH requires an object value")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(op.Value, &obj); err != nil {
		return invalidValueErr("pathless Group PATCH requires an object value")
	}
	if len(obj) == 0 {
		return invalidValueErr("pathless Group PATCH requires an object value")
	}
	if raw, ok := obj["displayName"]; ok {
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			return invalidValueErr("displayName must be a string")
		}
		group.Name = strings.TrimSpace(name)
	}
	if raw, ok := obj["externalId"]; ok {
		var ext string
		if err := json.Unmarshal(raw, &ext); err != nil {
			return invalidValueErr("externalId must be a string")
		}
		trimmed := strings.TrimSpace(ext)
		if trimmed == "" {
			group.ScimExternalID = nil
		} else {
			group.ScimExternalID = &trimmed
		}
	}
	if raw, ok := obj["members"]; ok {
		members, scimErr := MembersFromValue(raw)
		if scimErr != nil {
			return scimErr
		}
		ids, scimErr := memberValuesToUUIDs(members)
		if scimErr != nil {
			return scimErr
		}
		if err := store.ReplaceMembers(ctx, group.ID, ids); err != nil {
			if IsMemberNotFound(err) {
				return invalidValueErr("group member does not reference an existing user")
			}
			return scimErr500("failed to replace group members")
		}
	}
	return nil
}

// applyGroupRemove handles `op: remove`. Supported paths:
//   - "members" (or no path): clear all memberships.
//   - `members[value eq "<uuid>"]`: drop a single user.
// Anything else surfaces 400 invalidPath.
func applyGroupRemove(ctx context.Context, store GroupStore, group *GroupRecord, op ScimPatchOperation) *ScimError {
	if op.Path == nil || *op.Path == "" || *op.Path == "members" {
		if err := store.RemoveAllMembers(ctx, group.ID); err != nil {
			return scimErr500("failed to patch group")
		}
		return nil
	}
	userID, ok := ParseMemberFilterPath(*op.Path)
	if !ok {
		return scimErrInvalidPath("unsupported members remove path")
	}
	if err := store.RemoveMember(ctx, group.ID, userID); err != nil {
		return scimErr500("failed to patch group")
	}
	return nil
}

// ─── Tiny error constructors ────────────────────────────────────────

// scimErr is the generic factory for ScimError pointers used by
// the patch path. Most call sites use the more specific helpers
// below.
func scimErr(status int, detail, scimType string) *ScimError {
	t := scimType
	err := NewScimError(status, detail, &t)
	return &err
}

func scimErrMutability(detail string) *ScimError {
	return scimErr(400, detail, "mutability")
}

func scimErrInvalidPath(detail string) *ScimError {
	return scimErr(400, detail, "invalidPath")
}

func scimErr500(detail string) *ScimError {
	err := NewScimError(500, detail, nil)
	return &err
}

// silence unused-imports when patch.go is not the sole caller
// of uuid in this file.
var _ uuid.UUID
