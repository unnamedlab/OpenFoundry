//go:build cassandra_integration
// +build cassandra_integration

package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	cassandrakernel "github.com/openfoundry/openfoundry-go/libs/cassandra-kernel"
	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
	"github.com/openfoundry/openfoundry-go/services/ontology-query-service/internal/handlers"
)

func TestGetObjectWithCassandraStore(t *testing.T) {
	if os.Getenv("CASSANDRA_CONTACT_POINTS") == "" {
		t.Skip("set CASSANDRA_CONTACT_POINTS and run with -tags=cassandra_integration")
	}
	cluster, err := cassandrakernel.FromEnv()
	require.NoError(t, err)
	session, err := cluster.Connect()
	require.NoError(t, err)
	defer session.Close()

	store := cassandrakernel.NewObjectStore(session)
	tenant := uuid.NewString()
	objectID := uuid.NewString()
	owner := repos.OwnerId(uuid.NewString())
	now := time.Now().UnixMilli()
	_, err = store.Put(context.Background(), repos.Object{
		Tenant:      repos.TenantId(tenant),
		ID:          repos.ObjectId(objectID),
		TypeID:      repos.TypeId("integration_aircraft"),
		Payload:     json.RawMessage(`{"ok":true}`),
		CreatedAtMs: &now,
		UpdatedAtMs: now,
		Owner:       &owner,
		Markings:    []repos.MarkingId{"PUBLIC"},
	}, nil)
	require.NoError(t, err)

	h := handlers.New(handlers.AppState{Objects: store})
	req := authedReq("GET", "/objects/"+tenant+"/"+objectID, map[string]string{
		"tenant": tenant, "object_id": objectID,
	}, &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}})
	rec := httptest.NewRecorder()
	h.GetObject(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
