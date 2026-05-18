package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"

	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/audit"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/kms"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/repo"
)

// memRepo is a tiny in-memory Repository for handler tests. Not
// exposed outside this file — production wiring uses *repo.Repo.
type memRepo struct {
	mu             sync.Mutex
	keys           map[uuid.UUID]*domain.CipherKey
	versions       map[uuid.UUID]map[uint32]*domain.CipherKeyVersion
	peppers        map[uuid.UUID]*domain.Pepper
	pepperVersions map[uuid.UUID]map[uint32]*domain.PepperVersion
}

func newMemRepo() *memRepo {
	return &memRepo{
		keys:           make(map[uuid.UUID]*domain.CipherKey),
		versions:       make(map[uuid.UUID]map[uint32]*domain.CipherKeyVersion),
		peppers:        make(map[uuid.UUID]*domain.Pepper),
		pepperVersions: make(map[uuid.UUID]map[uint32]*domain.PepperVersion),
	}
}

func (m *memRepo) InsertKey(_ context.Context, p repo.CreateKeyParams) (*domain.CipherKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range m.keys {
		if k.TenantID == p.TenantID && k.Alias == p.Alias {
			return nil, repo.ErrAliasConflict
		}
	}
	now := time.Now().UTC()
	accessPolicy := p.AccessPolicy
	if len(accessPolicy.Encrypt.Roles) == 0 && len(accessPolicy.Encrypt.Groups) == 0 && len(accessPolicy.Encrypt.Projects) == 0 &&
		len(accessPolicy.Decrypt.Roles) == 0 && len(accessPolicy.Decrypt.Groups) == 0 && len(accessPolicy.Decrypt.Projects) == 0 &&
		len(accessPolicy.Manage.Roles) == 0 && len(accessPolicy.Manage.Groups) == 0 && len(accessPolicy.Manage.Projects) == 0 {
		accessPolicy = domain.DefaultAccessPolicy()
	}
	k := &domain.CipherKey{
		ID: p.ID, TenantID: p.TenantID, Alias: p.Alias, Algorithm: p.Algorithm,
		KeyMaterialRef: p.KMSKeyRef, KMSBackend: p.KMSBackend.OrDefault(), OwnerID: p.OwnerID,
		Organizations: append([]uuid.UUID(nil), p.Organizations...),
		Markings:      append([]string(nil), p.Markings...), IntendedScopes: append([]string(nil), p.IntendedScopes...),
		AccessPolicy: accessPolicy.Clone(),
		Version:      1, Status: domain.StatusActive, CreatedAt: now, ExpiresAt: p.ExpiresAt,
	}
	m.keys[p.ID] = k
	m.versions[p.ID] = map[uint32]*domain.CipherKeyVersion{
		1: {
			KeyID: p.ID, Version: 1,
			WrappedKeyMaterial: append([]byte(nil), p.WrappedKeyMaterial...),
			KMSKeyRef:          p.KMSKeyRef,
			CreatedAt:          now, ActivatedAt: now,
		},
	}
	return k, nil
}

func (m *memRepo) GetKey(_ context.Context, tenantID, id uuid.UUID) (*domain.CipherKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[id]
	if !ok || k.TenantID != tenantID {
		return nil, domain.ErrKeyNotFound
	}
	return k.Clone(), nil
}

func (m *memRepo) ListKeys(_ context.Context, tenantID uuid.UUID, _ repo.ListPage) (repo.ListResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*domain.CipherKey, 0)
	for _, k := range m.keys {
		if k.TenantID == tenantID {
			out = append(out, k.Clone())
		}
	}
	return repo.ListResult{Items: out}, nil
}

func (m *memRepo) UpdateKeyMetadata(_ context.Context, tenantID, id uuid.UUID, p repo.UpdateKeyParams) (*domain.CipherKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[id]
	if !ok || k.TenantID != tenantID {
		return nil, domain.ErrKeyNotFound
	}
	if p.Alias != nil {
		for _, existing := range m.keys {
			if existing.ID != id && existing.TenantID == tenantID && existing.Alias == *p.Alias {
				return nil, repo.ErrAliasConflict
			}
		}
		k.Alias = *p.Alias
	}
	if p.OwnerID != nil {
		k.OwnerID = *p.OwnerID
	}
	if p.OrganizationsSet {
		k.Organizations = append([]uuid.UUID(nil), p.Organizations...)
	}
	if p.MarkingsSet {
		k.Markings = append([]string(nil), p.Markings...)
	}
	if p.IntendedScopesSet {
		k.IntendedScopes = append([]string(nil), p.IntendedScopes...)
	}
	if p.ExpiresAtSet {
		k.ExpiresAt = p.ExpiresAt
	}
	return k.Clone(), nil
}

func (m *memRepo) DeleteKey(_ context.Context, tenantID, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[id]
	if !ok || k.TenantID != tenantID {
		return domain.ErrKeyNotFound
	}
	delete(m.keys, id)
	delete(m.versions, id)
	return nil
}

func (m *memRepo) GetVersion(_ context.Context, tenantID, keyID uuid.UUID, version uint32) (*domain.CipherKeyVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[keyID]
	if !ok || k.TenantID != tenantID {
		return nil, domain.ErrKeyNotFound
	}
	v, ok := m.versions[keyID][version]
	if !ok {
		return nil, domain.ErrKeyVersionNotFound
	}
	cp := *v
	return &cp, nil
}

func (m *memRepo) RotateKey(_ context.Context, tenantID, keyID uuid.UUID, wrappedDEK []byte, kmsRef string) (*domain.CipherKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[keyID]
	if !ok || k.TenantID != tenantID {
		return nil, domain.ErrKeyNotFound
	}
	if k.Status == domain.StatusRetired {
		return nil, domain.ErrKeyRetired
	}
	now := time.Now().UTC()
	if prev, ok := m.versions[keyID][k.Version]; ok {
		prev.RetiredAt = &now
	}
	k.Version++
	k.Status = domain.StatusRotating
	k.RotatedAt = &now
	m.versions[keyID][k.Version] = &domain.CipherKeyVersion{
		KeyID: keyID, Version: k.Version,
		WrappedKeyMaterial: append([]byte(nil), wrappedDEK...),
		KMSKeyRef:          kmsRef,
		CreatedAt:          now, ActivatedAt: now,
	}
	return k.Clone(), nil
}

func (m *memRepo) RetireKey(_ context.Context, tenantID, keyID uuid.UUID) (*domain.CipherKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[keyID]
	if !ok || k.TenantID != tenantID {
		return nil, domain.ErrKeyNotFound
	}
	k.Status = domain.StatusRetired
	return k.Clone(), nil
}

func (m *memRepo) MarkRotationComplete(_ context.Context, tenantID, keyID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[keyID]
	if !ok || k.TenantID != tenantID {
		return nil
	}
	if k.Status == domain.StatusRotating {
		k.Status = domain.StatusActive
	}
	return nil
}

func (m *memRepo) RevokeKey(_ context.Context, tenantID, keyID uuid.UUID) (*domain.CipherKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[keyID]
	if !ok || k.TenantID != tenantID {
		return nil, domain.ErrKeyNotFound
	}
	k.Status = domain.StatusRevoked
	return k.Clone(), nil
}

func (m *memRepo) RotateKeyToNewID(_ context.Context, tenantID, keyID, newKeyID uuid.UUID, wrappedDEK []byte, kmsRef string) (*domain.CipherKey, error) {
	m.mu.Lock()
	old, ok := m.keys[keyID]
	m.mu.Unlock()
	if !ok || old.TenantID != tenantID {
		return nil, domain.ErrKeyNotFound
	}
	return m.InsertKey(context.Background(), repo.CreateKeyParams{ID: newKeyID, TenantID: tenantID, Alias: old.Alias + "-rotated", Algorithm: old.Algorithm, WrappedKeyMaterial: wrappedDEK, KMSKeyRef: kmsRef, KMSBackend: old.KMSBackend, OwnerID: old.OwnerID, Organizations: old.Organizations, Markings: old.Markings, IntendedScopes: old.IntendedScopes, AccessPolicy: old.AccessPolicy, ExpiresAt: old.ExpiresAt})
}

func (m *memRepo) InsertPepper(_ context.Context, p repo.CreatePepperParams) (*domain.Pepper, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.peppers {
		if existing.TenantID == p.TenantID && existing.Name == p.Name {
			return nil, repo.ErrAliasConflict
		}
	}
	policy := p.AccessPolicy
	if len(policy.Encrypt.Roles) == 0 && len(policy.Manage.Roles) == 0 {
		policy = domain.DefaultAccessPolicy()
	}
	now := time.Now().UTC()
	pepper := &domain.Pepper{ID: p.ID, TenantID: p.TenantID, Name: p.Name, Algorithm: p.Algorithm, PepperMaterialRef: p.KMSKeyRef, Version: 1, AccessPolicy: policy.Clone(), CreatedAt: now}
	m.peppers[p.ID] = pepper
	m.pepperVersions[p.ID] = map[uint32]*domain.PepperVersion{1: {PepperID: p.ID, Version: 1, WrappedPepperMaterial: append([]byte(nil), p.WrappedPepperMaterial...), KMSKeyRef: p.KMSKeyRef, CreatedAt: now}}
	return pepper.Clone(), nil
}

func (m *memRepo) GetPepper(_ context.Context, tenantID, id uuid.UUID) (*domain.Pepper, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.peppers[id]
	if !ok || p.TenantID != tenantID {
		return nil, domain.ErrKeyNotFound
	}
	return p.Clone(), nil
}

func (m *memRepo) GetPepperVersion(_ context.Context, tenantID, pepperID uuid.UUID, version uint32) (*domain.PepperVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.peppers[pepperID]
	if !ok || p.TenantID != tenantID {
		return nil, domain.ErrKeyNotFound
	}
	v, ok := m.pepperVersions[pepperID][version]
	if !ok {
		return nil, domain.ErrKeyVersionNotFound
	}
	cp := *v
	return &cp, nil
}

func (m *memRepo) RotatePepper(_ context.Context, tenantID, id uuid.UUID, wrapped []byte, kmsRef string) (*domain.Pepper, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.peppers[id]
	if !ok || p.TenantID != tenantID {
		return nil, domain.ErrKeyNotFound
	}
	now := time.Now().UTC()
	p.Version++
	p.RotatedAt = &now
	m.pepperVersions[id][p.Version] = &domain.PepperVersion{PepperID: id, Version: p.Version, WrappedPepperMaterial: append([]byte(nil), wrapped...), KMSKeyRef: kmsRef, CreatedAt: now}
	return p.Clone(), nil
}

// ─── Test harness ──────────────────────────────────────────────────────

func newTestState(t *testing.T) *State {
	t.Helper()
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i + 1)
	}
	k, err := kms.NewLocalKMS(kek, "local:test")
	if err != nil {
		t.Fatalf("NewLocalKMS: %v", err)
	}
	return &State{
		Repo:  newMemRepo(),
		KMS:   k,
		Audit: audit.NewRecorder(nil, nil), // emitter omitted; Recorder is nil-safe
	}
}

// withClaims is a chi-compatible middleware that injects a fake
// authenticated subject so handlers see a populated tenant context.
func withClaims(tenant uuid.UUID) func(http.Handler) http.Handler {
	return withClaimsValue(&authmw.Claims{Sub: uuid.New(), OrgID: &tenant, Roles: []string{authmw.RoleAdmin}})
}

func withClaimsValue(c *authmw.Claims) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := authmw.ContextWithClaims(r.Context(), c)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func buildRouter(state *State, tenant uuid.UUID) http.Handler {
	return buildRouterWithMiddleware(state, withClaims(tenant))
}

func buildRouterWithMiddleware(state *State, mw func(http.Handler) http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(mw)
	r.Get("/algorithms", state.ListAlgorithms)
	r.Post("/keys", state.CreateKey)
	r.Get("/keys", state.ListKeys)
	r.Get("/keys/{id}", state.GetKey)
	r.Patch("/keys/{id}", state.UpdateKey)
	r.Delete("/keys/{id}", state.DeleteKey)
	r.Post("/keys/{id}/rotate", state.RotateKey)
	r.Post("/keys/{id}/rotate-new", state.RotateKeyToNewID)
	r.Post("/keys/{id}/wrap-for-promotion", state.WrapKeyForPromotion)
	r.Post("/keys/{id}/retire", state.RetireKey)
	r.Post("/keys/{id}/revoke", state.RevokeKey)
	r.Post("/peppers", state.CreatePepper)
	r.Post("/peppers/{id}/rotate", state.RotatePepper)
	r.Post("/encrypt", state.Encrypt)
	r.Post("/encrypt-batch", state.EncryptBatch)
	r.Post("/tokenize", state.Tokenize)
	r.Post("/decrypt", state.Decrypt)
	r.Post("/decrypt-batch", state.DecryptBatch)
	r.Post("/decrypt-stream", state.DecryptStream)
	return r
}

func decode[T any](t *testing.T, resp *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode (body=%q): %v", resp.Body.String(), err)
	}
	return out
}

// ─── Tests ─────────────────────────────────────────────────────────────

func TestListAlgorithms_RegistryMetadata(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/algorithms", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	got := decode[listAlgorithmsResponse](t, w)
	if len(got.Items) != 5 {
		t.Fatalf("algorithm count = %d, want 5: %+v", len(got.Items), got.Items)
	}
	byID := make(map[string]algorithmResponse, len(got.Items))
	for _, item := range got.Items {
		byID[item.ID] = item
		if item.StableIdentifier != item.ID {
			t.Fatalf("stable identifier must match registry id: %+v", item)
		}
		if item.OutputEncoding != "base64" {
			t.Fatalf("%s output encoding = %q, want base64", item.ID, item.OutputEncoding)
		}
	}
	if !byID["AES_256_GCM_SIV"].RecommendedDefault || !byID["AES_256_GCM_SIV"].Authenticated {
		t.Fatalf("GCM-SIV descriptor should be authenticated recommended default: %+v", byID["AES_256_GCM_SIV"])
	}
	if byID["AES_256_GCM"].NoncePolicy != "random_96_bit_per_encryption" || byID["AES_256_GCM"].KeyLengthBytes != 32 {
		t.Fatalf("GCM descriptor mismatch: %+v", byID["AES_256_GCM"])
	}
	if !byID["AES_256_SIV"].Deterministic || byID["AES_256_SIV"].SecurityNotice == "" {
		t.Fatalf("AES-SIV descriptor must document deterministic trade-off: %+v", byID["AES_256_SIV"])
	}
	if !byID["SHA_256"].PepperRequired || byID["SHA_256"].KeyLengthBytes != 32 {
		t.Fatalf("SHA_256 descriptor mismatch: %+v", byID["SHA_256"])
	}
	if !byID["SHA_512"].PepperRequired || byID["SHA_512"].KeyLengthBytes != 64 {
		t.Fatalf("SHA_512 descriptor mismatch: %+v", byID["SHA_512"])
	}
}

func TestCreateKey_RejectsHashAlgorithmUntilPepperResource(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)

	body := bytes.NewBufferString(`{"alias":"hash","algorithm":"SHA_256"}`)
	req := httptest.NewRequest(http.MethodPost, "/keys", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", w.Code, w.Body.String())
	}
}

func TestCreateKey_ReturnsMetadataOnly(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)

	body := bytes.NewBufferString(`{"name":"pii","algorithm":"AES_256_GCM","organizations":["` + tenant.String() + `"],"markings":["PII"],"intended_scopes":["datasets","object_properties"],"expires_at":"2030-01-02T03:04:05Z"}`)
	req := httptest.NewRequest(http.MethodPost, "/keys", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	got := decode[keyResponse](t, w)
	if got.Name != "pii" || got.Alias != "pii" || got.Version != 1 || got.Status != "active" {
		t.Fatalf("unexpected response: %+v", got)
	}
	if got.KeyMaterialRef == "" || got.OwnerID == "" {
		t.Fatalf("key material ref and owner should be opaque metadata only: %+v", got)
	}
	if len(got.Organizations) != 1 || got.Organizations[0] != tenant.String() {
		t.Fatalf("organizations not preserved: %+v", got.Organizations)
	}
	if len(got.Markings) != 1 || got.Markings[0] != "PII" {
		t.Fatalf("markings not preserved: %+v", got.Markings)
	}
	if len(got.IntendedScopes) != 2 || got.IntendedScopes[0] != "datasets" || got.IntendedScopes[1] != "object_properties" {
		t.Fatalf("intended scopes not preserved: %+v", got.IntendedScopes)
	}
	if got.ExpiresAt == nil || *got.ExpiresAt != "2030-01-02T03:04:05.000000Z" {
		t.Fatalf("expires_at not normalized: %+v", got.ExpiresAt)
	}
	// Body must not leak wrapped material — sanity check the JSON.
	if bytes.Contains(w.Body.Bytes(), []byte("wrapped")) {
		t.Fatal("response leaked wrapped key material")
	}
}

func TestUpdateAndDeleteKey_CRUDMetadata(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{"name":"crud"}`)))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	created := decode[keyResponse](t, w)

	owner := uuid.New()
	patchBody := bytes.NewBufferString(`{"name":"crud-updated","owner_id":"` + owner.String() + `","organizations":["` + tenant.String() + `"],"markings":["SENSITIVE"],"intended_scopes":["function_calls"],"expires_at":"2031-02-03T04:05:06Z"}`)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, "/keys/"+created.ID, patchBody))
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", w.Code, w.Body.String())
	}
	updated := decode[keyResponse](t, w)
	if updated.Name != "crud-updated" || updated.OwnerID != owner.String() {
		t.Fatalf("metadata update not reflected: %+v", updated)
	}
	if len(updated.Markings) != 1 || updated.Markings[0] != "SENSITIVE" || len(updated.IntendedScopes) != 1 || updated.IntendedScopes[0] != "function_calls" {
		t.Fatalf("classification/scope metadata not reflected: %+v", updated)
	}
	if updated.ExpiresAt == nil || *updated.ExpiresAt != "2031-02-03T04:05:06.000000Z" {
		t.Fatalf("expiry not reflected: %+v", updated.ExpiresAt)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/keys/"+created.ID, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/keys/"+created.ID, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestSingleEncryptDecrypt_SelfDescribingEnvelope(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{"name":"single"}`)))
	created := decode[keyResponse](t, w)

	w = httptest.NewRecorder()
	body := bytes.NewBufferString(`{"key_id":"` + created.ID + `","plaintext":"hello","algorithm":"AES_256_GCM"}`)
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", body))
	if w.Code != http.StatusOK {
		t.Fatalf("encrypt status=%d body=%s", w.Code, w.Body.String())
	}
	enc := decode[encryptResult](t, w)
	if enc.Ciphertext == "" || enc.CiphertextB64 == "" {
		t.Fatalf("single encrypt did not return ciphertext aliases: %+v", enc)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/decrypt", bytes.NewBufferString(`{"ciphertext":"`+enc.Ciphertext+`"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("decrypt status=%d body=%s", w.Code, w.Body.String())
	}
	dec := decode[decryptResult](t, w)
	if dec.Plaintext != "hello" {
		t.Fatalf("plaintext=%q", dec.Plaintext)
	}
}

func TestPerKeyAccessPolicy_DenyByDefaultAndRoleGrant(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	adminRouter := buildRouter(state, tenant)

	policy := `{"encrypt":{"roles":["cipher_user"]},"decrypt":{"roles":["cipher_user"]},"manage":{"roles":["admin"]}}`
	w := httptest.NewRecorder()
	adminRouter.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{"name":"policy","access_policy":`+policy+`}`)))
	created := decode[keyResponse](t, w)

	plain := base64.StdEncoding.EncodeToString([]byte("secret"))
	deniedClaims := &authmw.Claims{Sub: uuid.New(), OrgID: &tenant, Roles: []string{"analyst"}}
	deniedRouter := buildRouterWithMiddleware(state, withClaimsValue(deniedClaims))
	w = httptest.NewRecorder()
	deniedRouter.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", bytes.NewBufferString(`{"key_id":"`+created.ID+`","plaintext_b64":"`+plain+`"}`)))
	if w.Code != http.StatusBadRequest || !bytes.Contains(w.Body.Bytes(), []byte("access denied")) {
		t.Fatalf("expected access denied, status=%d body=%s", w.Code, w.Body.String())
	}

	allowedClaims := &authmw.Claims{Sub: uuid.New(), OrgID: &tenant, Roles: []string{"cipher_user"}}
	allowedRouter := buildRouterWithMiddleware(state, withClaimsValue(allowedClaims))
	w = httptest.NewRecorder()
	allowedRouter.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", bytes.NewBufferString(`{"key_id":"`+created.ID+`","plaintext_b64":"`+plain+`"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("role-granted encrypt status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDecrypt_MarkingDenied(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	adminRouter := buildRouter(state, tenant)
	policy := `{"encrypt":{"roles":["admin"]},"decrypt":{"roles":["reader"]},"manage":{"roles":["admin"]}}`
	w := httptest.NewRecorder()
	adminRouter.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{"name":"marked","markings":["pii"],"access_policy":`+policy+`}`)))
	created := decode[keyResponse](t, w)
	w = httptest.NewRecorder()
	adminRouter.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", bytes.NewBufferString(`{"key_id":"`+created.ID+`","plaintext":"secret"}`)))
	enc := decode[encryptResult](t, w)

	readerClaims := &authmw.Claims{Sub: uuid.New(), OrgID: &tenant, Roles: []string{"reader"}}
	readerRouter := buildRouterWithMiddleware(state, withClaimsValue(readerClaims))
	w = httptest.NewRecorder()
	readerRouter.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/decrypt", bytes.NewBufferString(`{"ciphertext":"`+enc.Ciphertext+`","resource_markings":["confidential"]}`)))
	if w.Code != http.StatusForbidden || !bytes.Contains(w.Body.Bytes(), []byte("MarkingDenied")) {
		t.Fatalf("expected MarkingDenied, status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateKey_AES256SIVExplicitOptIn(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{"name":"siv","algorithm":"AES_256_SIV"}`)))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	created := decode[keyResponse](t, w)
	if created.Algorithm != "AES_256_SIV" {
		t.Fatalf("algorithm = %s", created.Algorithm)
	}

	body := `{"key_id":"` + created.ID + `","plaintext":"same"}`
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", bytes.NewBufferString(body)))
	first := decode[encryptResult](t, w)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", bytes.NewBufferString(body)))
	second := decode[encryptResult](t, w)
	if first.Ciphertext == "" || first.Ciphertext != second.Ciphertext {
		t.Fatalf("SIV ciphertext should be deterministic: first=%+v second=%+v", first, second)
	}
}

func TestPepperTokenizeStableAndRotate(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/peppers", bytes.NewBufferString(`{"name":"p","algorithm":"SHA_256"}`)))
	if w.Code != http.StatusCreated {
		t.Fatalf("pepper create=%d body=%s", w.Code, w.Body.String())
	}
	pepper := decode[pepperResponse](t, w)
	body := `{"pepper_id":"` + pepper.ID + `","plaintext":"alice@example.com"}`
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/tokenize", bytes.NewBufferString(body)))
	first := decode[tokenizeResponse](t, w)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/tokenize", bytes.NewBufferString(body)))
	second := decode[tokenizeResponse](t, w)
	if first.Token == "" || first.Token != second.Token {
		t.Fatalf("token should be stable: %+v %+v", first, second)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/peppers/"+pepper.ID+"/rotate", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("pepper rotate=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRotateNewKeyAndRevoke(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{"name":"life"}`)))
	created := decode[keyResponse](t, w)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys/"+created.ID+"/rotate-new", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("rotate-new=%d body=%s", w.Code, w.Body.String())
	}
	next := decode[keyResponse](t, w)
	if next.ID == created.ID || next.AccessPolicy.Manage.Roles[0] != "admin" {
		t.Fatalf("bad successor: %+v", next)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys/"+created.ID+"/revoke", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("revoke=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", bytes.NewBufferString(`{"key_id":"`+created.ID+`","plaintext":"x"}`)))
	if !bytes.Contains(w.Body.Bytes(), []byte("revoked")) {
		t.Fatalf("revoked key should fail encrypt: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateKey_AliasConflict(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)

	for i, want := range []int{http.StatusCreated, http.StatusConflict} {
		body := bytes.NewBufferString(`{"alias":"dup"}`)
		req := httptest.NewRequest(http.MethodPost, "/keys", body)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("attempt %d: status = %d, want %d, body=%s", i, w.Code, want, w.Body.String())
		}
	}
}

// TestEncryptDecrypt_Roundtrip exercises the canonical happy path
// through the wire layer.
func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)

	// Create a key.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys",
		bytes.NewBufferString(`{"alias":"r"}`)))
	created := decode[keyResponse](t, w)

	plain := []byte("the secret")
	encReq := []encryptItem{{
		KeyID:        created.ID,
		PlaintextB64: base64.StdEncoding.EncodeToString(plain),
	}}
	encBody, _ := json.Marshal(encReq)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", bytes.NewReader(encBody)))
	if w.Code != http.StatusOK {
		t.Fatalf("encrypt status = %d body=%s", w.Code, w.Body.String())
	}
	encOut := decode[[]encryptResult](t, w)
	if len(encOut) != 1 || encOut[0].Error != "" || encOut[0].CiphertextB64 == "" {
		t.Fatalf("encrypt result = %+v", encOut)
	}
	envelopeJSON, err := base64.StdEncoding.DecodeString(encOut[0].CiphertextB64)
	if err != nil {
		t.Fatalf("envelope base64 decode: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(envelopeJSON, &envelope); err != nil {
		t.Fatalf("envelope must decode to JSON: %v body=%s", err, string(envelopeJSON))
	}
	for _, field := range []string{"key_id", "algorithm_id", "nonce", "ciphertext", "auth_tag", "schema_version"} {
		if _, ok := envelope[field]; !ok {
			t.Fatalf("self-describing envelope missing %s: %+v", field, envelope)
		}
	}

	decReq := []decryptItem{{
		CiphertextB64: encOut[0].CiphertextB64,
	}}
	decBody, _ := json.Marshal(decReq)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/decrypt", bytes.NewReader(decBody)))
	if w.Code != http.StatusOK {
		t.Fatalf("decrypt status = %d body=%s", w.Code, w.Body.String())
	}
	decOut := decode[[]decryptResult](t, w)
	if len(decOut) != 1 || decOut[0].Error != "" {
		t.Fatalf("decrypt result = %+v", decOut)
	}
	got, err := base64.StdEncoding.DecodeString(decOut[0].PlaintextB64)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("roundtrip mismatch: got %q want %q", got, plain)
	}
}

// TestEncrypt_RetiredKey enforces the domain invariant: retired keys
// refuse new encrypts but stay decryptable.
func TestEncrypt_RetiredKey(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys",
		bytes.NewBufferString(`{"alias":"to-retire"}`)))
	created := decode[keyResponse](t, w)

	// Encrypt while still active so we have a ciphertext to test
	// decrypt-after-retire below.
	encReq := []encryptItem{{
		KeyID:        created.ID,
		PlaintextB64: base64.StdEncoding.EncodeToString([]byte("locked")),
	}}
	encBody, _ := json.Marshal(encReq)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", bytes.NewReader(encBody)))
	encOut := decode[[]encryptResult](t, w)

	// Retire the key.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys/"+created.ID+"/retire", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("retire = %d", w.Code)
	}

	// New encrypts must fail per-item.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", bytes.NewReader(encBody)))
	postRetireEnc := decode[[]encryptResult](t, w)
	if len(postRetireEnc) != 1 || postRetireEnc[0].Error == "" {
		t.Fatalf("encrypt after retire must surface a per-item error: %+v", postRetireEnc)
	}

	// Old envelopes still decrypt.
	decReq := []decryptItem{{
		KeyID:         created.ID,
		Version:       encOut[0].Version,
		CiphertextB64: encOut[0].CiphertextB64,
	}}
	decBody, _ := json.Marshal(decReq)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/decrypt", bytes.NewReader(decBody)))
	decOut := decode[[]decryptResult](t, w)
	if len(decOut) != 1 || decOut[0].Error != "" {
		t.Fatalf("decrypt after retire must succeed for old envelopes: %+v", decOut)
	}
}

// TestRotateKey_NewVersionEncryptsOldDecrypts validates the rotation
// contract: new encrypts use v(N+1) but old ciphertexts still open.
func TestRotateKey_NewVersionEncryptsOldDecrypts(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys",
		bytes.NewBufferString(`{"alias":"rk"}`)))
	created := decode[keyResponse](t, w)

	// Encrypt with v1.
	encReq := []encryptItem{{KeyID: created.ID, PlaintextB64: base64.StdEncoding.EncodeToString([]byte("v1"))}}
	encBody, _ := json.Marshal(encReq)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", bytes.NewReader(encBody)))
	v1 := decode[[]encryptResult](t, w)[0]
	if v1.Version != 1 {
		t.Fatalf("expected v1, got %d", v1.Version)
	}

	// Rotate.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys/"+created.ID+"/rotate", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("rotate = %d body=%s", w.Code, w.Body.String())
	}

	// Encrypt again — must land on v2.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", bytes.NewReader(encBody)))
	v2 := decode[[]encryptResult](t, w)[0]
	if v2.Version != 2 {
		t.Fatalf("expected v2, got %d", v2.Version)
	}

	// Decrypt the old v1 envelope.
	decReq := []decryptItem{{KeyID: created.ID, Version: 1, CiphertextB64: v1.CiphertextB64}}
	decBody, _ := json.Marshal(decReq)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/decrypt", bytes.NewReader(decBody)))
	decOut := decode[[]decryptResult](t, w)
	if len(decOut) != 1 || decOut[0].Error != "" {
		t.Fatalf("decrypt of v1 envelope must succeed after rotation: %+v", decOut)
	}
}

// TestEncrypt_BatchLimits guards the batch-size envelope.
func TestEncrypt_BatchLimits(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", bytes.NewBufferString(`[]`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty batch should be 400, got %d", w.Code)
	}

	items := make([]encryptItem, MaxBatchItems+1)
	for i := range items {
		items[i] = encryptItem{KeyID: uuid.NewString(), PlaintextB64: ""}
	}
	body, _ := json.Marshal(items)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", bytes.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized batch should be 400, got %d", w.Code)
	}
}

// TestGetKey_CrossTenant ensures one tenant cannot probe another's
// key ids. mapErrorMessage collapses the domain sentinel into a 404.
func TestGetKey_CrossTenant(t *testing.T) {
	t.Parallel()
	tenantA := uuid.New()
	tenantB := uuid.New()
	state := newTestState(t)

	routerA := buildRouter(state, tenantA)
	w := httptest.NewRecorder()
	routerA.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{"alias":"x"}`)))
	created := decode[keyResponse](t, w)

	routerB := buildRouter(state, tenantB)
	w = httptest.NewRecorder()
	routerB.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/keys/"+created.ID, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant must 404, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestErrorsAreTyped pins the sentinel→HTTP mapping so the wire
// shape stays stable.
func TestErrorsAreTyped(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want int
	}{
		{domain.ErrKeyNotFound, http.StatusNotFound},
		{domain.ErrKeyVersionNotFound, http.StatusNotFound},
		{domain.ErrKeyRetired, http.StatusConflict},
		{errors.New("anything else"), http.StatusInternalServerError},
	} {
		w := httptest.NewRecorder()
		writeRepoError(w, tc.err)
		if w.Code != tc.want {
			t.Fatalf("err %v → %d, want %d", tc.err, w.Code, tc.want)
		}
	}
}

func TestMilestoneCWrapBatchStreamAndBudget(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{"name":"promo","kms_backend":"local"}`)))
	if w.Code != http.StatusCreated {
		t.Fatalf("create key=%d body=%s", w.Code, w.Body.String())
	}
	created := decode[keyResponse](t, w)
	if created.KMSBackend != "local" {
		t.Fatalf("key backend = %q", created.KMSBackend)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys/"+created.ID+"/wrap-for-promotion", bytes.NewBufferString(`{"target_environment":"prod","import_ciphertexts":true}`)))
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte("requires_destination_policy_import_approval")) {
		t.Fatalf("wrap promotion=%d body=%s", w.Code, w.Body.String())
	}

	encBody := `[{"key_id":"` + created.ID + `","plaintext":"one"},{"key_id":"` + created.ID + `","plaintext":"two"}]`
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt-batch", bytes.NewBufferString(encBody)))
	if w.Code != http.StatusOK {
		t.Fatalf("encrypt-batch=%d body=%s", w.Code, w.Body.String())
	}
	enc := decode[[]encryptResult](t, w)
	if len(enc) != 2 || enc[0].Ciphertext == "" || enc[1].Ciphertext == "" {
		t.Fatalf("bad batch result: %+v", enc)
	}

	decBody := `{"ciphertext":"` + enc[0].Ciphertext + `"}
{"ciphertext":"` + enc[1].Ciphertext + `"}
`
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/decrypt-stream", bytes.NewBufferString(decBody)))
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte("one")) || !bytes.Contains(w.Body.Bytes(), []byte("two")) {
		t.Fatalf("decrypt-stream=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDecryptBudgetExceeded(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	actor := uuid.New()
	state := newTestState(t)
	state.Budgets = NewDecryptBudgetManager(1, time.Hour)
	r := buildRouterWithMiddleware(state, withClaimsValue(&authmw.Claims{Sub: actor, OrgID: &tenant, Roles: []string{authmw.RoleAdmin}}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{"name":"budget"}`)))
	created := decode[keyResponse](t, w)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/encrypt", bytes.NewBufferString(`{"key_id":"`+created.ID+`","plaintext":"secret"}`)))
	enc := decode[encryptResult](t, w)
	body := `{"ciphertext":"` + enc.Ciphertext + `"}`
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/decrypt", bytes.NewBufferString(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("first decrypt=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/decrypt", bytes.NewBufferString(body)))
	if w.Code != http.StatusBadRequest || !bytes.Contains(w.Body.Bytes(), []byte("decrypt budget exceeded")) {
		t.Fatalf("budget should hard fail: status=%d body=%s", w.Code, w.Body.String())
	}
}
