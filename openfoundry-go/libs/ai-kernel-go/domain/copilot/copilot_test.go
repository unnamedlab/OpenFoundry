package copilot

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

func TestAssistGeneratesDatasetSpecificSQL(t *testing.T) {
	t.Parallel()
	datasetID := uuid.New()
	d := Assist("show me anomalies", []uuid.UUID{datasetID}, nil, nil, true, false)
	require.NotNil(t, d.SuggestedSQL)
	assert.Contains(t, *d.SuggestedSQL, "FROM dataset_"+strings.ReplaceAll(datasetID.String(), "-", ""))
	assert.Contains(t, *d.SuggestedSQL, "INTERVAL '30 days'")
}

func TestAssistFallsBackToGenericSQLWhenQuestionMentionsSQL(t *testing.T) {
	t.Parallel()
	d := Assist("draft a sql query", nil, nil, nil, true, false)
	require.NotNil(t, d.SuggestedSQL)
	assert.Contains(t, *d.SuggestedSQL, "FROM your_dataset")
}

func TestAssistOmitsSQLWhenNotRequested(t *testing.T) {
	t.Parallel()
	d := Assist("anything", []uuid.UUID{uuid.New()}, nil, nil, false, false)
	assert.Nil(t, d.SuggestedSQL)
}

func TestAssistPipelineSuggestionsToggle(t *testing.T) {
	t.Parallel()
	on := Assist("anything", nil, nil, nil, false, true)
	assert.Len(t, on.PipelineSuggestions, 3)
	off := Assist("anything", nil, nil, nil, false, false)
	assert.Empty(t, off.PipelineSuggestions)
}

func TestAssistOntologyHintsCombine(t *testing.T) {
	t.Parallel()
	typeID := uuid.New()
	d := Assist("how do I align with the ontology?", nil, []uuid.UUID{typeID}, nil, false, false)
	assert.Len(t, d.OntologyHints, 2, "first hint from ID, second from keyword match")
	assert.Contains(t, d.OntologyHints[0], strings.ReplaceAll(typeID.String(), "-", ""))
}

func TestAssistKnowledgeSummary(t *testing.T) {
	t.Parallel()
	empty := Assist("hi", nil, nil, nil, false, false)
	assert.Contains(t, empty.Answer, "No indexed knowledge passages were required")

	hits := []models.KnowledgeSearchResult{{DocumentTitle: "Audit log"}}
	with := Assist("hi", nil, nil, hits, false, false)
	assert.Contains(t, with.Answer, "Retrieved 1 supporting passage(s), starting with 'Audit log'")
}

func TestTruncateAddsEllipsisOnOverflow(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "abc", truncate("abc", 10))
	assert.Equal(t, "abcde...", truncate("abcdefghi", 5))
}

func TestBuildPromptCarriesKnowledgeAndRequestContext(t *testing.T) {
	t.Parallel()
	datasetID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ontologyID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	kbID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	draft := Assist("what happened?", []uuid.UUID{datasetID}, []uuid.UUID{ontologyID}, nil, true, true)
	prompt := BuildPrompt("what happened?", draft, []uuid.UUID{datasetID}, []uuid.UUID{ontologyID}, []uuid.UUID{kbID}, []models.KnowledgeSearchResult{{DocumentTitle: "Runbook", Excerpt: "Check provider latency."}})

	assert.Contains(t, prompt, "Question: what happened?")
	assert.Contains(t, prompt, "Dataset IDs: [11111111-1111-1111-1111-111111111111]")
	assert.Contains(t, prompt, "Ontology type IDs: [22222222-2222-2222-2222-222222222222]")
	assert.Contains(t, prompt, "Knowledge base IDs: [33333333-3333-3333-3333-333333333333]")
	assert.Contains(t, prompt, "Suggested SQL:")
	assert.Contains(t, prompt, "- Runbook: Check provider latency.")
}
