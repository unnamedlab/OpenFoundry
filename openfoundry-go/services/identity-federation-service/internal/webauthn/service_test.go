package webauthn_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/webauthn"
)

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("WEBAUTHN_RP_NAME", "")
	t.Setenv("WEBAUTHN_RP_ID", "")
	t.Setenv("WEBAUTHN_RP_ORIGIN", "")
	rp := webauthn.FromEnv()
	assert.Equal(t, "OpenFoundry", rp.DisplayName)
	assert.Equal(t, "localhost", rp.ID)
	assert.Equal(t, []string{"http://localhost:5173"}, rp.Origins)
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("WEBAUTHN_RP_NAME", "Acme")
	t.Setenv("WEBAUTHN_RP_ID", "auth.acme.dev")
	t.Setenv("WEBAUTHN_RP_ORIGIN", "https://acme.dev,https://staging.acme.dev")
	rp := webauthn.FromEnv()
	assert.Equal(t, "Acme", rp.DisplayName)
	assert.Equal(t, "auth.acme.dev", rp.ID)
	assert.Equal(t, []string{"https://acme.dev", "https://staging.acme.dev"}, rp.Origins)
}

// NewService should accept a valid RP config without erroring.
func TestNewServiceAcceptsValidConfig(t *testing.T) {
	t.Parallel()
	rp := webauthn.RelyingPartyConfig{
		DisplayName: "OpenFoundry",
		ID:          "localhost",
		Origins:     []string{"http://localhost:5173"},
	}
	// nil store is fine for construction; ceremony tests live behind
	// integration build tags (need a Postgres + a real client).
	_, err := webauthn.NewService(rp, nil)
	assert.NoError(t, err)
}
