//go:build integration

// Integration tests for the network-boundary-service Postgres-backed
// EgressPolicyStore. Boots an ephemeral postgres:16-alpine via
// libs/testing.BootPostgres and exercises the lifecycle the SG.34 handler
// surface relies on.
package repo_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/network-boundary-service/internal/handler"
	"github.com/openfoundry/openfoundry-go/services/network-boundary-service/internal/repo"
)

func bootStore(t *testing.T) (context.Context, *repo.PgEgressPolicyStore) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	pg := testingx.BootPostgres(ctx, t)
	require.NoError(t, repo.Migrate(ctx, pg.Pool))
	return ctx, repo.NewPgEgressPolicyStore(pg.Pool)
}

func adminClaims() *authmw.Claims {
	return &authmw.Claims{
		Sub:         uuid.New(),
		Email:       "admin@example.com",
		Roles:       []string{"admin"},
		Permissions: []string{"network-egress:manage", "network-egress:approve"},
	}
}

func buildActivePolicy(creator string, name string) handler.NetworkEgressPolicy {
	now := time.Now().UTC()
	return handler.NetworkEgressPolicy{
		ID:                   uuid.NewString(),
		Name:                 name,
		Kind:                 handler.EgressPolicyKindDirect,
		Address:              handler.EgressEndpoint{Kind: "host", Value: "api.example.com"},
		Port:                 handler.EgressPort{Kind: "single", Value: "443"},
		Protocol:             "https",
		SNIBehavior:          "verify",
		State:                handler.EgressPolicyStateActive,
		Status:               handler.EgressPolicyStateActive,
		AllowedOrganizations: []string{"org-main"},
		ImporterGrants:       []string{"group:warehouse-importers"},
		Permissions:          []string{"group:warehouse-importers"},
		CreatedBy:            creator,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
}

func TestPgEgressPolicyStore_CreateGetListLifecycle(t *testing.T) {
	ctx, store := bootStore(t)
	claims := adminClaims()
	policy := buildActivePolicy(claims.Sub.String(), "warehouse api")

	created, err := store.CreatePolicy(ctx, policy)
	require.NoError(t, err)
	require.Equal(t, policy.ID, created.ID)
	require.Equal(t, handler.EgressPolicyStateActive, created.State)

	fetched, found, err := store.GetPolicy(ctx, policy.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, policy.ID, fetched.ID)
	require.Equal(t, "api.example.com", fetched.Address.Value)
	require.Contains(t, fetched.Permissions, "group:warehouse-importers")
	require.True(t, fetched.ImportHighRisk)

	all, err := store.ListPolicies(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)

	_, missing, err := store.GetPolicy(ctx, uuid.NewString())
	require.NoError(t, err)
	require.False(t, missing)
}

func TestPgEgressPolicyStore_UpdateStateAndApprovalsRoundTrip(t *testing.T) {
	ctx, store := bootStore(t)
	claims := adminClaims()
	created, err := store.CreatePolicy(ctx, buildActivePolicy(claims.Sub.String(), "warehouse api"))
	require.NoError(t, err)

	paused, err := store.UpdateState(ctx, created.ID, handler.EgressPolicyStatePaused, claims.Sub.String(), "maintenance")
	require.NoError(t, err)
	require.Equal(t, handler.EgressPolicyStatePaused, paused.State)
	require.NotNil(t, paused.PausedAt)
	require.NotEmpty(t, paused.AuditEvents)
	require.Equal(t, "network_egress.policy.paused", lastLifecycleAudit(paused.AuditEvents))

	revoked, err := store.UpdateState(ctx, created.ID, handler.EgressPolicyStateRevoked, claims.Sub.String(), "no longer approved")
	require.NoError(t, err)
	require.Equal(t, handler.EgressPolicyStateRevoked, revoked.State)
	require.NotNil(t, revoked.RevokedAt)

	_, err = store.UpdateState(ctx, created.ID, handler.EgressPolicyStateActive, claims.Sub.String(), "undo")
	require.Error(t, err)
	require.Contains(t, err.Error(), "revoked")
}

func TestPgEgressPolicyStore_RequestStateChangeAndDecideApproval(t *testing.T) {
	ctx, store := bootStore(t)
	proposer := &authmw.Claims{Sub: uuid.New(), Email: "dev@example.com", Roles: []string{"viewer"}}
	created, err := store.CreatePolicy(ctx, buildActivePolicy(proposer.Sub.String(), "vendor api"))
	require.NoError(t, err)

	pending, err := store.RequestStateChange(ctx, created.ID, handler.EgressPolicyStatePaused, proposer.Sub.String(), "rotate vendor cert")
	require.NoError(t, err)
	require.Equal(t, handler.EgressPolicyStateActive, pending.State, "RequestStateChange should leave the policy untouched while a task is pending")
	require.NotEmpty(t, pending.ApprovalTasks)

	task := lastPendingTask(pending.ApprovalTasks)
	require.NotEmpty(t, task.ID)

	approvals, err := store.ListApprovals(ctx, handler.EgressApprovalStatusPending)
	require.NoError(t, err)
	require.NotEmpty(t, approvals)

	admin := adminClaims()
	decidedPolicy, decidedTask, err := store.DecideApproval(ctx, task.ID, handler.EgressApprovalStatusApproved, admin.Sub.String(), "approved by ISO")
	require.NoError(t, err)
	require.Equal(t, handler.EgressPolicyStatePaused, decidedPolicy.State)
	require.Equal(t, handler.EgressApprovalStatusApproved, decidedTask.Status)

	emptyPending, err := store.ListApprovals(ctx, handler.EgressApprovalStatusPending)
	require.NoError(t, err)
	require.Empty(t, emptyPending)
}

func TestPgEgressPolicyStore_RecordRuntimeUse(t *testing.T) {
	ctx, store := bootStore(t)
	claims := adminClaims()
	created, err := store.CreatePolicy(ctx, buildActivePolicy(claims.Sub.String(), "warehouse api"))
	require.NoError(t, err)

	body := handler.EvaluateWorkloadEgressRequest{
		WorkloadID:   "transform-1",
		WorkloadKind: "data_export",
		PolicyIDs:    []string{created.ID},
		Destination:  handler.EgressEndpoint{Kind: "host", Value: "api.example.com"},
		Port:         443,
		ActorGrants:  []string{"group:warehouse-importers"},
	}
	decision := handler.EgressPolicyRuntimeDecision{PolicyID: created.ID, Allowed: true, Code: "network_egress_allowed", Message: "ok"}

	require.NoError(t, store.RecordRuntimeUse(ctx, created.ID, claims, body, decision))

	fetched, found, err := store.GetPolicy(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.NotEmpty(t, fetched.WorkloadUsages, "RecordRuntimeUse must persist a usage row")
	require.Equal(t, "transform-1", fetched.WorkloadUsages[0].WorkloadID)
	require.NotEmpty(t, fetched.AuditEvents)
}

func TestPgEgressPolicyStore_UpdateSharing(t *testing.T) {
	ctx, store := bootStore(t)
	claims := adminClaims()
	created, err := store.CreatePolicy(ctx, buildActivePolicy(claims.Sub.String(), "warehouse api"))
	require.NoError(t, err)

	updated, err := store.UpdateSharing(ctx, created.ID,
		[]string{"group:viewers-2"},
		[]string{"group:importers-2"},
		[]string{"role:editor"},
		claims.Sub.String(),
		"rotated grants",
	)
	require.NoError(t, err)
	require.Contains(t, updated.ViewerGrants, "group:viewers-2")
	require.Contains(t, updated.ImporterGrants, "group:importers-2")
	require.Contains(t, updated.AdminGrants, "role:editor")
}

func lastLifecycleAudit(events []handler.EgressPolicyAuditEvent) string {
	for i := len(events) - 1; i >= 0; i-- {
		action := events[i].Action
		if strings.HasPrefix(action, "network_egress.policy.") &&
			(strings.HasSuffix(action, ".paused") ||
				strings.HasSuffix(action, ".revoked") ||
				strings.HasSuffix(action, ".activated") ||
				strings.HasSuffix(action, ".state_changed")) {
			return action
		}
	}
	return ""
}

func lastPendingTask(tasks []handler.EgressApprovalTask) handler.EgressApprovalTask {
	for i := len(tasks) - 1; i >= 0; i-- {
		if tasks[i].Status == handler.EgressApprovalStatusPending {
			return tasks[i]
		}
	}
	return handler.EgressApprovalTask{}
}
