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
	mu       sync.Mutex
	keys     map[uuid.UUID]*domain.CipherKey
	versions map[uuid.UUID]map[uint32]*domain.CipherKeyVersion
}

func newMemRepo() *memRepo {
	return &memRepo{
		keys:     make(map[uuid.UUID]*domain.CipherKey),
		versions: make(map[uuid.UUID]map[uint32]*domain.CipherKeyVersion),
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
	k := &domain.CipherKey{
		ID: p.ID, TenantID: p.TenantID, Alias: p.Alias, Algorithm: p.Algorithm,
		Version: 1, Status: domain.StatusActive, CreatedAt: now,
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
	cp := *k
	return &cp, nil
}

func (m *memRepo) ListKeys(_ context.Context, tenantID uuid.UUID, _ repo.ListPage) (repo.ListResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*domain.CipherKey, 0)
	for _, k := range m.keys {
		if k.TenantID == tenantID {
			cp := *k
			out = append(out, &cp)
		}
	}
	return repo.ListResult{Items: out}, nil
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
	cp := *k
	return &cp, nil
}

func (m *memRepo) RetireKey(_ context.Context, tenantID, keyID uuid.UUID) (*domain.CipherKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[keyID]
	if !ok || k.TenantID != tenantID {
		return nil, domain.ErrKeyNotFound
	}
	k.Status = domain.StatusRetired
	cp := *k
	return &cp, nil
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
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t := tenant
			c := &authmw.Claims{
				Sub:   uuid.New(),
				OrgID: &t,
				Roles: []string{authmw.RoleAdmin},
			}
			ctx := authmw.ContextWithClaims(r.Context(), c)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func buildRouter(state *State, tenant uuid.UUID) http.Handler {
	r := chi.NewRouter()
	r.Use(withClaims(tenant))
	r.Post("/keys", state.CreateKey)
	r.Get("/keys", state.ListKeys)
	r.Get("/keys/{id}", state.GetKey)
	r.Post("/keys/{id}/rotate", state.RotateKey)
	r.Post("/keys/{id}/retire", state.RetireKey)
	r.Post("/encrypt", state.Encrypt)
	r.Post("/decrypt", state.Decrypt)
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

func TestCreateKey_ReturnsMetadataOnly(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	state := newTestState(t)
	r := buildRouter(state, tenant)

	body := bytes.NewBufferString(`{"alias":"pii","algorithm":"AES_256_GCM"}`)
	req := httptest.NewRequest(http.MethodPost, "/keys", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	got := decode[keyResponse](t, w)
	if got.Alias != "pii" || got.Version != 1 || got.Status != "active" {
		t.Fatalf("unexpected response: %+v", got)
	}
	// Body must not leak wrapped material — sanity check the JSON.
	if bytes.Contains(w.Body.Bytes(), []byte("wrapped")) {
		t.Fatal("response leaked wrapped key material")
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

	decReq := []decryptItem{{
		KeyID:         created.ID,
		Version:       encOut[0].Version,
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
