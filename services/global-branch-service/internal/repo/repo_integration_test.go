//go:build integration

package repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/domain"
)

// TestGlobalBranchRepo_LifecycleE2E exercises the full branch + participation
// lifecycle against a real postgres container. Sequence:
//
//  1. create a branch, assert tenant-scoped get
//  2. cross-tenant get returns ErrBranchNotFound
//  3. duplicate (tenant, name) collides with ErrBranchNameConflict
//  4. add a participation; duplicate add fails with ErrParticipationExists
//  5. mark-all-merged flips both the participation and (separately) the branch
//  6. attempting to add a participation to a merged branch is rejected by
//     the domain helper (the repo itself does not enforce that — the
//     handler is the gate, which this test asserts on for clarity)
func TestGlobalBranchRepo_LifecycleE2E(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	harness := testingx.BootPostgres(ctx, t)
	require.NoError(t, Migrate(ctx, harness.Pool))

	r := New(harness.Pool)
	tenantA := uuid.New()
	tenantB := uuid.New()
	creator := uuid.New()

	// 1. Create + Get
	branch := &domain.GlobalBranch{
		TenantID: tenantA, Name: "release-q3", BaseRef: "main",
		Description: "Q3 cross-app release", CreatedBy: creator,
	}
	require.NoError(t, branch.ValidateNew())
	tx, err := r.BeginTx(ctx)
	require.NoError(t, err)
	created, err := r.CreateBranch(ctx, tx, branch)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	require.NotEqual(t, uuid.Nil, created.ID)
	require.Equal(t, domain.StatusOpen, created.Status)

	got, err := r.GetBranch(ctx, tenantA, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "Q3 cross-app release", got.Description)

	// 2. Cross-tenant isolation
	_, err = r.GetBranch(ctx, tenantB, created.ID)
	require.ErrorIs(t, err, domain.ErrBranchNotFound)

	// 3. Duplicate (tenant, name)
	dup := &domain.GlobalBranch{
		TenantID: tenantA, Name: "release-q3", BaseRef: "main", CreatedBy: creator,
	}
	require.NoError(t, dup.ValidateNew())
	tx, err = r.BeginTx(ctx)
	require.NoError(t, err)
	_, err = r.CreateBranch(ctx, tx, dup)
	var conflict *ErrBranchNameConflict
	require.True(t, errors.As(err, &conflict), "want ErrBranchNameConflict, got %v", err)
	require.NoError(t, tx.Rollback(ctx))

	// 4. Add + duplicate-add participations
	tx, err = r.BeginTx(ctx)
	require.NoError(t, err)
	_, err = r.AddParticipation(ctx, tx, &domain.Participation{
		GlobalBranchID: created.ID, ServiceName: "ontology-definition-service", LocalBranchRef: "release/q3",
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	tx, err = r.BeginTx(ctx)
	require.NoError(t, err)
	_, err = r.AddParticipation(ctx, tx, &domain.Participation{
		GlobalBranchID: created.ID, ServiceName: "ontology-definition-service", LocalBranchRef: "release/q3",
	})
	require.ErrorIs(t, err, domain.ErrParticipationExists)
	require.NoError(t, tx.Rollback(ctx))

	parts, err := r.ListParticipations(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	require.Equal(t, domain.ParticipationPending, parts[0].Status)

	// 5. Merge: mark all participations merged + flip branch status.
	tx, err = r.BeginTx(ctx)
	require.NoError(t, err)
	n, err := r.MarkAllParticipationsMerged(ctx, tx, created.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	mergedBy := creator
	now := time.Now().UTC()
	mergedBranch, err := r.SetStatus(ctx, tx, tenantA, created.ID, domain.StatusMerged, &mergedBy, &now)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	require.Equal(t, domain.StatusMerged, mergedBranch.Status)
	require.NotNil(t, mergedBranch.MergedAt)
	require.NotNil(t, mergedBranch.MergedBy)

	parts, err = r.ListParticipations(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ParticipationMerged, parts[0].Status)

	// 6. Domain rule: adding a participation to a merged branch is
	//    rejected by CanAcceptParticipation (the handler layer's gate).
	require.ErrorIs(t, mergedBranch.CanAcceptParticipation(), domain.ErrBranchClosed)
}

// TestGlobalBranchRepo_UpdateAndAbandon covers PATCH metadata and the
// abandon transition. Sequence:
//
//  1. UpdateMetadata changes name and description on an open branch.
//  2. UpdateMetadata to a name already taken by another row collides.
//  3. SetStatus -> abandoned flips status and leaves merged_at NULL.
func TestGlobalBranchRepo_UpdateAndAbandon(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	harness := testingx.BootPostgres(ctx, t)
	require.NoError(t, Migrate(ctx, harness.Pool))

	r := New(harness.Pool)
	tenant := uuid.New()
	creator := uuid.New()

	mkBranch := func(name string) *domain.GlobalBranch {
		tx, err := r.BeginTx(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx)
		b := &domain.GlobalBranch{TenantID: tenant, Name: name, BaseRef: "main", CreatedBy: creator}
		require.NoError(t, b.ValidateNew())
		created, err := r.CreateBranch(ctx, tx, b)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))
		return created
	}

	primary := mkBranch("feat-a")
	other := mkBranch("feat-b")

	newName := "feat-a-renamed"
	newDesc := "renamed description"
	updated, err := r.UpdateMetadata(ctx, tenant, primary.ID, UpdateMetadataParams{Name: &newName, Description: &newDesc})
	require.NoError(t, err)
	require.Equal(t, "feat-a-renamed", updated.Name)
	require.Equal(t, "renamed description", updated.Description)

	collide := other.Name
	_, err = r.UpdateMetadata(ctx, tenant, primary.ID, UpdateMetadataParams{Name: &collide})
	var conflict *ErrBranchNameConflict
	require.True(t, errors.As(err, &conflict))

	tx, err := r.BeginTx(ctx)
	require.NoError(t, err)
	abandoned, err := r.SetStatus(ctx, tx, tenant, primary.ID, domain.StatusAbandoned, nil, nil)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	require.Equal(t, domain.StatusAbandoned, abandoned.Status)
	require.Nil(t, abandoned.MergedAt)
	require.Nil(t, abandoned.MergedBy)
}
