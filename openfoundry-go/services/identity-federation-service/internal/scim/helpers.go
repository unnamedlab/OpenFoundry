package scim

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// ─── Pure helpers (mirrors the corresponding Rust fns) ──────────────

// PrimaryEmail mirrors fn primary_email — picks the email flagged
// `primary: true`, falling back to the first email in the slice
// when none is explicitly primary. Returns nil if `Emails` is
// empty.
func PrimaryEmail(user *ScimUser) *string {
	if len(user.Emails) == 0 {
		return nil
	}
	for i := range user.Emails {
		if user.Emails[i].Primary != nil && *user.Emails[i].Primary {
			v := user.Emails[i].Value
			return &v
		}
	}
	v := user.Emails[0].Value
	return &v
}

// DisplayNameFromScim mirrors fn display_name_from_scim. Priority:
//   1. Name.Formatted (when non-empty)
//   2. givenName + " " + familyName (when at least one is set)
//   3. fallback (typically userName)
func DisplayNameFromScim(name *ScimName, fallback string) string {
	if name == nil {
		return fallback
	}
	if name.Formatted != nil && *name.Formatted != "" {
		return *name.Formatted
	}
	parts := []string{}
	if name.GivenName != nil && *name.GivenName != "" {
		parts = append(parts, *name.GivenName)
	}
	if name.FamilyName != nil && *name.FamilyName != "" {
		parts = append(parts, *name.FamilyName)
	}
	joined := strings.TrimSpace(strings.Join(parts, " "))
	if joined == "" {
		return fallback
	}
	return joined
}

// UserAttributesFromScim mirrors fn user_attributes_from_scim —
// builds the canonical attributes JSONB blob from the wire-shape
// ScimUser. Pulls externalId / name / openfoundry-extension into
// `attributes.scim.{externalId, name, openfoundry}`.
func UserAttributesFromScim(user *ScimUser) json.RawMessage {
	scimMap := map[string]json.RawMessage{}

	if user.ExternalID != nil {
		raw, _ := json.Marshal(*user.ExternalID)
		scimMap["externalId"] = raw
	}
	if user.Name != nil {
		raw, err := json.Marshal(user.Name)
		if err == nil {
			scimMap["name"] = raw
		}
	}
	if ext, ok := user.Extensions[SchemaOpenfoundryUserExtension]; ok {
		scimMap["openfoundry"] = ext
	}

	scimRaw, _ := json.Marshal(scimMap)
	wrapper := map[string]json.RawMessage{"scim": scimRaw}
	out, _ := json.Marshal(wrapper)
	return out
}

// ScimExternalIDFromUser mirrors fn scim_external_id — returns
// the trimmed externalId when non-empty, nil otherwise.
func ScimExternalIDFromUser(user *ScimUser) *string {
	if user.ExternalID == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*user.ExternalID)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// ExternalIDFromAttributes mirrors fn external_id_from_attributes
// — pulls a trimmed-non-empty externalId off
// attributes."/scim/externalId".
func ExternalIDFromAttributes(attrs json.RawMessage) *string {
	v := jsonPointerString(attrs, "/scim/externalId")
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// MergeScimUserRecord mirrors fn merge_scim_user_record — applies
// idempotent SCIM-create updates to an existing UserRecord (used
// by CreateUser when externalId resolution finds an existing row).
func MergeScimUserRecord(
	dst *UserRecord,
	email, name string,
	active bool,
	organizationID *uuid.UUID,
	attributes json.RawMessage,
) {
	dst.Email = email
	dst.Name = name
	dst.IsActive = active
	dst.OrganizationID = organizationID
	dst.Attributes = attributes
}

// SetScimName mirrors fn set_scim_name — splices `name` under
// attributes."/scim/name", initialising the wrapper objects when
// missing.
func SetScimName(attrs json.RawMessage, name *ScimName) json.RawMessage {
	wrapper := decodeAttributesObject(attrs)
	scim := decodeRawObject(wrapper["scim"])
	if name != nil {
		raw, _ := json.Marshal(name)
		scim["name"] = raw
	}
	scimRaw, _ := json.Marshal(scim)
	wrapper["scim"] = scimRaw
	out, _ := json.Marshal(wrapper)
	return out
}

// SetScimExternalID splices `externalId` under
// attributes."/scim/externalId" — used by PATCH on /externalId.
func SetScimExternalID(attrs json.RawMessage, externalID string) json.RawMessage {
	wrapper := decodeAttributesObject(attrs)
	scim := decodeRawObject(wrapper["scim"])
	raw, _ := json.Marshal(externalID)
	scim["externalId"] = raw
	scimRaw, _ := json.Marshal(scim)
	wrapper["scim"] = scimRaw
	out, _ := json.Marshal(wrapper)
	return out
}

// SetScimOpenfoundryExtension splices a full openfoundry extension
// object under attributes."/scim/openfoundry".
func SetScimOpenfoundryExtension(attrs json.RawMessage, extension json.RawMessage) json.RawMessage {
	wrapper := decodeAttributesObject(attrs)
	scim := decodeRawObject(wrapper["scim"])
	scim["openfoundry"] = extension
	scimRaw, _ := json.Marshal(scim)
	wrapper["scim"] = scimRaw
	out, _ := json.Marshal(wrapper)
	return out
}

// SetScimOpenfoundryField splices a single key under the
// openfoundry extension — used by PATCH on
// `<URN>.<field>` paths.
func SetScimOpenfoundryField(attrs json.RawMessage, field string, value json.RawMessage) json.RawMessage {
	wrapper := decodeAttributesObject(attrs)
	scim := decodeRawObject(wrapper["scim"])
	openfoundry := decodeRawObject(scim["openfoundry"])
	openfoundry[field] = value

	openfoundryRaw, _ := json.Marshal(openfoundry)
	scim["openfoundry"] = openfoundryRaw
	scimRaw, _ := json.Marshal(scim)
	wrapper["scim"] = scimRaw
	out, _ := json.Marshal(wrapper)
	return out
}

// decodeAttributesObject parses a possibly-nil/empty attributes
// blob into a top-level map of raw values; returns an empty map
// when the input isn't a valid object.
func decodeAttributesObject(raw json.RawMessage) map[string]json.RawMessage {
	if len(raw) == 0 {
		return map[string]json.RawMessage{}
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return map[string]json.RawMessage{}
	}
	return obj
}

// decodeRawObject is the same idea but for nested raw values that
// might be missing.
func decodeRawObject(raw json.RawMessage) map[string]json.RawMessage {
	if len(raw) == 0 {
		return map[string]json.RawMessage{}
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return map[string]json.RawMessage{}
	}
	return obj
}

// ─── Organization resolver ──────────────────────────────────────────

// OrganizationResolver resolves a tenancy-organizations slug into
// its UUID. The Rust impl reads `tenancy_organizations.id` keyed
// by `slug`; the Go side pushes that lookup behind an interface so
// this package doesn't pull the tenancy schema into its build
// graph (the consuming server constructs the resolver at boot).
type OrganizationResolver interface {
	// ResolveSlug returns (id, true, nil) when the slug exists,
	// (zero, false, nil) when the slug is unknown, or (zero,
	// false, err) on backend failure.
	ResolveSlug(ctx context.Context, slug string) (uuid.UUID, bool, error)
}

// InMemoryOrganizationResolver is a thread-safe map-backed
// resolver useful for tests and the in-process dev profile.
type InMemoryOrganizationResolver struct {
	mu   sync.RWMutex
	rows map[string]uuid.UUID
}

// NewInMemoryOrganizationResolver returns a fresh resolver.
func NewInMemoryOrganizationResolver() *InMemoryOrganizationResolver {
	return &InMemoryOrganizationResolver{rows: map[string]uuid.UUID{}}
}

// Insert registers `slug → id`. Test helper, not part of the
// resolver contract.
func (r *InMemoryOrganizationResolver) Insert(slug string, id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[slug] = id
}

// ResolveSlug satisfies OrganizationResolver.
func (r *InMemoryOrganizationResolver) ResolveSlug(_ context.Context, slug string) (uuid.UUID, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.rows[slug]
	return id, ok, nil
}

// Compile-time interface assertion.
var _ OrganizationResolver = (*InMemoryOrganizationResolver)(nil)

// ─── User org-id resolution ─────────────────────────────────────────

// ResolveUserOrganizationID mirrors fn resolve_user_organization_id.
// Inspects the openfoundry extension on the wire payload first
// (organizationId / organization_id), falling back to the same
// keys nested inside attributes.scim.openfoundry, and finally
// resolving organizationSlug / organization through the resolver
// when neither id is present.
//
// Returns (nil, nil) when no organization hint is present.
// Returns (nil, *ScimError) when a hint is malformed (UUID parse
// failure, unknown slug, etc.) — caller maps to 400.
func ResolveUserOrganizationID(
	ctx context.Context,
	resolver OrganizationResolver,
	user *ScimUser,
	attributes json.RawMessage,
) (*uuid.UUID, *ScimError) {
	// Wire-payload extension first.
	if ext, ok := user.Extensions[SchemaOpenfoundryUserExtension]; ok {
		if id := openfoundryIDFromExtension(ext); id != nil {
			parsed, ok := parseUUIDStrict(*id)
			if !ok {
				return nil, invalidValueErr("organizationId must be a UUID")
			}
			return &parsed, nil
		}
	}
	return resolveOrganizationFromAttributes(ctx, resolver, attributes)
}

// resolveOrganizationFromAttributes mirrors fn
// resolve_organization_from_attributes — checks
// attributes.scim.openfoundry for organizationId/organization_id
// or organizationSlug/organization. The resolver is consulted on
// the slug branch.
func resolveOrganizationFromAttributes(
	ctx context.Context,
	resolver OrganizationResolver,
	attributes json.RawMessage,
) (*uuid.UUID, *ScimError) {
	if id := jsonPointerString(attributes, "/scim/openfoundry/organizationId"); id != nil {
		trimmed := strings.TrimSpace(*id)
		if trimmed != "" {
			parsed, ok := parseUUIDStrict(trimmed)
			if !ok {
				return nil, invalidValueErr("organizationId must be a UUID")
			}
			return &parsed, nil
		}
	}
	if id := jsonPointerString(attributes, "/scim/openfoundry/organization_id"); id != nil {
		trimmed := strings.TrimSpace(*id)
		if trimmed != "" {
			parsed, ok := parseUUIDStrict(trimmed)
			if !ok {
				return nil, invalidValueErr("organizationId must be a UUID")
			}
			return &parsed, nil
		}
	}

	var slug *string
	if v := jsonPointerString(attributes, "/scim/openfoundry/organizationSlug"); v != nil {
		slug = v
	} else if v := jsonPointerString(attributes, "/scim/openfoundry/organization"); v != nil {
		slug = v
	}
	if slug == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*slug)
	if trimmed == "" {
		return nil, nil
	}
	if resolver == nil {
		return nil, invalidValueErr("organizationSlug " + trimmed + " does not exist")
	}
	id, ok, err := resolver.ResolveSlug(ctx, trimmed)
	if err != nil {
		fail := NewScimError(500, "failed to resolve organizationSlug", nil)
		return nil, &fail
	}
	if !ok {
		return nil, invalidValueErr("organizationSlug " + trimmed + " does not exist")
	}
	return &id, nil
}

// openfoundryIDFromExtension returns the organization id field
// (organizationId or organization_id) from a raw extension
// object, trimmed; nil when absent or empty.
func openfoundryIDFromExtension(ext json.RawMessage) *string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(ext, &obj); err != nil {
		return nil
	}
	for _, key := range []string{"organizationId", "organization_id"} {
		if raw, ok := obj[key]; ok {
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					return &trimmed
				}
			}
		}
	}
	return nil
}

func parseUUIDStrict(raw string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.UUID{}, false
	}
	return parsed, true
}

// invalidValueErr builds a 400 ScimError tagged with
// scimType=invalidValue.
func invalidValueErr(detail string) *ScimError {
	t := "invalidValue"
	err := NewScimError(400, detail, &t)
	return &err
}
