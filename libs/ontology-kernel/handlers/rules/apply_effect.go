// ApplyRuleEffect — full 1:1 port of the Rust function of the same
// name in `libs/ontology-kernel/src/domain/rules.rs`.
//
// Lives in the rules handler package (instead of `domain/`) because
// it composes the writeback path through `handlers/objects`, which
// would cycle if reached from `domain/`. The Rust source crosses the
// same boundary internally — `handlers/objects` is `pub(crate)` and
// `domain::rules` reaches into it.
//
// Pipeline:
//
//  1. Pull `object_patch` from the rule's effect_preview. Empty patch
//     ⇒ no-op (return the object unchanged).
//  2. Load the object_type's effective property schema, validate
//     each patched property's type, merge into existing properties,
//     run the schema-level validator.
//  3. Read the latest `version` from the object store (`Strong`
//     consistency) so the optimistic-concurrency check on the Put
//     uses the canonical baseline.
//  4. Compose the next ObjectInstance, route through
//     `objects.ApplyObjectWrite` (writeback + outbox), then append
//     a `"revision"` entry via `objects.AppendObjectRevision`.
package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/handlers/objects"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// ApplyRuleEffectReal is the wired counterpart to
// `domain.ApplyRuleEffect`. Returns the updated ObjectInstance on
// success — the caller (ApplyRule handler) reads `updated.Properties`
// for the JSON response.
func ApplyRuleEffectReal(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	object *domain.ObjectInstance,
	effectPreview json.RawMessage,
) (*domain.ObjectInstance, error) {
	patch, err := extractObjectPatch(effectPreview)
	if err != nil {
		return nil, err
	}
	if len(patch) == 0 {
		// No-op: return the object unchanged. Mirrors the Rust early
		// return in `apply_rule_effect`.
		return object, nil
	}

	defs, err := domain.LoadEffectiveProperties(ctx, state.DB, object.ObjectTypeID)
	if err != nil {
		return nil, fmt.Errorf("failed to load property definitions: %w", err)
	}
	typeByName := map[string]string{}
	for _, p := range defs {
		typeByName[p.Name] = p.PropertyType
	}

	// Merge object_patch into existing properties.
	merged := map[string]json.RawMessage{}
	if len(object.Properties) > 0 {
		_ = json.Unmarshal(object.Properties, &merged)
	}
	for name, value := range patch {
		propertyType, ok := typeByName[name]
		if !ok {
			return nil, fmt.Errorf("unknown property '%s' in rule effect", name)
		}
		if err := domain.ValidatePropertyValue(propertyType, value); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		merged[name] = value
	}

	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("encode merged properties: %w", err)
	}
	normalized, err := domain.ValidateObjectProperties(defs, mergedJSON)
	if err != nil {
		return nil, fmt.Errorf("invalid rule effect patch: %w", err)
	}

	repoObject, err := objects.LoadRepoObjectFromStore(ctx, state, claims, object.ID, storage.Strong())
	if err != nil {
		return nil, err
	}
	if repoObject == nil {
		return nil, errors.New("object was not found in object store")
	}

	updated := &domain.ObjectInstance{
		ID:             object.ID,
		ObjectTypeID:   object.ObjectTypeID,
		Properties:     normalized,
		CreatedBy:      object.CreatedBy,
		OrganizationID: object.OrganizationID,
		Marking:        object.Marking,
		CreatedAt:      object.CreatedAt,
		UpdatedAt:      time.Now().UTC(),
	}

	extra, _ := json.Marshal(map[string]any{
		"source":         "ontology_rule",
		"effect_preview": json.RawMessage(effectPreview),
	})
	expected := repoObject.Version
	outcome, err := objects.ApplyObjectWrite(ctx, state, claims, updated, &expected, "update", extra)
	if err != nil {
		return nil, err
	}
	if err := objects.AppendObjectRevision(ctx, state, claims, updated, "update",
		int64(outcome.CommittedVersion), nil); err != nil {
		return nil, err
	}
	return updated, nil
}

// extractObjectPatch pulls the `object_patch` map out of the
// effect_preview JSON. Returns nil when the preview is null /
// missing the field — mirrors the Rust `Option`-typed extraction.
func extractObjectPatch(preview json.RawMessage) (map[string]json.RawMessage, error) {
	if len(preview) == 0 || string(preview) == "null" {
		return nil, nil
	}
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(preview, &asMap); err != nil {
		return nil, nil
	}
	raw, ok := asMap["object_patch"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(raw, &patch); err != nil {
		return nil, nil
	}
	return patch, nil
}
