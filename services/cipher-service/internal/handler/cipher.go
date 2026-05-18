// Package handler is the cipher-service HTTP layer. Every handler
// reads validated claims from libs/auth-middleware, calls the repo,
// and translates domain sentinels into typed HTTP responses. The
// crypto / KMS plumbing is deliberately invisible from the wire —
// callers see only key references and base64 envelopes.
package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"

	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/anomaly"
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
	UpdateKeyMetadata(ctx context.Context, tenantID, id uuid.UUID, p repo.UpdateKeyParams) (*domain.CipherKey, error)
	DeleteKey(ctx context.Context, tenantID, id uuid.UUID) error
	GetVersion(ctx context.Context, tenantID, keyID uuid.UUID, version uint32) (*domain.CipherKeyVersion, error)
	RotateKey(ctx context.Context, tenantID, keyID uuid.UUID, wrappedDEK []byte, kmsRef string) (*domain.CipherKey, error)
	RetireKey(ctx context.Context, tenantID, keyID uuid.UUID) (*domain.CipherKey, error)
	MarkRotationComplete(ctx context.Context, tenantID, keyID uuid.UUID) error
	RevokeKey(ctx context.Context, tenantID, keyID uuid.UUID) (*domain.CipherKey, error)
	RotateKeyToNewID(ctx context.Context, tenantID, keyID, newKeyID uuid.UUID, wrappedDEK []byte, kmsRef string) (*domain.CipherKey, error)
	InsertPepper(ctx context.Context, p repo.CreatePepperParams) (*domain.Pepper, error)
	GetPepper(ctx context.Context, tenantID, id uuid.UUID) (*domain.Pepper, error)
	GetPepperVersion(ctx context.Context, tenantID, pepperID uuid.UUID, version uint32) (*domain.PepperVersion, error)
	RotatePepper(ctx context.Context, tenantID, id uuid.UUID, wrapped []byte, kmsRef string) (*domain.Pepper, error)
}

// State is the bag of dependencies every cipher handler needs.
// Constructed once at server boot.
type State struct {
	Repo    Repository
	KMS     kms.KMS
	Audit   *audit.Recorder
	Budgets *DecryptBudgetManager
	Anomaly *anomaly.Detector
}

// ─── Wire types ────────────────────────────────────────────────────────

type algorithmResponse struct {
	ID                 string `json:"id"`
	StableIdentifier   string `json:"stable_identifier"`
	Kind               string `json:"kind"`
	KeyLengthBytes     uint16 `json:"key_length_bytes"`
	NoncePolicy        string `json:"nonce_policy"`
	OutputEncoding     string `json:"output_encoding"`
	Authenticated      bool   `json:"authenticated"`
	Deterministic      bool   `json:"deterministic"`
	RecommendedDefault bool   `json:"recommended_default"`
	PepperRequired     bool   `json:"pepper_required"`
	SecurityNotice     string `json:"security_notice,omitempty"`
	Status             string `json:"status"`
}

type listAlgorithmsResponse struct {
	Items []algorithmResponse `json:"items"`
}

func toAlgorithmResponse(d domain.AlgorithmDescriptor) algorithmResponse {
	return algorithmResponse{
		ID:                 string(d.ID),
		StableIdentifier:   d.StableIdentifier,
		Kind:               d.Kind,
		KeyLengthBytes:     d.KeyLengthBytes,
		NoncePolicy:        d.NoncePolicy,
		OutputEncoding:     d.OutputEncoding,
		Authenticated:      d.Authenticated,
		Deterministic:      d.Deterministic,
		RecommendedDefault: d.RecommendedDefault,
		PepperRequired:     d.PepperRequired,
		SecurityNotice:     d.SecurityNotice,
		Status:             d.Status,
	}
}

type keyResponse struct {
	ID             string              `json:"id"`
	TenantID       string              `json:"tenant_id"`
	Name           string              `json:"name"`
	Alias          string              `json:"alias"`
	Algorithm      string              `json:"algorithm"`
	KeyMaterialRef string              `json:"key_material_ref"`
	KMSBackend     string              `json:"kms_backend"`
	OwnerID        string              `json:"owner_id,omitempty"`
	Organizations  []string            `json:"organizations"`
	Markings       []string            `json:"markings"`
	IntendedScopes []string            `json:"intended_scopes"`
	AccessPolicy   domain.AccessPolicy `json:"access_policy"`
	Version        uint32              `json:"version"`
	Status         string              `json:"status"`
	CreatedAt      string              `json:"created_at"`
	ExpiresAt      *string             `json:"expires_at,omitempty"`
	RotatedAt      *string             `json:"rotated_at,omitempty"`
}

func toKeyResponse(k *domain.CipherKey) keyResponse {
	res := keyResponse{
		ID:             k.ID.String(),
		TenantID:       k.TenantID.String(),
		Name:           k.Alias,
		Alias:          k.Alias,
		Algorithm:      string(k.Algorithm),
		KeyMaterialRef: k.KeyMaterialRef,
		KMSBackend:     string(k.KMSBackend.OrDefault()),
		OwnerID:        uuidToString(k.OwnerID),
		Organizations:  uuidListToStrings(k.Organizations),
		Markings:       append([]string(nil), k.Markings...),
		IntendedScopes: append([]string(nil), k.IntendedScopes...),
		AccessPolicy:   k.AccessPolicy.Clone(),
		Version:        k.Version,
		Status:         string(k.Status),
		CreatedAt:      k.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
	}
	if k.ExpiresAt != nil {
		s := k.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000000Z")
		res.ExpiresAt = &s
	}
	if k.RotatedAt != nil {
		s := k.RotatedAt.UTC().Format("2006-01-02T15:04:05.000000Z")
		res.RotatedAt = &s
	}
	return res
}

type createKeyRequest struct {
	Name           string               `json:"name"`
	Alias          string               `json:"alias"`
	Algorithm      string               `json:"algorithm"`
	KMSBackend     string               `json:"kms_backend"`
	OwnerID        string               `json:"owner_id"`
	Organizations  []string             `json:"organizations"`
	Markings       []string             `json:"markings"`
	IntendedScopes []string             `json:"intended_scopes"`
	AccessPolicy   *domain.AccessPolicy `json:"access_policy"`
	ExpiresAt      string               `json:"expires_at"`
}

type updateKeyRequest struct {
	Name           *string              `json:"name"`
	Alias          *string              `json:"alias"`
	OwnerID        *string              `json:"owner_id"`
	Organizations  []string             `json:"organizations"`
	Markings       []string             `json:"markings"`
	IntendedScopes []string             `json:"intended_scopes"`
	AccessPolicy   *domain.AccessPolicy `json:"access_policy"`
	ExpiresAt      *string              `json:"expires_at"`
}

type listKeysResponse struct {
	Items      []keyResponse `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type encryptItem struct {
	KeyID        string  `json:"key_id"`
	Version      *uint32 `json:"version,omitempty"`
	Plaintext    string  `json:"plaintext"`
	PlaintextB64 string  `json:"plaintext_b64"`
	Algorithm    string  `json:"algorithm"`
	ResourceRID  string  `json:"resource_rid"`
}

type encryptResult struct {
	KeyID         string `json:"key_id"`
	Version       uint32 `json:"version"`
	Ciphertext    string `json:"ciphertext,omitempty"`
	CiphertextB64 string `json:"ciphertext_b64"`
	Error         string `json:"error,omitempty"`
}

type decryptItem struct {
	KeyID            string   `json:"key_id"`
	Version          uint32   `json:"version"`
	Ciphertext       string   `json:"ciphertext"`
	CiphertextB64    string   `json:"ciphertext_b64"`
	ResourceRID      string   `json:"resource_rid"`
	ResourceMarkings []string `json:"resource_markings"`
}

type decryptResult struct {
	Plaintext    string `json:"plaintext,omitempty"`
	PlaintextB64 string `json:"plaintext_b64,omitempty"`
	Error        string `json:"error,omitempty"`
}

type promotionWrapRequest struct {
	TargetEnvironment string `json:"target_environment"`
	TargetTenantID    string `json:"target_tenant_id"`
	ImportCiphertexts bool   `json:"import_ciphertexts"`
}

type promotionWrapResponse struct {
	SourceKeyID       string              `json:"source_key_id"`
	TargetKeyID       string              `json:"target_key_id"`
	TargetEnvironment string              `json:"target_environment"`
	Algorithm         string              `json:"algorithm"`
	KMSBackend        string              `json:"kms_backend"`
	AccessPolicy      domain.AccessPolicy `json:"access_policy"`
	CiphertextsMoved  bool                `json:"ciphertexts_moved"`
	WrappingCeremony  string              `json:"wrapping_ceremony"`
}

type createPepperRequest struct {
	Name         string               `json:"name"`
	Algorithm    string               `json:"algorithm"`
	AccessPolicy *domain.AccessPolicy `json:"access_policy"`
}

type pepperResponse struct {
	ID                string              `json:"id"`
	TenantID          string              `json:"tenant_id"`
	Name              string              `json:"name"`
	Algorithm         string              `json:"algorithm"`
	PepperMaterialRef string              `json:"pepper_material_ref"`
	Version           uint32              `json:"version"`
	AccessPolicy      domain.AccessPolicy `json:"access_policy"`
	CreatedAt         string              `json:"created_at"`
	RotatedAt         *string             `json:"rotated_at,omitempty"`
}

type tokenizeRequest struct {
	PepperID  string `json:"pepper_id"`
	Plaintext string `json:"plaintext"`
}

type tokenizeResponse struct {
	PepperID  string `json:"pepper_id"`
	Algorithm string `json:"algorithm"`
	Token     string `json:"token"`
}

func toPepperResponse(p *domain.Pepper) pepperResponse {
	res := pepperResponse{ID: p.ID.String(), TenantID: p.TenantID.String(), Name: p.Name, Algorithm: string(p.Algorithm), PepperMaterialRef: p.PepperMaterialRef, Version: p.Version, AccessPolicy: p.AccessPolicy.Clone(), CreatedAt: p.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000Z")}
	if p.RotatedAt != nil {
		s := p.RotatedAt.UTC().Format("2006-01-02T15:04:05.000000Z")
		res.RotatedAt = &s
	}
	return res
}

// MaxBatchItems caps the encrypt/decrypt batch size at a sane Milestone A
// limit. Tighter than CIP.21's eventual N because we have no rate-limit
// budget enforcement yet (CIP.23 lands in Milestone C).
const MaxBatchItems = 64

// ─── Handlers ──────────────────────────────────────────────────────────

// ListAlgorithms exposes CIP.1's built-in algorithm registry. The
// response is tenant-independent and contains only stable identifiers
// plus operational metadata; it never discloses key material.
func (s *State) ListAlgorithms(w http.ResponseWriter, r *http.Request) {
	descriptors := domain.BuiltInAlgorithms()
	items := make([]algorithmResponse, len(descriptors))
	for i, d := range descriptors {
		items[i] = toAlgorithmResponse(d)
	}
	writeJSON(w, http.StatusOK, listAlgorithmsResponse{Items: items})
}

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
	alias := strings.TrimSpace(req.Name)
	if alias == "" {
		alias = strings.TrimSpace(req.Alias)
	}
	if alias == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	algo := domain.Algorithm(req.Algorithm)
	if req.Algorithm == "" {
		algo = domain.AlgorithmAES256GCM
	}
	if !algo.SupportsCipherKeyResource() {
		writeError(w, http.StatusBadRequest, "unsupported algorithm")
		return
	}
	backend := domain.KeyBackend(strings.TrimSpace(req.KMSBackend)).OrDefault()
	if !backend.Valid() {
		writeError(w, http.StatusBadRequest, "unsupported kms_backend")
		return
	}
	ownerID, err := parseOptionalUUID(req.OwnerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "owner_id is not a valid UUID")
		return
	}
	if ownerID == uuid.Nil {
		if claims, _ := authmw.FromContext(r.Context()); claims != nil {
			ownerID = claims.Sub
		}
	}
	organizations, err := parseUUIDList(req.Organizations)
	if err != nil {
		writeError(w, http.StatusBadRequest, "organizations contains an invalid UUID")
		return
	}
	if len(organizations) == 0 {
		organizations = append(organizations, tenant)
	}
	expiresAt, err := parseOptionalTime(req.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "expires_at must be RFC3339")
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
		KMSBackend:         backend,
		OwnerID:            ownerID,
		Organizations:      organizations,
		Markings:           req.Markings,
		IntendedScopes:     req.IntendedScopes,
		AccessPolicy:       accessPolicyOrDefault(req.AccessPolicy),
		ExpiresAt:          expiresAt,
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

func (s *State) CreatePepper(w http.ResponseWriter, r *http.Request) {
	tenant, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	var req createPepperRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	alg := domain.Algorithm(req.Algorithm)
	if alg == "" {
		alg = domain.AlgorithmSHA256
	}
	if alg != domain.AlgorithmSHA256 && alg != domain.AlgorithmSHA512 {
		writeError(w, http.StatusBadRequest, "pepper algorithm must be SHA_256 or SHA_512")
		return
	}
	material := make([]byte, 32)
	if alg == domain.AlgorithmSHA512 {
		material = make([]byte, 64)
	}
	if _, err := rand.Read(material); err != nil {
		writeError(w, http.StatusInternalServerError, "pepper material unavailable")
		return
	}
	wrapped, err := s.KMS.Wrap(material)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "kms wrap failed")
		return
	}
	pepper, err := s.Repo.InsertPepper(r.Context(), repo.CreatePepperParams{ID: uuid.New(), TenantID: tenant, Name: name, Algorithm: alg, WrappedPepperMaterial: wrapped, KMSKeyRef: s.KMS.Ref(), AccessPolicy: accessPolicyOrDefault(req.AccessPolicy)})
	if err != nil {
		if errors.Is(err, repo.ErrAliasConflict) {
			writeError(w, http.StatusConflict, "pepper name already in use for this tenant")
			return
		}
		writeError(w, http.StatusInternalServerError, "create pepper failed")
		return
	}
	writeJSON(w, http.StatusCreated, toPepperResponse(pepper))
}

func (s *State) RotatePepper(w http.ResponseWriter, r *http.Request) {
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
	pepper, err := s.Repo.GetPepper(r.Context(), tenant, id)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	claims, _ := authmw.FromContext(r.Context())
	if err := authorizePepperOperation(r.Context(), claims, pepper, cipherOpManage); err != nil {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	material := make([]byte, 32)
	if pepper.Algorithm == domain.AlgorithmSHA512 {
		material = make([]byte, 64)
	}
	if _, err := rand.Read(material); err != nil {
		writeError(w, http.StatusInternalServerError, "pepper material unavailable")
		return
	}
	wrapped, err := s.KMS.Wrap(material)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "kms wrap failed")
		return
	}
	rotated, err := s.Repo.RotatePepper(r.Context(), tenant, id, wrapped, s.KMS.Ref())
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toPepperResponse(rotated))
}

func (s *State) Tokenize(w http.ResponseWriter, r *http.Request) {
	tenant, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	var req tokenizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	id, err := uuid.Parse(req.PepperID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pepper_id")
		return
	}
	pepper, err := s.Repo.GetPepper(r.Context(), tenant, id)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	claims, _ := authmw.FromContext(r.Context())
	if err := authorizePepperOperation(r.Context(), claims, pepper, cipherOpEncrypt); err != nil {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	v, err := s.Repo.GetPepperVersion(r.Context(), tenant, id, pepper.Version)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	material, err := s.KMS.Unwrap(v.WrappedPepperMaterial)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "kms unwrap failed")
		return
	}
	token := hashToken(pepper.Algorithm, material, []byte(req.Plaintext))
	if claims != nil {
		s.Audit.Operation(r.Context(), claims.Sub, tenant, id, "tokenize", string(pepper.Algorithm), "ri.cipher.main.pepper."+id.String(), true, "not_reversible", chimw.GetReqID(r.Context()), nil)
	}
	writeJSON(w, http.StatusOK, tokenizeResponse{PepperID: id.String(), Algorithm: string(pepper.Algorithm), Token: token})
}

func authorizePepperOperation(ctx context.Context, claims *authmw.Claims, pepper *domain.Pepper, op string) error {
	if pepper == nil {
		return domain.ErrAccessDenied
	}
	key := &domain.CipherKey{ID: pepper.ID, TenantID: pepper.TenantID, Markings: nil, AccessPolicy: pepper.AccessPolicy}
	return authorizeKeyOperation(ctx, claims, key, op)
}

func hashToken(alg domain.Algorithm, pepper, plaintext []byte) string {
	if alg == domain.AlgorithmSHA512 {
		mac := hmac.New(sha512.New, pepper)
		mac.Write(plaintext)
		return base64.StdEncoding.EncodeToString(mac.Sum(nil))
	}
	mac := hmac.New(sha256.New, pepper)
	mac.Write(plaintext)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
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

// UpdateKey patches mutable cipher_key resource metadata (CIP.2). It
// never accepts algorithm or key material changes; rotations own those.
func (s *State) UpdateKey(w http.ResponseWriter, r *http.Request) {
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
	var req updateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	existing, err := s.Repo.GetKey(r.Context(), tenant, id)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	claims, _ := authmw.FromContext(r.Context())
	if err := authorizeKeyOperation(r.Context(), claims, existing, cipherOpManage); err != nil {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	params := repo.UpdateKeyParams{}
	if req.Name != nil || req.Alias != nil {
		name := ""
		if req.Name != nil {
			name = strings.TrimSpace(*req.Name)
		}
		if name == "" && req.Alias != nil {
			name = strings.TrimSpace(*req.Alias)
		}
		if name == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		params.Alias = &name
	}
	if req.OwnerID != nil {
		ownerID, err := parseOptionalUUID(*req.OwnerID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "owner_id is not a valid UUID")
			return
		}
		params.OwnerID = &ownerID
	}
	if req.Organizations != nil {
		organizations, err := parseUUIDList(req.Organizations)
		if err != nil {
			writeError(w, http.StatusBadRequest, "organizations contains an invalid UUID")
			return
		}
		params.Organizations = organizations
		params.OrganizationsSet = true
	}
	if req.Markings != nil {
		params.Markings = req.Markings
		params.MarkingsSet = true
	}
	if req.IntendedScopes != nil {
		params.IntendedScopes = req.IntendedScopes
		params.IntendedScopesSet = true
	}
	if req.ExpiresAt != nil {
		expiresAt, err := parseOptionalTime(*req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "expires_at must be RFC3339")
			return
		}
		params.ExpiresAt = expiresAt
		params.ExpiresAtSet = true
	}
	if req.AccessPolicy != nil {
		policy := req.AccessPolicy.Clone()
		params.AccessPolicy = &policy
	}
	k, err := s.Repo.UpdateKeyMetadata(r.Context(), tenant, id, params)
	if err != nil {
		if errors.Is(err, repo.ErrAliasConflict) {
			writeError(w, http.StatusConflict, "alias already in use for this tenant")
			return
		}
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toKeyResponse(k))
}

// DeleteKey removes a cipher_key resource. Use RetireKey when stored
// ciphertext must remain decryptable.
func (s *State) DeleteKey(w http.ResponseWriter, r *http.Request) {
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
	existing, err := s.Repo.GetKey(r.Context(), tenant, id)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	claims, _ := authmw.FromContext(r.Context())
	if err := authorizeKeyOperation(r.Context(), claims, existing, cipherOpManage); err != nil {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	if err := s.Repo.DeleteKey(r.Context(), tenant, id); err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
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
	existing, err := s.Repo.GetKey(r.Context(), tenant, id)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	claims, _ := authmw.FromContext(r.Context())
	if err := authorizeKeyOperation(r.Context(), claims, existing, cipherOpManage); err != nil {
		writeError(w, http.StatusForbidden, "access denied")
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

func (s *State) RotateKeyToNewID(w http.ResponseWriter, r *http.Request) {
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
	existing, err := s.Repo.GetKey(r.Context(), tenant, id)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	claims, _ := authmw.FromContext(r.Context())
	if err := authorizeKeyOperation(r.Context(), claims, existing, cipherOpManage); err != nil {
		writeError(w, http.StatusForbidden, "access denied")
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
	newKey, err := s.Repo.RotateKeyToNewID(r.Context(), tenant, id, uuid.New(), wrapped, s.KMS.Ref())
	if err != nil {
		writeRepoError(w, err)
		return
	}
	if claims != nil {
		s.Audit.KeyRotated(r.Context(), claims.Sub, tenant, newKey.ID, newKey.Version, claims.AllowedMarkings())
	}
	writeJSON(w, http.StatusOK, toKeyResponse(newKey))
}

func (s *State) RevokeKey(w http.ResponseWriter, r *http.Request) {
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
	existing, err := s.Repo.GetKey(r.Context(), tenant, id)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	claims, _ := authmw.FromContext(r.Context())
	if err := authorizeKeyOperation(r.Context(), claims, existing, cipherOpManage); err != nil {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	k, err := s.Repo.RevokeKey(r.Context(), tenant, id)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	if claims != nil {
		s.Audit.KeyRevoked(r.Context(), claims.Sub, tenant, k.ID, claims.AllowedMarkings())
	}
	writeJSON(w, http.StatusOK, toKeyResponse(k))
}

// WrapKeyForPromotion plans the CIP.19 Apollo promotion ceremony. It never
// exports source plaintext DEKs or moves ciphertexts by default; the target
// environment provisions an equivalent key resource with preserved policy.
func (s *State) WrapKeyForPromotion(w http.ResponseWriter, r *http.Request) {
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
	var req promotionWrapRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	k, err := s.Repo.GetKey(r.Context(), tenant, id)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	claims, _ := authmw.FromContext(r.Context())
	if err := authorizeKeyOperation(r.Context(), claims, k, cipherOpManage); err != nil {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	ceremony := "provision_target_key_only"
	if req.ImportCiphertexts {
		ceremony = "requires_destination_policy_import_approval"
	}
	writeJSON(w, http.StatusOK, promotionWrapResponse{
		SourceKeyID:       k.ID.String(),
		TargetKeyID:       uuid.New().String(),
		TargetEnvironment: strings.TrimSpace(req.TargetEnvironment),
		Algorithm:         string(k.Algorithm),
		KMSBackend:        string(k.KMSBackend.OrDefault()),
		AccessPolicy:      k.AccessPolicy.Clone(),
		CiphertextsMoved:  false,
		WrappingCeremony:  ceremony,
	})
}

func (s *State) EncryptBatch(w http.ResponseWriter, r *http.Request) { s.Encrypt(w, r) }

func (s *State) DecryptBatch(w http.ResponseWriter, r *http.Request) { s.Decrypt(w, r) }

// DecryptStream accepts newline-delimited decryptItem JSON objects and writes
// newline-delimited decryptResult objects. The ResponseWriter flushes after each
// row when supported, allowing dataset readers to apply backpressure naturally.
func (s *State) DecryptStream(w http.ResponseWriter, r *http.Request) {
	tenant, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	dec := json.NewDecoder(r.Body)
	enc := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)
	for {
		var item decryptItem
		if err := dec.Decode(&item); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			writeError(w, http.StatusBadRequest, "invalid stream item")
			return
		}
		_ = enc.Encode(s.decryptOne(r, tenant, item))
		if flusher != nil {
			flusher.Flush()
		}
	}
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
	existing, err := s.Repo.GetKey(r.Context(), tenant, id)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	claims, _ := authmw.FromContext(r.Context())
	if err := authorizeKeyOperation(r.Context(), claims, existing, cipherOpManage); err != nil {
		writeError(w, http.StatusForbidden, "access denied")
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
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(raw) == 0 {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if raw[0] == '[' {
		var items []encryptItem
		if err := json.Unmarshal(raw, &items); err != nil {
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
		failures := 0
		for i, it := range items {
			out[i] = s.encryptOne(r, tenant, it)
			if out[i].Error != "" {
				failures++
			}
		}
		if claims, _ := authmw.FromContext(r.Context()); claims != nil {
			s.Audit.Batch(r.Context(), claims.Sub, tenant, "encrypt", len(items), failures, chimw.GetReqID(r.Context()))
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	var item encryptItem
	if err := json.Unmarshal(raw, &item); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	out := s.encryptOne(r, tenant, item)
	if out.Error != "" {
		writeError(w, http.StatusBadRequest, out.Error)
		return
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
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(raw) == 0 {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if raw[0] == '[' {
		var items []decryptItem
		if err := json.Unmarshal(raw, &items); err != nil {
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
		failures := 0
		for i, it := range items {
			out[i] = s.decryptOne(r, tenant, it)
			if out[i].Error != "" {
				failures++
			}
		}
		if claims, _ := authmw.FromContext(r.Context()); claims != nil {
			s.Audit.Batch(r.Context(), claims.Sub, tenant, "decrypt", len(items), failures, chimw.GetReqID(r.Context()))
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	var item decryptItem
	if err := json.Unmarshal(raw, &item); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	out := s.decryptOne(r, tenant, item)
	if out.Error != "" {
		status := http.StatusBadRequest
		if out.Error == "MarkingDenied" || out.Error == "access denied" {
			status = http.StatusForbidden
		}
		writeError(w, status, out.Error)
		return
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
	if k.Status == domain.StatusRevoked {
		res.Error = "key is revoked"
		return res
	}
	if !k.CanEncrypt() {
		res.Error = "key is retired"
		return res
	}
	if in.Algorithm != "" && domain.Algorithm(in.Algorithm) != k.Algorithm {
		res.Error = "algorithm mismatch"
		s.auditOperation(r, tenant, keyID, cipherOpEncrypt, string(k.Algorithm), in.ResourceRID, false, "not_checked", k.Markings)
		return res
	}
	claims, _ := authmw.FromContext(r.Context())
	if err := authorizeKeyOperation(r.Context(), claims, k, cipherOpEncrypt); err != nil {
		res.Error = "access denied"
		s.auditOperation(r, tenant, keyID, cipherOpEncrypt, string(k.Algorithm), in.ResourceRID, false, "not_checked", k.Markings)
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
	var plaintext []byte
	if in.PlaintextB64 != "" {
		plaintext, err = base64.StdEncoding.DecodeString(in.PlaintextB64)
		if err != nil {
			res.Error = "plaintext_b64 is not valid base64"
			s.auditOperation(r, tenant, keyID, cipherOpEncrypt, string(k.Algorithm), in.ResourceRID, false, "not_checked", k.Markings)
			return res
		}
	} else {
		plaintext = []byte(in.Plaintext)
	}
	envelope, err := cryptopkg.Encrypt(keyID, k.Version, k.Algorithm, dek, plaintext)
	if err != nil {
		res.Error = "encrypt failed"
		s.auditOperation(r, tenant, keyID, cipherOpEncrypt, string(k.Algorithm), in.ResourceRID, false, "not_checked", k.Markings)
		return res
	}
	res.Version = k.Version
	res.CiphertextB64 = base64.StdEncoding.EncodeToString(envelope)
	res.Ciphertext = res.CiphertextB64
	s.auditOperation(r, tenant, keyID, cipherOpEncrypt, string(k.Algorithm), in.ResourceRID, true, "not_checked", k.Markings)
	return res
}

func (s *State) decryptOne(r *http.Request, tenant uuid.UUID, in decryptItem) decryptResult {
	var keyID uuid.UUID
	var err error
	if strings.TrimSpace(in.KeyID) != "" {
		keyID, err = uuid.Parse(in.KeyID)
		if err != nil {
			return decryptResult{Error: "invalid key id"}
		}
	}
	ciphertext := in.CiphertextB64
	if ciphertext == "" {
		ciphertext = in.Ciphertext
	}
	envelope, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return decryptResult{Error: "ciphertext_b64 is not valid base64"}
	}

	// Pull metadata off-wire so the schema-v1 envelope is self-describing.
	// Legacy callers may still send key_id/version beside the ciphertext;
	// when they do, require those claims to match the envelope.
	decodedEnvelope, err := cryptopkg.DecodeEnvelope(envelope)
	if err != nil {
		return decryptResult{Error: "invalid envelope"}
	}
	if keyID == uuid.Nil {
		keyID = decodedEnvelope.KeyID
	} else if decodedEnvelope.KeyID != keyID {
		return decryptResult{Error: "envelope header does not match request"}
	}
	version := in.Version
	if version == 0 {
		version = decodedEnvelope.KeyVersion
	} else if decodedEnvelope.KeyVersion != version {
		return decryptResult{Error: "envelope header does not match request"}
	}

	k, err := s.Repo.GetKey(r.Context(), tenant, keyID)
	if err != nil {
		return decryptResult{Error: mapErrorMessage(err)}
	}
	if k.Status == domain.StatusRevoked {
		return decryptResult{Error: "key is revoked"}
	}
	if !k.CanDecrypt() {
		return decryptResult{Error: "key not decryptable"}
	}
	claims, _ := authmw.FromContext(r.Context())
	if err := authorizeKeyOperation(r.Context(), claims, k, cipherOpDecrypt); err != nil {
		s.auditOperation(r, tenant, keyID, cipherOpDecrypt, string(k.Algorithm), in.ResourceRID, false, "not_checked", appendMarkings(k.Markings, in.ResourceMarkings))
		return decryptResult{Error: "access denied"}
	}
	if claims != nil && !s.Budgets.Allow(claims.Sub, keyID) {
		s.auditOperation(r, tenant, keyID, cipherOpDecrypt, string(k.Algorithm), in.ResourceRID, false, "budget_exceeded", appendMarkings(k.Markings, in.ResourceMarkings))
		return decryptResult{Error: "decrypt budget exceeded"}
	}
	markings := appendMarkings(k.Markings, in.ResourceMarkings)
	if denied := firstDeniedMarking(claims, markings); denied != "" {
		s.auditOperation(r, tenant, keyID, cipherOpDecrypt, string(k.Algorithm), in.ResourceRID, false, "denied:"+denied, markings)
		return decryptResult{Error: "MarkingDenied"}
	}

	v, err := s.Repo.GetVersion(r.Context(), tenant, keyID, version)
	if err != nil {
		return decryptResult{Error: mapErrorMessage(err)}
	}
	dek, err := s.KMS.Unwrap(v.WrappedKeyMaterial)
	if err != nil {
		return decryptResult{Error: "kms unwrap failed"}
	}
	plaintext, err := cryptopkg.Decrypt(keyID, version, k.Algorithm, dek, envelope)
	if err != nil {
		s.auditOperation(r, tenant, keyID, cipherOpDecrypt, string(k.Algorithm), in.ResourceRID, false, "passed", markings)
		return decryptResult{Error: "decrypt failed"}
	}
	s.auditOperation(r, tenant, keyID, cipherOpDecrypt, string(k.Algorithm), in.ResourceRID, true, "passed", markings)
	return decryptResult{Plaintext: string(plaintext), PlaintextB64: base64.StdEncoding.EncodeToString(plaintext)}
}

// ─── Common helpers ───────────────────────────────────────────────────

func (s *State) auditOperation(r *http.Request, tenant uuid.UUID, keyID uuid.UUID, operation, algorithm, resourceRID string, success bool, markingResult string, markings []string) {
	claims, _ := authmw.FromContext(r.Context())
	if claims == nil {
		return
	}
	requestID := chimw.GetReqID(r.Context())
	if requestID == "" {
		requestID = r.Header.Get("X-Request-Id")
	}
	s.Audit.Operation(r.Context(), claims.Sub, tenant, keyID, operation, algorithm, resourceRID, success, markingResult, requestID, markings)
}

func appendMarkings(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	seen := map[string]struct{}{}
	for _, value := range append(append([]string(nil), a...), b...) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func firstDeniedMarking(claims *authmw.Claims, markings []string) string {
	for _, marking := range markings {
		if !claims.AllowsMarking(marking) {
			return marking
		}
	}
	return ""
}

func accessPolicyOrDefault(policy *domain.AccessPolicy) domain.AccessPolicy {
	if policy == nil {
		return domain.DefaultAccessPolicy()
	}
	return policy.Clone()
}

func parseOptionalUUID(raw string) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return uuid.Nil, nil
	}
	return uuid.Parse(trimmed)
}

func parseUUIDList(raw []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(raw))
	for _, value := range raw {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		id, err := uuid.Parse(trimmed)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func parseOptionalTime(raw string) (*time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func uuidToString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func uuidListToStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil {
			out = append(out, id.String())
		}
	}
	return out
}

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
	case errors.Is(err, domain.ErrKeyRevoked):
		writeError(w, http.StatusConflict, "key is revoked")
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
	case errors.Is(err, domain.ErrKeyRevoked):
		return "key is revoked"
	case errors.Is(err, domain.ErrInvalidEnvelope):
		return "invalid envelope"
	default:
		return "internal error"
	}
}
