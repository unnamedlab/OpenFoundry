package scim

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

// GroupToScim converts a GroupRecord + the freshly-loaded
// member list into the SCIM 2.0 Group wire shape. Mirrors fn
// group_to_scim verbatim — type="User", ref=/Users/{id},
// display=name|email-fallback per member.
func GroupToScim(record GroupRecord, members []MemberView, baseURL string) ScimGroup {
	idStr := record.ID.String()
	out := ScimGroup{
		Schemas:     []string{SchemaGroup},
		ID:          &idStr,
		DisplayName: record.Name,
		Members:     toScimMembers(members, baseURL),
		ExternalID:  record.ScimExternalID,
		Meta: &ScimMeta{
			ResourceType: "Group",
			Location:     GroupLocation(record.ID, baseURL),
		},
	}
	return out
}

// toScimMembers materialises a slice of MemberView as SCIM
// member objects with type="User", ref=/Users/{id}, display=name
// (falling back to email when the user has no name set — matches
// the Rust `if name.is_empty() { email } else { name }`).
func toScimMembers(members []MemberView, baseURL string) []ScimGroupMember {
	if len(members) == 0 {
		return nil
	}
	out := make([]ScimGroupMember, 0, len(members))
	for _, m := range members {
		ref := UserLocation(m.UserID, baseURL)
		typ := "User"
		display := m.Name
		if display == "" {
			display = m.Email
		}
		out = append(out, ScimGroupMember{
			Value:   m.UserID.String(),
			Ref:     &ref,
			Type:    &typ,
			Display: &display,
		})
	}
	return out
}

// MembersFromValue mirrors fn members_from_value — accepts a
// PATCH operation value that's either a single ScimGroupMember
// object or an array of them. Returns a typed ScimError on
// invalid shapes.
func MembersFromValue(raw json.RawMessage) ([]ScimGroupMember, *ScimError) {
	if len(raw) == 0 {
		return nil, invalidValueErr("members value is required")
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, invalidValueErr("members value is required")
	}
	if strings.HasPrefix(trimmed, "[") {
		var arr []ScimGroupMember
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, invalidValueErr("members must be SCIM member objects")
		}
		return arr, nil
	}
	// Single-object form: wrap in a slice.
	var single ScimGroupMember
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, invalidValueErr("members must be SCIM member objects")
	}
	return []ScimGroupMember{single}, nil
}

// ParseMemberFilterPath mirrors fn parse_member_filter_path —
// extracts the user UUID from a `members[value eq "<uuid>"]` PATH
// expression. Returns (zero, false) when the expression doesn't
// match or the embedded value isn't a UUID.
func ParseMemberFilterPath(path string) (uuid.UUID, bool) {
	const prefix = `members[value eq "`
	const suffix = `"]`
	trimmed := strings.TrimSpace(path)
	if !strings.HasPrefix(trimmed, prefix) || !strings.HasSuffix(trimmed, suffix) {
		return uuid.UUID{}, false
	}
	value := trimmed[len(prefix) : len(trimmed)-len(suffix)]
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.UUID{}, false
	}
	return parsed, true
}

// memberValuesToUUIDs parses each ScimGroupMember.Value as a UUID,
// returning a 400 invalidValue ScimError on the first malformed
// entry. Mirrors the Rust per-member parse step inside
// insert_group_members_tx.
func memberValuesToUUIDs(members []ScimGroupMember) ([]uuid.UUID, *ScimError) {
	out := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		id, err := uuid.Parse(strings.TrimSpace(m.Value))
		if err != nil {
			return nil, invalidValueErr("member value must be a user UUID")
		}
		out = append(out, id)
	}
	return out, nil
}

// loadGroupView is a thin convenience wrapper used by the handler
// to reload a group + its members in one call so the wire shape
// always sees a consistent (record, members) pair.
func loadGroupView(ctx context.Context, store GroupStore, id uuid.UUID) (*GroupRecord, []MemberView, error) {
	record, err := store.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if record == nil {
		return nil, nil, nil
	}
	members, err := store.Members(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return record, members, nil
}
