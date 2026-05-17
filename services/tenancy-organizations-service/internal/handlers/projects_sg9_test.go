package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
)

func TestAccessRequestWorkflowStatusSummary(t *testing.T) {
	t.Parallel()
	assert.Equal(t, models.ProjectAccessRequestStatusPending,
		summarizeAccessRequestTaskStatuses([]models.ProjectAccessRequestTask{{
			Status: models.ProjectAccessRequestTaskStatusReview,
		}}))
	assert.Equal(t, models.ProjectAccessRequestStatusDenied,
		summarizeAccessRequestTaskStatuses([]models.ProjectAccessRequestTask{{
			Status: models.ProjectAccessRequestTaskStatusRejected,
		}}))
	assert.Equal(t, models.ProjectAccessRequestStatusActionRequired,
		summarizeAccessRequestTaskStatuses([]models.ProjectAccessRequestTask{{
			Status: models.ProjectAccessRequestTaskStatusActionRequired,
		}}))
	assert.Equal(t, models.ProjectAccessRequestStatusCompleted,
		summarizeAccessRequestTaskStatuses([]models.ProjectAccessRequestTask{{
			Status: models.ProjectAccessRequestTaskStatusCompleted,
		}}))
	assert.Equal(t, models.ProjectAccessRequestStatusApproved,
		summarizeAccessRequestTaskStatuses([]models.ProjectAccessRequestTask{{
			Status: models.ProjectAccessRequestTaskStatusApproved,
		}, {
			Status: models.ProjectAccessRequestTaskStatusCompleted,
		}}))
}

func TestNormalizeAccessRequestType(t *testing.T) {
	t.Parallel()
	got, err := normalizeAccessRequestType(models.ProjectAccessRequestTypeAdditionalProjectAccess)
	require.NoError(t, err)
	assert.Equal(t, models.ProjectAccessRequestTypeAdditionalProjectAccess, got)

	_, err = normalizeAccessRequestType("other")
	require.Error(t, err)
	var typed accessWorkflowHTTPError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, http.StatusBadRequest, typed.status)
}

func TestUUIDListFromJSON(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	raw, err := json.Marshal([]uuid.UUID{id})
	require.NoError(t, err)
	got, err := uuidListFromJSON(raw)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, id, got[0])
}
