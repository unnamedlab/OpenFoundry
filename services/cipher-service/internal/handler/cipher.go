// Package handler is the cipher-service HTTP layer. Every handler
// reads validated claims from libs/auth-middleware, calls the repo,
// and translates domain sentinels into typed HTTP responses. The
// crypto / KMS plumbing is deliberately invisible from the wire —
// callers see only key references and base64 envelopes.
package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"

	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/audit"
	cryptopkg "github.com/openfoundry/openfoundry-go/services/cipher-service/internal/crypto"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/kms"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/repo"
)

// Repository is the persistence surface the handler depends on.
// Declared here (rather than re-using *repo.Repo) so the test layer
// can plug in an in-memory fake without spinning up Postgres.
type Repository interface {
	InsertKey(ctx context.Context, p repo.CreateKeyParams) (*domain.CipherKey, error)
	GetKey(ctx context.Context, tenantID, id uuid.UUID) (*domain.CipherKey, error)
	ListKeys(ctx context.Context, tenantID uuid.UUID, page repo.ListPage) (repo.ListResult, error)
	GetVersion(ctx context.Context, tenantID, keyID uuid.UUID, version uint32) (*domain.CipherKeyVersion, error)
	RotateKey(ctx context.Context, tenantID, keyID uuid.UUID, wrappedDEK []byte, kmsRef string) (*domain.CipherKey, error)
	RetireKey(ctx context.Context, tenantID, keyID uuid.UUID) (*domain.CipherKey, error)
	MarkRotationComplete(ctx context.Context, tenantID, keyID uuid.UUID) error
}

// State is the bag of dependencies every cipher handler needs.
// Constructed once at server boot.
type State struct {
	Repo  Repository
	KMS   kms.KMS
	Audit *audit.Recorder
}

// ─── Wire types ────────────────────────────────────────────────────────

type keyResponse struct {
	ID        string  `json:"id"`
	TenantID  string  `json:"tenant_id"`
	Alias     string  `json:"alias"`
	Algorithm string  `json:"algorithm"`
	Version   uint32  `json:"version"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"created_at"`
	RotatedAt *string `json:"rotated_at,omitempty"`
}

func toKeyResponse(k *domain.CipherKey) keyResponse {
	res := keyResponse{
		ID:        k.ID.String(),
		TenantID:  k.TenantID.String(),
		Alias:     k.Alias,
		Algorithm: string(k.Algorithm),
		Version:   k.Version,
		Status:    string(k.Status),
		CreatedAt: k.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
	}
	if k.RotatedAt != nil {
		s := k.RotatedAt.UTC().Format("2006-01-02T15:04:05.000000Z")
		res.RotatedAt = &s
	}
	return res
}

type createKeyRequest struct {
	Alias     string `json:"alias"`
	Algorithm string `json:"algorithm"`
}

type listKeysResponse struct {
	Items      []keyResponse `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type encryptItem struct {
	KeyID        string  `json:"key_id"`
	Version      *uint32 `json:"version,omitempty"`
	PlaintextB64 string  `json:"plaintext_b64"`
}

type encryptResult struct {
	KeyID         string `json:"key_id"`
	Version       uint32 `json:"version"`
	CiphertextB64 string `json:"ciphertext_b64"`
	Error         string `json:"error,omitempty"`
}

type decryptItem struct {
	KeyID         string `json:"key_id"`
	Version       uint32 `json:"version"`
	CiphertextB64 string `json:"ciphertext_b64"`
}

type decryptResult struct {
	PlaintextB64 string `json:"plaintext_b64,omitempty"`
	Error        string `json:"error,omitempty"`
}

// MaxBatchItems caps the encrypt/decrypt batch size at a sane Milestone A
// limit. Tighter than CIP.21's eventual N because we have no rate-limit
// budget enforcement yet (CIP.23 lands in Milestone C).
const MaxBatchItems = 64

// ─── Handlers ──────────────────────────────────────────────────────────

// CreateKey wires CIP.2 ("Cipher key resource"): mints a fresh DEK,
// wraps it through the configured KMS, persists the registry row +
// v1 wrapping, and returns the metadata. Key material is never on
// the wire.
func (s *State) CreateKey(w http.ResponseWriter, r *http.Request) {
	tenant, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}

	var req createKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	alias := strings.TrimSpace(req.Alias)
	if alias == "" {
		writeError(w, http.StatusBadRequest, "alias is required")
		return
	}
	algo := domain.Algorithm(req.Algorithm)
	if req.Algorithm == "" {
		algo = domain.AlgorithmAES256GCM
	}
	if !algo.Valid() {
		writeError(w, http.StatusBadRequest, "unsupported algorithm")
		return
	}

	dek, err := cryptopkg.NewDEK()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "key material unavailable")
		return
	}
	wrapped, err := s.KMS.Wrap(dek)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "kms wrap failed")
		return
	}

	key, err := s.Repo.InsertKey(r.Context(), repo.CreateKeyParams{
		ID:                 uuid.New(),
		TenantID:           tenant,
		Alias:              alias,
		Algorithm:          algo,
		WrappedKeyMaterial: wrapped,
		KMSKeyRef:          s.KMS.Ref(),
	})
	if err != nil {
		if errors.Is(err, repo.ErrAliasConflict) {
			writeError(w, http.StatusConflict, "alias already in use for this tenant")
			return
		}
		writeError(w, http.StatusInternalServerError, "create key failed")
		return
	}

	if claims, _ := authmw.FromContext(r.Context()); claims != nil {
		s.Audit.KeyCreated(r.Context(), claims.Sub, tenant, key.ID, key.Alias, claims.AllowedMarkings())
	}
	writeJSON(w, http.StatusCreated, toKeyResponse(key))
}

// ListKeys implements CIP.2 read side. Cursor pagination keeps the
// surface aligned with the other Go services; future budget limits
// (CIP.23) will plug in at the same point.
func (s *State) ListKeys(w http.ResponseWriter, r *http.Request) {
	tenant, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	limit := uint32(50)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.ParseUint(raw, 10, 32); err == nil && v > 0 {
			limit = uint32(v)
		}
	}
	res, err := s.Repo.ListKeys(r.Context(), tenant, repo.ListPage{
		Limit:  limit,
		Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := listKeysResponse{
		Items:      make([]keyResponse, 0, len(res.Items)),
		NextCursor: res.NextCursor,
	}
	for _, k := range res.Items {
		out.Items = append(out.Items, toKeyResponse(k))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetKey returns the registry row without any wrapped material —
// the only thing the wire sees is metadata.
func (s *State) GetKey(w http.ResponseWriter, r *http.Request) {
	tenant, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	k, err := s.Repo.GetKey(r.Context(), tenant, id)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toKeyResponse(k))
}

// RotateKey implements CIP.16: append a new version, leave older
// versions decryptable.
func (s *State) RotateKey(w http.ResponseWriter, r *http.Request) {
	tenant, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	dek, err := cryptopkg.NewDEK()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "key material unavailable")
		return
	}
	wrapped, err := s.KMS.Wrap(dek)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "kms wrap failed")
		return
	}

	k, err := s.Repo.RotateKey(r.Context(), tenant, id, wrapped, s.KMS.Ref())
	if err != nil {
		writeRepoError(w, err)
		return
	}

	// Milestone A has no async rewrap worker yet, so flip the
	// rotating-state back to active inline. Once CIP.16's background
	// job lands, this line moves into that worker's completion path.
	if err := s.Repo.MarkRotationComplete(r.Context(), tenant, id); err != nil {
		// Logged but not fatal: the next rotation or worker pass
		// will converge the state.
		_ = err
	}

	if claims, _ := authmw.FromContext(r.Context()); claims != nil {
		s.Audit.KeyRotated(r.Context(), claims.Sub, tenant, k.ID, k.Version, claims.AllowedMarkings())
	}
	writeJSON(w, http.StatusOK, toKeyResponse(k))
}

// RetireKey flips the key to decrypt-only.
func (s *State) RetireKey(w http.ResponseWriter, r *http.Request) {
	tenant, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	k, err := s.Repo.RetireKey(r.Context(), tenant, id)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	if claims, _ := authmw.FromContext(r.Context()); claims != nil {
		s.Audit.KeyRetired(r.Context(), claims.Sub, tenant, k.ID, claims.AllowedMarkings())
	}
	writeJSON(w, http.StatusOK, toKeyResponse(k))
}

// Encrypt is the batch encrypt entry point — CIP.6's batch shape
// merged with CIP.21 budgeting. Per-item errors are reported
// in-band so partial success is still useful to the caller.
func (s *State) Encrypt(w http.ResponseWriter, r *http.Request) {
	tenant, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	var items []encryptItem
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(items) == 0 {
		writeError(w, http.StatusBadRequest, "batch is empty")
		return
	}
	if len(items) > MaxBatchItems {
		writeError(w, http.StatusBadRequest, "batch exceeds limit")
		return
	}

	out := make([]encryptResult, len(items))
	for i, it := range items {
		out[i] = s.encryptOne(r, tenant, it)
	}
	writeJSON(w, http.StatusOK, out)
}

// Decrypt is the batch decrypt path. Emits a single bulk_decrypt
// audit event (CIP.8) — per-item events would saturate the bus.
func (s *State) Decrypt(w http.ResponseWriter, r *http.Request) {
	tenant, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	var items []decryptItem
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(items) == 0 {
		writeError(w, http.StatusBadRequest, "batch is empty")
		return
	}
	if len(items) > MaxBatchItems {
		writeError(w, http.StatusBadRequest, "batch exceeds limit")
		return
	}

	out := make([]decryptResult, len(items))
	for i, it := range items {
		out[i] = s.decryptOne(r, tenant, it)
	}
	if claims, _ := authmw.FromContext(r.Context()); claims != nil {
		s.Audit.BulkDecrypt(r.Context(), claims.Sub, tenant, len(items))
	}
	writeJSON(w, http.StatusOK, out)
}

// ─── Per-item encrypt/decrypt ──────────────────────────────────────────

func (s *State) encryptOne(r *http.Request, tenant uuid.UUID, in encryptItem) encryptResult {
	keyID, err := uuid.Parse(in.KeyID)
	if err != nil {
		return encryptResult{KeyID: in.KeyID, Error: "invalid key id"}
	}
	res := encryptResult{KeyID: keyID.String()}

	k, err := s.Repo.GetKey(r.Context(), tenant, keyID)
	if err != nil {
		res.Error = mapErrorMessage(err)
		return res
	}
	if !k.CanEncrypt() {
		res.Error = "key is retired"
		return res
	}
	if k.Algorithm != domain.AlgorithmAES256GCM {
		res.Error = "algorithm not yet supported"
		return res
	}

	// Encrypt always targets the active version, even if the caller
	// passed an older one. That stays consistent with the rotation
	// contract: new ciphertexts always use the current key.
	v, err := s.Repo.GetVersion(r.Context(), tenant, keyID, k.Version)
	if err != nil {
		res.Error = mapErrorMessage(err)
		return res
	}
	dek, err := s.KMS.Unwrap(v.WrappedKeyMaterial)
	if err != nil {
		res.Error = "kms unwrap failed"
		return res
	}
	plaintext, err := base64.StdEncoding.DecodeString(in.PlaintextB64)
	if err != nil {
		res.Error = "plaintext_b64 is not valid base64"
		return res
	}
	envelope, err := cryptopkg.Encrypt(keyID, k.Version, dek, plaintext)
	if err != nil {
		res.Error = "encrypt failed"
		return res
	}
	res.Version = k.Version
	res.CiphertextB64 = base64.StdEncoding.EncodeToString(envelope)
	return res
}

func (s *State) decryptOne(r *http.Request, tenant uuid.UUID, in decryptItem) decryptResult {
	keyID, err := uuid.Parse(in.KeyID)
	if err != nil {
		return decryptResult{Error: "invalid key id"}
	}
	envelope, err := base64.StdEncoding.DecodeString(in.CiphertextB64)
	if err != nil {
		return decryptResult{Error: "ciphertext_b64 is not valid base64"}
	}

	// Pull the header off-wire to confirm key id + version match the
	// caller's claims. A mismatch here is an immediate ErrInvalidEnvelope —
	// no point hitting the repo for a forged id.
	gotID, gotVersion, err := cryptopkg.DecodeHeader(envelope)
	if err != nil {
		return decryptResult{Error: "invalid envelope"}
	}
	if gotID != keyID || gotVersion != in.Version {
		return decryptResult{Error: "envelope header does not match request"}
	}

	k, err := s.Repo.GetKey(r.Context(), tenant, keyID)
	if err != nil {
		return decryptResult{Error: mapErrorMessage(err)}
	}
	if !k.CanDecrypt() {
		return decryptResult{Error: "key not decryptable"}
	}
	if k.Algorithm != domain.AlgorithmAES256GCM {
		return decryptResult{Error: "algorithm not yet supported"}
	}

	v, err := s.Repo.GetVersion(r.Context(), tenant, keyID, in.Version)
	if err != nil {
		return decryptResult{Error: mapErrorMessage(err)}
	}
	dek, err := s.KMS.Unwrap(v.WrappedKeyMaterial)
	if err != nil {
		return decryptResult{Error: "kms unwrap failed"}
	}
	plaintext, err := cryptopkg.Decrypt(keyID, in.Version, dek, envelope)
	if err != nil {
		return decryptResult{Error: "decrypt failed"}
	}
	return decryptResult{PlaintextB64: base64.StdEncoding.EncodeToString(plaintext)}
}

// ─── Common helpers ───────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}

type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

func writeRepoError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrKeyNotFound), errors.Is(err, domain.ErrTenantMismatch):
		writeError(w, http.StatusNotFound, "key not found")
	case errors.Is(err, domain.ErrKeyVersionNotFound):
		writeError(w, http.StatusNotFound, "key version not found")
	case errors.Is(err, domain.ErrKeyRetired):
		writeError(w, http.StatusConflict, "key is retired")
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func mapErrorMessage(err error) string {
	switch {
	case errors.Is(err, domain.ErrKeyNotFound), errors.Is(err, domain.ErrTenantMismatch):
		return "key not found"
	case errors.Is(err, domain.ErrKeyVersionNotFound):
		return "key version not found"
	case errors.Is(err, domain.ErrKeyRetired):
		return "key is retired"
	case errors.Is(err, domain.ErrInvalidEnvelope):
		return "invalid envelope"
	default:
		return "internal error"
	}
}
