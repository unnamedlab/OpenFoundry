package handlers_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
	"github.com/openfoundry/openfoundry-go/services/ontology-query-service/internal/handlers"
)

func makeObject(payload string) repos.Object {
	return repos.Object{
		Tenant:  repos.TenantId(uuid.NewString()),
		ID:      repos.ObjectId(uuid.NewString()),
		TypeID:  repos.TypeId("aircraft"),
		Version: 1,
		Payload: json.RawMessage(payload),
	}
}

func mustPayload(t *testing.T, obj repos.Object) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(obj.Payload, &m))
	return m
}

func TestApplyPropertyMaskNoSchemaIsNoop(t *testing.T) {
	t.Parallel()
	obj := makeObject(`{"callsign":"OF-1","secret":"hush"}`)
	claims := &authmw.Claims{Roles: []string{"user"}}
	out := handlers.ApplyPropertyMask(obj, nil, claims)
	assert.JSONEq(t, string(obj.Payload), string(out.Payload))
}

func TestApplyPropertyMaskNilClaimsIsNoop(t *testing.T) {
	t.Parallel()
	obj := makeObject(`{"callsign":"OF-1"}`)
	schema := []handlers.PropertyMarkings{{Name: "callsign", RequiredMarkings: []string{"PII"}}}
	out := handlers.ApplyPropertyMask(obj, schema, nil)
	assert.JSONEq(t, string(obj.Payload), string(out.Payload))
}

func TestApplyPropertyMaskAdminBypass(t *testing.T) {
	t.Parallel()
	obj := makeObject(`{"callsign":"OF-1","secret":"hush"}`)
	schema := []handlers.PropertyMarkings{
		{Name: "secret", RequiredMarkings: []string{"TOP_SECRET"}},
	}
	claims := &authmw.Claims{Roles: []string{"admin"}}
	out := handlers.ApplyPropertyMask(obj, schema, claims)
	assert.JSONEq(t, string(obj.Payload), string(out.Payload))
}

func TestApplyPropertyMaskPermissionWildcardBypass(t *testing.T) {
	t.Parallel()
	obj := makeObject(`{"callsign":"OF-1","secret":"hush"}`)
	schema := []handlers.PropertyMarkings{
		{Name: "secret", RequiredMarkings: []string{"TOP_SECRET"}},
	}
	for _, perm := range []string{"rows:all", "ontology:read_all"} {
		perm := perm
		t.Run(perm, func(t *testing.T) {
			t.Parallel()
			claims := &authmw.Claims{Roles: []string{"user"}, Permissions: []string{perm}}
			out := handlers.ApplyPropertyMask(obj, schema, claims)
			assert.JSONEq(t, string(obj.Payload), string(out.Payload))
		})
	}
}

func TestApplyPropertyMaskKeepsPropertyWhenCallerHoldsAllMarkings(t *testing.T) {
	t.Parallel()
	obj := makeObject(`{"callsign":"OF-1","pii":"jane@example.com"}`)
	schema := []handlers.PropertyMarkings{
		{Name: "pii", RequiredMarkings: []string{"PII"}},
	}
	claims := &authmw.Claims{
		Roles:        []string{"user"},
		SessionScope: &authmw.SessionScope{AllowedMarkings: []string{"PII", "PUBLIC"}},
	}
	out := handlers.ApplyPropertyMask(obj, schema, claims)
	body := mustPayload(t, out)
	assert.Equal(t, "OF-1", body["callsign"])
	assert.Equal(t, "jane@example.com", body["pii"])
	assert.NotContains(t, body, handlers.MaskedPropertiesKey)
}

func TestApplyPropertyMaskDropsPropertyWhenCallerLacksMarking(t *testing.T) {
	t.Parallel()
	obj := makeObject(`{"callsign":"OF-1","pii":"jane@example.com"}`)
	schema := []handlers.PropertyMarkings{
		{Name: "pii", RequiredMarkings: []string{"PII"}},
	}
	claims := &authmw.Claims{
		Roles:        []string{"user"},
		SessionScope: &authmw.SessionScope{AllowedMarkings: []string{"PUBLIC"}},
	}
	out := handlers.ApplyPropertyMask(obj, schema, claims)
	body := mustPayload(t, out)
	assert.Equal(t, "OF-1", body["callsign"])
	assert.NotContains(t, body, "pii")
	assert.Equal(t, []any{"pii"}, body[handlers.MaskedPropertiesKey])
}

func TestApplyPropertyMaskRequiresAllMarkingsConjunctively(t *testing.T) {
	t.Parallel()
	obj := makeObject(`{"clearance_view":42}`)
	schema := []handlers.PropertyMarkings{
		{Name: "clearance_view", RequiredMarkings: []string{"PII", "FINANCE"}},
	}
	// caller holds only one of the two required markings
	claims := &authmw.Claims{
		Roles:        []string{"user"},
		SessionScope: &authmw.SessionScope{AllowedMarkings: []string{"PII"}},
	}
	out := handlers.ApplyPropertyMask(obj, schema, claims)
	body := mustPayload(t, out)
	assert.NotContains(t, body, "clearance_view")
	assert.Equal(t, []any{"clearance_view"}, body[handlers.MaskedPropertiesKey])
}

func TestApplyPropertyMaskEmptyRequiredMarkingsIsAlwaysAllowed(t *testing.T) {
	t.Parallel()
	obj := makeObject(`{"callsign":"OF-1"}`)
	schema := []handlers.PropertyMarkings{
		{Name: "callsign", RequiredMarkings: nil},
	}
	claims := &authmw.Claims{Roles: []string{"user"}}
	out := handlers.ApplyPropertyMask(obj, schema, claims)
	body := mustPayload(t, out)
	assert.Equal(t, "OF-1", body["callsign"])
	assert.NotContains(t, body, handlers.MaskedPropertiesKey)
}

func TestApplyPropertyMaskIgnoresMissingProperty(t *testing.T) {
	t.Parallel()
	obj := makeObject(`{"callsign":"OF-1"}`)
	// "ssn" is in the schema but not in the payload — nothing to mask.
	schema := []handlers.PropertyMarkings{
		{Name: "ssn", RequiredMarkings: []string{"PII"}},
	}
	claims := &authmw.Claims{
		Roles:        []string{"user"},
		SessionScope: &authmw.SessionScope{AllowedMarkings: []string{"PUBLIC"}},
	}
	out := handlers.ApplyPropertyMask(obj, schema, claims)
	body := mustPayload(t, out)
	assert.Equal(t, "OF-1", body["callsign"])
	assert.NotContains(t, body, handlers.MaskedPropertiesKey)
}

func TestApplyPropertyMaskNonObjectPayloadIsReturnedVerbatim(t *testing.T) {
	t.Parallel()
	cases := []string{`null`, `[1,2,3]`, `42`, `"hello"`}
	schema := []handlers.PropertyMarkings{{Name: "x", RequiredMarkings: []string{"PII"}}}
	claims := &authmw.Claims{Roles: []string{"user"}}
	for _, raw := range cases {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			obj := makeObject(raw)
			out := handlers.ApplyPropertyMask(obj, schema, claims)
			assert.JSONEq(t, raw, string(out.Payload))
		})
	}
}

func TestApplyPropertyMaskEmptyPayloadIsNoop(t *testing.T) {
	t.Parallel()
	obj := repos.Object{Payload: nil}
	schema := []handlers.PropertyMarkings{{Name: "x", RequiredMarkings: []string{"PII"}}}
	claims := &authmw.Claims{Roles: []string{"user"}}
	out := handlers.ApplyPropertyMask(obj, schema, claims)
	assert.Empty(t, out.Payload)
}

func TestApplyPropertyMaskMultipleDrops(t *testing.T) {
	t.Parallel()
	obj := makeObject(`{"a":1,"b":2,"c":3,"d":4}`)
	schema := []handlers.PropertyMarkings{
		{Name: "a", RequiredMarkings: []string{"PII"}},
		{Name: "b", RequiredMarkings: []string{"FINANCE"}},
		{Name: "c", RequiredMarkings: nil}, // always allowed
		{Name: "d", RequiredMarkings: []string{"PUBLIC"}},
	}
	claims := &authmw.Claims{
		Roles:        []string{"user"},
		SessionScope: &authmw.SessionScope{AllowedMarkings: []string{"PUBLIC"}},
	}
	out := handlers.ApplyPropertyMask(obj, schema, claims)
	body := mustPayload(t, out)
	assert.NotContains(t, body, "a")
	assert.NotContains(t, body, "b")
	assert.Equal(t, float64(3), body["c"])
	assert.Equal(t, float64(4), body["d"])
	masked, ok := body[handlers.MaskedPropertiesKey].([]any)
	require.True(t, ok)
	assert.ElementsMatch(t, []any{"a", "b"}, masked)
}

func TestApplyPropertyMaskNilSessionScopeMasksAllRestrictedProperties(t *testing.T) {
	t.Parallel()
	obj := makeObject(`{"callsign":"OF-1","pii":"jane"}`)
	schema := []handlers.PropertyMarkings{
		{Name: "pii", RequiredMarkings: []string{"PII"}},
	}
	claims := &authmw.Claims{Roles: []string{"user"}} // no SessionScope
	out := handlers.ApplyPropertyMask(obj, schema, claims)
	body := mustPayload(t, out)
	assert.NotContains(t, body, "pii")
	assert.Equal(t, []any{"pii"}, body[handlers.MaskedPropertiesKey])
}

func TestApplyPropertyMaskPreservesUnrelatedFields(t *testing.T) {
	t.Parallel()
	created := int64(1717171717000)
	obj := repos.Object{
		Tenant:      repos.TenantId(uuid.NewString()),
		ID:          repos.ObjectId(uuid.NewString()),
		TypeID:      repos.TypeId("aircraft"),
		Version:     9,
		Payload:     json.RawMessage(`{"callsign":"OF-1","pii":"jane"}`),
		CreatedAtMs: &created,
		UpdatedAtMs: created + 1,
		Markings:    []repos.MarkingId{"PUBLIC"},
	}
	schema := []handlers.PropertyMarkings{
		{Name: "pii", RequiredMarkings: []string{"PII"}},
	}
	claims := &authmw.Claims{
		Roles:        []string{"user"},
		SessionScope: &authmw.SessionScope{AllowedMarkings: []string{"PUBLIC"}},
	}
	out := handlers.ApplyPropertyMask(obj, schema, claims)
	assert.Equal(t, obj.Tenant, out.Tenant)
	assert.Equal(t, obj.ID, out.ID)
	assert.Equal(t, obj.TypeID, out.TypeID)
	assert.Equal(t, obj.Version, out.Version)
	assert.Equal(t, obj.CreatedAtMs, out.CreatedAtMs)
	assert.Equal(t, obj.UpdatedAtMs, out.UpdatedAtMs)
	assert.Equal(t, obj.Markings, out.Markings)
}

func TestPropertyMarkingsFromSchemaExtractsNonEmptyOnly(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"properties": {
			"callsign": {"type": "string"},
			"pii":      {"type": "string", "required_markings": ["PII"]},
			"finance":  {"type": "number", "required_markings": ["FINANCE", "PII"]},
			"empty":    {"type": "string", "required_markings": []}
		}
	}`)
	got := handlers.PropertyMarkingsFromSchema(raw)
	names := map[string][]string{}
	for _, p := range got {
		names[p.Name] = p.RequiredMarkings
	}
	assert.NotContains(t, names, "callsign")
	assert.NotContains(t, names, "empty")
	assert.Equal(t, []string{"PII"}, names["pii"])
	assert.ElementsMatch(t, []string{"FINANCE", "PII"}, names["finance"])
}

func TestPropertyMarkingsFromSchemaHandlesGarbage(t *testing.T) {
	t.Parallel()
	assert.Nil(t, handlers.PropertyMarkingsFromSchema(nil))
	assert.Nil(t, handlers.PropertyMarkingsFromSchema(json.RawMessage(`not json`)))
	assert.Nil(t, handlers.PropertyMarkingsFromSchema(json.RawMessage(`{}`)))
	assert.Nil(t, handlers.PropertyMarkingsFromSchema(json.RawMessage(`{"properties":{}}`)))
}
