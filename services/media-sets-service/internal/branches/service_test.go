package branches

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
	cedarauthzlocal "github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/cedarauthz"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/repo"
)

// ── Fakes ────────────────────────────────────────────────────────

type fakeRepo struct {
	sets         map[string]*models.MediaSet
	branches     map[string]*models.MediaSetBranch // key = setRID|name
	transactions map[string]*models.MediaSetTransaction

	mergeSourceItems []repo.MergeSourceItem
	targetPaths      map[string]struct{}

	softDeletedItems int64
	insertedMerged   int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		sets:         map[string]*models.MediaSet{},
		branches:     map[string]*models.MediaSetBranch{},
		transactions: map[string]*models.MediaSetTransaction{},
		targetPaths:  map[string]struct{}{},
	}
}

func bkey(setRID, name string) string { return setRID + "|" + name }

func (f *fakeRepo) GetMediaSet(_ context.Context, rid string) (*models.MediaSet, error) {
	return f.sets[rid], nil
}
func (f *fakeRepo) GetTransaction(_ context.Context, rid string) (*models.MediaSetTransaction, error) {
	return f.transactions[rid], nil
}
func (f *fakeRepo) RequireBranch(_ context.Context, mediaSetRID, branchName string) (*models.MediaSetBranch, error) {
	return f.branches[bkey(mediaSetRID, branchName)], nil
}
func (f *fakeRepo) LockBranch(_ context.Context, _ pgx.Tx, mediaSetRID, branchName string) (*models.MediaSetBranch, error) {
	return f.branches[bkey(mediaSetRID, branchName)], nil
}
func (f *fakeRepo) ListBranches(_ context.Context, mediaSetRID string) ([]models.MediaSetBranch, error) {
	out := []models.MediaSetBranch{}
	for k, b := range f.branches {
		if len(k) > len(mediaSetRID) && k[:len(mediaSetRID)] == mediaSetRID {
			out = append(out, *b)
		}
	}
	return out, nil
}
func (f *fakeRepo) CreateBranch(_ context.Context, _ pgx.Tx, p repo.CreateBranchParams) (*models.MediaSetBranch, error) {
	if _, ok := f.branches[bkey(p.MediaSetRID, p.BranchName)]; ok {
		return nil, &repo.ErrBranchExists{Name: p.BranchName, MediaSetRID: p.MediaSetRID}
	}
	row := &models.MediaSetBranch{
		MediaSetRID:        p.MediaSetRID,
		BranchName:         p.BranchName,
		BranchRID:          "ri.foundry.main.media_branch." + uuid.New().String(),
		ParentBranchRID:    p.ParentBranchRID,
		HeadTransactionRID: p.HeadTransactionRID,
		CreatedAt:          time.Now(),
		CreatedBy:          p.CreatedBy,
	}
	f.branches[bkey(p.MediaSetRID, p.BranchName)] = row
	return row, nil
}
func (f *fakeRepo) ReparentChildren(_ context.Context, _ pgx.Tx, mediaSetRID, oldRID string, newRID *string) error {
	for _, b := range f.branches {
		if b.MediaSetRID == mediaSetRID && b.ParentBranchRID != nil && *b.ParentBranchRID == oldRID {
			b.ParentBranchRID = newRID
		}
	}
	return nil
}
func (f *fakeRepo) SoftDeleteItemsOnBranch(_ context.Context, _ pgx.Tx, _ string) (int64, error) {
	n := f.softDeletedItems
	f.softDeletedItems = 0 // reset for the next call
	return n, nil
}
func (f *fakeRepo) DeleteBranchRow(_ context.Context, _ pgx.Tx, mediaSetRID, branchName string) error {
	delete(f.branches, bkey(mediaSetRID, branchName))
	return nil
}
func (f *fakeRepo) RewindBranchHead(_ context.Context, _ pgx.Tx, mediaSetRID, branchName string) (*models.MediaSetBranch, error) {
	b := f.branches[bkey(mediaSetRID, branchName)]
	if b == nil {
		return nil, pgx.ErrNoRows
	}
	b.HeadTransactionRID = nil
	return b, nil
}
func (f *fakeRepo) ListMergeSourceItems(_ context.Context, _ pgx.Tx, _ string) ([]repo.MergeSourceItem, error) {
	return f.mergeSourceItems, nil
}
func (f *fakeRepo) LiveTargetPaths(_ context.Context, _ pgx.Tx, _ string) (map[string]struct{}, error) {
	return f.targetPaths, nil
}
func (f *fakeRepo) SoftDeleteAtPath(_ context.Context, _ pgx.Tx, _, _ string) error { return nil }
func (f *fakeRepo) InsertMergedItem(_ context.Context, _ pgx.Tx, _, _, _ string, _ repo.MergeSourceItem) (bool, error) {
	f.insertedMerged++
	return false, nil
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

func newSvc(t *testing.T) (*Service, *fakeRepo, *recordingEmitter) {
	t.Helper()
	bundled, err := cedarauthzlocal.BundledPolicyRecords()
	require.NoError(t, err)
	store, err := cedarauthzlib.NewWithPolicies(bundled)
	require.NoError(t, err)
	engine := cedarauthzlocal.NewEngine(cedarauthzlib.NewEngineNoopAudit(store))
	r := newFakeRepo()
	em := &recordingEmitter{}
	s := New(r, &realCedarGate{engine})
	s.EmitAudit = em.emit
	return s, r, em
}

func makeClaims(roles, allowedMarkings []string) *authmw.Claims {
	tenant := uuid.New()
	return &authmw.Claims{
		Sub: uuid.New(), Email: "u@example.test", Name: "U",
		Roles: roles, OrgID: &tenant,
		SessionScope: &authmw.SessionScope{AllowedMarkings: allowedMarkings},
	}
}

func seedSet(r *fakeRepo, rid string, policy string) *models.MediaSet {
	s := &models.MediaSet{
		RID: rid, ProjectRID: "ri.proj.1", Schema: "IMAGE",
		TransactionPolicy: policy,
	}
	r.sets[rid] = s
	return s
}

func seedBranch(r *fakeRepo, setRID, name string, head *string) *models.MediaSetBranch {
	b := &models.MediaSetBranch{
		MediaSetRID:        setRID,
		BranchName:         name,
		BranchRID:          "ri.foundry.main.media_branch." + name,
		HeadTransactionRID: head,
	}
	r.branches[bkey(setRID, name)] = b
	return b
}

// ── Tests ────────────────────────────────────────────────────────

func TestCreateBranchInheritsParentHead(t *testing.T) {
	t.Parallel()
	s, r, em := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONLESS")
	head := "ri.tx.1"
	seedBranch(r, "ri.set.1", "main", &head)

	row, err := s.Create(context.Background(), CreateInput{
		MediaSetRID: "ri.set.1",
		Body:        models.CreateBranchRequest{Name: "feature"},
		Claims:      makeClaims([]string{"editor"}, nil),
	})
	require.NoError(t, err)
	assert.Equal(t, "feature", row.BranchName)
	require.NotNil(t, row.HeadTransactionRID)
	assert.Equal(t, "ri.tx.1", *row.HeadTransactionRID)
	require.Len(t, em.events, 1)
	assert.Equal(t, audittrail.KindMediaSetCreated, em.events[0].Kind)
	assert.Equal(t, "branch:feature", em.events[0].Name)
}

func TestCreateBranchValidatesName(t *testing.T) {
	t.Parallel()
	s, r, _ := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONLESS")
	seedBranch(r, "ri.set.1", "main", nil)
	editor := makeClaims([]string{"editor"}, nil)

	cases := []string{"", "  ", "a/b", "with space"}
	for _, name := range cases {
		_, err := s.Create(context.Background(), CreateInput{
			MediaSetRID: "ri.set.1",
			Body:        models.CreateBranchRequest{Name: name},
			Claims:      editor,
		})
		require.Error(t, err, "name=%q should fail", name)
		var bad *ErrBadRequest
		require.True(t, errors.As(err, &bad))
	}
}

func TestCreateBranchRejectsDuplicate(t *testing.T) {
	t.Parallel()
	s, r, _ := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONLESS")
	seedBranch(r, "ri.set.1", "main", nil)
	seedBranch(r, "ri.set.1", "feature", nil)
	editor := makeClaims([]string{"editor"}, nil)

	_, err := s.Create(context.Background(), CreateInput{
		MediaSetRID: "ri.set.1",
		Body:        models.CreateBranchRequest{Name: "feature"},
		Claims:      editor,
	})
	require.Error(t, err)
	var bad *ErrBadRequest
	require.True(t, errors.As(err, &bad))
	assert.Contains(t, bad.Msg, "already exists")
}

func TestDeleteBranchRejectsMain(t *testing.T) {
	t.Parallel()
	s, r, _ := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONLESS")
	seedBranch(r, "ri.set.1", "main", nil)

	err := s.Delete(context.Background(), DeleteInput{
		MediaSetRID: "ri.set.1", BranchName: "main",
		Claims: makeClaims([]string{"editor"}, nil),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "implicit `main`")
}

func TestDeleteBranchSoftDeletesItemsAndAudits(t *testing.T) {
	t.Parallel()
	s, r, em := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONLESS")
	seedBranch(r, "ri.set.1", "main", nil)
	seedBranch(r, "ri.set.1", "feature", nil)

	err := s.Delete(context.Background(), DeleteInput{
		MediaSetRID: "ri.set.1", BranchName: "feature",
		Claims: makeClaims([]string{"editor"}, nil),
	})
	require.NoError(t, err)
	_, ok := r.branches[bkey("ri.set.1", "feature")]
	assert.False(t, ok)
	require.Len(t, em.events, 1)
	assert.Equal(t, audittrail.KindMediaSetDeleted, em.events[0].Kind)
}

func TestResetBranchRejectsTransactionless(t *testing.T) {
	t.Parallel()
	s, r, _ := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONLESS")
	seedBranch(r, "ri.set.1", "main", nil)

	_, err := s.Reset(context.Background(), ResetInput{
		MediaSetRID: "ri.set.1", BranchName: "main",
		Claims: makeClaims([]string{"editor"}, nil),
	})
	require.Error(t, err)
	var t1 *ErrTransactionlessRejectsReset
	require.True(t, errors.As(err, &t1))
}

func TestResetBranchClearsHeadAndAudits(t *testing.T) {
	t.Parallel()
	s, r, em := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONAL")
	head := "ri.tx.99"
	seedBranch(r, "ri.set.1", "feature", &head)
	r.softDeletedItems = 7

	resp, err := s.Reset(context.Background(), ResetInput{
		MediaSetRID: "ri.set.1", BranchName: "feature",
		Claims: makeClaims([]string{"editor"}, nil),
	})
	require.NoError(t, err)
	assert.Nil(t, resp.Branch.HeadTransactionRID)
	assert.Equal(t, int64(7), resp.ItemsSoftDeleted)
	require.Len(t, em.events, 1)
	assert.Equal(t, audittrail.KindMediaSetTransactionAborted, em.events[0].Kind)
}

func TestMergeFailOnConflictReturnsConflictPaths(t *testing.T) {
	t.Parallel()
	s, r, _ := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONLESS")
	seedBranch(r, "ri.set.1", "main", nil)
	seedBranch(r, "ri.set.1", "feature", nil)
	r.mergeSourceItems = []repo.MergeSourceItem{
		{Path: "img/a.png", MimeType: "image/png", SHA256: "aa", SizeBytes: 1, StorageURI: "s3://...", SourceRID: "ri.item.1", Markings: []string{}},
		{Path: "img/b.png", MimeType: "image/png", SHA256: "bb", SizeBytes: 1, StorageURI: "s3://...", SourceRID: "ri.item.2", Markings: []string{}},
	}
	r.targetPaths["img/a.png"] = struct{}{}

	_, err := s.Merge(context.Background(), MergeInput{
		MediaSetRID:  "ri.set.1",
		SourceBranch: "feature",
		Body:         models.MergeBranchRequest{TargetBranch: "main", Resolution: models.MergeFailOnConflict},
		Claims:       makeClaims([]string{"editor"}, nil),
	})
	require.Error(t, err)
	var conflict *ErrMergeConflict
	require.True(t, errors.As(err, &conflict))
	assert.Equal(t, []string{"img/a.png"}, conflict.Paths)
}

func TestMergeLatestWinsCountsCopiedAndOverwritten(t *testing.T) {
	t.Parallel()
	s, r, em := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONLESS")
	seedBranch(r, "ri.set.1", "main", nil)
	seedBranch(r, "ri.set.1", "feature", nil)
	r.mergeSourceItems = []repo.MergeSourceItem{
		{Path: "img/a.png", MimeType: "image/png", SHA256: "aa", SizeBytes: 1, StorageURI: "s3://a", SourceRID: "ri.item.1", Markings: []string{}},
		{Path: "img/b.png", MimeType: "image/png", SHA256: "bb", SizeBytes: 1, StorageURI: "s3://b", SourceRID: "ri.item.2", Markings: []string{}},
	}
	r.targetPaths["img/a.png"] = struct{}{}

	resp, err := s.Merge(context.Background(), MergeInput{
		MediaSetRID: "ri.set.1", SourceBranch: "feature",
		Body:   models.MergeBranchRequest{TargetBranch: "main"},
		Claims: makeClaims([]string{"editor"}, nil),
	})
	require.NoError(t, err)
	assert.Equal(t, "LATEST_WINS", resp.Resolution)
	assert.Equal(t, int64(1), resp.PathsCopied)
	assert.Equal(t, int64(1), resp.PathsOverwritten)
	assert.Equal(t, int64(0), resp.PathsSkipped)
	assert.Equal(t, 2, r.insertedMerged)
	require.Len(t, em.events, 1)
	assert.Equal(t, audittrail.KindMediaSetTransactionCommitted, em.events[0].Kind)
}

func TestMergeRejectsSelfTarget(t *testing.T) {
	t.Parallel()
	s, r, _ := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONLESS")
	seedBranch(r, "ri.set.1", "main", nil)

	_, err := s.Merge(context.Background(), MergeInput{
		MediaSetRID: "ri.set.1", SourceBranch: "main",
		Body:   models.MergeBranchRequest{TargetBranch: "main"},
		Claims: makeClaims([]string{"editor"}, nil),
	})
	require.Error(t, err)
	var bad *ErrBadRequest
	require.True(t, errors.As(err, &bad))
}

func TestCedarManageRequiredOnAllMutations(t *testing.T) {
	t.Parallel()
	s, r, _ := newSvc(t)
	seedSet(r, "ri.set.1", "TRANSACTIONAL")
	seedBranch(r, "ri.set.1", "main", nil)
	viewer := makeClaims([]string{"viewer"}, nil)

	_, err := s.Create(context.Background(), CreateInput{
		MediaSetRID: "ri.set.1",
		Body:        models.CreateBranchRequest{Name: "feature"},
		Claims:      viewer,
	})
	require.Error(t, err)
	var f *cedarauthzlocal.ErrForbidden
	require.True(t, errors.As(err, &f), "viewer must be denied on Create")

	err = s.Delete(context.Background(), DeleteInput{
		MediaSetRID: "ri.set.1", BranchName: "feature", Claims: viewer,
	})
	require.Error(t, err)
	require.True(t, errors.As(err, &f))

	_, err = s.Reset(context.Background(), ResetInput{
		MediaSetRID: "ri.set.1", BranchName: "main", Claims: viewer,
	})
	require.Error(t, err)
	require.True(t, errors.As(err, &f))
}
