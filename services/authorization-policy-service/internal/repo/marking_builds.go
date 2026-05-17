// marking_builds.go: SG.15 marking-aware build/output publication
// planning, propagation, and security diffs.

package repo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

var ErrMarkingBuildBlocked = errors.New("marking build output blocked")

type markingBuildEventWriter interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (r *Repo) PublishMarkingBuild(ctx context.Context, tenantID *uuid.UUID, actorID uuid.UUID, body *models.PublishMarkingBuildRequest) (*models.PublishMarkingBuildResponse, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	resp := &models.PublishMarkingBuildResponse{
		Allowed:       true,
		DryRun:        body.DryRun,
		BuildID:       body.BuildID,
		TransactionID: body.TransactionID,
		OutputDiffs:   []models.MarkingBuildOutputDiff{},
		CheckedAt:     time.Now().UTC(),
	}

	for _, output := range body.OutputResources {
		before, err := r.effectiveResourceMarkings(ctx, tx, tenantID, output.ResourceKind, output.ResourceID, 0)
		if err != nil {
			return nil, err
		}
		if err := r.applyBuildLineageEdgesTx(ctx, tx, tenantID, actorID, output, body); err != nil {
			return nil, err
		}
		after, err := r.effectiveResourceMarkings(ctx, tx, tenantID, output.ResourceKind, output.ResourceID, 0)
		if err != nil {
			return nil, err
		}
		diff := buildOutputDiff(output, before.Items, after.Items)
		resp.OutputDiffs = append(resp.OutputDiffs, diff)
		blocked, err := r.blockedMarkingRemovals(ctx, tenantID, actorID, body.GroupIDs, output, diff, body.ResourceUpdateMarkingsAllowed, body.ExpandAccessAllowed)
		if err != nil {
			return nil, err
		}
		resp.BlockedRemovals = append(resp.BlockedRemovals, blocked...)
	}

	if len(resp.BlockedRemovals) > 0 {
		resp.Allowed = false
		resp.Applied = false
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		if err := r.insertBuildEventsForResponse(ctx, r.Pool, tenantID, actorID, body, resp, models.MarkingBuildStatusBlocked); err != nil {
			return nil, err
		}
		return resp, ErrMarkingBuildBlocked
	}

	if body.DryRun {
		resp.Applied = false
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		if err := r.insertBuildEventsForResponse(ctx, r.Pool, tenantID, actorID, body, resp, models.MarkingBuildStatusDryRun); err != nil {
			return nil, err
		}
		return resp, nil
	}

	resp.Applied = true
	if err := r.insertBuildEventsForResponse(ctx, tx, tenantID, actorID, body, resp, models.MarkingBuildStatusApplied); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return resp, nil
}

func (r *Repo) ListMarkingBuildEvents(ctx context.Context, tenantID *uuid.UUID, buildID, transactionID, resourceKind, resourceID string) ([]models.MarkingBuildEvent, error) {
	pred, args := tenantPredicate("resource_marking_build_events", tenantID, 1)
	conds := []string{pred}
	if strings.TrimSpace(buildID) != "" {
		args = append(args, strings.TrimSpace(buildID))
		conds = append(conds, fmt.Sprintf("build_id = $%d", len(args)))
	}
	if strings.TrimSpace(transactionID) != "" {
		args = append(args, strings.TrimSpace(transactionID))
		conds = append(conds, fmt.Sprintf("transaction_id = $%d", len(args)))
	}
	if resourceKind != "" && resourceID != "" {
		args = append(args, resourceKind, resourceID)
		conds = append(conds, fmt.Sprintf("output_resource_kind = $%d AND output_resource_id = $%d", len(args)-1, len(args)))
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT id, tenant_id, build_id, transaction_id, output_resource_kind,
		        output_resource_id, actor_id, status, reason, input_resources,
		        before_state, after_state, diff, metadata, created_at
		   FROM resource_marking_build_events
		  WHERE `+strings.Join(conds, " AND ")+`
		  ORDER BY created_at DESC
		  LIMIT 500`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.MarkingBuildEvent, 0)
	for rows.Next() {
		item := models.MarkingBuildEvent{}
		if err := rows.Scan(&item.ID, &item.TenantID, &item.BuildID, &item.TransactionID,
			&item.OutputResourceKind, &item.OutputResourceID, &item.ActorID, &item.Status,
			&item.Reason, &item.InputResources, &item.BeforeState, &item.AfterState,
			&item.Diff, &item.Metadata, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.BeforeState = normalizeJSONObject(item.BeforeState)
		item.AfterState = normalizeJSONObject(item.AfterState)
		item.Diff = normalizeJSONObject(item.Diff)
		item.Metadata = normalizeJSONObject(item.Metadata)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repo) applyBuildLineageEdgesTx(ctx context.Context, tx pgx.Tx, tenantID *uuid.UUID, actorID uuid.UUID, output models.MarkingBuildResourceRef, body *models.PublishMarkingBuildRequest) error {
	if body.ReplaceExistingLineageToOutput {
		pred, args := tenantPredicate("resource_marking_edges", tenantID, 1)
		args = append(args, output.ResourceKind, output.ResourceID, models.ResourceMarkingRelationLineage)
		if _, err := tx.Exec(ctx,
			`DELETE FROM resource_marking_edges
			  WHERE `+pred+fmt.Sprintf(" AND target_resource_kind = $%d AND target_resource_id = $%d AND relation_kind = $%d", len(args)-2, len(args)-1, len(args)),
			args...,
		); err != nil {
			return err
		}
	}
	for _, input := range body.InputResources {
		if input.ResourceKind == output.ResourceKind && input.ResourceID == output.ResourceID {
			continue
		}
		metadata := mustJSONObject(map[string]any{
			"build_id":       body.BuildID,
			"transaction_id": body.TransactionID,
			"reason":         body.Reason,
			"source":         "sg15_marking_aware_build",
		})
		edge, err := getResourceMarkingEdgeForUpdateTx(ctx, tx, tenantID, input.ResourceKind, input.ResourceID, output.ResourceKind, output.ResourceID, models.ResourceMarkingRelationLineage)
		if errors.Is(err, pgx.ErrNoRows) {
			_, err = tx.Exec(ctx,
				`INSERT INTO resource_marking_edges
				    (id, tenant_id, source_resource_kind, source_resource_id,
				     target_resource_kind, target_resource_id, relation_kind,
				     metadata, created_by, created_at, updated_at)
				 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW(),NOW())`,
				uuid.New(), tenantID, input.ResourceKind, input.ResourceID,
				output.ResourceKind, output.ResourceID, models.ResourceMarkingRelationLineage,
				metadata, actorID,
			)
		} else if err == nil {
			_, err = tx.Exec(ctx,
				`UPDATE resource_marking_edges
				    SET metadata = $2, created_by = $3, updated_at = NOW()
				  WHERE id = $1`,
				edge.ID, metadata, actorID,
			)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) blockedMarkingRemovals(ctx context.Context, tenantID *uuid.UUID, actorID uuid.UUID, groupIDs []uuid.UUID, output models.MarkingBuildResourceRef, diff models.MarkingBuildOutputDiff, resourceUpdateAllowed, expandAccessAllowed bool) ([]models.MarkingBuildBlockedRemoval, error) {
	out := []models.MarkingBuildBlockedRemoval{}
	seen := map[uuid.UUID]bool{}
	for _, removed := range diff.Removed {
		if seen[removed.MarkingID] {
			continue
		}
		seen[removed.MarkingID] = true
		check, err := r.CheckMarkingPermission(ctx, tenantID, removed.MarkingID, actorID, groupIDs, resourceUpdateAllowed, expandAccessAllowed)
		if err != nil {
			return nil, err
		}
		if check == nil || !check.CanRemoveFromResource {
			if check == nil {
				check = &models.MarkingPermissionCheckResponse{
					MarkingID:   removed.MarkingID,
					PrincipalID: actorID,
					Reasons:     []string{"marking no longer exists, so removal cannot be authorized"},
				}
			}
			out = append(out, models.MarkingBuildBlockedRemoval{
				OutputResource: output,
				MarkingID:      removed.MarkingID,
				MarkingName:    removed.MarkingName,
				RequiredFor:    removed.RequiredFor,
				Permission:     *check,
			})
		}
	}
	return out, nil
}

func (r *Repo) insertBuildEventsForResponse(ctx context.Context, exec markingBuildEventWriter, tenantID *uuid.UUID, actorID uuid.UUID, body *models.PublishMarkingBuildRequest, resp *models.PublishMarkingBuildResponse, status string) error {
	for _, diff := range resp.OutputDiffs {
		if err := insertMarkingBuildEvent(ctx, exec, tenantID, actorID, body, status, diff); err != nil {
			return err
		}
	}
	return nil
}

func insertMarkingBuildEvent(ctx context.Context, exec markingBuildEventWriter, tenantID *uuid.UUID, actorID uuid.UUID, body *models.PublishMarkingBuildRequest, status string, diff models.MarkingBuildOutputDiff) error {
	_, err := exec.Exec(ctx,
		`INSERT INTO resource_marking_build_events
		    (id, tenant_id, build_id, transaction_id, output_resource_kind,
		     output_resource_id, actor_id, status, reason, input_resources,
		     before_state, after_state, diff, metadata)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		uuid.New(), tenantID, body.BuildID, body.TransactionID,
		diff.OutputResource.ResourceKind, diff.OutputResource.ResourceID, actorID,
		status, body.Reason, mustJSONObject(body.InputResources),
		mustJSONObject(map[string]any{"effective_markings": diff.Before}),
		mustJSONObject(map[string]any{"effective_markings": diff.After}),
		mustJSONObject(map[string]any{
			"added":     diff.Added,
			"removed":   diff.Removed,
			"unchanged": diff.Unchanged,
		}),
		normalizeJSONObject(body.Metadata),
	)
	return err
}

func buildOutputDiff(output models.MarkingBuildResourceRef, before, after []models.EffectiveResourceMarking) models.MarkingBuildOutputDiff {
	beforeEntries := flattenEffectiveMarkingEntries(before)
	afterEntries := flattenEffectiveMarkingEntries(after)
	beforeByKey := map[string]models.MarkingDiffEntry{}
	afterByKey := map[string]models.MarkingDiffEntry{}
	for _, entry := range beforeEntries {
		beforeByKey[markingDiffEntryKey(entry)] = entry
	}
	for _, entry := range afterEntries {
		afterByKey[markingDiffEntryKey(entry)] = entry
	}
	diff := models.MarkingBuildOutputDiff{
		OutputResource: output,
		Before:         before,
		After:          after,
		Added:          []models.MarkingDiffEntry{},
		Removed:        []models.MarkingDiffEntry{},
		Unchanged:      []models.MarkingDiffEntry{},
	}
	for key, entry := range afterByKey {
		if _, ok := beforeByKey[key]; ok {
			diff.Unchanged = append(diff.Unchanged, entry)
		} else {
			diff.Added = append(diff.Added, entry)
		}
	}
	for key, entry := range beforeByKey {
		if _, ok := afterByKey[key]; !ok {
			diff.Removed = append(diff.Removed, entry)
		}
	}
	sortMarkingDiffEntries(diff.Added)
	sortMarkingDiffEntries(diff.Removed)
	sortMarkingDiffEntries(diff.Unchanged)
	return diff
}

func flattenEffectiveMarkingEntries(items []models.EffectiveResourceMarking) []models.MarkingDiffEntry {
	out := []models.MarkingDiffEntry{}
	for _, item := range items {
		for _, requiredFor := range item.RequiredFor {
			out = append(out, models.MarkingDiffEntry{
				MarkingID:   item.MarkingID,
				MarkingName: item.MarkingName,
				RequiredFor: []string{requiredFor},
			})
		}
	}
	return out
}

func markingDiffEntryKey(entry models.MarkingDiffEntry) string {
	return entry.MarkingID.String() + "|" + strings.Join(entry.RequiredFor, ",")
}

func sortMarkingDiffEntries(entries []models.MarkingDiffEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].MarkingName != entries[j].MarkingName {
			return entries[i].MarkingName < entries[j].MarkingName
		}
		if entries[i].MarkingID != entries[j].MarkingID {
			return entries[i].MarkingID.String() < entries[j].MarkingID.String()
		}
		return strings.Join(entries[i].RequiredFor, ",") < strings.Join(entries[j].RequiredFor, ",")
	})
}
