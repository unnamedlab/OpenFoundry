package adapters_test

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

// fakeAdapter is the in-test [adapters.ConnectorAdapter] fixture used by
// the registry tests. Each capability returns [adapters.ErrNotImplemented]
// unless the test explicitly overrides it, which mirrors the behaviour
// CMA-14 will mount for skeleton connectors.
type fakeAdapter struct {
	name string
}

func (f *fakeAdapter) DiscoverSources(context.Context, *models.Connection, string) ([]adapters.Source, error) {
	return nil, adapters.ErrNotImplemented
}

func (f *fakeAdapter) QueryVirtualTable(context.Context, *models.Connection, *adapters.Query, string) (*adapters.Result, error) {
	return nil, adapters.ErrNotImplemented
}

func (f *fakeAdapter) StreamArrow(context.Context, *models.Connection, *adapters.Query, string) (adapters.ArrowStream, error) {
	return nil, adapters.ErrNotImplemented
}

func (f *fakeAdapter) BuildIngestSpec(context.Context, *models.Connection, *adapters.Source) (*adapters.IngestSpec, error) {
	return nil, adapters.ErrNotImplemented
}

func TestRegistryRegisterAndLookup(t *testing.T) {
	r := adapters.NewRegistry()

	require.NoError(t, r.Register("postgresql", adapters.SingletonFactory(&fakeAdapter{name: "postgresql"})))
	require.True(t, r.Has("postgresql"))

	got, err := r.Lookup("postgresql")
	require.NoError(t, err)
	require.IsType(t, &fakeAdapter{}, got)
	require.Equal(t, "postgresql", got.(*fakeAdapter).name)
}

func TestRegistryRegisterRejectsEmptyTypeAndNilFactory(t *testing.T) {
	r := adapters.NewRegistry()

	require.Error(t, r.Register("", adapters.SingletonFactory(&fakeAdapter{})))
	require.Error(t, r.Register("bigquery", nil))
}

func TestRegistryRegisterRejectsDuplicate(t *testing.T) {
	r := adapters.NewRegistry()
	require.NoError(t, r.Register("kafka", adapters.SingletonFactory(&fakeAdapter{name: "first"})))

	err := r.Register("kafka", adapters.SingletonFactory(&fakeAdapter{name: "second"}))
	require.ErrorIs(t, err, adapters.ErrAlreadyRegistered)
	require.Contains(t, err.Error(), "kafka")

	got, err := r.Lookup("kafka")
	require.NoError(t, err)
	require.Equal(t, "first", got.(*fakeAdapter).name)
}

func TestRegistryGetReportsAdapterNotFound(t *testing.T) {
	r := adapters.NewRegistry()

	_, err := r.Get("does-not-exist")
	require.ErrorIs(t, err, adapters.ErrAdapterNotFound)
	require.Contains(t, err.Error(), "does-not-exist")

	_, err = r.Lookup("does-not-exist")
	require.ErrorIs(t, err, adapters.ErrAdapterNotFound)
}

func TestRegistryUnregister(t *testing.T) {
	r := adapters.NewRegistry()
	require.NoError(t, r.Register("snowflake", adapters.SingletonFactory(&fakeAdapter{name: "snowflake"})))

	require.NoError(t, r.Unregister("snowflake"))
	require.False(t, r.Has("snowflake"))
	require.ErrorIs(t, r.Unregister("snowflake"), adapters.ErrAdapterNotFound)
}

func TestRegistryNamesAreSortedAndStable(t *testing.T) {
	r := adapters.NewRegistry()
	for _, name := range []string{"snowflake", "bigquery", "postgresql", "kafka"} {
		require.NoError(t, r.Register(name, adapters.SingletonFactory(&fakeAdapter{name: name})))
	}

	require.Equal(t, []string{"bigquery", "kafka", "postgresql", "snowflake"}, r.Names())
}

func TestRegistryMustRegisterPanicsOnDuplicate(t *testing.T) {
	r := adapters.NewRegistry()
	r.MustRegister("oracle", adapters.SingletonFactory(&fakeAdapter{name: "oracle"}))

	defer func() {
		v := recover()
		require.NotNil(t, v, "MustRegister must panic on duplicate")
		err, ok := v.(error)
		require.True(t, ok, "MustRegister must panic with an error value")
		require.ErrorIs(t, err, adapters.ErrAlreadyRegistered)
	}()
	r.MustRegister("oracle", adapters.SingletonFactory(&fakeAdapter{name: "oracle-dup"}))
}

func TestRegistryConcurrentRegisterAndLookup(t *testing.T) {
	r := adapters.NewRegistry()
	const workers = 16

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			name := connectorName(i)
			_ = r.Register(name, adapters.SingletonFactory(&fakeAdapter{name: name}))
			_, _ = r.Get(name)
			_ = r.Has(name)
			_ = r.Names()
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		require.True(t, r.Has(connectorName(i)))
	}
}

func connectorName(i int) string {
	return "fake-" + string(rune('a'+i%26))
}

func TestFactoryFuncReturnsFreshInstances(t *testing.T) {
	calls := 0
	f := adapters.FactoryFunc(func() adapters.ConnectorAdapter {
		calls++
		return &fakeAdapter{name: "fresh"}
	})

	a1 := f.New()
	a2 := f.New()
	require.NotNil(t, a1)
	require.NotNil(t, a2)
	require.Equal(t, 2, calls)
}

func TestEmptyArrowStreamReturnsEOF(t *testing.T) {
	var s adapters.ArrowStream = adapters.EmptyArrowStream{}
	chunk, err := s.Next(context.Background())
	require.Nil(t, chunk)
	require.ErrorIs(t, err, io.EOF)
	require.NoError(t, s.Close())
}

func TestDefaultCapabilityMatrixDocumentsImplementedAndMissingCapabilities(t *testing.T) {
	r := adapters.NewRegistry()
	matrix := r.CapabilityMatrix([]string{"mysql", "ldap", "oracle"})
	byType := map[string]models.ConnectorCapabilityMatrix{}
	for _, capability := range matrix {
		byType[capability.ConnectorType] = capability
	}

	mysql := byType["mysql"]
	require.True(t, mysql.DiscoverSources)
	require.True(t, mysql.QueryVirtualTable)
	require.True(t, mysql.StreamArrow)
	require.True(t, mysql.BuildIngestSpec)
	require.Empty(t, mysql.Limitations)

	ldap := byType["ldap"]
	require.False(t, ldap.DiscoverSources)
	require.False(t, ldap.QueryVirtualTable)
	require.False(t, ldap.StreamArrow)
	require.False(t, ldap.BuildIngestSpec)
	require.NotEmpty(t, ldap.Limitations)

	oracle := byType["oracle"]
	require.True(t, oracle.DiscoverSources)
	require.True(t, oracle.QueryVirtualTable)
	require.False(t, oracle.StreamArrow)
	require.False(t, oracle.BuildIngestSpec)
	require.NotEmpty(t, oracle.Limitations)
}
