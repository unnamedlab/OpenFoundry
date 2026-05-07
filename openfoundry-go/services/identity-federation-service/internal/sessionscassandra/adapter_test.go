package sessionscassandra_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/sessionscassandra"
)

func TestHashRefreshTokenIsDeterministic(t *testing.T) {
	t.Parallel()
	a := sessionscassandra.HashRefreshToken("token-x")
	b := sessionscassandra.HashRefreshToken("token-x")
	c := sessionscassandra.HashRefreshToken("token-y")
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
	assert.Len(t, a, 32, "sha-256 raw digest is 32 bytes")
}

func TestMigrationsArePinned(t *testing.T) {
	t.Parallel()
	require := assert.New(t)
	require.Len(sessionscassandra.Migrations, 2,
		"slice 2 ports user_session + refresh_token only")
	require.Equal("0001_user_session", sessionscassandra.Migrations[0].Name)
	require.Equal("0002_refresh_token", sessionscassandra.Migrations[1].Name)
	for _, m := range sessionscassandra.Migrations {
		require.Contains(m.DDL, "auth_runtime.",
			"every DDL targets the auth_runtime keyspace")
		require.Contains(m.DDL, "IF NOT EXISTS",
			"every DDL is idempotent")
	}
}
