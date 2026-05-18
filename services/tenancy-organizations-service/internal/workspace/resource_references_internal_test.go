package workspace

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestDedupeReferenceRowsKeepsOneDirectedEdge(t *testing.T) {
	t.Parallel()
	sourceID := uuid.New()
	targetID := uuid.New()
	rows := []referenceRow{
		{
			SourceKind:   ResourceDashboard,
			SourceID:     sourceID,
			TargetKind:   ResourceQuery,
			TargetID:     targetID,
			Relationship: "reads",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
		{
			SourceKind:   ResourceDashboard,
			SourceID:     sourceID,
			TargetKind:   ResourceQuery,
			TargetID:     targetID,
			Relationship: "reads",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
		{
			SourceKind:   ResourceDashboard,
			SourceID:     sourceID,
			TargetKind:   ResourceQuery,
			TargetID:     targetID,
			Relationship: "embeds",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
	}

	deduped := dedupeReferenceRows(rows)

	assert.Len(t, deduped, 2)
}

func TestResourceRIDForKindUsesCanonicalNamespaces(t *testing.T) {
	t.Parallel()
	id := uuid.New()

	assert.Equal(t, "ri.foundry.main.query."+id.String(), resourceRIDForKind(ResourceQuery, id))
	assert.Equal(t, "ri.foundry.main.dashboard."+id.String(), resourceRIDForKind(ResourceDashboard, id))
	assert.Equal(t, "ri.compass.main.project."+id.String(), resourceRIDForKind(ResourceOntologyProject, id))
}
