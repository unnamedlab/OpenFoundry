package graphql

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

func TestCapabilitiesReturnNotImplemented(t *testing.T) {
	a := New()
	conn := &models.Connection{}
	query := &adapters.Query{}
	source := &adapters.Source{}

	_, err := a.DiscoverSources(context.Background(), conn, "")
	require.True(t, errors.Is(err, adapters.ErrNotImplemented))

	_, err = a.QueryVirtualTable(context.Background(), conn, query, "")
	require.True(t, errors.Is(err, adapters.ErrNotImplemented))

	_, err = a.StreamArrow(context.Background(), conn, query, "")
	require.True(t, errors.Is(err, adapters.ErrNotImplemented))

	_, err = a.BuildIngestSpec(context.Background(), conn, source)
	require.True(t, errors.Is(err, adapters.ErrNotImplemented))
}

func TestFactoryProducesAdapter(t *testing.T) {
	a := Factory().New()
	require.NotNil(t, a)
	_, ok := a.(*Adapter)
	require.True(t, ok)
}
