package mediaitems

import (
	"context"
	"errors"
	"strings"
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
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/mediapath"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/storage"
)

// ── Fakes ─────────────────────────────────────────────────────────

type fakeRepo struct {
	sets         map[string]*models.MediaSet
	items        map[string]*models.MediaItem
	transactions map[string]*models.MediaSetTransaction
	deleted      []string // RIDs the soft-delete saw
	created      []*models.MediaItem
	beginEr      error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{sets: map[string]*models.MediaSet{}, items: map[string]*models.MediaItem{}}
}

func (f *fakeRepo) GetMediaSet(_ context.Context, rid string) (*models.MediaSet, error) {
	return f.sets[rid], nil
}
func (f *fakeRepo) GetMediaItem(_ context.Context, rid string) (*models.MediaItem, error) {
	v, ok := f.items[rid]
	if !ok || (v != nil && v.DeletedAt != nil) {
		return nil, nil
	}
	return v, nil
}
func (f *fakeRepo) GetMediaItemFull(_ context.Context, rid string) (*models.MediaItem, error) {
	return f.items[rid], nil
}
func (f *fakeRepo) ListMediaItems(_ context.Context, p repo.ListMediaItemsParams) ([]models.MediaItem, error) {
	out := []models.MediaItem{}
	for _, it := range f.items {
		if it.MediaSetRID == p.MediaSetRID && it.DeletedAt == nil {
			out = append(out, *it)
		}
	}
	return out, nil
}
func (f *fakeRepo) CreateMediaItem(_ context.Context, _ pgx.Tx, p repo.CreateMediaItemParams) (*models.MediaItem, error) {
	row := &models.MediaItem{
		RID: p.RID, MediaSetRID: p.MediaSetRID, Branch: p.Branch,
		TransactionRID: p.TransactionRID, Path: p.Path, MimeType: p.MimeType,
		SizeBytes: p.SizeBytes, SHA256: p.SHA256, Metadata: p.Metadata,
		StorageURI: p.StorageURI, DeduplicatedFrom: p.DeduplicatedFrom,
		Markings: []string{}, CreatedAt: time.Now(),
	}
	f.items[p.RID] = row
	f.created = append(f.created, row)
	return row, nil
}
func (f *fakeRepo) SoftDeletePreviousAtPath(_ context.Context, _ pgx.Tx, mediaSetRID, branch, path string) (*string, error) {
	for _, v := range f.items {
		if v.MediaSetRID == mediaSetRID && v.Branch == branch && v.Path == path && v.DeletedAt == nil {
			now := time.Now()
			v.DeletedAt = &now
			rid := v.RID
			return &rid, nil
		}
	}
	return nil, nil
}
func (f *fakeRepo) SoftDeleteMediaItem(_ context.Context, _ pgx.Tx, rid string) (bool, error) {
	v, ok := f.items[rid]
	if !ok || v.DeletedAt != nil {
		return false, nil
	}
	now := time.Now()
	v.DeletedAt = &now
	f.deleted = append(f.deleted, rid)
	return true, nil
}
func (f *fakeRepo) PatchMediaItemMarkings(_ context.Context, _ pgx.Tx, rid string, markings []string) (*models.MediaItem, error) {
	v, ok := f.items[rid]
	if !ok {
		return nil, nil
	}
	v.Markings = markings
	return v, nil
}
func (f *fakeRepo) CountTransactionLiveItems(_ context.Context, _ string) (int64, error) {
	return 0, nil
}
func (f *fakeRepo) GetTransaction(_ context.Context, rid string) (*models.MediaSetTransaction, error) {
	if f.transactions == nil {
		return nil, nil
	}
	return f.transactions[rid], nil
}
func (f *fakeRepo) BeginTx(_ context.Context) (pgx.Tx, error) {
	if f.beginEr != nil {
		return nil, f.beginEr
	}
	return &noopTx{}, nil
}

type noopTx struct{ pgx.Tx }

func (*noopTx) Commit(context.Context) error   { return nil }
func (*noopTx) Rollback(context.Context) error { return nil }

// fakeBackend records what the service asked of storage.
type fakeBackend struct {
	uploads   int
	downloads int
	deletes   int
}

func (b *fakeBackend) Bucket() string { return "media" }
func (b *fakeBackend) PresignUpload(_ context.Context, k mediapath.Key, mime string, ttl time.Duration) (*storage.PresignedURL, error) {
	b.uploads++
	return &storage.PresignedURL{
		URL: "https://gw/media/" + k.ObjectKey(), ExpiresAt: time.Now().Add(ttl),
		Headers: []storage.HeaderPair{{Name: "Content-Type", Value: mime}},
	}, nil
}
func (b *fakeBackend) PresignDownload(_ context.Context, k mediapath.Key, ttl time.Duration) (*storage.PresignedURL, error) {
	b.downloads++
	return &storage.PresignedURL{URL: "https://gw/media/" + k.ObjectKey(), ExpiresAt: time.Now().Add(ttl)}, nil
}
func (b *fakeBackend) Delete(_ context.Context, _ mediapath.Key) error {
	b.deletes++
	return nil
}

type recordingEmitter struct {
	events []audittrail.AuditEvent
}

func (r *recordingEmitter) emit(_ context.Context, _ pgx.Tx, e audittrail.AuditEvent, _ audittrail.AuditContext) error {
	r.events = append(r.events, e)
	return nil
}

// realEngineCedarGate adapts *cedarauthzlocal.Engine to the service's
// CedarGate interface. Tests run against the real engine + bundled
// policies so the policies themselves are exercised end-to-end.
type realCedarGate struct{ *cedarauthzlocal.Engine }

func (g *realCedarGate) CheckMediaSet(ctx context.Context, c *authmw.Claims, action cedar.EntityUID, set *models.MediaSet) error {
	return g.Engine.CheckMediaSet(ctx, c, action, set)
}
func (g *realCedarGate) CheckMediaItem(ctx context.Context, c *authmw.Claims, action cedar.EntityUID, item *models.MediaItem, parent *models.MediaSet) error {
	return g.Engine.CheckMediaItem(ctx, c, action, item, parent)
}

// ── Fixtures ─────────────────────────────────────────────────────

func newSvc(t *testing.T) (*Service, *fakeRepo, *fakeBackend, *recordingEmitter) {
	t.Helper()
	bundled, err := cedarauthzlocal.BundledPolicyRecords()
	require.NoError(t, err)
	store, err := cedarauthzlib.NewWithPolicies(bundled)
	require.NoError(t, err)
	engine := cedarauthzlocal.NewEngine(cedarauthzlib.NewEngineNoopAudit(store))

	r := newFakeRepo()
	be := &fakeBackend{}
	em := &recordingEmitter{}

	s := New(r, &realCedarGate{engine}, be, time.Minute)
	s.EmitAudit = em.emit
	return s, r, be, em
}

func makeClaims(roles, allowedMarkings []string) *authmw.Claims {
	tenant := uuid.New()
	return &authmw.Claims{
		Sub: uuid.New(), Email: "u@example.test", Name: "U",
		Roles: roles, OrgID: &tenant,
		SessionScope: &authmw.SessionScope{AllowedMarkings: allowedMarkings},
	}
}

func seedSet(r *fakeRepo, rid string, markings []string, virtual bool) *models.MediaSet {
	s := &models.MediaSet{
		RID: rid, ProjectRID: "ri.project.1", Schema: "IMAGE",
		TransactionPolicy: "TRANSACTIONLESS", Virtual: virtual,
		Markings: markings,
	}
	r.sets[rid] = s
	return s
}

// ── Tests ────────────────────────────────────────────────────────

func TestPresignUploadHappyPathEmitsAuditAndDedups(t *testing.T) {
	t.Parallel()
	s, r, be, em := newSvc(t)
	seedSet(r, "ri.set.1", nil, false)
	editor := makeClaims([]string{"editor"}, nil)
	auditCtx := audittrail.AuditContext{ActorID: editor.Sub.String()}

	// First upload — no dedup.
	body := models.PresignedUploadRequest{Path: "img/1.png", MimeType: "image/png"}
	res1, err := s.PresignUpload(context.Background(), PresignUploadInput{
		MediaSetRID: "ri.set.1", Body: body, Claims: editor, AuditCtx: auditCtx,
	})
	require.NoError(t, err)
	assert.Nil(t, res1.Item.DeduplicatedFrom)
	assert.Equal(t, 1, be.uploads)
	require.Len(t, em.events, 1)
	assert.Equal(t, audittrail.KindMediaItemUploaded, em.events[0].Kind)
	assert.Equal(t, "img/1.png", em.events[0].Path)

	// Second upload at the same path — first is soft-deleted, new
	// row stamps DeduplicatedFrom = res1.RID.
	res2, err := s.PresignUpload(context.Background(), PresignUploadInput{
		MediaSetRID: "ri.set.1", Body: body, Claims: editor, AuditCtx: auditCtx,
	})
	require.NoError(t, err)
	require.NotNil(t, res2.Item.DeduplicatedFrom)
	assert.Equal(t, res1.Item.RID, *res2.Item.DeduplicatedFrom)
}

func TestPresignUploadValidatesEmptyPath(t *testing.T) {
	t.Parallel()
	s, r, _, _ := newSvc(t)
	seedSet(r, "ri.set.1", nil, false)
	_, err := s.PresignUpload(context.Background(), PresignUploadInput{
		MediaSetRID: "ri.set.1",
		Body:        models.PresignedUploadRequest{Path: "  ", MimeType: "image/png"},
		Claims:      makeClaims([]string{"editor"}, nil),
	})
	require.Error(t, err)
	var bad *ErrBadRequest
	require.True(t, errors.As(err, &bad))
	assert.Contains(t, bad.Msg, "path")
}

func TestPresignUploadDeniedWithoutEditorRole(t *testing.T) {
	t.Parallel()
	s, r, _, _ := newSvc(t)
	seedSet(r, "ri.set.1", nil, false)
	viewer := makeClaims([]string{"viewer"}, nil)
	_, err := s.PresignUpload(context.Background(), PresignUploadInput{
		MediaSetRID: "ri.set.1",
		Body:        models.PresignedUploadRequest{Path: "p", MimeType: "image/png"},
		Claims:      viewer,
	})
	require.Error(t, err)
	var f *cedarauthzlocal.ErrForbidden
	require.True(t, errors.As(err, &f))
}

func TestPresignUploadVirtualSetIsRejected(t *testing.T) {
	t.Parallel()
	s, r, _, _ := newSvc(t)
	seedSet(r, "ri.set.1", nil, true)
	editor := makeClaims([]string{"editor"}, nil)
	_, err := s.PresignUpload(context.Background(), PresignUploadInput{
		MediaSetRID: "ri.set.1",
		Body:        models.PresignedUploadRequest{Path: "p", MimeType: "image/png"},
		Claims:      editor,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "virtual media sets")
}

// stubResolver implements VirtualResolver returning a deterministic URL.
type stubResolver struct{ called bool }

func (s *stubResolver) Resolve(_ context.Context, set *models.MediaSet, item *models.MediaItem, ttl time.Duration) (*storage.PresignedURL, error) {
	s.called = true
	return &storage.PresignedURL{
		URL:       "https://external.example/" + item.RID,
		ExpiresAt: time.Now().Add(ttl).UTC(),
	}, nil
}

// stubSigner implements PresignSigner returning a constant token so
// the test asserts the URL is augmented as expected.
type stubSigner struct{ called bool }

func (s *stubSigner) Sign(sub, itemRID string, _ []string, _ time.Duration) (string, error) {
	s.called = true
	return "TOK-" + sub + "-" + itemRID, nil
}

func TestPresignDownloadVirtualUsesResolver(t *testing.T) {
	t.Parallel()
	s, r, _, _ := newSvc(t)
	parent := seedSet(r, "ri.set.virtual", nil, true)
	parent.SourceRID = func() *string { v := "ri.foundry.main.source.x"; return &v }()
	r.items["ri.item.v"] = &models.MediaItem{
		RID: "ri.item.v", MediaSetRID: parent.RID, Branch: "main",
		StorageURI: "s3://external/key/object.png", Markings: []string{},
	}
	resolver := &stubResolver{}
	s.VirtualResolver = resolver

	res, err := s.PresignDownload(context.Background(), PresignDownloadInput{
		ItemRID: "ri.item.v",
		Claims:  makeClaims([]string{"viewer"}, nil),
	})
	require.NoError(t, err)
	assert.True(t, resolver.called, "virtual resolver must be consulted")
	assert.Equal(t, "https://external.example/ri.item.v", res.URL.URL)
}

func TestPresignDownloadAppendsClaimWhenSignerWired(t *testing.T) {
	t.Parallel()
	s, r, _, _ := newSvc(t)
	parent := seedSet(r, "ri.set.1", nil, false)
	r.items["ri.item.1"] = &models.MediaItem{
		RID: "ri.item.1", MediaSetRID: parent.RID, Branch: "main",
		SHA256: "abcd", Markings: []string{},
	}
	signer := &stubSigner{}
	s.PresignSigner = signer

	res, err := s.PresignDownload(context.Background(), PresignDownloadInput{
		ItemRID: "ri.item.1",
		Claims:  makeClaims([]string{"viewer"}, nil),
	})
	require.NoError(t, err)
	assert.True(t, signer.called)
	// The fake storage backend returns "https://gw/media/<key>";
	// the signer appends "?claim=TOK-..." (or "&claim=..." if a "?"
	// already exists). Either way the claim must be present.
	assert.Contains(t, res.URL.URL, "claim=TOK-")
}

func TestPresignDownloadCedarRequiresClearance(t *testing.T) {
	t.Parallel()
	s, r, be, em := newSvc(t)
	parent := seedSet(r, "ri.set.1", []string{"pii"}, false)
	now := time.Now()
	r.items["ri.item.1"] = &models.MediaItem{
		RID: "ri.item.1", MediaSetRID: parent.RID, Branch: "main",
		Path: "p", MimeType: "image/png", SizeBytes: 1024,
		SHA256: "deadbeef", Markings: []string{}, CreatedAt: now,
	}

	uncleared := makeClaims([]string{"viewer"}, nil)
	_, err := s.PresignDownload(context.Background(), PresignDownloadInput{
		ItemRID: "ri.item.1", Claims: uncleared,
	})
	require.Error(t, err)
	var f *cedarauthzlocal.ErrForbidden
	require.True(t, errors.As(err, &f))

	cleared := makeClaims([]string{"viewer"}, []string{"pii"})
	res, err := s.PresignDownload(context.Background(), PresignDownloadInput{
		ItemRID: "ri.item.1", Claims: cleared,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, be.downloads)
	require.NotEmpty(t, res.URL.URL)
	require.Len(t, em.events, 1)
	assert.Equal(t, audittrail.KindMediaItemDownloaded, em.events[0].Kind)
}

func TestDeleteSoftDeletesAndEmitsAudit(t *testing.T) {
	t.Parallel()
	s, r, be, em := newSvc(t)
	parent := seedSet(r, "ri.set.1", nil, false)
	r.items["ri.item.1"] = &models.MediaItem{
		RID: "ri.item.1", MediaSetRID: parent.RID, Branch: "main",
		SHA256: "deadbeef", Markings: []string{}, SizeBytes: 4096,
	}
	editor := makeClaims([]string{"editor"}, nil)

	require.NoError(t, s.Delete(context.Background(), DeleteInput{
		ItemRID: "ri.item.1", Claims: editor,
	}))
	assert.Equal(t, []string{"ri.item.1"}, r.deleted)
	assert.Equal(t, 1, be.deletes)
	require.Len(t, em.events, 1)
	assert.Equal(t, audittrail.KindMediaItemDeleted, em.events[0].Kind)

	// Idempotent: second call no-ops, no extra audit.
	require.NoError(t, s.Delete(context.Background(), DeleteInput{
		ItemRID: "ri.item.1", Claims: editor,
	}))
	require.Len(t, em.events, 1, "second delete must not re-emit")
}

func TestPatchMarkingsNormalizesAndAuditsOverride(t *testing.T) {
	t.Parallel()
	s, r, _, em := newSvc(t)
	parent := seedSet(r, "ri.set.1", nil, false)
	r.items["ri.item.1"] = &models.MediaItem{
		RID: "ri.item.1", MediaSetRID: parent.RID, Branch: "main",
		Markings: []string{"PII"},
	}
	editor := makeClaims([]string{"editor"}, nil)

	row, err := s.PatchMarkings(context.Background(), PatchMarkingsInput{
		ItemRID:  "ri.item.1",
		Markings: []string{"  Secret ", "secret", "PII"},
		Claims:   editor,
	})
	require.NoError(t, err)
	// dedup + lowercase + sorted.
	assert.Equal(t, []string{"pii", "secret"}, row.Markings)

	require.Len(t, em.events, 1)
	got := em.events[0]
	assert.Equal(t, audittrail.KindMediaItemMarkingOverridden, got.Kind)
	assert.Equal(t, []string{"pii"}, got.PreviousMarkings)
}

func TestRegisterVirtualOnlyOnVirtualSet(t *testing.T) {
	t.Parallel()
	s, r, _, em := newSvc(t)
	seedSet(r, "ri.set.1", nil, true)
	editor := makeClaims([]string{"editor"}, nil)

	row, err := s.RegisterVirtual(context.Background(), RegisterVirtualInput{
		MediaSetRID: "ri.set.1",
		Body: models.RegisterVirtualItemRequest{
			PhysicalPath: "s3://external/key",
			ItemPath:     "imgs/1.png",
		},
		Claims: editor,
	})
	require.NoError(t, err)
	assert.Equal(t, "s3://external/key", row.StorageURI)
	require.Len(t, em.events, 1)
	assert.Equal(t, audittrail.KindVirtualMediaItemRegistered, em.events[0].Kind)

	// Same op on a non-virtual set is rejected.
	seedSet(r, "ri.set.2", nil, false)
	_, err = s.RegisterVirtual(context.Background(), RegisterVirtualInput{
		MediaSetRID: "ri.set.2",
		Body:        models.RegisterVirtualItemRequest{PhysicalPath: "x", ItemPath: "y"},
		Claims:      editor,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only valid on virtual media sets")
}

func TestListFiltersToReadableItems(t *testing.T) {
	t.Parallel()
	s, r, _, _ := newSvc(t)
	parent := seedSet(r, "ri.set.1", []string{"pii"}, false)
	r.items["ri.a"] = &models.MediaItem{
		RID: "ri.a", MediaSetRID: parent.RID, Branch: "main",
		Path: "a", Markings: []string{},
	}
	// Item ri.b adds SECRET — only operators with SECRET can read.
	r.items["ri.b"] = &models.MediaItem{
		RID: "ri.b", MediaSetRID: parent.RID, Branch: "main",
		Path: "b", Markings: []string{"secret"},
	}
	piiOnly := makeClaims([]string{"viewer"}, []string{"pii"})

	visible, err := s.List(context.Background(), ListInput{
		MediaSetRID: parent.RID, Claims: piiOnly,
	})
	require.NoError(t, err)
	require.Len(t, visible, 1, "PII-only viewer sees only ri.a")
	assert.Equal(t, "ri.a", visible[0].RID)
}

func TestPresignUploadRejectsTransactionalWithoutTransactionRID(t *testing.T) {
	t.Parallel()
	s, r, _, _ := newSvc(t)
	r.sets["ri.set.1"] = &models.MediaSet{
		RID: "ri.set.1", ProjectRID: "ri.proj.1",
		Schema: "IMAGE", TransactionPolicy: "TRANSACTIONAL",
	}
	editor := makeClaims([]string{"editor"}, nil)
	_, err := s.PresignUpload(context.Background(), PresignUploadInput{
		MediaSetRID: "ri.set.1",
		Body:        models.PresignedUploadRequest{Path: "p", MimeType: "image/png"},
		Claims:      editor,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transaction_rid")
}

func TestPresignUploadDefaultsBranchAndComputesPlaceholderSha(t *testing.T) {
	t.Parallel()
	s, r, _, _ := newSvc(t)
	seedSet(r, "ri.set.1", nil, false)
	res, err := s.PresignUpload(context.Background(), PresignUploadInput{
		MediaSetRID: "ri.set.1",
		Body:        models.PresignedUploadRequest{Path: "p", MimeType: "image/png"},
		Claims:      makeClaims([]string{"editor"}, nil),
	})
	require.NoError(t, err)
	assert.Equal(t, "main", res.Item.Branch)
	assert.Len(t, res.Item.SHA256, 64, "placeholder hash is exactly 64 hex chars (sha256 length)")
	assert.True(t, strings.HasPrefix(res.Item.StorageURI, "s3://media/media-sets/ri.set.1/main/"))
}
