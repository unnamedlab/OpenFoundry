package httpruntime

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// Ports `tests::blocks_hosts_outside_allowlist` from
// `services/connector-management-service/src/domain/egress.rs`.
func TestEgressBlocksHostsOutsideAllowlist(t *testing.T) {
	policy := &EgressPolicy{
		AllowedHosts:         []string{"api.example.com"},
		AllowPrivateNetworks: false,
		AllowInsecureHTTP:    false,
	}
	u, err := url.Parse("https://internal.example.net/v1/data")
	require.NoError(t, err)
	require.Error(t, ValidateURL(u, policy))
}

// Ports `tests::allows_wildcard_subdomains`.
func TestEgressAllowsWildcardSubdomains(t *testing.T) {
	policy := &EgressPolicy{
		AllowedHosts:         []string{"*.example.com"},
		AllowPrivateNetworks: false,
		AllowInsecureHTTP:    false,
	}
	u, err := url.Parse("https://sales.eu.example.com/odata")
	require.NoError(t, err)
	require.NoError(t, ValidateURL(u, policy))
}

// Ports `tests::blocks_private_network_when_disabled`.
func TestEgressBlocksPrivateNetworkWhenDisabled(t *testing.T) {
	policy := &EgressPolicy{
		AllowPrivateNetworks: false,
		AllowInsecureHTTP:    true,
	}
	u, err := url.Parse("http://10.0.0.4/feed")
	require.NoError(t, err)
	require.Error(t, ValidateURL(u, policy))
}

func TestEgressBlocksInsecureHTTPToPublicHost(t *testing.T) {
	policy := &EgressPolicy{
		AllowedHosts:         []string{"api.example.com"},
		AllowPrivateNetworks: true,
		AllowInsecureHTTP:    false,
	}
	u, err := url.Parse("http://api.example.com/v1")
	require.NoError(t, err)
	err = ValidateURL(u, policy)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insecure HTTP")
}

func TestEgressAllowsLocalhostHTTP(t *testing.T) {
	policy := &EgressPolicy{
		AllowPrivateNetworks: false,
		AllowInsecureHTTP:    false,
	}
	u, err := url.Parse("http://127.0.0.1:8080/probe")
	require.NoError(t, err)
	require.NoError(t, ValidateURL(u, policy))
}

func TestEgressBlockedHostsTrumpAllowed(t *testing.T) {
	policy := &EgressPolicy{
		AllowedHosts:         []string{"*.example.com"},
		BlockedHosts:         []string{"sales.eu.example.com"},
		AllowPrivateNetworks: false,
		AllowInsecureHTTP:    false,
	}
	u, err := url.Parse("https://sales.eu.example.com/odata")
	require.NoError(t, err)
	require.Error(t, ValidateURL(u, policy))
}

func TestEgressPolicyFromStateMergesConfigOverrides(t *testing.T) {
	policy := EgressPolicyFromState(
		[]string{"api.upstream.example.com"},
		false,
		map[string]any{
			"egress": map[string]any{
				"allowed_hosts":          []any{"*.override.example.com"},
				"blocked_hosts":          []any{"bad.override.example.com"},
				"allow_private_networks": true,
				"allow_insecure_http":    true,
			},
		},
	)
	require.Equal(t, []string{"*.override.example.com"}, policy.AllowedHosts)
	require.Equal(t, []string{"bad.override.example.com"}, policy.BlockedHosts)
	require.True(t, policy.AllowPrivateNetworks)
	require.True(t, policy.AllowInsecureHTTP)
}

func TestEgressPolicyFromStateFallsBackToStateDefaults(t *testing.T) {
	policy := EgressPolicyFromState(
		[]string{"api.upstream.example.com"},
		true,
		nil,
	)
	require.Equal(t, []string{"api.upstream.example.com"}, policy.AllowedHosts)
	require.Empty(t, policy.BlockedHosts)
	require.True(t, policy.AllowPrivateNetworks)
	require.False(t, policy.AllowInsecureHTTP)
}
