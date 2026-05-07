// Package copilot hosts the deterministic copilot draft helper —
// pure-logic mirror of libs/ai-kernel/src/domain/copilot.rs. Returns
// the SQL / pipeline / ontology suggestions the chat handler folds
// into a CopilotResponse when the runtime port lands.
package copilot

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

type Draft struct {
	Answer              string
	SuggestedSQL        *string
	PipelineSuggestions []string
	OntologyHints       []string
}

func Assist(
	question string,
	datasetIDs []uuid.UUID,
	ontologyTypeIDs []uuid.UUID,
	knowledgeHits []models.KnowledgeSearchResult,
	includeSQL, includePipelinePlan bool,
) Draft {
	lowered := strings.ToLower(question)
	var firstDataset *uuid.UUID
	if len(datasetIDs) > 0 {
		v := datasetIDs[0]
		firstDataset = &v
	}
	var firstOntologyType *uuid.UUID
	if len(ontologyTypeIDs) > 0 {
		v := ontologyTypeIDs[0]
		firstOntologyType = &v
	}

	var suggestedSQL *string
	if includeSQL {
		if firstDataset != nil {
			s := fmt.Sprintf(
				"SELECT *\nFROM dataset_%s\nWHERE event_date >= CURRENT_DATE - INTERVAL '30 days'\nORDER BY event_date DESC\nLIMIT 100;",
				simpleUUID(*firstDataset),
			)
			suggestedSQL = &s
		} else if strings.Contains(lowered, "sql") || strings.Contains(lowered, "query") {
			s := "SELECT *\nFROM your_dataset\nWHERE created_at >= CURRENT_DATE - INTERVAL '7 days';"
			suggestedSQL = &s
		}
	}

	var pipelineSuggestions []string
	if includePipelinePlan {
		pipelineSuggestions = []string{
			"Profile the incoming source and verify schema drift before inference.",
			"Materialize embeddings and retrieval chunks as a scheduled upstream step.",
			"Add a guardrail validation node before publishing generated outputs.",
		}
	}

	ontologyHints := make([]string, 0, 2)
	if firstOntologyType != nil {
		ontologyHints = append(ontologyHints, fmt.Sprintf(
			"Map the response to ontology type %s for downstream actioning.",
			simpleUUID(*firstOntologyType),
		))
	}
	if strings.Contains(lowered, "ontology") || strings.Contains(lowered, "object") {
		ontologyHints = append(ontologyHints,
			"Prefer stable object identifiers and link types when grounding answers.")
	}

	knowledgeSummary := "No indexed knowledge passages were required for this answer."
	if len(knowledgeHits) > 0 {
		knowledgeSummary = fmt.Sprintf(
			"Retrieved %d supporting passage(s), starting with '%s'.",
			len(knowledgeHits), knowledgeHits[0].DocumentTitle,
		)
	}

	return Draft{
		Answer: fmt.Sprintf(
			"Copilot reviewed the request '%s'. %s Focus the next action on the most recent operational signal and keep the response ready for human verification.",
			truncate(question, 140), knowledgeSummary,
		),
		SuggestedSQL:        suggestedSQL,
		PipelineSuggestions: pipelineSuggestions,
		OntologyHints:       ontologyHints,
	}
}

func truncate(content string, limit int) string {
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	return string(runes[:limit]) + "..."
}

// simpleUUID mirrors Rust Uuid::simple() — 32 hex chars without
// hyphens. Lowercase, matching the Rust default formatting.
func simpleUUID(id uuid.UUID) string {
	s := id.String()
	return strings.ReplaceAll(s, "-", "")
}
