package handlers

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

func TestPromoteEventTypeAndTopicConstants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "foundry.global.branch.promote.requested.v1", PromoteTopic)
	assert.Equal(t, "global.branch.promote.requested.v1", PromoteEventType)
}

func TestBuildPromotePayloadShape(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	raw, err := buildPromotePayload(id, "release-2026Q2", "alice")
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	assert.Equal(t, PromoteEventType, body["event_type"])
	assert.Equal(t, id.String(), body["global_branch_id"])
	assert.Equal(t, "release-2026Q2", body["global_branch_name"])
	assert.Equal(t, "alice", body["actor"])
	assert.Contains(t, body, "occurred_at")
}

func TestGitHTTPAcceptsBasicPasswordOIDCToken(t *testing.T) {
	t.Parallel()
	jwt := authmw.NewJWTConfig("git-http-basic-test-secret")
	token := testTokenForGit(t, jwt)
	req := httptest.NewRequest(http.MethodGet, "/v1/code-repos/git/00000000-0000-0000-0000-000000000001.git/info/refs", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("oidc:"+token)))

	claims, ok := gitClaimsFromRequest(req, jwt)
	require.True(t, ok)
	require.Equal(t, "git-user@example.com", claims.Email)
}

func TestGitPathParsesRepositoryID(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	parsed, ok := gitRepositoryIDFromPath("/" + id.String() + ".git/info/refs")
	require.True(t, ok)
	require.Equal(t, id, parsed)
}

func testTokenForGit(t *testing.T, cfg *authmw.JWTConfig) string {
	t.Helper()
	now := time.Now()
	accessUse := "access"
	tok, err := authmw.EncodeToken(cfg, &authmw.Claims{
		Sub:      uuid.New(),
		IAT:      now.Unix(),
		EXP:      now.Add(time.Hour).Unix(),
		JTI:      uuid.New(),
		Email:    "git-user@example.com",
		Name:     "Git User",
		Roles:    []string{"developer"},
		TokenUse: &accessUse,
	})
	require.NoError(t, err)
	return tok
}
