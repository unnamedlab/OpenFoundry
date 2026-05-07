package serving

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

func TestHTTPDeploymentRuntimeDeployAndTransition(t *testing.T) {
	t.Parallel()
	deploymentID := uuid.New()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		var body deploymentRuntimeRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, deploymentID, body.Deployment.ID)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	runtime := NewHTTPDeploymentRuntime(srv.URL, srv.Client())
	deployment := models.ModelDeployment{ID: deploymentID, Name: "fraud"}

	require.NoError(t, runtime.Deploy(context.Background(), deployment))
	require.NoError(t, runtime.Transition(context.Background(), deployment, "paused"))

	assert.Equal(t, []string{"/deployments", "/deployments/" + deploymentID.String() + "/transition"}, paths)
}

func TestHTTPDeploymentRuntimeBackendError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "backend down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	runtime := NewHTTPDeploymentRuntime(srv.URL, srv.Client())

	err := runtime.Deploy(context.Background(), models.ModelDeployment{ID: uuid.New()})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "serving backend returned 503")
}

func TestUnavailableDeploymentRuntimeWrapsSentinel(t *testing.T) {
	t.Parallel()
	runtime := UnavailableDeploymentRuntime{Reason: "missing backend"}

	err := runtime.Deploy(context.Background(), models.ModelDeployment{})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRuntimeUnavailable))
}
