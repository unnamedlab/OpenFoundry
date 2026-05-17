package signingkeys

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Default lifecycle knobs. The task contract pins the grace window
// (retiring keys leave the JWKS 6h after rotation) and the lower
// bound on the active-key remaining lifetime that triggers a refresh
// (must be > now + 24h).
const (
	DefaultActiveLifetime = 30 * 24 * time.Hour
	DefaultGraceWindow    = 6 * time.Hour
	BootstrapRefreshFloor = 24 * time.Hour
)

// Clock is the time source the manager pulls "now" from. Tests
// inject a controllable clock; production wires time.Now.
type Clock func() time.Time

// Policy tunes the manager's lifecycle decisions.
type Policy struct {
	ActiveLifetime        time.Duration
	GraceWindow           time.Duration
	BootstrapRefreshFloor time.Duration
}

// DefaultPolicy returns the production defaults.
func DefaultPolicy() Policy {
	return Policy{
		ActiveLifetime:        DefaultActiveLifetime,
		GraceWindow:           DefaultGraceWindow,
		BootstrapRefreshFloor: BootstrapRefreshFloor,
	}
}

// ErrNoActiveKey is returned by ActiveKey / SignKey when the store
// has not yet been bootstrapped. Callers should EnsureBootstrap
// first.
var ErrNoActiveKey = errors.New("no active signing key")

// Manager owns the signing-key lifecycle: bootstrap, rotation,
// grace-window cleanup, and the JWKS publication used by verifiers.
//
// Verifiers (sign middleware always uses the active key; verify
// middleware accepts active + retiring kids) consume the published
// material through KeyMaterial / VerifierKeys.
type Manager struct {
	store  Store
	sealer *Sealer
	policy Policy
	clock  Clock

	// cache speeds up the hot read paths (sign + verify) without
	// hitting Postgres on every JWT. Invalidated by Rotate /
	// EnsureBootstrap so a peer's rotation eventually propagates
	// once any local mutation runs.
	mu          sync.RWMutex
	activeCache *KeyMaterial
	verifyCache map[string]*KeyMaterial
}

// NewManager wires the dependencies. Use EnsureBootstrap before the
// first sign call so the store is guaranteed to have an active row.
func NewManager(store Store, sealer *Sealer, policy Policy, clock Clock) *Manager {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Manager{
		store:       store,
		sealer:      sealer,
		policy:      policy,
		clock:       clock,
		verifyCache: map[string]*KeyMaterial{},
	}
}

// EnsureBootstrap creates an active key when:
//   - the store is empty, or
//   - the current active key's not_after is within the refresh floor.
//
// It also collapses expired retiring rows to 'retired' before
// returning so the JWKS publisher does not have to re-filter on
// every call.
func (m *Manager) EnsureBootstrap(ctx context.Context) error {
	now := m.clock()
	if _, err := m.store.MarkExpired(ctx, now); err != nil {
		return fmt.Errorf("mark expired: %w", err)
	}
	active, err := m.store.Active(ctx)
	if err != nil {
		return fmt.Errorf("load active: %w", err)
	}
	if active != nil && active.NotAfter.After(now.Add(m.policy.BootstrapRefreshFloor)) {
		m.invalidateCache()
		return nil
	}
	previousKid := ""
	if active != nil {
		previousKid = active.Kid
	}
	if _, err := m.rotateLocked(ctx, previousKid, now); err != nil {
		return err
	}
	return nil
}

// Rotate forces a new active key, demotes the existing one to
// retiring (with not_after = now + GraceWindow), and returns the
// outcome. Concurrent Rotate calls serialise on the manager mutex
// so two parallel forces produce two distinct kids without losing
// either.
func (m *Manager) Rotate(ctx context.Context) (RotationOutcome, error) {
	now := m.clock()
	expired, err := m.store.MarkExpired(ctx, now)
	if err != nil {
		return RotationOutcome{}, fmt.Errorf("mark expired: %w", err)
	}
	active, err := m.store.Active(ctx)
	if err != nil {
		return RotationOutcome{}, fmt.Errorf("load active: %w", err)
	}
	previousKid := ""
	if active != nil {
		previousKid = active.Kid
	}
	rec, err := m.rotateLocked(ctx, previousKid, now)
	if err != nil {
		return RotationOutcome{}, err
	}
	return RotationOutcome{
		PreviousKid:  previousKid,
		ActiveKid:    rec.Kid,
		GraceUntil:   now.Add(m.policy.GraceWindow),
		RetiredCount: expired,
	}, nil
}

// rotateLocked owns the mint + persist sequence shared by
// EnsureBootstrap and Rotate.
func (m *Manager) rotateLocked(ctx context.Context, previousKid string, now time.Time) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	priv, err := GenerateRSAKey()
	if err != nil {
		return Record{}, fmt.Errorf("generate rsa key: %w", err)
	}
	privPEM, err := EncodePrivateKeyPEM(priv)
	if err != nil {
		return Record{}, err
	}
	pubPEM, err := EncodePublicKeyPEM(&priv.PublicKey)
	if err != nil {
		return Record{}, err
	}
	sealed, err := m.sealer.Seal(privPEM)
	if err != nil {
		return Record{}, fmt.Errorf("seal private key: %w", err)
	}
	kid, err := ComputeKid(&priv.PublicKey)
	if err != nil {
		return Record{}, err
	}
	rec := Record{
		Kid:           kid,
		Algorithm:     AlgorithmRS256,
		PublicKeyPEM:  string(pubPEM),
		PrivateKeyEnc: sealed,
		CreatedAt:     now,
		NotBefore:     now,
		NotAfter:      now.Add(m.policy.ActiveLifetime),
		Status:        StatusActive,
	}
	retireAt := now.Add(m.policy.GraceWindow)
	if previousKid == "" {
		if err := m.store.Insert(ctx, rec); err != nil {
			return Record{}, fmt.Errorf("insert: %w", err)
		}
	} else {
		if err := m.store.Rotate(ctx, previousKid, rec, retireAt); err != nil {
			return Record{}, fmt.Errorf("rotate: %w", err)
		}
	}
	m.activeCache = nil
	m.verifyCache = map[string]*KeyMaterial{}
	return rec, nil
}

func (m *Manager) invalidateCache() {
	m.mu.Lock()
	m.activeCache = nil
	m.verifyCache = map[string]*KeyMaterial{}
	m.mu.Unlock()
}

// ActiveKey returns the current signing key, ready for use. Reads
// hit a cached value when warm; cold reads decrypt + parse the PEM.
func (m *Manager) ActiveKey(ctx context.Context) (*KeyMaterial, error) {
	m.mu.RLock()
	cached := m.activeCache
	m.mu.RUnlock()
	if cached != nil {
		return cached, nil
	}
	rec, err := m.store.Active(ctx)
	if err != nil {
		return nil, fmt.Errorf("load active: %w", err)
	}
	if rec == nil {
		return nil, ErrNoActiveKey
	}
	mat, err := m.materialise(*rec, true)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.activeCache = mat
	m.verifyCache[mat.Record.Kid] = mat
	m.mu.Unlock()
	return mat, nil
}

// VerifierKey resolves a kid against the active + retiring set. A
// retired (or unknown) kid returns ErrNoActiveKey.
func (m *Manager) VerifierKey(ctx context.Context, kid string) (*KeyMaterial, error) {
	now := m.clock()
	m.mu.RLock()
	cached := m.verifyCache[kid]
	m.mu.RUnlock()
	if cached != nil && cached.Record.NotAfter.After(now) {
		return cached, nil
	}
	if cached != nil {
		m.mu.Lock()
		delete(m.verifyCache, kid)
		m.mu.Unlock()
	}
	if _, err := m.store.MarkExpired(ctx, now); err != nil {
		return nil, fmt.Errorf("mark expired: %w", err)
	}
	active, err := m.store.Active(ctx)
	if err != nil {
		return nil, fmt.Errorf("load active: %w", err)
	}
	if active != nil && active.Kid == kid {
		mat, err := m.materialise(*active, false)
		if err != nil {
			return nil, err
		}
		m.mu.Lock()
		m.verifyCache[kid] = mat
		m.mu.Unlock()
		return mat, nil
	}
	// Refresh `now` after MarkExpired in case retire boundary moved.
	retiring, err := m.store.Retiring(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("load retiring: %w", err)
	}
	for _, rec := range retiring {
		if rec.Kid != kid {
			continue
		}
		mat, err := m.materialise(rec, false)
		if err != nil {
			return nil, err
		}
		m.mu.Lock()
		m.verifyCache[kid] = mat
		m.mu.Unlock()
		return mat, nil
	}
	return nil, ErrNoActiveKey
}

// PublicKeyResolver returns a closure suitable for the
// authmw.PublicKeyResolver contract.
func (m *Manager) PublicKeyResolver(ctx context.Context) func(string) (*rsa.PublicKey, error) {
	return func(kid string) (*rsa.PublicKey, error) {
		mat, err := m.VerifierKey(ctx, kid)
		if err != nil {
			return nil, err
		}
		return mat.PublicKey, nil
	}
}

// Jwks returns the active + retiring set formatted for the
// /.well-known/jwks.json document. The active key is always first.
func (m *Manager) Jwks(ctx context.Context) (Jwks, error) {
	now := m.clock()
	if _, err := m.store.MarkExpired(ctx, now); err != nil {
		return Jwks{}, fmt.Errorf("mark expired: %w", err)
	}
	keys := make([]Jwk, 0, 2)
	active, err := m.store.Active(ctx)
	if err != nil {
		return Jwks{}, fmt.Errorf("load active: %w", err)
	}
	if active != nil {
		pub, err := DecodePublicKeyPEM([]byte(active.PublicKeyPEM))
		if err != nil {
			return Jwks{}, err
		}
		keys = append(keys, PublicKeyToJwk(pub, active.Kid))
	}
	retiring, err := m.store.Retiring(ctx, now)
	if err != nil {
		return Jwks{}, fmt.Errorf("load retiring: %w", err)
	}
	for _, rec := range retiring {
		pub, err := DecodePublicKeyPEM([]byte(rec.PublicKeyPEM))
		if err != nil {
			return Jwks{}, err
		}
		keys = append(keys, PublicKeyToJwk(pub, rec.Kid))
	}
	return Jwks{Keys: keys}, nil
}

// materialise unwraps the sealed private key (when needed for
// signing) and parses the PEM into rsa structs.
func (m *Manager) materialise(rec Record, withPrivate bool) (*KeyMaterial, error) {
	pub, err := DecodePublicKeyPEM([]byte(rec.PublicKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("decode public pem: %w", err)
	}
	mat := &KeyMaterial{Record: rec, PublicKey: pub}
	if withPrivate {
		raw, err := m.sealer.Open(rec.PrivateKeyEnc)
		if err != nil {
			return nil, fmt.Errorf("open private key: %w", err)
		}
		priv, err := DecodePrivateKeyPEM(raw)
		if err != nil {
			return nil, fmt.Errorf("decode private pem: %w", err)
		}
		mat.PrivateKey = priv
	}
	return mat, nil
}
