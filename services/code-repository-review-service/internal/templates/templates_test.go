package templates_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/code-repository-review-service/internal/templates"
)

func TestBuiltInTemplatesCoverCRW3Languages(t *testing.T) {
	want := []string{"python-transform", "java-transform", "sql-transform", "r-transform", "typescript-function"}
	for _, id := range want {
		t.Run(id, func(t *testing.T) {
			tmpl, ok := templates.Get(id)
			require.True(t, ok)
			require.NotEmpty(t, tmpl.Files)
			require.Contains(t, tmpl.Files, "openfoundry.template.json")
			require.NotEmpty(t, tmpl.BuildCommand)
		})
	}
}

func TestTemplateAliasesNormalize(t *testing.T) {
	tmpl, ok := templates.Get("foundry-functions-typescript")
	require.True(t, ok)
	require.Equal(t, "typescript-function", tmpl.ID)
}
