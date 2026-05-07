package mediapath

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestObjectKeyUsesSha256Shard(t *testing.T) {
	t.Parallel()
	k := New(
		"ri.foundry.main.media_set.abc",
		"main",
		"deadbeef0000000000000000000000000000000000000000000000000000abcd",
	)
	assert.Equal(t,
		"media-sets/ri.foundry.main.media_set.abc/main/de/deadbeef0000000000000000000000000000000000000000000000000000abcd",
		k.ObjectKey(),
	)
}

func TestStorageURIUsesS3Scheme(t *testing.T) {
	t.Parallel()
	k := New("ms", "main", "ffeeddcc00112233445566778899aabbccddeeff00112233445566778899aabb")
	assert.Equal(t,
		"s3://media/media-sets/ms/main/ff/ffeeddcc00112233445566778899aabbccddeeff00112233445566778899aabb",
		StorageURI("media", k),
	)
}

func TestObjectKeyShortSha256FallsBackTo00(t *testing.T) {
	t.Parallel()
	k := New("ms", "main", "f")
	// Short hash → "00" shard so the key never crashes / panics on a
	// degenerate input. Mirrors the Rust impl.
	assert.Equal(t, "media-sets/ms/main/00/f", k.ObjectKey())
}
