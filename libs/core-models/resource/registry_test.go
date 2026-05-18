package resource_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/core-models/resource"
)

func TestDefaultRegistryResolvesRIDNamespace(t *testing.T) {
	t.Parallel()

	definition, ok, err := resource.DefaultRegistry().LookupRID("ri.foundry.main.dataset.018f2f1c-aaaa-7bbb-8ccc-000000000001")
	require.NoError(t, err)
	require.True(t, ok)

	assert.Equal(t, resource.TypeFoundryDataset, definition.ID)
	assert.Equal(t, "Dataset", definition.DisplayName)
	assert.Equal(t, "dataset-versioning-service", definition.OwningService)
	assert.True(t, definition.Supports(resource.ActionMove))
}

func TestDefaultRegistryResolvesAliases(t *testing.T) {
	t.Parallel()

	definition, ok := resource.DefaultRegistry().LookupNamespace("openfoundry", "project")
	require.True(t, ok)
	assert.Equal(t, resource.TypeCompassProject, definition.ID)
}

func TestUnknownTypesUsePlaceholder(t *testing.T) {
	t.Parallel()

	definition, ok := resource.DefaultRegistry().ResolveRID("ri.foundry.main.unregistered.018f2f1c-aaaa-7bbb-8ccc-000000000001")
	require.False(t, ok)

	assert.Equal(t, resource.TypeUnknown, definition.ID)
	assert.Equal(t, "Unknown resource", definition.DisplayName)
	assert.Equal(t, "object", definition.DefaultIcon)
	assert.Empty(t, definition.SupportedActions)
}

func TestDefinitionOpenURL(t *testing.T) {
	t.Parallel()

	definition, ok := resource.DefaultRegistry().ResolveRID("ri.foundry.main.source.018f2f1c-aaaa-7bbb-8ccc-000000000001")
	require.True(t, ok)

	url, err := definition.OpenURL("ri.foundry.main.source.018f2f1c-aaaa-7bbb-8ccc-000000000001")
	require.NoError(t, err)
	assert.Equal(t, "/data-connection/sources/ri.foundry.main.source.018f2f1c-aaaa-7bbb-8ccc-000000000001", url)
}

func TestRegistryRejectsDuplicateTypeID(t *testing.T) {
	t.Parallel()

	definition := minimalDefinition(resource.TypeFoundryDataset, "foundry", "dataset")
	_, err := resource.NewRegistry(definition, definition)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate resource type id")
}

func TestRegistryRejectsDuplicateRIDNamespace(t *testing.T) {
	t.Parallel()

	_, err := resource.NewRegistry(
		minimalDefinition("TYPE_A", "foundry", "dataset"),
		minimalDefinition("TYPE_B", "foundry", "dataset"),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestRegistryRejectsIncompleteDefinition(t *testing.T) {
	t.Parallel()

	definition := minimalDefinition("TYPE_A", "foundry", "dataset")
	definition.OpenAppURLTemplate = "/datasets"
	_, err := resource.NewRegistry(definition)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must include {rid}")
}

func TestAllDefinitionsAreSortedCopies(t *testing.T) {
	t.Parallel()

	registry := resource.DefaultRegistry()
	definitions := registry.All()
	require.GreaterOrEqual(t, len(definitions), 10)
	for i := 1; i < len(definitions); i++ {
		assert.LessOrEqual(t, string(definitions[i-1].ID), string(definitions[i].ID))
	}

	definitions[0].SupportedActions = append(definitions[0].SupportedActions, resource.ActionTrash)
	next, ok := registry.Lookup(definitions[0].ID)
	require.True(t, ok)
	assert.NotEqual(t, len(definitions[0].SupportedActions), len(next.SupportedActions))
}

func TestRegistryTracksReferenceTargets(t *testing.T) {
	t.Parallel()

	definition, ok := resource.DefaultRegistry().Lookup(resource.TypeWorkshopDashboard)
	require.True(t, ok)

	assert.Contains(t, definition.ReferenceTargets, resource.ReferenceTarget{
		Relationship: "reads",
		TargetType:   resource.TypeFoundryQuery,
	})
	assert.Contains(t, definition.ReferenceTargets, resource.ReferenceTarget{
		Relationship: "reads",
		TargetType:   resource.TypeFoundryDataset,
	})
}

func TestRegistryRejectsUnregisteredReferenceTarget(t *testing.T) {
	t.Parallel()

	definition := minimalDefinition("TYPE_A", "foundry", "type-a")
	definition.ReferenceTargets = []resource.ReferenceTarget{{
		Relationship: "reads",
		TargetType:   "MISSING_TYPE",
	}}
	_, err := resource.NewRegistry(definition)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reference target MISSING_TYPE is not registered")
}

func minimalDefinition(id resource.TypeID, service, resourceType string) resource.TypeDefinition {
	return resource.TypeDefinition{
		ID:                 id,
		DisplayName:        string(id),
		OwningService:      "test-service",
		RIDService:         service,
		RIDResourceType:    resourceType,
		DefaultIcon:        "object",
		SupportedActions:   []resource.Action{resource.ActionShare},
		OpenAppURLTemplate: "/resources/{rid}",
	}
}
