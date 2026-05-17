package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

type fakeScopedRepo struct {
	user   *models.User
	groups []models.Group
	roles  []models.Role
}

func (f *fakeScopedRepo) FindUserByID(context.Context, uuid.UUID) (*models.User, error) {
	return f.user, nil
}

func (f *fakeScopedRepo) ListUserGroups(context.Context, uuid.UUID) ([]models.Group, error) {
	return f.groups, nil
}

func (f *fakeScopedRepo) ListUserRoles(context.Context, uuid.UUID) ([]models.Role, error) {
	return f.roles, nil
}

type fakeScopedIssuer struct {
	scope *authmw.SessionScope
}

func (f *fakeScopedIssuer) IssueTokensWithScope(_ context.Context, _ *models.User, _ []string, scope *authmw.SessionScope) (string, string, error) {
	f.scope = scope
	return "access-token", "refresh-token", nil
}

func (f *fakeScopedIssuer) AccessTokenTTL() time.Duration { return time.Hour }

func TestScopedSessionOptionsLimitPresetsByRequiredMarkings(t *testing.T) {
	userID := uuid.New()
	panel := NewControlPanel()
	panel.settings.ScopedSessions = ScopedSessionConfig{
		Enabled:              true,
		AllowNoScopedSession: false,
		AlwaysShowSelector:   true,
		Presets: []ScopedSessionPreset{
			{ID: "public", Name: "Public", RequiredMarkings: []string{"public"}, AllowedMarkings: []string{"public"}, Enabled: true},
			{ID: "pii", Name: "PII", RequiredMarkings: []string{"public", "pii"}, AllowedMarkings: []string{"public", "pii"}, Enabled: true},
		},
	}
	repo := &fakeScopedRepo{user: &models.User{
		ID:         userID,
		Email:      "ana@example.com",
		Name:       "Ana",
		IsActive:   true,
		Attributes: json.RawMessage(`{"allowed_markings":["public"]}`),
	}}
	handler := NewScopedSessions(panel, repo, &fakeScopedIssuer{})
	req := httptest.NewRequest(http.MethodGet, "/auth/scoped-sessions", nil).
		WithContext(authmw.ContextWithClaims(context.Background(), &authmw.Claims{Sub: userID, Email: "ana@example.com"}))
	rec := httptest.NewRecorder()

	handler.Options(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var out ScopedSessionOptionsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	require.Len(t, out.Presets, 2)
	assert.True(t, out.Presets[0].Selectable)
	assert.False(t, out.Presets[1].Selectable)
	assert.Equal(t, []string{"pii"}, out.Presets[1].MissingMarkings)
	assert.False(t, out.NoScopedSessionAvailable)
}

func TestScopedSessionSelectIssuesScopedToken(t *testing.T) {
	userID := uuid.New()
	panel := NewControlPanel()
	panel.settings.ScopedSessions = ScopedSessionConfig{
		Enabled:              true,
		AllowNoScopedSession: true,
		Presets: []ScopedSessionPreset{
			{ID: "pii", Name: "PII", RequiredMarkings: []string{"public", "pii"}, AllowedMarkings: []string{"public", "pii"}, Enabled: true},
		},
	}
	repo := &fakeScopedRepo{user: &models.User{
		ID:         userID,
		Email:      "ana@example.com",
		Name:       "Ana",
		IsActive:   true,
		Attributes: json.RawMessage(`{"allowed_markings":["public","pii"]}`),
	}}
	issuer := &fakeScopedIssuer{}
	handler := NewScopedSessions(panel, repo, issuer)
	req := httptest.NewRequest(http.MethodPost, "/auth/scoped-sessions/select", strings.NewReader(`{"preset_id":"pii"}`)).
		WithContext(authmw.ContextWithClaims(context.Background(), &authmw.Claims{Sub: userID, Email: "ana@example.com"}))
	rec := httptest.NewRecorder()

	handler.Select(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, issuer.scope)
	assert.Equal(t, []string{"public", "pii"}, issuer.scope.AllowedMarkings)
}

func TestNoScopedSessionIssuesExplicitFullMarkingScope(t *testing.T) {
	userID := uuid.New()
	panel := NewControlPanel()
	panel.settings.ScopedSessions = ScopedSessionConfig{
		Enabled:              true,
		AllowNoScopedSession: true,
	}
	repo := &fakeScopedRepo{user: &models.User{
		ID:         userID,
		Email:      "ana@example.com",
		Name:       "Ana",
		IsActive:   true,
		Attributes: json.RawMessage(`{"allowed_markings":["public","pii"]}`),
	}}
	issuer := &fakeScopedIssuer{}
	handler := NewScopedSessions(panel, repo, issuer)
	req := httptest.NewRequest(http.MethodPost, "/auth/scoped-sessions/select", strings.NewReader(`{"preset_id":"no_scoped_session"}`)).
		WithContext(authmw.ContextWithClaims(context.Background(), &authmw.Claims{Sub: userID, Email: "ana@example.com"}))
	rec := httptest.NewRecorder()

	handler.Select(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, issuer.scope)
	assert.Equal(t, []string{"public", "pii"}, issuer.scope.AllowedMarkings)
}

func TestScopedSessionBypassCanBeLimitedToGroups(t *testing.T) {
	groupID := uuid.New()
	cfg := ScopedSessionConfig{
		Enabled:              true,
		AllowNoScopedSession: true,
		AllowedBypassGroups:  []string{"security-reviewers"},
	}
	claims := &authmw.Claims{Sub: uuid.New()}
	assert.False(t, canUseNoScopedSession(cfg, claims, nil, nil))
	assert.True(t, canUseNoScopedSession(cfg, claims, []models.Group{{ID: groupID, Name: "security-reviewers"}}, nil))
}
