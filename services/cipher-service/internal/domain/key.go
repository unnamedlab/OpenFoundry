// Package domain holds the pure-Go cipher-service types and the
// lifecycle invariants that bind them. Nothing in this package may
// import internal/repo or internal/handler — these are the
// vocabulary the rest of the service speaks.
//
// Foundry-Cipher Milestone A (see
// docs/migration/foundry-cipher-1to1-checklist.md, items CIP.1–CIP.8)
// requires a credible authenticated-mode encrypt/decrypt path with a
// versioned, tenant-scoped key registry. The deterministic SIV mode
// (CIP.9) is intentionally out of scope here — the algorithm enum
// reserves the identifier so callers can declare intent at key
// creation, but encrypt/decrypt of SIV-tagged keys returns
// ErrAlgorithmUnsupported until Milestone B lands.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// KeyBackend identifies where a key version's wrapping material lives.
// The value is wire-visible so promotions can provision equivalent backend
// policy in the target environment without copying ciphertext across envs.
type KeyBackend string

const (
	KeyBackendLocal         KeyBackend = "local"
	KeyBackendVaultTransit  KeyBackend = "vault_transit"
	KeyBackendAWSKMS        KeyBackend = "aws_kms"
	KeyBackendGCPKMS        KeyBackend = "gcp_kms"
	KeyBackendAzureKeyVault KeyBackend = "azure_key_vault"
	KeyBackendPKCS11        KeyBackend = "pkcs11"
)

// Valid reports whether the backend is a supported registry identifier.
func (b KeyBackend) Valid() bool {
	switch b {
	case KeyBackendLocal, KeyBackendVaultTransit, KeyBackendAWSKMS, KeyBackendGCPKMS, KeyBackendAzureKeyVault, KeyBackendPKCS11:
		return true
	}
	return false
}

func (b KeyBackend) OrDefault() KeyBackend {
	if b == "" {
		return KeyBackendLocal
	}
	return b
}

// Algorithm names the primitive a key or pepper-backed operation binds
// to. Stable string identifiers are intentionally wire-visible: they
// are copied into ciphertext envelopes, key registry rows, and audit
// events so readers can choose the right decrypt/hash implementation
// without tenant-local lookup tables.
type Algorithm string

const (
	// AlgorithmAES256GCMSIV is the recommended authenticated encryption
	// default. Its nonce is misuse-resistant and may be synthetic for
	// deterministic joins once the Milestone B SIV path lands.
	AlgorithmAES256GCMSIV Algorithm = "AES_256_GCM_SIV"

	// AlgorithmAES256SIV is the explicit opt-in deterministic mode used
	// for join/group-by ciphertext comparisons. It leaks equality/frequency
	// patterns for repeated plaintexts under the same key, so it is never
	// selected as an implicit default.
	AlgorithmAES256SIV Algorithm = "AES_256_SIV"

	// AlgorithmAES256GCM is the AEAD primitive the service currently
	// implements end-to-end. It uses a fresh 12-byte nonce per encryption
	// and is therefore authenticated but non-deterministic.
	AlgorithmAES256GCM Algorithm = "AES_256_GCM"

	// AlgorithmSHA256 is a pepper-backed digest mode. It can only execute
	// through the pepper registry so pepper material never leaves the
	// cipher service.
	AlgorithmSHA256 Algorithm = "SHA_256"

	// AlgorithmSHA512 is the 512-bit pepper-backed digest mode.
	AlgorithmSHA512 Algorithm = "SHA_512"
)

// AlgorithmDescriptor is the immutable catalog entry for one built-in
// algorithm. It is deliberately richer than the Algorithm enum so HTTP
// clients, CLIs, and migration tooling can discover envelope IDs, key
// lengths, nonce behavior, and default encodings without hard-coding the
// service's constants.
type AlgorithmDescriptor struct {
	ID                 Algorithm
	StableIdentifier   string
	Kind               string
	KeyLengthBytes     uint16
	NoncePolicy        string
	OutputEncoding     string
	Authenticated      bool
	Deterministic      bool
	RecommendedDefault bool
	PepperRequired     bool
	SecurityNotice     string
	Status             string
}

const (
	algorithmKindAEAD = "aead"
	algorithmKindHash = "hash"

	algorithmStatusAvailable = "available"
	algorithmStatusReserved  = "reserved"

	noncePolicyRandom96Bit       = "random_96_bit_per_encryption"
	noncePolicySyntheticSIV      = "synthetic_siv_or_caller_supplied_96_bit"
	noncePolicyPepperHash        = "not_applicable_pepper_required"
	defaultAlgorithmOutputEncode = "base64"
)

var builtInAlgorithms = []AlgorithmDescriptor{
	{
		ID:                 AlgorithmAES256GCMSIV,
		StableIdentifier:   string(AlgorithmAES256GCMSIV),
		Kind:               algorithmKindAEAD,
		KeyLengthBytes:     32,
		NoncePolicy:        noncePolicySyntheticSIV,
		OutputEncoding:     defaultAlgorithmOutputEncode,
		Authenticated:      true,
		Deterministic:      true,
		RecommendedDefault: true,
		Status:             algorithmStatusReserved,
	},
	{
		ID:               AlgorithmAES256SIV,
		StableIdentifier: string(AlgorithmAES256SIV),
		Kind:             algorithmKindAEAD,
		KeyLengthBytes:   32,
		NoncePolicy:      noncePolicySyntheticSIV,
		OutputEncoding:   defaultAlgorithmOutputEncode,
		Authenticated:    true,
		Deterministic:    true,
		SecurityNotice:   "Deterministic encryption enables ciphertext joins but exposes equality and frequency patterns; require explicit opt-in on key creation.",
		Status:           algorithmStatusAvailable,
	},
	{
		ID:               AlgorithmAES256GCM,
		StableIdentifier: string(AlgorithmAES256GCM),
		Kind:             algorithmKindAEAD,
		KeyLengthBytes:   32,
		NoncePolicy:      noncePolicyRandom96Bit,
		OutputEncoding:   defaultAlgorithmOutputEncode,
		Authenticated:    true,
		Status:           algorithmStatusAvailable,
	},
	{
		ID:               AlgorithmSHA256,
		StableIdentifier: string(AlgorithmSHA256),
		Kind:             algorithmKindHash,
		KeyLengthBytes:   32,
		NoncePolicy:      noncePolicyPepperHash,
		OutputEncoding:   defaultAlgorithmOutputEncode,
		PepperRequired:   true,
		Status:           algorithmStatusAvailable,
	},
	{
		ID:               AlgorithmSHA512,
		StableIdentifier: string(AlgorithmSHA512),
		Kind:             algorithmKindHash,
		KeyLengthBytes:   64,
		NoncePolicy:      noncePolicyPepperHash,
		OutputEncoding:   defaultAlgorithmOutputEncode,
		PepperRequired:   true,
		Status:           algorithmStatusAvailable,
	},
}

// BuiltInAlgorithms returns the deterministic, immutable algorithm
// registry order exposed on the wire. Callers get a defensive copy so
// tests and handlers cannot mutate package-level catalog state.
func BuiltInAlgorithms() []AlgorithmDescriptor {
	out := make([]AlgorithmDescriptor, len(builtInAlgorithms))
	copy(out, builtInAlgorithms)
	return out
}

// Descriptor resolves an algorithm to its registry metadata.
func (a Algorithm) Descriptor() (AlgorithmDescriptor, bool) {
	for _, d := range builtInAlgorithms {
		if d.ID == a {
			return d, true
		}
	}
	return AlgorithmDescriptor{}, false
}

// Valid reports whether `a` is a known registry identifier.
func (a Algorithm) Valid() bool {
	_, ok := a.Descriptor()
	return ok
}

// SupportsCipherKeyResource reports whether the algorithm can back a
// cipher_key row. Pepper-backed hash modes are discoverable registry
// entries, but their dedicated pepper resource/API lands with CIP.13.
func (a Algorithm) SupportsCipherKeyResource() bool {
	d, ok := a.Descriptor()
	return ok && d.Kind == algorithmKindAEAD
}

// Status captures the lifecycle position of a key. Three states is the
// minimum that lets the cipher-service distinguish "encrypt + decrypt",
// "decrypt-only", and "no operations" — matches the public Foundry
// Cipher key-lifecycle contract (CIP.16, CIP.17 in the checklist).
type Status string

const (
	// StatusActive permits encrypt and decrypt.
	StatusActive Status = "active"

	// StatusRotating is an active key whose latest version has just
	// been rolled. Encrypt + decrypt both still succeed; the state is
	// surfaced so dashboards can show in-flight rotations.
	StatusRotating Status = "rotating"

	// StatusRetired refuses new encrypts but keeps decrypt available
	// so stored ciphertext stays readable. Reversible only by admins.
	StatusRetired Status = "retired"

	// StatusRevoked hard-stops both encrypt and decrypt immediately.
	StatusRevoked Status = "revoked"
)

// Valid reports whether `s` is one of the recognised lifecycle states.
func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusRotating, StatusRetired, StatusRevoked:
		return true
	}
	return false
}

// OperationPolicy lists principals allowed to perform one operation on a
// cipher key. Role/group/project grants are later compiled into a small
// Cedar policy for request-time evaluation; empty lists mean deny.
type OperationPolicy struct {
	Roles    []string `json:"roles,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	Projects []string `json:"projects,omitempty"`
}

// Clone returns a defensive copy.
func (p OperationPolicy) Clone() OperationPolicy {
	return OperationPolicy{
		Roles:    append([]string(nil), p.Roles...),
		Groups:   append([]string(nil), p.Groups...),
		Projects: append([]string(nil), p.Projects...),
	}
}

// AccessPolicy is the per-key CIP.4 policy surface.
type AccessPolicy struct {
	Encrypt OperationPolicy `json:"encrypt"`
	Decrypt OperationPolicy `json:"decrypt"`
	Manage  OperationPolicy `json:"manage"`
}

// Clone returns a defensive copy.
func (p AccessPolicy) Clone() AccessPolicy {
	return AccessPolicy{Encrypt: p.Encrypt.Clone(), Decrypt: p.Decrypt.Clone(), Manage: p.Manage.Clone()}
}

// DefaultAccessPolicy grants the admin role all key operations. Callers can
// replace it at create/update time; evaluation remains deny-by-default for
// any operation with no matching grant.
func DefaultAccessPolicy() AccessPolicy {
	admin := OperationPolicy{Roles: []string{"admin"}}
	return AccessPolicy{Encrypt: admin.Clone(), Decrypt: admin.Clone(), Manage: admin.Clone()}
}

// CipherKey is the tenant-scoped registry row. Every encrypt/decrypt
// envelope carries a key id that resolves to a CipherKey row plus the
// CipherKeyVersion the envelope references.
type CipherKey struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Alias     string
	Algorithm Algorithm
	// KeyMaterialRef is the opaque KMS/Vault reference for the active
	// wrapping. It is safe to return because it never contains plaintext
	// DEK bytes; callers still cannot unwrap material without KMS access.
	KeyMaterialRef string
	KMSBackend     KeyBackend
	OwnerID        uuid.UUID
	Organizations  []uuid.UUID
	Markings       []string
	IntendedScopes []string
	AccessPolicy   AccessPolicy
	// Version is the currently active version number (1-indexed). The
	// list of historical versions lives in CipherKeyVersion rows
	// keyed by KeyID.
	Version   uint32
	Status    Status
	CreatedAt time.Time
	ExpiresAt *time.Time
	// RotatedAt is the last time a new version was activated. nil
	// when the key has only the initial v1.
	RotatedAt *time.Time
}

// Clone returns a defensive copy of a key, including slice metadata used
// by the API layer. The wrapped/plaintext key material is never part of
// CipherKey, so copying this struct is safe for responses.
func (k *CipherKey) Clone() *CipherKey {
	if k == nil {
		return nil
	}
	cp := *k
	cp.Organizations = append([]uuid.UUID(nil), k.Organizations...)
	cp.Markings = append([]string(nil), k.Markings...)
	cp.IntendedScopes = append([]string(nil), k.IntendedScopes...)
	cp.AccessPolicy = k.AccessPolicy.Clone()
	return &cp
}

// CipherKeyVersion is one wrapping of the DEK (data-encryption key)
// belonging to a CipherKey. The plaintext DEK is never persisted; only
// the result of KMS.Wrap(DEK) sits in WrappedKeyMaterial.
//
// A version is "active" when its row matches the parent key's Version.
// Older versions stay decryptable until the key is retired.
type CipherKeyVersion struct {
	KeyID              uuid.UUID
	Version            uint32
	WrappedKeyMaterial []byte
	// KMSKeyRef identifies the KEK that wrapped this DEK so a future
	// rotation of the KEK can rewrap without losing provenance.
	KMSKeyRef   string
	CreatedAt   time.Time
	ActivatedAt time.Time
	// RetiredAt marks the moment a version stopped being the active
	// one. Decrypt continues to work as long as the parent key is not
	// itself retired.
	RetiredAt *time.Time
}

// Pepper is the tenant-scoped registry row for irreversible tokenization.
// PepperMaterialRef is an opaque KMS/Vault reference; plaintext pepper bytes
// are only represented by PepperVersion.WrappedPepperMaterial.
type Pepper struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	Name              string
	Algorithm         Algorithm
	PepperMaterialRef string
	Version           uint32
	AccessPolicy      AccessPolicy
	CreatedAt         time.Time
	RotatedAt         *time.Time
}

func (p *Pepper) Clone() *Pepper {
	if p == nil {
		return nil
	}
	cp := *p
	cp.AccessPolicy = p.AccessPolicy.Clone()
	return &cp
}

// PepperVersion stores one KMS-wrapped pepper version.
type PepperVersion struct {
	PepperID              uuid.UUID
	Version               uint32
	WrappedPepperMaterial []byte
	KMSKeyRef             string
	CreatedAt             time.Time
}

// Sentinel errors. Handlers map these to HTTP status codes; the
// errors.Is contract lets the repo layer wrap them with context.
var (
	// ErrKeyNotFound is returned when the registry has no row for a
	// (tenant, key id) pair.
	ErrKeyNotFound = errors.New("cipher: key not found")

	// ErrKeyVersionNotFound is returned when the requested version is
	// missing — either it never existed or it was rotated out before
	// the row could be persisted.
	ErrKeyVersionNotFound = errors.New("cipher: key version not found")

	// ErrKeyRetired blocks encrypt operations against a retired key.
	// Decrypt callers see this only when the surrounding key is fully
	// retired (versions still resolve for older envelopes).
	ErrKeyRetired = errors.New("cipher: key retired")

	// ErrKeyRevoked blocks all operations against a revoked key.
	ErrKeyRevoked = errors.New("cipher: key revoked")

	// ErrTenantMismatch is returned when a caller resolves a key from
	// a different tenant. Mapped to 404 to avoid leaking existence
	// across tenant boundaries.
	ErrTenantMismatch = errors.New("cipher: tenant mismatch")

	// ErrAlgorithmUnsupported is returned when a caller attempts an
	// encrypt/decrypt against a key whose algorithm has no
	// implementation yet (currently AES_256_GCM_SIV).
	ErrAlgorithmUnsupported = errors.New("cipher: algorithm not yet supported")

	// ErrInvalidEnvelope is returned by Decrypt when the on-wire
	// ciphertext is too short, malformed, or fails AEAD authentication.
	ErrInvalidEnvelope = errors.New("cipher: invalid envelope")

	// ErrAccessDenied is returned when the per-key Cedar policy denies an operation.
	ErrAccessDenied = errors.New("cipher: access denied")

	// ErrMarkingDenied is returned when decrypt markings exceed caller clearances.
	ErrMarkingDenied = errors.New("cipher: marking denied")
)

// CanEncrypt reports whether the key allows fresh encryptions in its
// current state.
func (k *CipherKey) CanEncrypt() bool {
	return k != nil && (k.Status == StatusActive || k.Status == StatusRotating)
}

// CanDecrypt reports whether the key still serves decrypt requests.
// Retired keys keep decrypt available so historical ciphertexts remain
// readable; revoked keys hard-block decrypt immediately.
func (k *CipherKey) CanDecrypt() bool {
	return k != nil && (k.Status == StatusActive || k.Status == StatusRotating || k.Status == StatusRetired)
}
