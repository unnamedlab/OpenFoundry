package domain

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
	"github.com/stretchr/testify/require"
)

func TestGenerateAccountSASMatchesRustGolden(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("supersecretkey1234567890123456"))
	sas, err := GenerateAccountSAS("acct", key, "rl", 1_900_000_000_000)
	require.NoError(t, err)
	require.Equal(t, "sv=2022-11-02&ss=bf&srt=co&sp=rl&se=2030-03-17T17%3A46%3A40Z&spr=https&sig=iudTSQUdzidNXBkMLCXKVHxzVGIb3BTosPVTAemVx7s%3D", sas)
}

func TestGenerateServiceSASMatchesRustGolden(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("supersecretkey1234567890123456"))
	sas, err := GenerateServiceSASContainer("acct", key, "warehouse", "rl", 1_900_000_000_000)
	require.NoError(t, err)
	require.Equal(t, "sv=2022-11-02&sr=c&sp=rl&se=2030-03-17T17%3A46%3A40Z&spr=https&sig=qkLbmou1jnhW2wkVUHrT2eOJtr7P9h8Ozm%2BvyfaPXpg%3D", sas)
}

func TestVendCredentialsStaticS3(t *testing.T) {
	cfg, _ := json.Marshal(map[string]any{"region": "us-east-1", "endpoint": "http://minio:9000", "path_style": true, "access_key_id": "ak", "secret_access_key": "sk", "session_token": "tok"})
	conn := &models.Connection{ID: uuid.New(), ConnectorType: "s3", Config: cfg}
	vended := VendCredentials(conn, 900, time.UnixMilli(1_700_000_000_000))
	require.Equal(t, int64(1_700_000_900_000), vended.ExpiresAtMS)
	require.Equal(t, "1700000900000", vended.Entries["expires-at-ms"])
	require.Equal(t, "us-east-1", vended.Entries["s3.region"])
	require.Equal(t, "us-east-1", vended.Entries["client.region"])
	require.Equal(t, "http://minio:9000", vended.Entries["s3.endpoint"])
	require.Equal(t, "true", vended.Entries["s3.path-style-access"])
	require.Equal(t, "ak", vended.Entries["s3.access-key-id"])
	require.Equal(t, "sk", vended.Entries["s3.secret-access-key"])
	require.Equal(t, "tok", vended.Entries["s3.session-token"])
}

func TestVendCredentialsAzurePrefersContainerSAS(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("supersecretkey1234567890123456"))
	cfg, _ := json.Marshal(map[string]any{"account_name": "acct", "account_key": key, "container_name": "warehouse"})
	conn := &models.Connection{ID: uuid.New(), ConnectorType: "azure_blob", Config: cfg}
	vended := VendCredentials(conn, 900, time.UnixMilli(1_899_999_100_000))
	require.Equal(t, "acct", vended.Entries["adls.account-name"])
	require.Equal(t, "warehouse", vended.Entries["adls.container"])
	require.Equal(t, "sv=2022-11-02&sr=c&sp=rl&se=2030-03-17T17%3A46%3A40Z&spr=https&sig=qkLbmou1jnhW2wkVUHrT2eOJtr7P9h8Ozm%2BvyfaPXpg%3D", vended.Entries["adls.sas-token"])
}
