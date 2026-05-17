package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseListSchedulesQueryDiscoveryFilters(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/schedules?files=ri.dataset.1&users=alice&projects=ri.project.1&paused=true&q=nightly&branch=release&latest_outcome=failed&sort=last_run_at&limit=25&offset=50", nil)

	query := parseListSchedulesQuery(req)

	require.NotNil(t, query.Paused)
	require.True(t, *query.Paused)
	require.Equal(t, []string{"ri.dataset.1"}, query.Files)
	require.Equal(t, []string{"alice"}, query.Users)
	require.Equal(t, []string{"ri.project.1"}, query.Projects)
	require.Equal(t, "nightly", query.Q)
	require.Equal(t, "release", query.Branch)
	require.Equal(t, "FAILED", query.LatestOutcome)
	require.Equal(t, "last_run_at", query.Sort)
	require.Equal(t, int64(25), query.Limit)
	require.Equal(t, int64(50), query.Offset)
}

func TestParseListSchedulesQueryAcceptsLastRunOutcomeAlias(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/schedules?last_run_outcome=NEVER", nil)

	query := parseListSchedulesQuery(req)

	require.Equal(t, "NEVER", query.LatestOutcome)
}
