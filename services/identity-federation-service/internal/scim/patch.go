package scim

import (
	"encoding/json"
	"strings"
)

// ApplyUserPatch dispatches one PatchOp against `user`, mirroring
// fn apply_user_patch. Supported ops: add / replace (both follow
// the same code path — SCIM treats add ≡ replace for non-multi-
// valued fields). `remove` is rejected with scimType="mutability"
// since DELETE is the canonical deactivation surface.
//
// Returns (nil) on success or (*ScimError) on a domain failure
// the handler should surface as 400.
func ApplyUserPatch(user *UserRecord, op ScimPatchOperation) *ScimError {
	switch strings.ToLower(op.Op) {
	case "add", "replace":
		return applyUserReplace(user, op.Path, op.Value)
	case "remove":
		t := "mutability"
		err := NewScimError(400, "remove is unsupported for SCIM User; use DELETE to deactivate", &t)
		return &err
	default:
		t := "invalidSyntax"
		err := NewScimError(400, "unsupported PATCH op "+op.Op, &t)
		return &err
	}
}

// applyUserReplace mirrors fn apply_user_replace — pathless ops
// require an object body and recurse into apply_user_field for
// each key; path-bearing ops dispatch a single field.
func applyUserReplace(user *UserRecord, path *string, value json.RawMessage) *ScimError {
	if len(value) == 0 {
		return invalidValueErr("PATCH operation value is required")
	}
	if path != nil && *path != "" {
		return applyUserField(user, *path, value)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(value, &obj); err != nil {
		return invalidValueErr("pathless User PATCH requires an object value")
	}
	if len(obj) == 0 {
		return invalidValueErr("pathless User PATCH requires an object value")
	}
	for field, raw := range obj {
		if scimErr := applyUserField(user, field, raw); scimErr != nil {
			return scimErr
		}
	}
	return nil
}

// applyUserField mirrors fn apply_user_field — handles the small
// set of patchable User fields (userName, active, name, emails,
// externalId, the openfoundry extension URN, and
// `<URN>.<field>` paths).
func applyUserField(user *UserRecord, path string, value json.RawMessage) *ScimError {
	switch path {
	case "userName":
		var email string
		if err := json.Unmarshal(value, &email); err != nil {
			return invalidValueErr("userName must be a non-empty string")
		}
		trimmed := strings.TrimSpace(email)
		if trimmed == "" {
			return invalidValueErr("userName must be a non-empty string")
		}
		user.Email = trimmed
		return nil
	case "active":
		var active bool
		if err := json.Unmarshal(value, &active); err != nil {
			return invalidValueErr("active must be a boolean")
		}
		user.IsActive = active
		return nil
	case "name":
		var name ScimName
		if err := json.Unmarshal(value, &name); err != nil {
			return invalidValueErr("name must be a SCIM name object")
		}
		user.Name = DisplayNameFromScim(&name, user.Email)
		user.Attributes = SetScimName(user.Attributes, &name)
		return nil
	case "emails":
		var emails []ScimEmail
		if err := json.Unmarshal(value, &emails); err != nil {
			return invalidValueErr("emails must be a SCIM email array")
		}
		// Pick the primary (or first) value, trimmed-non-empty.
		var picked *string
		for i := range emails {
			if emails[i].Primary != nil && *emails[i].Primary {
				v := strings.TrimSpace(emails[i].Value)
				if v != "" {
					picked = &v
				}
				break
			}
		}
		if picked == nil && len(emails) > 0 {
			v := strings.TrimSpace(emails[0].Value)
			if v != "" {
				picked = &v
			}
		}
		if picked == nil {
			return invalidValueErr("emails must contain at least one value")
		}
		user.Email = *picked
		return nil
	case "externalId":
		var externalID string
		if err := json.Unmarshal(value, &externalID); err != nil {
			return invalidValueErr("externalId must be a string")
		}
		user.Attributes = SetScimExternalID(user.Attributes, externalID)
		return nil
	}

	// Openfoundry extension paths.
	if path == SchemaOpenfoundryUserExtension {
		// Validate the value is a JSON object.
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(value, &obj); err != nil {
			return invalidValueErr("OpenFoundry SCIM extension must be an object")
		}
		user.Attributes = SetScimOpenfoundryExtension(user.Attributes, value)
		return nil
	}
	if strings.HasPrefix(path, SchemaOpenfoundryUserExtension) {
		suffix := strings.TrimPrefix(path, SchemaOpenfoundryUserExtension)
		field := strings.TrimPrefix(suffix, ".")
		if field == "" || field == suffix {
			t := "mutability"
			err := NewScimError(400, "unsupported User PATCH path "+path, &t)
			return &err
		}
		user.Attributes = SetScimOpenfoundryField(user.Attributes, field, value)
		return nil
	}

	t := "mutability"
	err := NewScimError(400, "unsupported User PATCH path "+path, &t)
	return &err
}
