// Property-level RBAC mask. The object-level marking check in
// canReadMarkings gates access to the whole object; ApplyPropertyMask
// then redacts individual fields the caller is not cleared to see.
//
// Convention for schemas returned by SchemaStore.GetLatest:
// each entry under JSONSchema.properties.<name> may carry an
// optional `required_markings: ["marking_a", ...]` array. When the
// array is empty (or absent) the property has no per-property
// marking requirement and is always returned.
package handlers

import (
	"encoding/json"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// MaskedPropertiesKey is the JSON key under which redacted property
// names are reported back in the payload, for caller-side auditing.
const MaskedPropertiesKey = "_masked_properties"

// PropertyMarkings declares the markings a caller must possess in
// order to read a single property. RequiredMarkings is conjunctive:
// the caller must hold every marking listed.
type PropertyMarkings struct {
	Name             string
	RequiredMarkings []string
}

// ApplyPropertyMask returns a copy of obj with any property the
// caller cannot read removed from obj.Payload. The names of the
// removed properties are surfaced under the MaskedPropertiesKey
// entry inside the payload so downstream auditors can attribute the
// masking to a specific request.
//
// The function is a no-op when:
//   - the schema list is empty
//   - claims is nil
//   - the caller has the admin role, ontology:read_all, or rows:all
//   - the payload is not a JSON object (e.g., null / array / scalar)
//
// Properties whose RequiredMarkings slice is empty are always kept.
func ApplyPropertyMask(obj repos.Object, schema []PropertyMarkings, claims *authmw.Claims) repos.Object {
	if claims == nil || len(schema) == 0 || len(obj.Payload) == 0 {
		return obj
	}
	if claims.HasRole("admin") || claims.HasPermissionKey("rows:all") || claims.HasPermissionKey("ontology:read_all") {
		return obj
	}

	allowed := map[string]struct{}{}
	if claims.SessionScope != nil {
		for _, m := range claims.SessionScope.AllowedMarkings {
			allowed[m] = struct{}{}
		}
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(obj.Payload, &payload); err != nil {
		return obj
	}

	masked := make([]string, 0)
	for _, prop := range schema {
		if len(prop.RequiredMarkings) == 0 {
			continue
		}
		if _, present := payload[prop.Name]; !present {
			continue
		}
		if hasAllMarkings(prop.RequiredMarkings, allowed) {
			continue
		}
		delete(payload, prop.Name)
		masked = append(masked, prop.Name)
	}

	if len(masked) == 0 {
		return obj
	}

	maskedRaw, err := json.Marshal(masked)
	if err != nil {
		return obj
	}
	payload[MaskedPropertiesKey] = maskedRaw

	out := obj
	if newPayload, err := json.Marshal(payload); err == nil {
		out.Payload = newPayload
	}
	return out
}

func hasAllMarkings(required []string, allowed map[string]struct{}) bool {
	for _, r := range required {
		if _, ok := allowed[r]; !ok {
			return false
		}
	}
	return true
}

// PropertyMarkingsFromSchema extracts per-property marking
// requirements from a SchemaStore JsonSchema. The expected shape is:
//
//	{
//	  "properties": {
//	    "<name>": { "required_markings": ["..."], ... },
//	    ...
//	  },
//	  ...
//	}
//
// Returns nil when raw is empty or fails to decode — callers should
// treat the absence of a schema as "no per-property markings", not
// as a fatal error.
func PropertyMarkingsFromSchema(raw json.RawMessage) []PropertyMarkings {
	if len(raw) == 0 {
		return nil
	}
	var envelope struct {
		Properties map[string]struct {
			RequiredMarkings []string `json:"required_markings"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil
	}
	if len(envelope.Properties) == 0 {
		return nil
	}
	out := make([]PropertyMarkings, 0, len(envelope.Properties))
	for name, meta := range envelope.Properties {
		if len(meta.RequiredMarkings) == 0 {
			continue
		}
		out = append(out, PropertyMarkings{Name: name, RequiredMarkings: meta.RequiredMarkings})
	}
	return out
}
