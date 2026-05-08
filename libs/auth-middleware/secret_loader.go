package authmw

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Env var precedence chains, mirroring libs/auth-middleware/src/jwt.rs.
var (
	jwtSecretEnvKeys         = []string{"OPENFOUNDRY_JWT_SECRET", "JWT_SECRET"}
	jwtSecretPathEnvKeys     = []string{"OPENFOUNDRY_JWT_SECRET_PATH", "JWT_SECRET_PATH"}
	jwtIssuerEnvKeys         = []string{"OPENFOUNDRY_JWT_ISSUER", "JWT_ISSUER"}
	jwtAudienceEnvKeys       = []string{"OPENFOUNDRY_JWT_AUDIENCE", "JWT_AUDIENCE"}
	jwtKeyIDEnvKeys          = []string{"OPENFOUNDRY_JWT_KID", "JWT_KID"}
	jwtPrivateKeyEnvKeys     = []string{"OPENFOUNDRY_JWT_PRIVATE_KEY_PEM", "JWT_PRIVATE_KEY_PEM"}
	jwtPrivateKeyPathEnvKeys = []string{"OPENFOUNDRY_JWT_PRIVATE_KEY_PATH", "JWT_PRIVATE_KEY_PATH"}
	jwtPublicKeyEnvKeys      = []string{"OPENFOUNDRY_JWT_PUBLIC_KEY_PEM", "JWT_PUBLIC_KEY_PEM"}
	jwtPublicKeyPathEnvKeys  = []string{"OPENFOUNDRY_JWT_PUBLIC_KEY_PATH", "JWT_PUBLIC_KEY_PATH"}
)

// SecretLoadErrorKind discriminates [SecretLoadError] variants.
type SecretLoadErrorKind uint8

const (
	// SecretLoadIO — filesystem error reading or writing the secret.
	SecretLoadIO SecretLoadErrorKind = iota + 1
	// SecretLoadEmpty — the secret file exists but contains nothing
	// useful (only ASCII whitespace).
	SecretLoadEmpty
)

// SecretLoadError is returned by [LoadOrGenerate] /
// [ResolveUnattended]. Mirrors the Rust enum verbatim.
type SecretLoadError struct {
	Kind  SecretLoadErrorKind
	Path  string
	Cause error
}

func (e *SecretLoadError) Error() string {
	switch e.Kind {
	case SecretLoadEmpty:
		return fmt.Sprintf("JWT secret file at %s is empty", e.Path)
	default:
		return fmt.Sprintf("failed to access JWT secret at %s: %s", e.Path, e.Cause)
	}
}

// Unwrap exposes the underlying I/O cause for errors.Is / errors.As.
func (e *SecretLoadError) Unwrap() error { return e.Cause }

// LoadOrGenerate loads an HS256 secret from `path`, generating and
// persisting a new random secret on first use so deployments can
// start fully unattended.
//
// On Unix the persisted file is created with mode 0600 and the
// parent directory with mode 0700 (when it does not already exist).
// Subsequent restarts read the same secret from disk so tokens
// issued before the restart remain valid.
func LoadOrGenerate(path string) (*JWTConfig, error) {
	secret, err := loadOrGenerateSecret(path)
	if err != nil {
		return nil, err
	}
	return FromSecretBytes(secret), nil
}

// ResolveUnattended resolves the HS256 secret following an
// unattended precedence:
//
//  1. OPENFOUNDRY_JWT_SECRET / JWT_SECRET (raw secret in the env).
//  2. OPENFOUNDRY_JWT_SECRET_PATH / JWT_SECRET_PATH (file path).
//  3. The supplied defaultPath, auto-generated if missing.
//
// This lets operators inject a managed secret when one is
// available while still allowing the service to boot with no
// configuration.
func ResolveUnattended(defaultPath string) (*JWTConfig, error) {
	if v, ok := readFirstEnv(jwtSecretEnvKeys); ok {
		return FromSecretBytes([]byte(v)), nil
	}
	path, ok := readFirstEnv(jwtSecretPathEnvKeys)
	if !ok {
		path = defaultPath
	}
	return LoadOrGenerate(path)
}

// WithEnvDefaults populates issuer / audience / key id and RSA
// keys from the canonical OPENFOUNDRY_JWT_* (with JWT_* fallback)
// env vars. Returns the receiver so it chains with the other
// builder methods.
//
// When only one half of the RSA key pair is found the function
// logs a warning at slog.WarnLevel and falls back to HS256, same
// behaviour as the Rust source.
func (c *JWTConfig) WithEnvDefaults() *JWTConfig {
	if v, ok := readFirstEnv(jwtIssuerEnvKeys); ok {
		c.WithIssuer(v)
	}
	if v, ok := readFirstEnv(jwtAudienceEnvKeys); ok {
		c.WithAudience(v)
	}
	if v, ok := readFirstEnv(jwtKeyIDEnvKeys); ok {
		c.WithKeyID(v)
	}

	priv := readPemFromEnv(jwtPrivateKeyEnvKeys, jwtPrivateKeyPathEnvKeys)
	pub := readPemFromEnv(jwtPublicKeyEnvKeys, jwtPublicKeyPathEnvKeys)
	switch {
	case priv != "" && pub != "":
		if _, err := c.WithRSAKeys(priv, pub); err != nil {
			slog.Warn("invalid JWT RSA configuration; falling back to shared-secret HS256", "err", err)
		}
	case priv != "" || pub != "":
		slog.Warn("partial JWT RSA configuration detected; falling back to shared-secret HS256")
	}
	return c
}

// readFirstEnv returns the first non-empty (after trimming) value
// among `keys`, mirroring Rust's `read_first_env` + `read_env`.
func readFirstEnv(keys []string) (string, bool) {
	for _, k := range keys {
		raw, ok := os.LookupEnv(k)
		if !ok {
			continue
		}
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		return v, true
	}
	return "", false
}

// readPemFromEnv reads PEM contents from the value envs first
// (with `\n` literal expansion) then from the path envs (file
// contents, trimmed).
func readPemFromEnv(valueKeys, pathKeys []string) string {
	if v, ok := readFirstEnv(valueKeys); ok {
		return strings.ReplaceAll(v, `\n`, "\n")
	}
	if path, ok := readFirstEnv(pathKeys); ok {
		raw, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		v := strings.TrimSpace(string(raw))
		if v == "" {
			return ""
		}
		return v
	}
	return ""
}

func loadOrGenerateSecret(path string) ([]byte, error) {
	bytes, err := os.ReadFile(path)
	if err == nil {
		trimmed := trimASCIIWhitespace(bytes)
		if len(trimmed) == 0 {
			return nil, &SecretLoadError{Kind: SecretLoadEmpty, Path: path}
		}
		// Accept both hex-encoded and raw byte payloads. Hex is
		// preferred (it is what we write ourselves), but we
		// tolerate raw bytes so a secret seeded by a key-management
		// system also works.
		if decoded, ok := decodeHex(trimmed); ok && len(decoded) > 0 {
			return decoded, nil
		}
		out := make([]byte, len(trimmed))
		copy(out, trimmed)
		return out, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, &SecretLoadError{Kind: SecretLoadIO, Path: path, Cause: err}
	}

	// Generate, persist, return.
	secret := make([]byte, GeneratedSecretBytes)
	if _, randErr := rand.Read(secret); randErr != nil {
		return nil, &SecretLoadError{Kind: SecretLoadIO, Path: path, Cause: randErr}
	}
	if persistErr := persistSecret(path, secret); persistErr != nil {
		return nil, &SecretLoadError{Kind: SecretLoadIO, Path: path, Cause: persistErr}
	}
	slog.Warn("generated new JWT signing secret; persisted for unattended restarts", "path", path)
	return secret, nil
}

func persistSecret(path string, bytes []byte) error {
	parent := filepath.Dir(path)
	if parent != "" && parent != "." {
		if err := createDirAllSecure(parent); err != nil {
			return err
		}
	}
	return writeSecure(path, encodeHex(bytes))
}

// trimASCIIWhitespace mirrors Rust's `trim_ascii_whitespace` —
// strict ASCII, not Unicode-aware. Whitespace is the set 0x09-0x0D
// + 0x20 (matching std::ascii::is_ascii_whitespace).
func trimASCIIWhitespace(bytes []byte) []byte {
	start := 0
	for start < len(bytes) && isASCIIWhitespace(bytes[start]) {
		start++
	}
	end := len(bytes)
	for end > start && isASCIIWhitespace(bytes[end-1]) {
		end--
	}
	return bytes[start:end]
}

func isASCIIWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == 0x0B || b == 0x0C
}

func encodeHex(bytes []byte) []byte {
	const digits = "0123456789abcdef"
	out := make([]byte, len(bytes)*2)
	for i, b := range bytes {
		out[i*2] = digits[b>>4]
		out[i*2+1] = digits[b&0x0f]
	}
	return out
}

func decodeHex(bytes []byte) ([]byte, bool) {
	if len(bytes) == 0 || len(bytes)%2 != 0 {
		return nil, false
	}
	out := make([]byte, len(bytes)/2)
	for i := 0; i < len(out); i++ {
		hi, ok1 := hexValue(bytes[i*2])
		lo, ok2 := hexValue(bytes[i*2+1])
		if !ok1 || !ok2 {
			return nil, false
		}
		out[i] = (hi << 4) | lo
	}
	return out, true
}

func hexValue(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}
