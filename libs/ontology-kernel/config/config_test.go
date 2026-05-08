package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// libs/ontology-kernel/src/config.rs `default_*()` helpers — every
// defaulted field carries the verbatim Rust string / numeric value.
func TestAppConfigDefaults(t *testing.T) {
	d := Default()
	assert.Equal(t, "0.0.0.0", d.Host)
	assert.Equal(t, uint16(50057), d.Port)
	assert.Equal(t, "http://localhost:50070", d.AuditServiceURL)
	assert.Equal(t, "http://localhost:50079", d.DatasetServiceURL)
	assert.Equal(t, "http://localhost:50057", d.OntologyServiceURL)
	assert.Equal(t, "http://localhost:50081", d.PipelineServiceURL)
	assert.Equal(t, "http://localhost:50127", d.AIServiceURL)
	assert.Equal(t, "deterministic-hash", d.SearchEmbeddingProvider)
	assert.Equal(t, "http://localhost:50114", d.NotificationServiceURL)
	assert.Equal(t, "node", d.NodeRuntimeCommand)
	assert.Equal(t, "http://localhost:50130", d.ConnectorManagementServiceURL)
}

// libs/ontology-kernel/src/config.rs `AppConfig::from_env()` —
// missing required field rejects the build (same as `try_deserialize`
// failing on a missing non-Option field).
func TestFromEnvRequiresDatabaseURL(t *testing.T) {
	get := func(key string) string {
		if key == "JWT_SECRET" {
			return "shh"
		}
		return ""
	}
	_, err := FromGetenv(get)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL")
}

// libs/ontology-kernel/src/config.rs `AppConfig::from_env()` —
// missing JWT_SECRET also rejects the build.
func TestFromEnvRequiresJWTSecret(t *testing.T) {
	get := func(key string) string {
		if key == "DATABASE_URL" {
			return "postgres://x"
		}
		return ""
	}
	_, err := FromGetenv(get)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_SECRET")
}

// libs/ontology-kernel/src/config.rs — env vars override the
// defaulted fields, mirroring the `config` crate's environment-source
// layering. Required fields populate the non-defaulted slots.
func TestFromEnvLayersOverDefaults(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL":              "postgres://prod",
		"JWT_SECRET":                "abc",
		"HOST":                      "127.0.0.1",
		"PORT":                      "9999",
		"AUDIT_SERVICE_URL":         "http://audit:1",
		"AI_SERVICE_URL":            "http://ai:2",
		"SEARCH_EMBEDDING_PROVIDER": "openai",
	}
	get := func(key string) string { return env[key] }
	c, err := FromGetenv(get)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", c.Host)
	assert.Equal(t, uint16(9999), c.Port)
	assert.Equal(t, "postgres://prod", c.DatabaseURL)
	assert.Equal(t, "abc", c.JWTSecret)
	assert.Equal(t, "http://audit:1", c.AuditServiceURL)
	assert.Equal(t, "http://ai:2", c.AIServiceURL)
	assert.Equal(t, "openai", c.SearchEmbeddingProvider)
	// Non-overridden fields keep their defaults.
	assert.Equal(t, "http://localhost:50081", c.PipelineServiceURL)
	assert.Equal(t, "node", c.NodeRuntimeCommand)
}

// libs/ontology-kernel/src/config.rs — PORT must parse as u16 in
// [0, 65535]. Out-of-range or non-numeric values reject.
func TestFromEnvRejectsBadPort(t *testing.T) {
	cases := []string{"abc", "70000", "-1"}
	for _, p := range cases {
		get := func(key string) string {
			switch key {
			case "DATABASE_URL":
				return "postgres://x"
			case "JWT_SECRET":
				return "y"
			case "PORT":
				return p
			}
			return ""
		}
		_, err := FromGetenv(get)
		require.Error(t, err, "want error for PORT=%q", p)
		assert.Contains(t, err.Error(), "PORT")
	}
}
