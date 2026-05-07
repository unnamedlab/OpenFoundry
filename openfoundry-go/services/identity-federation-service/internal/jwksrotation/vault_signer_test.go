package jwksrotation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Compile-time interface assertion ----------------------------------

func TestVaultTransitSignerSatisfiesInterface(t *testing.T) {
	t.Parallel()
	var _ TransitKeyClient = (*VaultTransitSigner)(nil)
}

// --- decodeVaultSignature -----------------------------------------------

func TestDecodeVaultSignatureStandardBase64(t *testing.T) {
	t.Parallel()
	raw := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	encoded := base64.StdEncoding.EncodeToString(raw)
	wire := "vault:v1:" + encoded
	got, err := decodeVaultSignature(wire)
	require.NoError(t, err)
	assert.Equal(t, raw, got)
}

func TestDecodeVaultSignatureURLSafeBase64NoPad(t *testing.T) {
	t.Parallel()
	raw := []byte{0xff, 0xee, 0xdd}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	wire := "vault:v2:" + encoded
	got, err := decodeVaultSignature(wire)
	require.NoError(t, err)
	assert.Equal(t, raw, got)
}

func TestDecodeVaultSignatureRejectsMissingColon(t *testing.T) {
	t.Parallel()
	_, err := decodeVaultSignature("not-a-vault-signature")
	require.Error(t, err)
	var se *SignError
	require.True(t, errors.As(err, &se))
	assert.Contains(t, err.Error(), "invalid format")
}

func TestDecodeVaultSignatureRejectsMalformedBase64(t *testing.T) {
	t.Parallel()
	_, err := decodeVaultSignature("vault:v1:not!valid!base64")
	require.Error(t, err)
}

// --- url builder -------------------------------------------------------

func TestVaultURLJoinsV1AndSegments(t *testing.T) {
	t.Parallel()
	s, err := NewVaultTransitSigner(VaultTransitConfig{
		VaultAddr: "https://vault.example",
		Auth:      VaultAuthConfig{Kind: VaultAuthToken, Token: "x"},
		Key:       VaultKeyRef{Name: "openfoundry-jwt", Version: 1},
		Timeout:   time.Second,
	})
	require.NoError(t, err)
	u, err := s.url("transit", "keys", "openfoundry-jwt", "rotate")
	require.NoError(t, err)
	assert.Equal(t, "/v1/transit/keys/openfoundry-jwt/rotate", u.Path)
	assert.Equal(t, "vault.example", u.Host)
}

func TestVaultURLPreservesExistingBasePath(t *testing.T) {
	t.Parallel()
	s, err := NewVaultTransitSigner(VaultTransitConfig{
		VaultAddr: "https://gateway.example/proxy/",
		Auth:      VaultAuthConfig{Kind: VaultAuthToken, Token: "x"},
		Key:       VaultKeyRef{Name: "k", Version: 1},
		Timeout:   time.Second,
	})
	require.NoError(t, err)
	u, err := s.url("transit", "sign", "k")
	require.NoError(t, err)
	// /proxy + /v1/transit/sign/k
	assert.Equal(t, "/proxy/v1/transit/sign/k", u.Path)
}

func TestVaultURLIgnoresEmptySegments(t *testing.T) {
	t.Parallel()
	s, err := NewVaultTransitSigner(VaultTransitConfig{
		VaultAddr: "https://vault.example",
		Auth:      VaultAuthConfig{Kind: VaultAuthToken, Token: "x"},
		Key:       VaultKeyRef{Name: "k", Version: 1},
		Timeout:   time.Second,
	})
	require.NoError(t, err)
	u, err := s.url("/transit/", "keys", "/k/", "rotate")
	require.NoError(t, err)
	assert.Equal(t, "/v1/transit/keys/k/rotate", u.Path)
}

// --- retryable classifier ----------------------------------------------

func TestRetryableTransportError(t *testing.T) {
	t.Parallel()
	wrapped := errors.New("connection reset")
	se := &SignError{Reason: "vault http error during transit_sign", Wrapped: wrapped}
	assert.True(t, retryable(se))
}

func TestRetryableUpstream5xx(t *testing.T) {
	t.Parallel()
	se := &SignError{Reason: "vault returned 503 during transit_sign: backend down"}
	assert.True(t, retryable(se))
}

func TestRetryableUpstream429(t *testing.T) {
	t.Parallel()
	se := &SignError{Reason: "vault returned 429 during transit_sign: rate limit"}
	assert.True(t, retryable(se))
}

func TestRetryable4xxNotRetried(t *testing.T) {
	t.Parallel()
	for _, msg := range []string{
		"vault returned 400 during transit_sign: bad request",
		"vault returned 403 during transit_sign: forbidden",
		"vault returned 404 during transit_sign: key not found",
	} {
		se := &SignError{Reason: msg}
		assert.False(t, retryable(se), "%s should NOT be retried", msg)
	}
}

func TestRetryableNonSignErrorReturnsFalse(t *testing.T) {
	t.Parallel()
	assert.False(t, retryable(errors.New("plain")))
}

// --- env-driven config -------------------------------------------------

func TestVaultTransitConfigFromEnvFullToken(t *testing.T) {
	resetVaultEnv(t)
	t.Setenv("VAULT_ADDR", "https://vault.example")
	t.Setenv("VAULT_TRANSIT_KEY", "openfoundry-jwt")
	t.Setenv("VAULT_TOKEN", "s.dev-token")
	t.Setenv("VAULT_TRANSIT_KEY_VERSION", "3")
	t.Setenv("VAULT_TRANSIT_MOUNT", "transit-prod")
	t.Setenv("VAULT_TIMEOUT_MS", "10000")
	t.Setenv("VAULT_RETRY_ATTEMPTS", "5")
	t.Setenv("VAULT_RETRY_BACKOFF_MS", "500")

	cfg, err := VaultTransitConfigFromEnv()
	require.NoError(t, err)
	assert.Equal(t, "https://vault.example", cfg.VaultAddr)
	assert.Equal(t, VaultAuthToken, cfg.Auth.Kind)
	assert.Equal(t, "s.dev-token", cfg.Auth.Token)
	assert.Equal(t, "openfoundry-jwt", cfg.Key.Name)
	assert.Equal(t, uint32(3), cfg.Key.Version)
	assert.Equal(t, "transit-prod", cfg.TransitMount)
	assert.Equal(t, 10*time.Second, cfg.Timeout)
	assert.Equal(t, 5, cfg.RetryPolicy.Attempts)
	assert.Equal(t, 500*time.Millisecond, cfg.RetryPolicy.Backoff)
}

func TestVaultTransitConfigFromEnvKubernetesAuthDefaults(t *testing.T) {
	resetVaultEnv(t)
	t.Setenv("VAULT_ADDR", "https://vault.example")
	t.Setenv("VAULT_TRANSIT_KEY", "k")
	t.Setenv("VAULT_K8S_ROLE", "of-identity")

	cfg, err := VaultTransitConfigFromEnv()
	require.NoError(t, err)
	assert.Equal(t, VaultAuthKubernetesRole, cfg.Auth.Kind)
	assert.Equal(t, "of-identity", cfg.Auth.Role)
	assert.Equal(t, "kubernetes", cfg.Auth.Mount)
	assert.Equal(t, "/var/run/secrets/kubernetes.io/serviceaccount/token", cfg.Auth.JWTPath)
}

func TestVaultTransitConfigFromEnvAcceptsAlternateKeyNameVars(t *testing.T) {
	resetVaultEnv(t)
	t.Setenv("VAULT_ADDR", "https://vault.example")
	t.Setenv("VAULT_TOKEN", "x")
	// VAULT_TRANSIT_KEY is unset; VAULT_TRANSIT_KEY_NAME provides the value.
	t.Setenv("VAULT_TRANSIT_KEY_NAME", "alt-key")
	cfg, err := VaultTransitConfigFromEnv()
	require.NoError(t, err)
	assert.Equal(t, "alt-key", cfg.Key.Name)
}

func TestVaultTransitConfigFromEnvRequiresVaultAddr(t *testing.T) {
	resetVaultEnv(t)
	_, err := VaultTransitConfigFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing VAULT_ADDR")
}

func TestVaultTransitConfigFromEnvRequiresKeyName(t *testing.T) {
	resetVaultEnv(t)
	t.Setenv("VAULT_ADDR", "https://vault.example")
	t.Setenv("VAULT_TOKEN", "x")
	_, err := VaultTransitConfigFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OPENFOUNDRY_JWT_TRANSIT_KEY")
}

func TestVaultTransitConfigFromEnvRejectsZeroVersion(t *testing.T) {
	resetVaultEnv(t)
	t.Setenv("VAULT_ADDR", "https://vault.example")
	t.Setenv("VAULT_TOKEN", "x")
	t.Setenv("VAULT_TRANSIT_KEY", "k")
	t.Setenv("VAULT_TRANSIT_KEY_VERSION", "0")
	_, err := VaultTransitConfigFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be greater than zero")
}

func resetVaultEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"VAULT_ADDR", "VAULT_TOKEN",
		"VAULT_TRANSIT_KEY", "VAULT_TRANSIT_KEY_NAME", "OPENFOUNDRY_JWT_TRANSIT_KEY",
		"VAULT_TRANSIT_KEY_VERSION", "VAULT_TRANSIT_MOUNT",
		"VAULT_TIMEOUT_MS", "VAULT_RETRY_ATTEMPTS", "VAULT_RETRY_BACKOFF_MS",
		"VAULT_TRANSIT_HASH_ALGORITHM", "VAULT_TRANSIT_SIGNATURE_ALGORITHM",
		"VAULT_K8S_ROLE", "VAULT_K8S_AUTH_MOUNT", "VAULT_K8S_JWT_PATH",
	} {
		t.Setenv(k, "")
	}
}

// --- HTTP round-trip via httptest --------------------------------------

// fakeVault is a tiny in-memory Vault transit server for tests. It
// supports the four endpoints the signer talks to and tracks every
// hit so tests can assert call sequencing.
type fakeVault struct {
	t          *testing.T
	keyName    string
	publicPEM  map[uint32]string // version → PEM
	latestVer  uint32
	signatures map[uint32][]byte // version → raw signature bytes
	mu         struct{}
	hits       []string

	// Per-endpoint behaviour overrides.
	signStatus     int
	signFailFirst  int
	rotateStatus   int
	rotateFailures int
}

func newFakeVault(t *testing.T) *fakeVault {
	return &fakeVault{
		t:          t,
		keyName:    "openfoundry-jwt",
		publicPEM:  map[uint32]string{1: "PEM(v1)", 2: "PEM(v2)"},
		latestVer:  1,
		signatures: map[uint32][]byte{1: []byte{0xaa, 0xbb}, 2: []byte{0xcc, 0xdd}},
	}
}

func (f *fakeVault) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits = append(f.hits, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sign/"+f.keyName):
			if f.signFailFirst > 0 {
				f.signFailFirst--
				w.WriteHeader(503)
				_, _ = io.WriteString(w, `{"errors":["temporary"]}`)
				return
			}
			body, _ := io.ReadAll(r.Body)
			var req vaultSignRequest
			_ = json.Unmarshal(body, &req)
			sig := f.signatures[req.KeyVersion]
			encoded := base64.StdEncoding.EncodeToString(sig)
			_, _ = io.WriteString(w, `{"data":{"signature":"vault:v`+
				stringify(req.KeyVersion)+`:`+encoded+`"}}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/keys/"+f.keyName+"/rotate"):
			if f.rotateStatus != 0 {
				w.WriteHeader(f.rotateStatus)
				return
			}
			f.latestVer++
			w.WriteHeader(204) // no body
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/keys/"+f.keyName):
			payload := map[string]any{
				"data": map[string]any{
					"latest_version": f.latestVer,
					"keys": func() map[string]any {
						out := map[string]any{}
						for v, pem := range f.publicPEM {
							out[stringify(v)] = map[string]any{"public_key": pem}
						}
						return out
					}(),
				},
			}
			_ = json.NewEncoder(w).Encode(payload)
		default:
			w.WriteHeader(404)
		}
	})
}

func stringify(v uint32) string { return strconv.FormatUint(uint64(v), 10) }

func TestVaultTransitSignerSignWithKeyHappyPath(t *testing.T) {
	t.Parallel()
	fv := newFakeVault(t)
	srv := httptest.NewServer(fv.handler())
	defer srv.Close()

	s, err := NewVaultTransitSigner(VaultTransitConfig{
		VaultAddr:          srv.URL,
		Auth:               VaultAuthConfig{Kind: VaultAuthToken, Token: "s.dev"},
		Key:                VaultKeyRef{Name: "openfoundry-jwt", Version: 1},
		TransitMount:       "transit",
		Timeout:            2 * time.Second,
		RetryPolicy:        VaultRetryPolicy{Attempts: 1, Backoff: 10 * time.Millisecond},
		HashAlgorithm:      "sha2-256",
		SignatureAlgorithm: "pkcs1v15",
	})
	require.NoError(t, err)

	sig, err := s.SignWithKey(context.Background(), VaultKeyRef{Name: "openfoundry-jwt", Version: 1}, []byte("digest"))
	require.NoError(t, err)
	assert.Equal(t, []byte{0xaa, 0xbb}, sig)
}

func TestVaultTransitSignerSignWithKeyRetriesOn5xx(t *testing.T) {
	t.Parallel()
	fv := newFakeVault(t)
	fv.signFailFirst = 2 // first two attempts return 503
	srv := httptest.NewServer(fv.handler())
	defer srv.Close()

	s, err := NewVaultTransitSigner(VaultTransitConfig{
		VaultAddr:          srv.URL,
		Auth:               VaultAuthConfig{Kind: VaultAuthToken, Token: "x"},
		Key:                VaultKeyRef{Name: "openfoundry-jwt", Version: 1},
		TransitMount:       "transit",
		Timeout:            2 * time.Second,
		RetryPolicy:        VaultRetryPolicy{Attempts: 3, Backoff: 1 * time.Millisecond},
		HashAlgorithm:      "sha2-256",
		SignatureAlgorithm: "pkcs1v15",
	})
	require.NoError(t, err)

	sig, err := s.SignWithKey(context.Background(), VaultKeyRef{Name: "openfoundry-jwt", Version: 1}, []byte("d"))
	require.NoError(t, err)
	assert.Equal(t, []byte{0xaa, 0xbb}, sig)
}

func TestVaultTransitSignerLatestKeyRefReadsMetadata(t *testing.T) {
	t.Parallel()
	fv := newFakeVault(t)
	fv.latestVer = 7
	srv := httptest.NewServer(fv.handler())
	defer srv.Close()

	s, err := NewVaultTransitSigner(VaultTransitConfig{
		VaultAddr:    srv.URL,
		Auth:         VaultAuthConfig{Kind: VaultAuthToken, Token: "x"},
		Key:          VaultKeyRef{Name: "openfoundry-jwt", Version: 1},
		TransitMount: "transit",
		Timeout:      2 * time.Second,
		RetryPolicy:  DefaultVaultRetryPolicy,
	})
	require.NoError(t, err)
	got, err := s.LatestKeyRef(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "openfoundry-jwt", got.Name)
	assert.Equal(t, uint32(7), got.Version)
}

func TestVaultTransitSignerRotateKeyBumpsVersion(t *testing.T) {
	t.Parallel()
	fv := newFakeVault(t)
	srv := httptest.NewServer(fv.handler())
	defer srv.Close()

	s, err := NewVaultTransitSigner(VaultTransitConfig{
		VaultAddr:    srv.URL,
		Auth:         VaultAuthConfig{Kind: VaultAuthToken, Token: "x"},
		Key:          VaultKeyRef{Name: "openfoundry-jwt", Version: 1},
		TransitMount: "transit",
		Timeout:      2 * time.Second,
		RetryPolicy:  DefaultVaultRetryPolicy,
	})
	require.NoError(t, err)
	got, err := s.RotateKey(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint32(2), got.Version)
}

func TestVaultTransitSignerPublicKeyPEM(t *testing.T) {
	t.Parallel()
	fv := newFakeVault(t)
	srv := httptest.NewServer(fv.handler())
	defer srv.Close()

	s, err := NewVaultTransitSigner(VaultTransitConfig{
		VaultAddr:    srv.URL,
		Auth:         VaultAuthConfig{Kind: VaultAuthToken, Token: "x"},
		Key:          VaultKeyRef{Name: "openfoundry-jwt", Version: 1},
		TransitMount: "transit",
		Timeout:      2 * time.Second,
		RetryPolicy:  DefaultVaultRetryPolicy,
	})
	require.NoError(t, err)
	pem, err := s.PublicKeyPEM(context.Background(), VaultKeyRef{Name: "openfoundry-jwt", Version: 2})
	require.NoError(t, err)
	assert.Equal(t, "PEM(v2)", pem)
}

func TestVaultTransitSignerPublicKeyPEMMissingVersion(t *testing.T) {
	t.Parallel()
	fv := newFakeVault(t)
	srv := httptest.NewServer(fv.handler())
	defer srv.Close()

	s, err := NewVaultTransitSigner(VaultTransitConfig{
		VaultAddr:    srv.URL,
		Auth:         VaultAuthConfig{Kind: VaultAuthToken, Token: "x"},
		Key:          VaultKeyRef{Name: "openfoundry-jwt", Version: 1},
		TransitMount: "transit",
		Timeout:      2 * time.Second,
		RetryPolicy:  DefaultVaultRetryPolicy,
	})
	require.NoError(t, err)
	_, err = s.PublicKeyPEM(context.Background(), VaultKeyRef{Name: "openfoundry-jwt", Version: 9})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "data.keys[version].public_key")
}

// --- Kubernetes-role auth flow -----------------------------------------

func TestVaultTransitSignerKubernetesRoleLoginThenSign(t *testing.T) {
	t.Parallel()
	// Drop a fake SA-JWT to disk.
	dir := t.TempDir()
	jwtPath := dir + "/sa-jwt"
	require.NoError(t, os.WriteFile(jwtPath, []byte("eyJhbGciOiJSUzI1NiJ9.fake.jwt\n"), 0600))

	fv := newFakeVault(t)
	loginCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/kubernetes/login", func(w http.ResponseWriter, r *http.Request) {
		loginCalls++
		body, _ := io.ReadAll(r.Body)
		var req vaultK8sLoginRequest
		_ = json.Unmarshal(body, &req)
		if req.Role != "of-identity" || req.JWT != "eyJhbGciOiJSUzI1NiJ9.fake.jwt" {
			w.WriteHeader(401)
			return
		}
		_, _ = io.WriteString(w, `{"auth":{"client_token":"s.k8s-issued-token"}}`)
	})
	mux.Handle("/", fv.handler())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s, err := NewVaultTransitSigner(VaultTransitConfig{
		VaultAddr:          srv.URL,
		Auth:               VaultAuthConfig{Kind: VaultAuthKubernetesRole, Role: "of-identity", Mount: "kubernetes", JWTPath: jwtPath},
		Key:                VaultKeyRef{Name: "openfoundry-jwt", Version: 1},
		TransitMount:       "transit",
		Timeout:            2 * time.Second,
		RetryPolicy:        DefaultVaultRetryPolicy,
		HashAlgorithm:      "sha2-256",
		SignatureAlgorithm: "pkcs1v15",
	})
	require.NoError(t, err)

	// Two consecutive signs should hit /login exactly once (token cached).
	_, err = s.SignWithKey(context.Background(), VaultKeyRef{Name: "openfoundry-jwt", Version: 1}, []byte("d1"))
	require.NoError(t, err)
	_, err = s.SignWithKey(context.Background(), VaultKeyRef{Name: "openfoundry-jwt", Version: 1}, []byte("d2"))
	require.NoError(t, err)
	assert.Equal(t, 1, loginCalls, "kubernetes login must be cached after first call")
}

func TestVaultTransitSignerNonRetryable4xxFailsImmediately(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = io.WriteString(w, `{"errors":["forbidden"]}`)
	}))
	defer srv.Close()

	s, err := NewVaultTransitSigner(VaultTransitConfig{
		VaultAddr:    srv.URL,
		Auth:         VaultAuthConfig{Kind: VaultAuthToken, Token: "x"},
		Key:          VaultKeyRef{Name: "openfoundry-jwt", Version: 1},
		TransitMount: "transit",
		Timeout:      2 * time.Second,
		RetryPolicy:  VaultRetryPolicy{Attempts: 5, Backoff: 1 * time.Millisecond},
	})
	require.NoError(t, err)
	_, err = s.SignWithKey(context.Background(), VaultKeyRef{Name: "openfoundry-jwt", Version: 1}, []byte("d"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault returned 403")
}

// --- Construction guards ----------------------------------------------

func TestNewVaultTransitSignerRejectsZeroTimeout(t *testing.T) {
	t.Parallel()
	_, err := NewVaultTransitSigner(VaultTransitConfig{
		VaultAddr: "https://vault.example",
		Auth:      VaultAuthConfig{Kind: VaultAuthToken, Token: "x"},
		Key:       VaultKeyRef{Name: "k", Version: 1},
		Timeout:   0,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault timeout must be > 0")
}
