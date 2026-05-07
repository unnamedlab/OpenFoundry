package domain_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/domain"
)

// workspaceClaims rebuilds the Rust test fixture: the JWT carries an
// attribute-level workspace ("finance-lab") AND a session-scoped
// workspace ("project-alpha"); the session scope must win.
func workspaceClaims() *authmw.Claims {
	ws := "project-alpha"
	return &authmw.Claims{
		Sub:        uuid.Nil,
		IAT:        0,
		EXP:        math.MaxInt64,
		JTI:        uuid.Nil,
		Email:      "user@example.com",
		Name:       "User",
		Roles:      []string{"viewer"},
		Attributes: json.RawMessage(`{"workspace":"finance-lab"}`),
		SessionScope: &authmw.SessionScope{
			Workspace: &ws,
		},
	}
}

func TestResourceKindParserAcceptsSupportedValues(t *testing.T) {
	t.Parallel()

	got, err := domain.ParseOntologyResourceKind("action_type")
	if err != nil {
		t.Fatalf("ParseOntologyResourceKind(action_type) returned error: %v", err)
	}
	if got != domain.OntologyResourceKindActionType {
		t.Fatalf("ParseOntologyResourceKind(action_type) = %q, want %q",
			got, domain.OntologyResourceKindActionType)
	}

	if _, err := domain.ParseOntologyResourceKind("unknown"); err == nil {
		t.Fatalf("ParseOntologyResourceKind(unknown) returned nil error, want non-nil")
	}
}

func TestWorkspaceSlugPrefersSessionScopeOverAttributes(t *testing.T) {
	t.Parallel()

	got := domain.ClaimsWorkspaceSlug(workspaceClaims())
	if got == nil {
		t.Fatal("ClaimsWorkspaceSlug returned nil, want \"project-alpha\"")
	}
	if *got != "project-alpha" {
		t.Fatalf("ClaimsWorkspaceSlug = %q, want %q", *got, "project-alpha")
	}
}
