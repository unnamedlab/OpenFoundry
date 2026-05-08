// Retention preview — simulates which transactions/files a policy
// would purge `as_of_days` days from now. Mirrors
// `domain/retention.rs::run_preview` 1:1, including the "DVS table
// not found" fallback (returns warnings without failing the request).

package retentionpolicy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// RunPreview ports `domain::retention::run_preview`.
func RunPreview(ctx context.Context, db *pgxpool.Pool, rid string, asOfDays int64, resolved *ResolvedPolicies) (models.RetentionPreviewResponse, error) {
	asOf := time.Now().UTC().AddDate(0, 0, int(asOfDays))

	datasetID, warnings, err := lookupDatasetID(ctx, db, rid)
	if err != nil {
		return models.RetentionPreviewResponse{
			DatasetRid:      rid,
			AsOfDays:        asOfDays,
			AsOf:            asOf,
			EffectivePolicy: resolved.Effective,
			Transactions:    []models.RetentionPreviewTransaction{},
			Files:           []models.RetentionPreviewFile{},
			Summary:         models.RetentionPreviewSummary{},
			Warnings:        []string{fmt.Sprintf("dataset lookup failed: %s", err.Error())},
		}, nil
	}
	if datasetID == nil {
		return models.RetentionPreviewResponse{
			DatasetRid:      rid,
			AsOfDays:        asOfDays,
			AsOf:            asOf,
			EffectivePolicy: resolved.Effective,
			Transactions:    []models.RetentionPreviewTransaction{},
			Files:           []models.RetentionPreviewFile{},
			Summary:         models.RetentionPreviewSummary{},
			Warnings:        []string{"dataset not found in catalog"},
		}, nil
	}

	policies := applicablePoliciesInOrder(resolved)

	txns, err := loadTransactions(ctx, db, *datasetID)
	if err != nil {
		return models.RetentionPreviewResponse{}, err
	}
	transactions := make([]models.RetentionPreviewTransaction, 0, len(txns))
	wouldDelete := 0
	purgedTxIDs := make([]uuid.UUID, 0)
	purgePolicyForTx := make(map[uuid.UUID]struct {
		ID     uuid.UUID
		Name   string
		Reason string
	})

	for i := range txns {
		t := txns[i]
		var hitPolicy *models.RetentionPolicy
		var hitReason string
		for j := range policies {
			p := policies[j]
			if p.TargetKind != "transaction" && !strings.Contains(p.Scope, "transaction") {
				continue
			}
			if reason, ok := matchesTransaction(&p, &t, asOf); ok {
				hitPolicy = &p
				hitReason = reason
				break
			}
		}
		preview := models.RetentionPreviewTransaction{
			ID:          t.ID,
			TxType:      t.TxType,
			Status:      t.Status,
			StartedAt:   t.StartedAt,
			CommittedAt: t.CommittedAt,
		}
		if hitPolicy != nil {
			preview.WouldDelete = true
			id := hitPolicy.ID
			name := hitPolicy.Name
			reason := hitReason
			preview.PolicyID = &id
			preview.PolicyName = &name
			preview.Reason = &reason
			wouldDelete++
			purgedTxIDs = append(purgedTxIDs, t.ID)
			purgePolicyForTx[t.ID] = struct {
				ID     uuid.UUID
				Name   string
				Reason string
			}{ID: hitPolicy.ID, Name: hitPolicy.Name, Reason: hitReason}
		}
		transactions = append(transactions, preview)
	}

	files := []previewFileRow{}
	if len(purgedTxIDs) > 0 {
		fromCommitted, err := loadFilesFromDatasetFiles(ctx, db, *datasetID, purgedTxIDs)
		if err != nil {
			return models.RetentionPreviewResponse{}, err
		}
		fromStaged, err := loadFilesFromStaging(ctx, db, purgedTxIDs, transactions)
		if err != nil {
			return models.RetentionPreviewResponse{}, err
		}
		files = append(files, fromCommitted...)
		files = append(files, fromStaged...)
	}
	bytesTotal := int64(0)
	previewFiles := make([]models.RetentionPreviewFile, 0, len(files))
	for _, f := range files {
		bytesTotal += f.SizeBytes
		hit, ok := purgePolicyForTx[f.TransactionID]
		policyID := uuid.Nil
		policyName := "unknown"
		reason := "transaction purged"
		if ok {
			policyID = hit.ID
			policyName = hit.Name
			reason = hit.Reason
		}
		previewFiles = append(previewFiles, models.RetentionPreviewFile{
			ID:            f.ID,
			TransactionID: f.TransactionID,
			LogicalPath:   f.LogicalPath,
			PhysicalURI:   f.PhysicalURI,
			SizeBytes:     f.SizeBytes,
			PolicyID:      policyID,
			PolicyName:    policyName,
			Reason:        reason,
		})
	}

	summary := models.RetentionPreviewSummary{
		TransactionsTotal:       len(transactions),
		TransactionsWouldDelete: wouldDelete,
		FilesTotal:              len(previewFiles),
		BytesTotal:              bytesTotal,
	}

	return models.RetentionPreviewResponse{
		DatasetRid:      rid,
		AsOfDays:        asOfDays,
		AsOf:            asOf,
		EffectivePolicy: resolved.Effective,
		Transactions:    transactions,
		Files:           previewFiles,
		Summary:         summary,
		Warnings:        warnings,
	}, nil
}

func applicablePoliciesInOrder(resolved *ResolvedPolicies) []models.RetentionPolicy {
	out := make([]models.RetentionPolicy, 0)
	out = append(out, resolved.Explicit...)
	out = append(out, resolved.Inherited.Project...)
	out = append(out, resolved.Inherited.Space...)
	out = append(out, resolved.Inherited.Org...)
	return out
}

type transactionPreviewRow struct {
	ID          uuid.UUID
	TxType      string
	Status      string
	StartedAt   time.Time
	CommittedAt *time.Time
	AbortedAt   *time.Time
}

type previewFileRow struct {
	ID            uuid.UUID
	TransactionID uuid.UUID
	LogicalPath   string
	PhysicalURI   string
	SizeBytes     int64
}

func lookupDatasetID(ctx context.Context, db *pgxpool.Pool, rid string) (*uuid.UUID, []string, error) {
	row := db.QueryRow(ctx, `SELECT id FROM datasets WHERE rid = $1`, rid)
	var id uuid.UUID
	if err := row.Scan(&id); err != nil {
		if isMissingRelationError(err) {
			return nil, nil, nil // table not present in this DB — surfaces as 404 below
		}
		if errors.Is(err, errNoRows) {
			return nil, nil, nil
		}
		// Treat the literal "no rows" result as "dataset not found" too
		// without polluting the warnings list.
		if strings.Contains(err.Error(), "no rows in result set") {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	return &id, nil, nil
}

func loadTransactions(ctx context.Context, db *pgxpool.Pool, datasetID uuid.UUID) ([]transactionPreviewRow, error) {
	rows, err := db.Query(ctx,
		`SELECT id, tx_type, status, started_at, committed_at, aborted_at
		   FROM dataset_transactions
		  WHERE dataset_id = $1
		  ORDER BY started_at ASC`,
		datasetID,
	)
	if err != nil {
		if isMissingRelationError(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	out := make([]transactionPreviewRow, 0)
	for rows.Next() {
		var t transactionPreviewRow
		if err := rows.Scan(&t.ID, &t.TxType, &t.Status, &t.StartedAt, &t.CommittedAt, &t.AbortedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func loadFilesFromDatasetFiles(ctx context.Context, db *pgxpool.Pool, datasetID uuid.UUID, txnIDs []uuid.UUID) ([]previewFileRow, error) {
	rows, err := db.Query(ctx,
		`SELECT id, transaction_id, logical_path, physical_uri, size_bytes
		   FROM dataset_files
		  WHERE dataset_id = $1
		    AND transaction_id = ANY($2)
		    AND deleted_at IS NULL`,
		datasetID, txnIDs,
	)
	if err != nil {
		if isMissingRelationError(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	out := make([]previewFileRow, 0)
	for rows.Next() {
		var v previewFileRow
		if err := rows.Scan(&v.ID, &v.TransactionID, &v.LogicalPath, &v.PhysicalURI, &v.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func loadFilesFromStaging(ctx context.Context, db *pgxpool.Pool, txnIDs []uuid.UUID, transactions []models.RetentionPreviewTransaction) ([]previewFileRow, error) {
	abortedSet := map[uuid.UUID]struct{}{}
	for i := range transactions {
		t := transactions[i]
		if t.WouldDelete && t.Status == "ABORTED" {
			abortedSet[t.ID] = struct{}{}
		}
	}
	abortedIDs := make([]uuid.UUID, 0)
	for _, id := range txnIDs {
		if _, ok := abortedSet[id]; ok {
			abortedIDs = append(abortedIDs, id)
		}
	}
	if len(abortedIDs) == 0 {
		return nil, nil
	}
	rows, err := db.Query(ctx,
		`SELECT
		      gen_random_uuid()                                    AS id,
		      transaction_id                                       AS transaction_id,
		      logical_path                                         AS logical_path,
		      CASE
		          WHEN COALESCE(physical_path, '') <> ''
		              THEN 'local:///' || trim(both '/' from physical_path)
		          ELSE 'local:///' || transaction_id::text
		                              || '/' || trim(both '/' from logical_path)
		      END                                                  AS physical_uri,
		      size_bytes                                           AS size_bytes
		    FROM dataset_transaction_files
		   WHERE transaction_id = ANY($1)
		     AND op <> 'REMOVE'`,
		abortedIDs,
	)
	if err != nil {
		if isMissingRelationError(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	out := make([]previewFileRow, 0)
	for rows.Next() {
		var v previewFileRow
		if err := rows.Scan(&v.ID, &v.TransactionID, &v.LogicalPath, &v.PhysicalURI, &v.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// matchesTransaction mirrors `domain::retention::matches_transaction`.
func matchesTransaction(policy *models.RetentionPolicy, txn *transactionPreviewRow, asOf time.Time) (string, bool) {
	criteria, err := models.CriteriaFromRaw(policy.Criteria)
	if err != nil {
		return "", false
	}
	if criteria.TransactionState != nil && !strings.EqualFold(*criteria.TransactionState, txn.Status) {
		return "", false
	}
	anchor := txn.StartedAt
	if txn.CommittedAt != nil {
		anchor = *txn.CommittedAt
	} else if txn.AbortedAt != nil {
		anchor = *txn.AbortedAt
	}
	if criteria.TransactionAgeSeconds != nil {
		elapsed := int64(asOf.Sub(anchor).Seconds())
		if elapsed < *criteria.TransactionAgeSeconds {
			return "", false
		}
	}
	if policy.RetentionDays > 0 {
		earliest := anchor.AddDate(0, 0, int(policy.RetentionDays))
		if asOf.Before(earliest) {
			return "", false
		}
	}
	parts := []string{}
	if criteria.TransactionState != nil {
		parts = append(parts, fmt.Sprintf("transaction_state=%s", *criteria.TransactionState))
	}
	if criteria.TransactionAgeSeconds != nil {
		parts = append(parts, fmt.Sprintf("transaction_age>=%ds", *criteria.TransactionAgeSeconds))
	}
	if policy.RetentionDays > 0 {
		parts = append(parts, fmt.Sprintf("retention_days=%d", policy.RetentionDays))
	}
	if len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("policy=%s", policy.Name))
	}
	return strings.Join(parts, ", "), true
}

// errNoRows is the sentinel pgx.ErrNoRows alias kept here so the
// preview helper can stay free of the pgx import (we already pull pgx
// transitively through pgxpool).
var errNoRows = pgxErrNoRows

func isMissingRelationError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "does not exist")
}
