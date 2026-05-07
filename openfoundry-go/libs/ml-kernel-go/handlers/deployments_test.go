package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

func TestCreateDeployment_RejectsEmptyName(t *testing.T) {
	t.Parallel()
	h := &DeploymentsHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"  ","endpoint_path":"/api/v1/predict"}`))
	w := httptest.NewRecorder()
	h.CreateDeployment(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "deployment name and endpoint path are required", body.Error)
}

func TestCreateDeployment_RejectsBadJSON(t *testing.T) {
	t.Parallel()
	h := &DeploymentsHandlers{Pool: nil}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	h.CreateDeployment(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNormaliseTrafficSplit_RequiresAtLeastOneEntry(t *testing.T) {
	t.Parallel()
	_, err := normaliseTrafficSplit("ab_test", nil)
	require.Error(t, err)
	assert.Equal(t, "at least one traffic split entry is required", err.Error())
}

func TestNormaliseTrafficSplit_SingleStrategyCollapses(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	got, err := normaliseTrafficSplit("single", []models.TrafficSplitEntry{
		{ModelVersionID: id, Allocation: 30},
		{ModelVersionID: uuid.New(), Allocation: 70},
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, uint8(100), got[0].Allocation)
	assert.Equal(t, id, got[0].ModelVersionID)
}

func TestNormaliseTrafficSplit_AssignsDefaultLabel(t *testing.T) {
	t.Parallel()
	got, err := normaliseTrafficSplit("ab_test", []models.TrafficSplitEntry{
		{ModelVersionID: uuid.New(), Label: "  ", Allocation: 50},
		{ModelVersionID: uuid.New(), Label: "champion", Allocation: 50},
	})
	require.NoError(t, err)
	assert.Equal(t, "variant-1", got[0].Label)
	assert.Equal(t, "champion", got[1].Label)
}

func TestNormaliseTrafficSplit_ABTestRebalancesTo100(t *testing.T) {
	t.Parallel()
	got, err := normaliseTrafficSplit("ab_test", []models.TrafficSplitEntry{
		{ModelVersionID: uuid.New(), Label: "a", Allocation: 30},
		{ModelVersionID: uuid.New(), Label: "b", Allocation: 30},
		{ModelVersionID: uuid.New(), Label: "c", Allocation: 30},
	})
	require.NoError(t, err)
	var total uint32
	for _, s := range got {
		total += uint32(s.Allocation)
	}
	assert.Equal(t, uint32(100), total, "ab_test allocations always sum to 100")
}

func TestNormaliseTrafficSplit_ABTestRejectsZeroTotal(t *testing.T) {
	t.Parallel()
	_, err := normaliseTrafficSplit("ab_test", []models.TrafficSplitEntry{
		{ModelVersionID: uuid.New(), Label: "a", Allocation: 0},
	})
	require.Error(t, err)
	assert.Equal(t, "traffic allocation must be greater than zero", err.Error())
}
