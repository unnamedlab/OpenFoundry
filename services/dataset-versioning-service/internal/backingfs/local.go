// Package backingfs contains the dataset-versioning-service-local backing
// filesystem port. It mirrors the Rust LocalBackingFs contract used by the
// Files tab: stable logical-to-physical object-key mapping, HMAC presigned
// local proxy URLs, and local round-trip storage operations for development
// and tests.
package backingfs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	storageabstraction "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// PutOpts is reserved for parity with Rust storage_abstraction::backing_fs::PutOpts.
type PutOpts struct{}

// ListEntry is one object listed under a logical prefix.
type ListEntry struct {
	LogicalPath string
	PhysicalURI string
	SizeBytes   int64
}

// LocalConfig is the Go equivalent of Rust LocalBackingFsConfig.
type LocalConfig struct {
	FSID          string
	BaseDirectory string
	PresignSecret string
	PublicOrigin  string
	RootDir       string
}

// LocalBackingFS implements a local, HMAC-presigned backing filesystem. The
// physical URI is deterministic: base_directory + logical path.
type LocalBackingFS struct {
	fsID         string
	baseDir      string
	publicOrigin string
	rootDir      string
	secret       []byte
	Now          func() time.Time
}

func NewLocal(cfg LocalConfig) (*LocalBackingFS, error) {
	fsID := strings.TrimSpace(cfg.FSID)
	if fsID == "" {
		fsID = "local"
	}
	if fsID != "local" {
		return nil, errors.New("local backing filesystem requires fs_id=local")
	}
	secret := []byte(cfg.PresignSecret)
	if len(secret) == 0 {
		secret = []byte("openfoundry-local-backing-fs")
	}
	origin := strings.TrimRight(strings.TrimSpace(cfg.PublicOrigin), "/")
	if origin == "" {
		origin = "http://localhost"
	}
	root := cfg.RootDir
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &LocalBackingFS{fsID: fsID, baseDir: cleanKeyAllowEmpty(cfg.BaseDirectory), publicOrigin: origin, rootDir: absRoot, secret: secret}, nil
}

func (l *LocalBackingFS) FSID() string          { return l.fsID }
func (l *LocalBackingFS) BaseDirectory() string { return l.baseDir }

func (l *LocalBackingFS) Put(_ context.Context, logicalPath string, data []byte, _ PutOpts) (storageabstraction.PhysicalLocation, error) {
	logical, err := cleanKey(logicalPath)
	if err != nil {
		return storageabstraction.PhysicalLocation{}, err
	}
	physical := storageabstraction.PhysicalLocation{FSID: l.fsID, BaseDirectory: l.baseDir, RelativePath: logical}
	if err := l.WriteLocalObject(physicalKey(physical), data); err != nil {
		return storageabstraction.PhysicalLocation{}, err
	}
	return physical, nil
}

func (l *LocalBackingFS) Get(_ context.Context, physical storageabstraction.PhysicalLocation) ([]byte, error) {
	return l.ReadLocalObject(physicalKey(physical))
}

func (l *LocalBackingFS) Delete(_ context.Context, physical storageabstraction.PhysicalLocation) error {
	p, err := l.localPath(physicalKey(physical))
	if err != nil {
		return err
	}
	return os.Remove(p)
}

func (l *LocalBackingFS) List(_ context.Context, prefix string) ([]ListEntry, error) {
	prefix = cleanKeyAllowEmpty(prefix)
	basePrefix := joinObjectKey(l.baseDir, prefix)
	root := l.rootDir
	if l.baseDir != "" {
		var err error
		root, err = l.localPath(l.baseDir)
		if err != nil {
			return nil, err
		}
	}
	entries := []ListEntry{}
	if _, statErr := os.Stat(root); errors.Is(statErr, fs.ErrNotExist) {
		return entries, nil
	} else if statErr != nil {
		return nil, statErr
	}
	err := filepath.WalkDir(root, func(filePath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(l.rootDir, filePath)
		if err != nil {
			return err
		}
		objectKey := filepath.ToSlash(rel)
		if basePrefix != "" && objectKey != basePrefix && !strings.HasPrefix(objectKey, basePrefix+"/") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		logical := strings.TrimPrefix(objectKey, strings.Trim(l.baseDir, "/"))
		logical = strings.Trim(logical, "/")
		entries = append(entries, ListEntry{LogicalPath: logical, PhysicalURI: storageabstraction.PhysicalLocation{FSID: l.fsID, RelativePath: objectKey}.URI(), SizeBytes: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].LogicalPath < entries[j].LogicalPath })
	return entries, nil
}

func (l *LocalBackingFS) PresignedURL(location storageabstraction.PhysicalLocation, ttl time.Duration) (storageabstraction.PresignedURL, error) {
	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	expiresAt := now().UTC().Add(ttl)
	key, err := cleanKey(physicalKey(location))
	if err != nil {
		return storageabstraction.PresignedURL{}, err
	}
	values := url.Values{}
	values.Set("expires", strconv.FormatInt(expiresAt.Unix(), 10))
	values.Set("sig", l.SignLocalKey(key, expiresAt.Unix()))
	return storageabstraction.PresignedURL{URL: l.publicOrigin + "/v1/_internal/local-fs/" + key + "?" + values.Encode(), ExpiresAt: expiresAt, Method: "GET"}, nil
}

func (l *LocalBackingFS) SignLocalKey(key string, expires int64) string {
	mac := hmac.New(sha256.New, l.secret)
	mac.Write([]byte(cleanKeyAllowEmpty(key)))
	mac.Write([]byte("\n"))
	mac.Write([]byte(time.Unix(expires, 0).UTC().Format(time.RFC3339)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (l *LocalBackingFS) VerifyLocalSignature(key string, expires time.Time, sig string) bool {
	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	if now().UTC().After(expires.UTC()) {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	want, _ := hex.DecodeString(l.SignLocalKey(key, expires.Unix()))
	return hmac.Equal(got, want)
}

func (l *LocalBackingFS) ReadLocalObject(key string) ([]byte, error) {
	p, err := l.localPath(key)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

func (l *LocalBackingFS) WriteLocalObject(key string, data []byte) error {
	p, err := l.localPath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (l *LocalBackingFS) localPath(key string) (string, error) {
	cleaned, err := cleanKey(key)
	if err != nil {
		return "", err
	}
	absPath := filepath.Join(l.rootDir, filepath.FromSlash(cleaned))
	rel, err := filepath.Rel(l.rootDir, absPath)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("local object key escapes root")
	}
	return absPath, nil
}

func physicalKey(p storageabstraction.PhysicalLocation) string {
	return joinObjectKey(p.BaseDirectory, p.RelativePath)
}

func cleanKeyAllowEmpty(key string) string {
	cleaned, err := cleanKey(key)
	if err != nil {
		return ""
	}
	return cleaned
}

func cleanKey(key string) (string, error) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return "", errors.New("invalid local object key")
	}
	cleaned := path.Clean("/" + trimmed)[1:]
	if cleaned == "." || cleaned == "" || cleaned != strings.Trim(trimmed, "/") {
		return "", errors.New("invalid local object key")
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == "" || part == ".." {
			return "", errors.New("invalid local object key")
		}
	}
	return cleaned, nil
}

func joinObjectKey(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if p := strings.Trim(part, "/"); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}
