package transactions

import (
	"context"
	"errors"
	"testing"
	"time"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	cedarauthzlib "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	cedarauthzlocal "github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/cedarauthz"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/metrics"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/repo"
)

// ── Fakes ────────────────────────────────────────────────────────

type fakeRepo struct {
	sets     map[string]*models.MediaSet
	txs      map[string]*models.MediaSetTransaction
	conflict bool

	createCalls       int
	hardDeleted       []string // tx RIDs whose items were hard-deleted on abort
	replaceSweep      []string // tx RIDs that triggered the REPLACE soft-delete
	branchHeadAdvance map[string]string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		sets:              map[string]*models.MediaSet{},
		txs:               map[string]*models.MediaSetTransaction{},
		branchHeadAdvance: map[string]string{},
	}
}

func (f *fakeRepo) GetMediaSet(_ context.Context, rid string) (*models.MediaSet, error) {
	return f.sets[rid], nil
}
func (f *fakeRepo) GetTransaction(_ context.Context, rid string) (*models.MediaSetTransaction, error) {
	return f.txs[rid], nil
}
func (f *fakeRepo) CreateTransaction(_ context.Context, _ pgx.Tx, p repo.CreateTransactionParams) (*models.MediaSetTransaction, error) {
	f.createCalls++
	if f.conflict {
		return nil, &repo.ErrTransactionConflict{MediaSetRID: p.MediaSetRID, Branch: p.Branch}
	}
	rid := repo.NewTransactionRID()
	row := &models.MediaSetTransaction{
		RID:         rid,
		MediaSetRID: p.MediaSetRID,
		Branch:      p.Branch,
		State:       string(models.TxStateOpen),
		WriteMode:   p.WriteMode,
		OpenedAt:    time.Now(),
		OpenedBy:    p.OpenedBy,
	}
	f.txs[rid] = row
	return row, nil
}
func (f *fakeRepo) CloseTransaction(_ context.Context, _ pgx.Tx, p repo.CloseTransactionParams) (*models.MediaSetTransaction, error) {
	row := f.txs[p.RID]
	row.State = string(p.Target)
	now := time.Now()
	row.ClosedAt = &now
	return row, nil
}
func (f *fakeRepo) HardDeleteAbortedItems(_ context.Context, _ pgx.Tx, transactionRID string) error {
	f.hardDeleted = append(f.hardDeleted, transactionRID)
	return nil
}
func (f *fakeRepo) SoftDeletePriorReplaceItems(_ context.Context, _ pgx.Tx, _, _, transactionRID string) error {
	f.replaceSweep = append(f.replaceSweep, transactionRID)
	return nil
}
func (f *fakeRepo) AdvanceBranchHead(_ context.Context, _ pgx.Tx, mediaSetRID, branch, txRID string) error {
	f.branchHeadAdvance[mediaSetRID+"|"+branch] = txRID
	return nil
}
func (f *fakeRepo) ListTransactionHistory(_ context.Context, mediaSetRID string) ([]models.TransactionHistoryEntry, error) {
	out := []models.TransactionHistoryEntry{}
	for _, t := range f.txs {
		if t.MediaSetRID != mediaSetRID {
			continue
		}
		out = append(out, models.TransactionHistoryEntry{
			RID: t.RID, MediaSetRID: t.MediaSetRID, Branch: t.Branch,
			State: t.State, WriteMode: t.WriteMode, OpenedAt: t.OpenedAt,
			ClosedAt: t.ClosedAt, OpenedBy: t.OpenedBy,
		})
	}
	return out, nil
}
func (f *fakeRepo) BeginTx(_ context.Context) (pgx.Tx, error) { return &noopTx{}, nil }

type noopTx struct{ pgx.Tx }

func (*noopTx) Commit(context.Context) error   { return nil }
func (*noopTx) Rollback(context.Context) error { return nil }

type recordingEmitter struct{ events []audittrail.AuditEvent }

func (r *recordingEmitter) emit(_ context.Context, _ pgx.Tx, e audittrail.AuditEvent, _ audittrail.AuditContext) error {
	r.events = append(r.events, e)
	return nil
}

type realCedarGate struct{ *cedarauthzlocal.Engine }

func (g *realCedarGate) CheckMediaSet(ctx context.Context, c *authmw.Claims, action cedar.EntityUID, set *models.MediaSet) error {
	return g.Engine.CheckMediaSet(ctx, c, action, set)
}

func newSvc(t *testing.T) (*Service, *fakeRepo, *recordingEmitter, *metrics.Metrics) {
	t.Helper()
	bundled, err := cedarauthzlocal.BundledPolicyRecords()
	require.NoError(t, err)
	store, err := cedarauthzlib.NewWithPolicies(bundled)
	require.NoError(t, err)
	engine := cedarauthzlocal.NewEngine(cedarauthzlib.NewEngineNoopAudit(store))
	r := newFakeRepo()
	em := &recordingEmitter{}
	m := metrics.New(observability.NewMetrics())
	s := New(r, &realCedarGate{engine}, m)
	s.EmitAudit = em.emit
	return s, r, em, m
}

func makeClaims(roles []string) *authmw.Claims {
	tenant := uuid.New()
	return &authmw.Claims{
		Sub: uuid.New(), Email: "u@example.test", Roles: roles, OrgID: &tenant,
		SessionScope: &authmw.SessionScope{},
	}
}

func seedSet(r *fakeRepo, rid, policy string) *models.MediaSet {
	s := &models.MediaSet{RID: rid, ProjectRID: "ri.proj.1", Schema: "IMAGE", TransactionPolicy: policy}
	r.sets[rid] = s
	return s
}

// ── Tests ────────────────────────────────────────────────────────

func TestOpenRejectsTransactionless(t *testing.T) {
	t.Parallel()
	s, r, _, _ := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONLESS")
	_, err := s.Open(context.Background(), OpenInput{
		MediaSetRID: "ri.set.1", Claims: makeClaims([]string{"editor"}),
	})
	require.Error(t, err)
	var t1 *ErrTransactionless
	require.True(t, errors.As(err, &t1))
}

func TestOpenHappyPathCreatesAndAuditsAndBumpsGauge(t *testing.T) {
	t.Parallel()
	s, r, em, _ := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONAL")

	row, err := s.Open(context.Background(), OpenInput{
		MediaSetRID: "ri.set.1",
		Body:        models.OpenTransactionRequest{},
		Claims:      makeClaims([]string{"editor"}),
	})
	require.NoError(t, err)
	assert.Equal(t, string(models.TxStateOpen), row.State)
	assert.Equal(t, "main", row.Branch)
	assert.Equal(t, string(models.WriteModeModify), row.WriteMode)

	require.Len(t, em.events, 1)
	assert.Equal(t, audittrail.KindMediaSetTransactionOpened, em.events[0].Kind)
}

func TestOpenSurfacesConflictOnConcurrentOpen(t *testing.T) {
	t.Parallel()
	s, r, _, _ := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONAL")
	r.conflict = true

	_, err := s.Open(context.Background(), OpenInput{
		MediaSetRID: "ri.set.1", Claims: makeClaims([]string{"editor"}),
	})
	require.Error(t, err)
	var c *ErrTransactionConflict
	require.True(t, errors.As(err, &c))
}

func TestCommitAdvancesBranchHeadAndAudits(t *testing.T) {
	t.Parallel()
	s, r, em, _ := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONAL")
	tx := &models.MediaSetTransaction{
		RID: "ri.tx.1", MediaSetRID: "ri.set.1", Branch: "main",
		State: string(models.TxStateOpen), WriteMode: string(models.WriteModeModify),
		OpenedAt: time.Now(),
	}
	r.txs[tx.RID] = tx

	row, err := s.Commit(context.Background(), CloseInput{
		RID: tx.RID, Claims: makeClaims([]string{"editor"}),
	})
	require.NoError(t, err)
	assert.Equal(t, string(models.TxStateCommitted), row.State)
	assert.Equal(t, "ri.tx.1", r.branchHeadAdvance["ri.set.1|main"])
	assert.Empty(t, r.replaceSweep, "MODIFY mode does not trigger REPLACE sweep")
	require.Len(t, em.events, 1)
	assert.Equal(t, audittrail.KindMediaSetTransactionCommitted, em.events[0].Kind)
}

func TestCommitReplaceModeSoftDeletesPriorItems(t *testing.T) {
	t.Parallel()
	s, r, _, _ := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONAL")
	tx := &models.MediaSetTransaction{
		RID: "ri.tx.2", MediaSetRID: "ri.set.1", Branch: "main",
		State: string(models.TxStateOpen), WriteMode: string(models.WriteModeReplace),
	}
	r.txs[tx.RID] = tx

	_, err := s.Commit(context.Background(), CloseInput{
		RID: tx.RID, Claims: makeClaims([]string{"editor"}),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"ri.tx.2"}, r.replaceSweep)
}

func TestAbortHardDeletesStagedItemsAndAudits(t *testing.T) {
	t.Parallel()
	s, r, em, _ := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONAL")
	tx := &models.MediaSetTransaction{
		RID: "ri.tx.3", MediaSetRID: "ri.set.1", Branch: "main",
		State: string(models.TxStateOpen), WriteMode: string(models.WriteModeModify),
	}
	r.txs[tx.RID] = tx

	row, err := s.Abort(context.Background(), CloseInput{
		RID: tx.RID, Claims: makeClaims([]string{"editor"}),
	})
	require.NoError(t, err)
	assert.Equal(t, string(models.TxStateAborted), row.State)
	assert.Equal(t, []string{"ri.tx.3"}, r.hardDeleted)
	assert.Empty(t, r.branchHeadAdvance, "abort never advances head")
	require.Len(t, em.events, 1)
	assert.Equal(t, audittrail.KindMediaSetTransactionAborted, em.events[0].Kind)
}

func TestCommitRejectsTerminalTransaction(t *testing.T) {
	t.Parallel()
	s, r, _, _ := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONAL")
	r.txs["ri.tx.4"] = &models.MediaSetTransaction{
		RID: "ri.tx.4", MediaSetRID: "ri.set.1", Branch: "main",
		State: string(models.TxStateCommitted),
	}

	_, err := s.Commit(context.Background(), CloseInput{
		RID: "ri.tx.4", Claims: makeClaims([]string{"editor"}),
	})
	require.Error(t, err)
	var term *ErrTransactionTerminal
	require.True(t, errors.As(err, &term))
	assert.Equal(t, "COMMITTED", term.State)
}

func TestListHistoryRequiresViewClearance(t *testing.T) {
	t.Parallel()
	s, r, _, _ := newSvc(t)
	set := seedSet(r, "ri.set.1", "TRANSACTIONAL")
	set.Markings = []string{"pii"}

	uncleared := makeClaims([]string{"viewer"})
	_, err := s.ListHistory(context.Background(), ListInput{
		MediaSetRID: "ri.set.1", Claims: uncleared,
	})
	require.Error(t, err)
	var f *cedarauthzlocal.ErrForbidden
	require.True(t, errors.As(err, &f))

	cleared := &authmw.Claims{
		Sub: uuid.New(), Roles: []string{"viewer"},
		OrgID:        uncleared.OrgID,
		SessionScope: &authmw.SessionScope{AllowedMarkings: []string{"pii"}},
	}
	_, err = s.ListHistory(context.Background(), ListInput{
		MediaSetRID: "ri.set.1", Claims: cleared,
	})
	require.NoError(t, err)
}
