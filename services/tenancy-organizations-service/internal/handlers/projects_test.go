package handlers

// Tests for the unexported normalisation helpers in projects.go.
// They live in `package handlers` so the assertions can call the
// helpers directly without re-exporting them. The Rust handler keeps
// the same parity tests in `mod tests` at the bottom of projects.rs.

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
)

func TestNormalizeFolderNameCollapsesWhitespace(t *testing.T) {
	t.Parallel()
	got, err := normalizeFolderName("  Weekly   Reviews  ")
	require.NoError(t, err)
	assert.Equal(t, "Weekly Reviews", got)
}

func TestNormalizeFolderNameRejectsEmpty(t *testing.T) {
	t.Parallel()
	_, err := normalizeFolderName("   ")
	assert.Error(t, err)
}

func TestFolderSlugFromHumanReadableName(t *testing.T) {
	t.Parallel()
	got, err := folderSlugFromName("Weekly Reviews / Q2")
	require.NoError(t, err)
	assert.Equal(t, "weekly-reviews-q2", got)
}

func TestFolderSlugRejectsNonAlphanumericInput(t *testing.T) {
	t.Parallel()
	_, err := folderSlugFromName("✨ / 🚀")
	assert.Error(t, err)
}

func TestNormalizeSlugAccepts(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"FraudModels":     "fraudmodels",
		"  fraud-models ": "fraud-models",
		"PROJECT-2026":    "project-2026",
	}
	for in, want := range cases {
		got, err := normalizeSlug(in, "slug")
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
}

func TestNormalizeSlugRejectsEmpty(t *testing.T) {
	t.Parallel()
	_, err := normalizeSlug("   ", "slug")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slug is required")
}

func TestNormalizeSlugRejectsInvalidChars(t *testing.T) {
	t.Parallel()
	_, err := normalizeSlug("fraud_models", "slug")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lowercase letters, digits, and hyphens")
}

func TestNormalizeSlugRejectsLeadingTrailingHyphen(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"-fraud", "fraud-", "-fraud-"} {
		_, err := normalizeSlug(in, "slug")
		require.Error(t, err, in)
		assert.Contains(t, err.Error(), "cannot start or end with a hyphen", in)
	}
}

func TestNormalizeOptionalSlugCollapsesEmpty(t *testing.T) {
	t.Parallel()
	got, err := normalizeOptionalSlug(nil, "workspace_slug")
	require.NoError(t, err)
	assert.Nil(t, got)

	blank := "   "
	got, err = normalizeOptionalSlug(&blank, "workspace_slug")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestNormalizeOptionalSlugReturnsCanonicalForm(t *testing.T) {
	t.Parallel()
	in := "  Engineering "
	got, err := normalizeOptionalSlug(&in, "workspace_slug")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "engineering", *got)
}

func TestAsciiLowerLeavesNonASCIIBytesIntact(t *testing.T) {
	t.Parallel()
	// Mirrors Rust `str::to_ascii_lowercase`: non-ASCII bytes pass
	// through, ASCII A–Z lowers in place. The downstream slug check
	// then rejects the non-ASCII bytes — same observable result.
	assert.Equal(t, "abc", asciiLower("ABC"))
	assert.Equal(t, "café", asciiLower("CAFé"))
}

func TestParseFolderRIDLocator(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("018f2f1c-aaaa-7bbb-8ccc-000000000002")
	got, err := parseFolderRIDLocator("ri.compass.main.folder."+id.String(), "parent_folder_rid")
	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestParseFolderRIDLocatorRejectsOtherResourceTypes(t *testing.T) {
	t.Parallel()
	_, err := parseFolderRIDLocator("ri.compass.main.project.018f2f1c-aaaa-7bbb-8ccc-000000000001", "parent_folder_rid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compass folder RID")
}

func TestProjectTemplateSubstitution(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		"project.name": "Alpha",
		"var.region":   "eu-west",
	}
	got := substituteTemplateString("{{ project.name }} / {{var.region}} / {{missing}}", values)
	assert.Equal(t, "Alpha / eu-west / ", got)
}

func TestValidateProjectTemplateDeploymentRequiresFeaturePermissions(t *testing.T) {
	t.Parallel()
	role := models.OntologyProjectRoleViewer
	template := &models.ProjectTemplate{
		DefaultRole: models.OntologyProjectRoleOwner,
		GeneratedGroups: []models.ProjectTemplateGeneratedGroup{{
			Role: models.OntologyProjectRoleViewer,
		}},
		DefaultRoleGrants: []models.ProjectTemplateRoleGrant{{
			PrincipalKind:      models.ProjectTemplatePrincipalGeneratedGroup,
			GeneratedGroupRole: &role,
			Role:               models.OntologyProjectRoleViewer,
		}},
		Markings: []models.ProjectTemplateMarking{{
			DisplayName: "Confidential", CreateIfMissing: true,
		}},
		Constraints: []models.ProjectTemplateConstraint{{
			Name: "No export",
		}},
	}
	denied := validateProjectTemplateDeployment(&authmw.Claims{}, template)
	assert.False(t, denied.Allowed)
	assert.Contains(t, denied.MissingPermissions, "groups:write or groups:manage")
	assert.Contains(t, denied.MissingPermissions, "markings:apply or markings:write")

	allowed := validateProjectTemplateDeployment(&authmw.Claims{
		Permissions: []string{"projects:manage", "groups:manage", "markings:apply", "project_constraints:apply"},
	}, template)
	assert.True(t, allowed.Allowed)
	assert.Empty(t, allowed.MissingPermissions)
}

func TestResolveProjectTemplateVariablesRequiresMissingInput(t *testing.T) {
	t.Parallel()
	template := &models.ProjectTemplate{
		Variables: []models.ProjectTemplateVariable{{Key: "region", Required: true}},
	}
	_, err := resolveProjectTemplateVariables(template, &models.CreateOntologyProjectRequest{}, "alpha", "Alpha", uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "region")
}
