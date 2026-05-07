// Tests for the Phase 5C inline-edit helpers — input-name resolution
// (single mapping, ambiguous, configured), build_inline_edit_parameters
// back-fill from current target properties.
package actions

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

func ptr[T any](v T) *T { return &v }

func TestResolveInlineEditInputNameSingleMapping(t *testing.T) {
	t.Parallel()
	cfg := models.UpdateObjectActionConfig{
		PropertyMappings: []models.ActionPropertyMapping{
			{PropertyName: "amount", InputName: ptr("amount_input")},
		},
	}
	configBytes, _ := json.Marshal(cfg)
	action := models.ActionType{Config: configBytes, OperationKind: "update_object"}

	got, err := resolveInlineEditInputName(action, "amount", models.PropertyInlineEditConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "amount_input" {
		t.Errorf("got %q want amount_input", got)
	}
}

func TestResolveInlineEditInputNameAmbiguousRequiresExplicit(t *testing.T) {
	t.Parallel()
	cfg := models.UpdateObjectActionConfig{
		PropertyMappings: []models.ActionPropertyMapping{
			{PropertyName: "amount", InputName: ptr("input_a")},
			{PropertyName: "amount", InputName: ptr("input_b")},
		},
	}
	configBytes, _ := json.Marshal(cfg)
	action := models.ActionType{Config: configBytes}

	if _, err := resolveInlineEditInputName(action, "amount", models.PropertyInlineEditConfig{}); err == nil {
		t.Fatal("expected ambiguity error")
	}

	got, err := resolveInlineEditInputName(action, "amount",
		models.PropertyInlineEditConfig{InputName: ptr("input_b")})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != "input_b" {
		t.Errorf("explicit selection drift: %q", got)
	}
}

func TestResolveInlineEditInputNameNoMapping(t *testing.T) {
	t.Parallel()
	cfg := models.UpdateObjectActionConfig{}
	configBytes, _ := json.Marshal(cfg)
	action := models.ActionType{Config: configBytes}
	if _, err := resolveInlineEditInputName(action, "amount", models.PropertyInlineEditConfig{}); err == nil {
		t.Fatal("expected error when property is not mapped")
	}
}

func TestBuildInlineEditParametersBackfillsOtherInputs(t *testing.T) {
	t.Parallel()
	cfg := models.UpdateObjectActionConfig{
		PropertyMappings: []models.ActionPropertyMapping{
			{PropertyName: "amount", InputName: ptr("amount_input")},
			{PropertyName: "currency", InputName: ptr("currency_input")},
		},
	}
	configBytes, _ := json.Marshal(cfg)
	action := models.ActionType{Config: configBytes}
	property := models.Property{
		ID:           uuid.New(),
		ObjectTypeID: uuid.New(),
		Name:         "amount",
		PropertyType: "number",
	}
	target := &domain.ObjectInstance{
		Properties: json.RawMessage(`{"amount":100,"currency":"USD"}`),
	}
	got, err := buildInlineEditParameters(action, property, target,
		models.PropertyInlineEditConfig{InputName: ptr("amount_input")},
		json.RawMessage(`200`))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(got, &asMap); err != nil {
		t.Fatalf("output not JSON object: %v", err)
	}
	if string(asMap["amount_input"]) != "200" {
		t.Errorf("amount_input drift: %s", asMap["amount_input"])
	}
	if string(asMap["currency_input"]) != `"USD"` {
		t.Errorf("currency_input back-fill drift: %s", asMap["currency_input"])
	}
}
