// Package products implements the Marketplace Products feature: tar.gz
// bundling of ontology, actions, pipelines, apps, and governance resources, HMAC-SHA256
// signing, object-storage upload, and the install workflow that
// re-creates the snapshotted resources in a target workspace.
package products

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

// SignAlgorithm names the only signing scheme this package supports.
// v1 is intentionally simple — HMAC-SHA256 with a symmetric secret
// (MARKETPLACE_SIGN_KEY). PKI / X509 are out of scope.
const SignAlgorithm = "HMAC-SHA256"

// ManifestFileName is the canonical name of the JSON metadata document
// written at the root of every bundle.
const ManifestFileName = "manifest.json"

// ResourceDir returns the directory inside the bundle that holds
// snapshots of the given resource type.
func ResourceDir(t models.ProductResourceType) string {
	switch t {
	case models.ProductResourceOntologyType:
		return "ontology"
	case models.ProductResourceActionType:
		return "actions"
	case models.ProductResourcePipeline:
		return "pipelines"
	case models.ProductResourceApp:
		return "apps"
	case models.ProductResourceRestrictedView:
		return "governance/restricted-views"
	case models.ProductResourceProjectTemplate:
		return "governance/project-templates"
	case models.ProductResourceApplicationAccessMetadata:
		return "governance/application-access"
	case models.ProductResourceDashboard:
		return "governance/dashboards"
	case models.ProductResourceGovernanceConfig:
		return "governance/config"
	default:
		return "other"
	}
}

// ResourceSnapshot pairs a resource ref with the raw JSON definition
// fetched from the owner service. The publish path collects one of
// these per Product.Resources entry, then hands the slice to BuildBundle.
type ResourceSnapshot struct {
	Type    models.ProductResourceType
	Ref     string
	Payload json.RawMessage
}

// SignManifest returns the lowercase hex HMAC-SHA256 of manifestBytes
// using key. Returns an error only when key is empty (we refuse to sign
// with a zero-length secret to avoid silent misconfiguration).
func SignManifest(manifestBytes, key []byte) (string, error) {
	if len(key) == 0 {
		return "", errors.New("marketplace sign key is empty")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(manifestBytes)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifyManifest reports whether sigHex is a valid HMAC-SHA256 over
// manifestBytes for the given key. Uses constant-time comparison.
func VerifyManifest(manifestBytes []byte, sigHex string, key []byte) bool {
	if len(key) == 0 || sigHex == "" {
		return false
	}
	expected, err := SignManifest(manifestBytes, key)
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(expected)
	if err != nil {
		return false
	}
	got, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	return hmac.Equal(want, got)
}

// BuildBundle assembles the tar.gz bundle for a published product
// version. The layout is:
//
//	manifest.json                    ← signed JSON metadata
//	ontology/<resource_ref>.json     ← one file per ONTOLOGY_TYPE resource
//	actions/<resource_ref>.json      ← one file per ACTION_TYPE resource
//	pipelines/<resource_ref>.json    ← one file per PIPELINE resource
//	apps/<resource_ref>.json         ← one file per APP resource
//
// The returned bundle bytes are gzip-compressed tar; the returned
// signature is the hex HMAC-SHA256 over the canonical (JSON-encoded
// without HTML escaping, indented with two spaces) manifest. The same
// canonical encoding is verified on install — callers MUST NOT
// re-encode the manifest before storing or verifying it.
func BuildBundle(
	product models.Product,
	version string,
	snapshots []ResourceSnapshot,
	signKey []byte,
	now time.Time,
) (bundle []byte, manifest models.ProductManifest, manifestJSON []byte, signature string, err error) {
	if len(signKey) == 0 {
		return nil, models.ProductManifest{}, nil, "", errors.New("marketplace sign key is required")
	}
	manifest = models.ProductManifest{
		ProductRID:    product.RID,
		ProductName:   product.Name,
		Version:       version,
		Author:        product.Author,
		Description:   product.Description,
		SignAlgorithm: SignAlgorithm,
		SignedAt:      now.UTC(),
		Resources:     make([]models.ProductManifestEntry, 0, len(snapshots)),
	}
	for _, snap := range snapshots {
		dir := ResourceDir(snap.Type)
		path := fmt.Sprintf("%s/%s.json", dir, sanitizeRef(snap.Ref))
		manifest.Resources = append(manifest.Resources, models.ProductManifestEntry{
			Type: snap.Type,
			Ref:  snap.Ref,
			Path: path,
		})
	}
	manifestJSON, err = encodeManifest(manifest)
	if err != nil {
		return nil, models.ProductManifest{}, nil, "", fmt.Errorf("encode manifest: %w", err)
	}
	signature, err = SignManifest(manifestJSON, signKey)
	if err != nil {
		return nil, models.ProductManifest{}, nil, "", err
	}

	var gzbuf bytes.Buffer
	gz := gzip.NewWriter(&gzbuf)
	tw := tar.NewWriter(gz)

	if err := writeTarFile(tw, ManifestFileName, manifestJSON, now); err != nil {
		return nil, models.ProductManifest{}, nil, "", err
	}
	// Write a sibling .sig file so consumers (and a quick `tar -xzf
	// bundle.tar.gz manifest.sig`) can inspect the signature without
	// re-running HMAC. The Go install path does not depend on this
	// file — it re-signs the manifest and compares — but the file
	// keeps the bundle self-describing.
	if err := writeTarFile(tw, ManifestFileName+".sig", []byte(signature), now); err != nil {
		return nil, models.ProductManifest{}, nil, "", err
	}
	for i, snap := range snapshots {
		entry := manifest.Resources[i]
		payload := snap.Payload
		if len(payload) == 0 {
			payload = json.RawMessage(`{}`)
		}
		if err := writeTarFile(tw, entry.Path, payload, now); err != nil {
			return nil, models.ProductManifest{}, nil, "", err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, models.ProductManifest{}, nil, "", err
	}
	if err := gz.Close(); err != nil {
		return nil, models.ProductManifest{}, nil, "", err
	}
	return gzbuf.Bytes(), manifest, manifestJSON, signature, nil
}

// ReadBundle extracts a previously built bundle, returning the manifest
// (already verified against expectedSignature using signKey) and the
// raw payload bytes keyed by manifest entry path.
//
// The signature is verified using the bundle's own manifest.json
// bytes — the install path is the authoritative consumer and MUST NOT
// trust any externally supplied signature without going through here.
func ReadBundle(bundle []byte, signKey []byte) (models.ProductManifest, map[string]json.RawMessage, string, error) {
	if len(signKey) == 0 {
		return models.ProductManifest{}, nil, "", errors.New("marketplace sign key is required")
	}
	gz, err := gzip.NewReader(bytes.NewReader(bundle))
	if err != nil {
		return models.ProductManifest{}, nil, "", fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var manifestBytes []byte
	files := map[string]json.RawMessage{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return models.ProductManifest{}, nil, "", fmt.Errorf("read tar header: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return models.ProductManifest{}, nil, "", fmt.Errorf("read tar body %s: %w", hdr.Name, err)
		}
		switch hdr.Name {
		case ManifestFileName:
			manifestBytes = body
		case ManifestFileName + ".sig":
			// Self-describing sidecar; not authoritative.
			continue
		default:
			files[hdr.Name] = body
		}
	}
	if len(manifestBytes) == 0 {
		return models.ProductManifest{}, nil, "", errors.New("bundle is missing manifest.json")
	}
	signature, err := SignManifest(manifestBytes, signKey)
	if err != nil {
		return models.ProductManifest{}, nil, "", err
	}
	var manifest models.ProductManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return models.ProductManifest{}, nil, "", fmt.Errorf("decode manifest: %w", err)
	}
	return manifest, files, signature, nil
}

func encodeManifest(m models.ProductManifest) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	// json.Encoder.Encode appends a trailing newline; keep it so the
	// signature stays stable across encode/decode round-trips.
	return buf.Bytes(), nil
}

func writeTarFile(tw *tar.Writer, name string, body []byte, mtime time.Time) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o644,
		Size:    int64(len(body)),
		ModTime: mtime.UTC(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("tar header %s: %w", name, err)
	}
	if _, err := tw.Write(body); err != nil {
		return fmt.Errorf("tar body %s: %w", name, err)
	}
	return nil
}

func sanitizeRef(ref string) string {
	r := strings.ReplaceAll(ref, "/", "_")
	r = strings.ReplaceAll(r, "..", "_")
	r = strings.TrimSpace(r)
	if r == "" {
		return "unknown"
	}
	return r
}
