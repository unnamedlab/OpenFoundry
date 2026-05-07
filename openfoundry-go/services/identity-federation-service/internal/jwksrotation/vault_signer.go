package jwksrotation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// VaultTransitSigner is the production TransitKeyClient backed by
// HashiCorp Vault Transit. Mirrors hardening/vault_signer.rs::
// VaultTransitSigner verbatim:
//
//   - Pluggable auth: static `VAULT_TOKEN` OR Kubernetes
//     ServiceAccount JWT login (with the role-issued token cached
//     in-process for the rest of the binary's lifetime).
//   - Retry policy: configurable attempts × backoff, only retries
//     on classifiable transient failures (HTTP transport errors,
//     5xx and 429 from Vault).
//   - Wire shape: signs over a SHA-256 digest with prehashed=true,
//     hash_algorithm=sha2-256, signature_algorithm=pkcs1v15.
//   - Key bookkeeping: rotates the named key via
//     POST /transit/keys/<name>/rotate, then re-reads metadata to
//     materialise the new VaultKeyRef.
//
// The pure-logic surface (URL builder, signature decoder, env
// parsing) is exercised by unit tests in vault_signer_test.go.
// HTTP round-trips are validated against an httptest server.
type VaultTransitSigner struct {
	config VaultTransitConfig
	http   *http.Client

	roleTokenMu    sync.RWMutex
	cachedRoleTok  string
}

// Compile-time interface assertion.
var _ TransitKeyClient = (*VaultTransitSigner)(nil)

// ─── Config ──────────────────────────────────────────────────────────

// VaultAuthKind discriminates VaultAuthConfig variants.
type VaultAuthKind uint8

const (
	// VaultAuthToken — static `VAULT_TOKEN` from env. Suited to
	// dev / CI environments.
	VaultAuthToken VaultAuthKind = iota
	// VaultAuthKubernetesRole — service-account JWT login against
	// `auth/<mount>/login`. Production default in the Foundry-Pattern
	// helm chart.
	VaultAuthKubernetesRole
)

// VaultAuthConfig is the auth-flow knob: static token OR Kubernetes
// role login. Mirrors the Rust enum.
type VaultAuthConfig struct {
	Kind    VaultAuthKind
	Token   string // when Kind == VaultAuthToken
	Role    string // when Kind == VaultAuthKubernetesRole
	Mount   string // when Kind == VaultAuthKubernetesRole
	JWTPath string // when Kind == VaultAuthKubernetesRole
}

// VaultRetryPolicy controls how Vault HTTP failures are retried.
// Only transport timeouts/connect errors and 5xx / 429 responses
// are retried — domain errors (4xx) surface immediately.
type VaultRetryPolicy struct {
	Attempts int
	Backoff  time.Duration
}

// DefaultVaultRetryPolicy mirrors the Rust default
// (DEFAULT_RETRY_ATTEMPTS = 3, DEFAULT_RETRY_BACKOFF_MS = 200).
var DefaultVaultRetryPolicy = VaultRetryPolicy{Attempts: 3, Backoff: 200 * time.Millisecond}

// VaultTransitConfig is the boot-time configuration for a signer.
// Mirrors struct VaultTransitConfig.
type VaultTransitConfig struct {
	VaultAddr          string
	Auth               VaultAuthConfig
	Key                VaultKeyRef
	TransitMount       string
	Timeout            time.Duration
	RetryPolicy        VaultRetryPolicy
	HashAlgorithm      string
	SignatureAlgorithm string
}

const (
	defaultK8sJWTPath          = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultHashAlgorithm       = "sha2-256"
	defaultSignatureAlgorithm  = "pkcs1v15"
	defaultVaultTimeout        = 5 * time.Second
	defaultVaultTransitMount   = "transit"
	defaultVaultK8sAuthMount   = "kubernetes"
)

// VaultTransitConfigFromEnv builds a VaultTransitConfig from the
// canonical environment-variable contract. Mirrors fn from_env on
// VaultTransitConfig.
//
// Required:
//
//	VAULT_ADDR
//	VAULT_TRANSIT_KEY  (or VAULT_TRANSIT_KEY_NAME or
//	                    OPENFOUNDRY_JWT_TRANSIT_KEY)
//
// Auth (one of):
//
//	VAULT_TOKEN                                — static token
//	VAULT_K8S_ROLE + VAULT_K8S_AUTH_MOUNT?     — Kubernetes login
//
// Optional:
//
//	VAULT_TRANSIT_KEY_VERSION   (default 1)
//	VAULT_TRANSIT_MOUNT         (default "transit")
//	VAULT_TIMEOUT_MS            (default 5000)
//	VAULT_RETRY_ATTEMPTS        (default 3)
//	VAULT_RETRY_BACKOFF_MS      (default 200)
//	VAULT_TRANSIT_HASH_ALGORITHM       (default sha2-256)
//	VAULT_TRANSIT_SIGNATURE_ALGORITHM  (default pkcs1v15)
//	VAULT_K8S_JWT_PATH          (default /var/run/secrets/...)
func VaultTransitConfigFromEnv() (VaultTransitConfig, error) {
	addr, err := requiredEnv("VAULT_ADDR")
	if err != nil {
		return VaultTransitConfig{}, err
	}

	keyName, ok := firstEnv("VAULT_TRANSIT_KEY", "VAULT_TRANSIT_KEY_NAME", "OPENFOUNDRY_JWT_TRANSIT_KEY")
	if !ok {
		return VaultTransitConfig{}, NewSignError(
			"missing VAULT_TRANSIT_KEY/VAULT_TRANSIT_KEY_NAME/OPENFOUNDRY_JWT_TRANSIT_KEY")
	}
	keyVersion := uint32(1)
	if v, ok := optionalEnvUint32("VAULT_TRANSIT_KEY_VERSION"); ok {
		if v == 0 {
			return VaultTransitConfig{}, NewSignError("VAULT_TRANSIT_KEY_VERSION must be greater than zero")
		}
		keyVersion = v
	}

	auth, err := vaultAuthFromEnv()
	if err != nil {
		return VaultTransitConfig{}, err
	}

	timeout := defaultVaultTimeout
	if v, ok := optionalEnvUint64("VAULT_TIMEOUT_MS"); ok {
		timeout = time.Duration(v) * time.Millisecond
	}
	retry := DefaultVaultRetryPolicy
	if v, ok := optionalEnvUint64("VAULT_RETRY_ATTEMPTS"); ok && v > 0 {
		retry.Attempts = int(v)
	}
	if v, ok := optionalEnvUint64("VAULT_RETRY_BACKOFF_MS"); ok {
		retry.Backoff = time.Duration(v) * time.Millisecond
	}

	hash := nonEmptyEnv("VAULT_TRANSIT_HASH_ALGORITHM")
	if hash == "" {
		hash = defaultHashAlgorithm
	}
	sigAlg := nonEmptyEnv("VAULT_TRANSIT_SIGNATURE_ALGORITHM")
	if sigAlg == "" {
		sigAlg = defaultSignatureAlgorithm
	}
	transitMount := nonEmptyEnv("VAULT_TRANSIT_MOUNT")
	if transitMount == "" {
		transitMount = defaultVaultTransitMount
	}

	return VaultTransitConfig{
		VaultAddr:          addr,
		Auth:               auth,
		Key:                VaultKeyRef{Name: keyName, Version: keyVersion},
		TransitMount:       transitMount,
		Timeout:            timeout,
		RetryPolicy:        retry,
		HashAlgorithm:      hash,
		SignatureAlgorithm: sigAlg,
	}, nil
}

func vaultAuthFromEnv() (VaultAuthConfig, error) {
	if token := nonEmptyEnv("VAULT_TOKEN"); token != "" {
		return VaultAuthConfig{Kind: VaultAuthToken, Token: token}, nil
	}
	role := nonEmptyEnv("VAULT_K8S_ROLE")
	if role == "" {
		return VaultAuthConfig{}, NewSignError("missing VAULT_TOKEN or VAULT_K8S_ROLE")
	}
	mount := nonEmptyEnv("VAULT_K8S_AUTH_MOUNT")
	if mount == "" {
		mount = defaultVaultK8sAuthMount
	}
	jwtPath := nonEmptyEnv("VAULT_K8S_JWT_PATH")
	if jwtPath == "" {
		jwtPath = defaultK8sJWTPath
	}
	return VaultAuthConfig{
		Kind:    VaultAuthKubernetesRole,
		Role:    role,
		Mount:   mount,
		JWTPath: jwtPath,
	}, nil
}

// ─── Signer construction ─────────────────────────────────────────────

// NewVaultTransitSigner wraps a config + HTTP client. Returns an
// error if the timeout produces an invalid client config.
func NewVaultTransitSigner(config VaultTransitConfig) (*VaultTransitSigner, error) {
	if config.Timeout <= 0 {
		return nil, NewSignError("vault timeout must be > 0")
	}
	return &VaultTransitSigner{
		config: config,
		http:   &http.Client{Timeout: config.Timeout},
	}, nil
}

// NewVaultTransitSignerFromEnv is the env-driven constructor.
// Returns SignError when any required env var is missing.
func NewVaultTransitSignerFromEnv() (*VaultTransitSigner, error) {
	cfg, err := VaultTransitConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return NewVaultTransitSigner(cfg)
}

// Config returns the underlying transit config.
func (s *VaultTransitSigner) Config() VaultTransitConfig { return s.config }

// ─── TransitKeyClient impl ───────────────────────────────────────────

// ConfiguredKeyRef returns the boot-time configured key reference
// (name + version 1 by default; mirrors fn configured_key_ref).
func (s *VaultTransitSigner) ConfiguredKeyRef() VaultKeyRef { return s.config.Key }

// SignWithKey signs `digest` with the given Vault key version.
// Wraps the per-attempt sign call with the retry policy.
func (s *VaultTransitSigner) SignWithKey(ctx context.Context, key VaultKeyRef, digest []byte) ([]byte, error) {
	attempts := s.config.RetryPolicy.Attempts
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		sig, err := s.signOnce(ctx, key, digest)
		if err == nil {
			return sig, nil
		}
		if attempt < attempts && retryable(err) {
			slog.Warn("vault transit signing failed; retrying",
				slog.Int("attempt", attempt),
				slog.Int("attempts", attempts),
				slog.String("error", err.Error()))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(s.config.RetryPolicy.Backoff * time.Duration(attempt)):
			}
			continue
		}
		return nil, err
	}
	return nil, NewSignError("vault transit retry loop exited unexpectedly")
}

// LatestKeyRef returns the most-recently-rotated key version for
// the configured key name.
func (s *VaultTransitSigner) LatestKeyRef(ctx context.Context) (VaultKeyRef, error) {
	meta, err := s.keyMetadata(ctx)
	if err != nil {
		return VaultKeyRef{}, err
	}
	return VaultKeyRef{Name: s.config.Key.Name, Version: meta.Data.LatestVersion}, nil
}

// RotateKey asks Vault to bump the named key's version, then
// re-reads metadata to materialise the new VaultKeyRef.
func (s *VaultTransitSigner) RotateKey(ctx context.Context) (VaultKeyRef, error) {
	token, err := s.vaultToken(ctx)
	if err != nil {
		return VaultKeyRef{}, err
	}
	endpoint, err := s.url(s.config.TransitMount, "keys", s.config.Key.Name, "rotate")
	if err != nil {
		return VaultKeyRef{}, err
	}
	body, status, err := s.do(ctx, http.MethodPost, endpoint, token, nil, "transit_rotate")
	if err != nil {
		return VaultKeyRef{}, err
	}
	if !isSuccess(status) {
		return VaultKeyRef{}, &SignError{
			Reason: fmt.Sprintf("vault returned %d during transit_rotate: %s", status, string(body)),
		}
	}
	return s.LatestKeyRef(ctx)
}

// PublicKeyPEM returns the PEM-encoded public half of the given
// (name, version) Vault key.
func (s *VaultTransitSigner) PublicKeyPEM(ctx context.Context, key VaultKeyRef) (string, error) {
	meta, err := s.keyMetadata(ctx)
	if err != nil {
		return "", err
	}
	versionStr := strconv.FormatUint(uint64(key.Version), 10)
	entry, ok := meta.Data.Keys[versionStr]
	if !ok || entry.PublicKey == "" {
		return "", NewSignError("vault response missing field: data.keys[version].public_key")
	}
	return entry.PublicKey, nil
}

// ─── Internals: HTTP plumbing ────────────────────────────────────────

func (s *VaultTransitSigner) signOnce(ctx context.Context, key VaultKeyRef, digest []byte) ([]byte, error) {
	token, err := s.vaultToken(ctx)
	if err != nil {
		return nil, err
	}
	endpoint, err := s.url(s.config.TransitMount, "sign", key.Name)
	if err != nil {
		return nil, err
	}
	req := vaultSignRequest{
		Input:              base64.StdEncoding.EncodeToString(digest),
		Prehashed:          true,
		HashAlgorithm:      s.config.HashAlgorithm,
		SignatureAlgorithm: s.config.SignatureAlgorithm,
		KeyVersion:         key.Version,
	}
	body, status, err := s.do(ctx, http.MethodPost, endpoint, token, req, "transit_sign")
	if err != nil {
		return nil, err
	}
	if !isSuccess(status) {
		return nil, &SignError{
			Reason: fmt.Sprintf("vault returned %d during transit_sign: %s", status, string(body)),
		}
	}
	var resp vaultSignResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, NewSignError("vault response missing field: data.signature")
	}
	return decodeVaultSignature(resp.Data.Signature)
}

func (s *VaultTransitSigner) keyMetadata(ctx context.Context) (vaultKeyMetadataResponse, error) {
	token, err := s.vaultToken(ctx)
	if err != nil {
		return vaultKeyMetadataResponse{}, err
	}
	endpoint, err := s.url(s.config.TransitMount, "keys", s.config.Key.Name)
	if err != nil {
		return vaultKeyMetadataResponse{}, err
	}
	body, status, err := s.do(ctx, http.MethodGet, endpoint, token, nil, "transit_key_metadata")
	if err != nil {
		return vaultKeyMetadataResponse{}, err
	}
	if !isSuccess(status) {
		return vaultKeyMetadataResponse{}, &SignError{
			Reason: fmt.Sprintf("vault returned %d during transit_key_metadata: %s", status, string(body)),
		}
	}
	var resp vaultKeyMetadataResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return vaultKeyMetadataResponse{}, NewSignError("vault response missing field: data.keys")
	}
	return resp, nil
}

// vaultToken returns a token suitable for the X-Vault-Token header.
// For static-token auth the value is the configured token. For
// Kubernetes-role auth the SA-JWT login flow runs once per process
// and the resulting token is cached for the rest of the binary's
// life.
func (s *VaultTransitSigner) vaultToken(ctx context.Context) (string, error) {
	switch s.config.Auth.Kind {
	case VaultAuthToken:
		return s.config.Auth.Token, nil
	case VaultAuthKubernetesRole:
		s.roleTokenMu.RLock()
		cached := s.cachedRoleTok
		s.roleTokenMu.RUnlock()
		if cached != "" {
			return cached, nil
		}
		jwtBytes, err := os.ReadFile(s.config.Auth.JWTPath)
		if err != nil {
			return "", &SignError{Reason: "vault auth failed: " + err.Error(), Wrapped: err}
		}
		jwt := strings.TrimSpace(string(jwtBytes))
		endpoint, err := s.url("auth", s.config.Auth.Mount, "login")
		if err != nil {
			return "", err
		}
		body, status, err := s.do(ctx, http.MethodPost, endpoint, "",
			vaultK8sLoginRequest{Role: s.config.Auth.Role, JWT: jwt},
			"kubernetes_login")
		if err != nil {
			return "", err
		}
		if !isSuccess(status) {
			return "", &SignError{
				Reason: fmt.Sprintf("vault returned %d during kubernetes_login: %s", status, string(body)),
			}
		}
		var resp vaultK8sLoginResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return "", NewSignError("vault response missing field: auth.client_token")
		}
		if resp.Auth.ClientToken == "" {
			return "", NewSignError("vault response missing field: auth.client_token")
		}
		s.roleTokenMu.Lock()
		s.cachedRoleTok = resp.Auth.ClientToken
		s.roleTokenMu.Unlock()
		return resp.Auth.ClientToken, nil
	}
	return "", NewSignError("unsupported VaultAuthConfig kind")
}

// do is the HTTP shim every Vault call routes through. Encodes the
// optional payload as JSON, attaches the X-Vault-Token header when
// non-empty, and returns (body bytes, status, err).
//
// `context` is the operation label baked into transport-error
// messages (mirrors Rust SignError::Http {context}).
func (s *VaultTransitSigner) do(
	ctx context.Context,
	method string,
	endpoint *url.URL,
	token string,
	payload any,
	opContext string,
) ([]byte, int, error) {
	var bodyReader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, &SignError{
				Reason:  fmt.Sprintf("vault http error during %s: %v", opContext, err),
				Wrapped: err,
			}
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bodyReader)
	if err != nil {
		return nil, 0, &SignError{
			Reason:  fmt.Sprintf("vault http error during %s: %v", opContext, err),
			Wrapped: err,
		}
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, 0, &SignError{
			Reason:  fmt.Sprintf("vault http error during %s: %v", opContext, err),
			Wrapped: err,
		}
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, &SignError{
			Reason:  fmt.Sprintf("vault http error during read_%s: %v", opContext, err),
			Wrapped: err,
		}
	}
	return out, resp.StatusCode, nil
}

// url joins VAULT_ADDR + /v1/ + the variadic path segments. Mirrors
// fn url. Empty / slash-only segments are ignored so callers can
// pass mount values that already carry leading or trailing slashes.
func (s *VaultTransitSigner) url(segments ...string) (*url.URL, error) {
	base, err := url.Parse(s.config.VaultAddr)
	if err != nil {
		return nil, NewSignError("vault transit configuration error: invalid VAULT_ADDR: " + err.Error())
	}
	pathParts := []string{"v1"}
	for _, segment := range segments {
		for _, part := range strings.Split(strings.Trim(segment, "/"), "/") {
			if part != "" {
				pathParts = append(pathParts, part)
			}
		}
	}
	// Preserve any existing path on VAULT_ADDR (e.g. via reverse proxy).
	existing := strings.Trim(base.Path, "/")
	if existing != "" {
		pathParts = append([]string{existing}, pathParts...)
	}
	base.Path = "/" + strings.Join(pathParts, "/")
	return base, nil
}

// ─── Wire payloads ───────────────────────────────────────────────────

type vaultSignRequest struct {
	Input              string `json:"input"`
	Prehashed          bool   `json:"prehashed"`
	HashAlgorithm      string `json:"hash_algorithm"`
	SignatureAlgorithm string `json:"signature_algorithm"`
	KeyVersion         uint32 `json:"key_version"`
}

type vaultSignResponse struct {
	Data vaultSignData `json:"data"`
}

type vaultSignData struct {
	Signature string `json:"signature"`
}

type vaultK8sLoginRequest struct {
	Role string `json:"role"`
	JWT  string `json:"jwt"`
}

type vaultK8sLoginResponse struct {
	Auth vaultK8sLoginAuth `json:"auth"`
}

type vaultK8sLoginAuth struct {
	ClientToken string `json:"client_token"`
}

type vaultKeyMetadataResponse struct {
	Data vaultKeyMetadata `json:"data"`
}

type vaultKeyMetadata struct {
	LatestVersion uint32                          `json:"latest_version"`
	Keys          map[string]vaultTransitKeyVersion `json:"keys"`
}

type vaultTransitKeyVersion struct {
	PublicKey string `json:"public_key"`
}

// ─── Pure helpers ────────────────────────────────────────────────────

// decodeVaultSignature accepts the Vault transit signature wire form
// (`vault:vN:<base64>` or `vault:vN:<base64url-no-pad>`) and returns
// the raw signature bytes. Mirrors fn decode_vault_signature.
func decodeVaultSignature(sig string) ([]byte, error) {
	idx := strings.LastIndex(sig, ":")
	if idx < 0 {
		return nil, &SignError{Reason: "vault transit signature has invalid format: " + sig}
	}
	encoded := sig[idx+1:]
	if raw, err := base64.StdEncoding.DecodeString(encoded); err == nil {
		return raw, nil
	}
	if raw, err := base64.RawURLEncoding.DecodeString(encoded); err == nil {
		return raw, nil
	}
	return nil, &SignError{Reason: "vault transit signature has invalid format: " + encoded}
}

// retryable mirrors SignError::retryable — only transport-level
// timeouts/connect errors and 5xx / 429 from Vault are retried.
func retryable(err error) bool {
	var se *SignError
	if !errors.As(err, &se) {
		return false
	}
	// Wrapped transport error → retryable. We can't easily tell
	// timeout-vs-connect apart in net/http without sniffing the
	// concrete error type, so the lib retries all transport-class
	// failures.
	if se.Wrapped != nil {
		return true
	}
	// "vault returned <N> during" — sniff for 429 / 5xx from the
	// reason string.
	for _, marker := range []string{"vault returned 429", "vault returned 5"} {
		if strings.Contains(se.Reason, marker) {
			return true
		}
	}
	return false
}

// isSuccess matches the canonical "2xx" gate.
func isSuccess(status int) bool { return status >= 200 && status < 300 }

// requiredEnv pulls a required env var, surfacing a Config-typed
// SignError when absent. Mirrors fn required_env.
func requiredEnv(key string) (string, error) {
	v := nonEmptyEnv(key)
	if v == "" {
		return "", NewSignError("missing " + key)
	}
	return v, nil
}

// firstEnv returns the first non-empty env var from the list.
// Mirrors fn first_env.
func firstEnv(keys ...string) (string, bool) {
	for _, k := range keys {
		if v := nonEmptyEnv(k); v != "" {
			return v, true
		}
	}
	return "", false
}

// nonEmptyEnv reads + trims env[key], returning "" when absent or
// blank. Mirrors fn non_empty_env.
func nonEmptyEnv(key string) string { return strings.TrimSpace(os.Getenv(key)) }

// optionalEnvUint32 parses a uint32 env var; returns (0, false)
// when absent. Mirrors fn optional_env_u32.
func optionalEnvUint32(key string) (uint32, bool) {
	v := nonEmptyEnv(key)
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}

// optionalEnvUint64 parses a uint64 env var; returns (0, false)
// when absent. Mirrors fn optional_env_u64.
func optionalEnvUint64(key string) (uint64, bool) {
	v := nonEmptyEnv(key)
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
