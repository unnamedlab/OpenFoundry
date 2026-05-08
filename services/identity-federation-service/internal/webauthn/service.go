// Package webauthn wraps github.com/go-webauthn/webauthn for
// identity-federation-service slice 4.
//
// Mirrors the Rust crate's `hardening::webauthn::WebAuthnService`
// surface (register_challenge, register_finish, login_challenge,
// login_finish, has_credentials). Storage is Postgres in slice 4;
// the slice-2b Cassandra port mirrors the same Store interface so
// the swap is a one-line change.
package webauthn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

// RelyingPartyConfig is the Web Authentication relying party identity.
//
// - DisplayName is shown in the UA prompt.
// - ID is the host (no scheme, no port). Browsers refuse to register
//   credentials when this differs from the request origin's eTLD+1.
// - Origins is the allow-list of full origins (scheme + host + port).
type RelyingPartyConfig struct {
	DisplayName string
	ID          string
	Origins     []string
}

// FromEnv builds a RelyingPartyConfig from the Rust-aligned env contract.
//
//	WEBAUTHN_RP_NAME (default "OpenFoundry")
//	WEBAUTHN_RP_ID (default "localhost")
//	WEBAUTHN_RP_ORIGIN (CSV; default "http://localhost:5173")
func FromEnv() RelyingPartyConfig {
	rp := RelyingPartyConfig{
		DisplayName: defaultStr(os.Getenv("WEBAUTHN_RP_NAME"), "OpenFoundry"),
		ID:          defaultStr(os.Getenv("WEBAUTHN_RP_ID"), "localhost"),
	}
	originsCSV := defaultStr(os.Getenv("WEBAUTHN_RP_ORIGIN"), "http://localhost:5173")
	for _, s := range splitCSV(originsCSV) {
		rp.Origins = append(rp.Origins, s)
	}
	return rp
}

// Store is the persistence interface: credentials + transient challenges.
//
// Implementations are expected to be goroutine-safe.
type Store interface {
	// Credentials.
	HasCredentials(ctx context.Context, userID uuid.UUID) (bool, error)
	ListCredentials(ctx context.Context, userID uuid.UUID) ([]Credential, error)
	GetCredentialByCredentialID(ctx context.Context, credentialID []byte) (*Credential, error)
	InsertCredential(ctx context.Context, c Credential) error
	UpdateSignCount(ctx context.Context, credentialID []byte, signCount uint32, lastUsedAt time.Time) error

	// Challenges (server-side session data binding the ceremony).
	StoreChallenge(ctx context.Context, ch ChallengeRecord) error
	LoadChallenge(ctx context.Context, challengeID uuid.UUID) (*ChallengeRecord, error)
	DeleteChallenge(ctx context.Context, challengeID uuid.UUID) error
}

// Credential is a registered authenticator (matches webauthn.Credential
// + the columns we persist).
type Credential struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	CredentialID    []byte
	PublicKey       []byte
	SignCount       uint32
	Transports      []string
	AttestationType string
	AAGUID          *uuid.UUID
	Label           string
	CreatedAt       time.Time
	LastUsedAt      *time.Time
}

// ChallengeRecord stores the go-webauthn SessionData blob keyed by id.
type ChallengeRecord struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Kind        string // "register" | "login"
	SessionData []byte
	ExpiresAt   time.Time
}

// User adapts our User to webauthn.User without baking go-webauthn into models/.
type User struct {
	ID          uuid.UUID
	Name        string
	DisplayName string
	Credentials []Credential
}

// WebAuthnID returns the user's unique handle for the RP.
//
// Spec: 1-64 bytes, opaque. We use the v7 UUID bytes — guaranteed
// unique, time-ordered, and the right length.
func (u *User) WebAuthnID() []byte { b, _ := u.ID.MarshalBinary(); return b }

// WebAuthnName / WebAuthnDisplayName satisfy webauthn.User.
func (u *User) WebAuthnName() string        { return u.Name }
func (u *User) WebAuthnDisplayName() string { return u.DisplayName }

// WebAuthnCredentials maps Credential[] → webauthn.Credential[].
func (u *User) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(u.Credentials))
	for _, c := range u.Credentials {
		w := webauthn.Credential{
			ID:        c.CredentialID,
			PublicKey: c.PublicKey,
			Authenticator: webauthn.Authenticator{
				SignCount: c.SignCount,
			},
			AttestationType: c.AttestationType,
		}
		for _, t := range c.Transports {
			w.Transport = append(w.Transport, protocol.AuthenticatorTransport(t))
		}
		if c.AAGUID != nil {
			b, _ := c.AAGUID.MarshalBinary()
			w.Authenticator.AAGUID = b
		}
		out = append(out, w)
	}
	return out
}

// Service is the typed surface handlers consume.
type Service struct {
	wa    *webauthn.WebAuthn
	store Store
	rp    RelyingPartyConfig

	// Challenge TTL — the spec recommends 5 minutes max; the Rust
	// crate uses 5 too. Configurable for tests.
	ChallengeTTL time.Duration
}

// NewService wires a Service.
func NewService(rp RelyingPartyConfig, store Store) (*Service, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPDisplayName: rp.DisplayName,
		RPID:          rp.ID,
		RPOrigins:     rp.Origins,
	})
	if err != nil {
		return nil, fmt.Errorf("webauthn config: %w", err)
	}
	return &Service{wa: wa, store: store, rp: rp, ChallengeTTL: 5 * time.Minute}, nil
}

// HasCredentials proxies the store call.
func (s *Service) HasCredentials(ctx context.Context, userID uuid.UUID) (bool, error) {
	return s.store.HasCredentials(ctx, userID)
}

// BeginRegistration generates the registration ceremony options + a
// server-side challenge record. Returns (options, challenge_id) — the
// client posts the challenge_id back with the attestation in
// FinishRegistration.
func (s *Service) BeginRegistration(ctx context.Context, user *models.User) (*protocol.CredentialCreation, uuid.UUID, error) {
	creds, err := s.store.ListCredentials(ctx, user.ID)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("list credentials: %w", err)
	}
	wuser := &User{
		ID:          user.ID,
		Name:        user.Email,
		DisplayName: user.Name,
		Credentials: creds,
	}
	options, sessionData, err := s.wa.BeginRegistration(wuser)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("begin registration: %w", err)
	}
	chID, err := s.persistSessionData(ctx, user.ID, "register", sessionData)
	if err != nil {
		return nil, uuid.Nil, err
	}
	return options, chID, nil
}

// FinishRegistration validates the attestation and persists the credential.
//
// `parsedResp` is the *protocol.ParsedCredentialCreationData the
// caller built from the client JSON.
func (s *Service) FinishRegistration(ctx context.Context, user *models.User, challengeID uuid.UUID, parsedResp *protocol.ParsedCredentialCreationData) (*Credential, error) {
	rec, err := s.store.LoadChallenge(ctx, challengeID)
	if err != nil {
		return nil, fmt.Errorf("load challenge: %w", err)
	}
	if rec == nil || rec.UserID != user.ID || rec.Kind != "register" {
		return nil, ErrChallengeMismatch
	}
	if time.Now().After(rec.ExpiresAt) {
		return nil, ErrChallengeExpired
	}

	var sessionData webauthn.SessionData
	if err := jsonUnmarshal(rec.SessionData, &sessionData); err != nil {
		return nil, fmt.Errorf("decode session data: %w", err)
	}

	wuser := &User{
		ID:          user.ID,
		Name:        user.Email,
		DisplayName: user.Name,
		Credentials: nil,
	}
	cred, err := s.wa.CreateCredential(wuser, sessionData, parsedResp)
	if err != nil {
		return nil, fmt.Errorf("validate attestation: %w", err)
	}

	transports := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		transports = append(transports, string(t))
	}

	persisted := Credential{
		ID:              uuidNew(),
		UserID:          user.ID,
		CredentialID:    cred.ID,
		PublicKey:       cred.PublicKey,
		SignCount:       cred.Authenticator.SignCount,
		Transports:      transports,
		AttestationType: cred.AttestationType,
		Label:           "",
		CreatedAt:       time.Now().UTC(),
	}
	if err := s.store.InsertCredential(ctx, persisted); err != nil {
		return nil, fmt.Errorf("insert credential: %w", err)
	}
	_ = s.store.DeleteChallenge(ctx, challengeID)
	return &persisted, nil
}

// BeginLogin generates the assertion ceremony options + a challenge id.
func (s *Service) BeginLogin(ctx context.Context, user *models.User) (*protocol.CredentialAssertion, uuid.UUID, error) {
	creds, err := s.store.ListCredentials(ctx, user.ID)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("list credentials: %w", err)
	}
	if len(creds) == 0 {
		return nil, uuid.Nil, ErrNoCredentials
	}
	wuser := &User{
		ID:          user.ID,
		Name:        user.Email,
		DisplayName: user.Name,
		Credentials: creds,
	}
	options, sessionData, err := s.wa.BeginLogin(wuser)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("begin login: %w", err)
	}
	chID, err := s.persistSessionData(ctx, user.ID, "login", sessionData)
	if err != nil {
		return nil, uuid.Nil, err
	}
	return options, chID, nil
}

// FinishLogin validates the assertion + bumps the credential's sign_count.
func (s *Service) FinishLogin(ctx context.Context, user *models.User, challengeID uuid.UUID, parsedResp *protocol.ParsedCredentialAssertionData) (*Credential, error) {
	rec, err := s.store.LoadChallenge(ctx, challengeID)
	if err != nil {
		return nil, fmt.Errorf("load challenge: %w", err)
	}
	if rec == nil || rec.UserID != user.ID || rec.Kind != "login" {
		return nil, ErrChallengeMismatch
	}
	if time.Now().After(rec.ExpiresAt) {
		return nil, ErrChallengeExpired
	}
	var sessionData webauthn.SessionData
	if err := jsonUnmarshal(rec.SessionData, &sessionData); err != nil {
		return nil, fmt.Errorf("decode session data: %w", err)
	}
	creds, err := s.store.ListCredentials(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	wuser := &User{
		ID:          user.ID,
		Name:        user.Email,
		DisplayName: user.Name,
		Credentials: creds,
	}
	cred, err := s.wa.ValidateLogin(wuser, sessionData, parsedResp)
	if err != nil {
		return nil, fmt.Errorf("validate assertion: %w", err)
	}
	now := time.Now().UTC()
	if err := s.store.UpdateSignCount(ctx, cred.ID, cred.Authenticator.SignCount, now); err != nil {
		return nil, fmt.Errorf("update sign count: %w", err)
	}
	_ = s.store.DeleteChallenge(ctx, challengeID)

	// Find the persisted record to return.
	persisted, err := s.store.GetCredentialByCredentialID(ctx, cred.ID)
	if err != nil {
		return nil, err
	}
	return persisted, nil
}

func (s *Service) persistSessionData(ctx context.Context, userID uuid.UUID, kind string, sd *webauthn.SessionData) (uuid.UUID, error) {
	blob, err := jsonMarshal(sd)
	if err != nil {
		return uuid.Nil, fmt.Errorf("encode session data: %w", err)
	}
	id := uuidNew()
	rec := ChallengeRecord{
		ID:          id,
		UserID:      userID,
		Kind:        kind,
		SessionData: blob,
		ExpiresAt:   time.Now().Add(s.ChallengeTTL),
	}
	if err := s.store.StoreChallenge(ctx, rec); err != nil {
		return uuid.Nil, fmt.Errorf("persist challenge: %w", err)
	}
	return id, nil
}

// Sentinel errors.
var (
	ErrChallengeMismatch = errors.New("webauthn challenge does not match user / kind")
	ErrChallengeExpired  = errors.New("webauthn challenge expired")
	ErrNoCredentials     = errors.New("user has no registered webauthn credentials")
)
