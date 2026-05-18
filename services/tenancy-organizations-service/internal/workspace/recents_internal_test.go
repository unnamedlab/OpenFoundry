package workspace

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestListRecentsSQLAppliesVisibilityBeforeLimit(t *testing.T) {
	t.Parallel()

	sql := listRecentsSQL("", 2, 3)

	assert.Contains(t, sql, "FROM ontology_projects p")
	assert.Contains(t, sql, "FROM ontology_project_folders f")
	assert.Contains(t, sql, "FROM ontology_project_resources r")
	assert.Contains(t, sql, "FROM compass_resource_search_index idx")
	assert.Contains(t, sql, "idx.owning_project_id = ANY($2::uuid[])")
	assert.Contains(t, sql, "ORDER BY last_accessed_at DESC")
	assert.True(t,
		strings.Index(sql, "WHERE") < strings.LastIndex(sql, "LIMIT $3"),
		"visibility predicate must run before the recents cap is applied",
	)
}

func TestListRecentsSQLKindFilterUsesSeparateProjectParam(t *testing.T) {
	t.Parallel()

	sql := listRecentsSQL("AND resource_kind = $2", 3, 4)

	assert.Contains(t, sql, "AND resource_kind = $2")
	assert.Contains(t, sql, "p.id = ANY($3::uuid[])")
	assert.Contains(t, sql, "LIMIT $4")
}
