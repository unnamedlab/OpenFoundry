// Package mediapath ports services/media-sets-service/src/domain/path.rs.
// The user-facing object key is derived from the content hash, not the
// logical path, so two media sets with the same SHA-256 share the same
// physical bytes while each MediaItem row tracks its own logical
// (media_set_rid, branch, path).
//
// Layout:
//
//	{bucket}/media-sets/{media_set_rid}/{branch}/{sha256[:2]}/{sha256}
//
// The {sha256[:2]} shard prevents single-prefix hot spots in S3 / GCS.
package mediapath

import "fmt"

// Key materialises an object-store key for a single byte payload.
type Key struct {
	MediaSetRID string
	Branch      string
	SHA256      string
}

// New constructs a Key.
func New(mediaSetRID, branch, sha256 string) Key {
	return Key{MediaSetRID: mediaSetRID, Branch: branch, SHA256: sha256}
}

// ObjectKey returns the no-scheme, no-bucket key suitable for s3 / minio.
func (k Key) ObjectKey() string {
	prefix := "00"
	if len(k.SHA256) >= 2 {
		prefix = k.SHA256[:2]
	}
	return fmt.Sprintf("media-sets/%s/%s/%s/%s",
		k.MediaSetRID, k.Branch, prefix, k.SHA256)
}

// StorageURI builds the canonical s3://{bucket}/<key> URI persisted in
// `media_items.storage_uri`. The scheme is always s3 regardless of the
// actual backend so consumers can address bytes through any S3-
// compatible client by swapping the endpoint.
func StorageURI(bucket string, k Key) string {
	return "s3://" + bucket + "/" + k.ObjectKey()
}
