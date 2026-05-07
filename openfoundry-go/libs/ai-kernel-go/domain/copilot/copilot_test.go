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
