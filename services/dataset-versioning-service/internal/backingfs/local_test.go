package backingfs

import (
	"context"
	"strings"
	"testing"
	"time"

	storageabstraction "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
	"github.com/stretchr/testify/require"
)

func buildLocalForTest(t *testing.T) *LocalBackingFS {
	t.Helper()
	fs, err := NewLocal(LocalConfig{FSID: "local", BaseDirectory: "foundry/datasets", PresignSecret: "x", PublicOrigin: "http://files.local", RootDir: t.TempDir()})
	require.NoError(t, err)
	return fs
}

func TestLocalBackingFSRoundTripListAndDelete(t *testing.T) {
	fs := buildLocalForTest(t)
	ctx := context.Background()

	p1, err := fs.Put(ctx, "rid-x/transactions/t1/file-a.parquet", []byte("alpha"), PutOpts{})
	require.NoError(t, err)
	p2, err := fs.Put(ctx, "rid-x/transactions/t1/file-b.parquet", []byte("beta"), PutOpts{})
	require.NoError(t, err)
	p3, err := fs.Put(ctx, "rid-y/transactions/t1/file-c.parquet", []byte("gamma"), PutOpts{})
	require.NoError(t, err)

	got, err := fs.Get(ctx, p1)
	require.NoError(t, err)
	require.Equal(t, "alpha", string(got))
	got, err = fs.Get(ctx, p2)
	require.NoError(t, err)
	require.Equal(t, "beta", string(got))
	got, err = fs.Get(ctx, p3)
	require.NoError(t, err)
	require.Equal(t, "gamma", string(got))

	entries, err := fs.List(ctx, "rid-x")
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, "rid-x/transactions/t1/file-a.parquet", entries[0].LogicalPath)
	require.Equal(t, "rid-x/transactions/t1/file-b.parquet", entries[1].LogicalPath)

	require.NoError(t, fs.Delete(ctx, p1))
	_, err = fs.Get(ctx, p1)
	require.Error(t, err)
}

func TestLocalBackingFSURIFormatMatchesPersistedColumn(t *testing.T) {
	fs := buildLocalForTest(t)
	physical, err := fs.Put(context.Background(), "rid/transactions/t1/file.parquet", []byte("data"), PutOpts{})
	require.NoError(t, err)
	require.Equal(t, "local:///foundry/datasets/rid/transactions/t1/file.parquet", physical.URI())
}

func TestLogicalToPhysicalMappingStable(t *testing.T) {
	fs := buildLocalForTest(t)
	ctx := context.Background()
	p1, err := fs.Put(ctx, "rid/transactions/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/data.parquet", []byte("v1"), PutOpts{})
	require.NoError(t, err)
	p2, err := fs.Put(ctx, "rid/transactions/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb/data.parquet", []byte("v2"), PutOpts{})
	require.NoError(t, err)
	require.NotEqual(t, p1.URI(), p2.URI())

	p3, err := fs.Put(ctx, "rid/transactions/t1/data.parquet", []byte("v1"), PutOpts{})
	require.NoError(t, err)
	p4, err := fs.Put(ctx, "rid/transactions/t1/data.parquet", []byte("v2"), PutOpts{})
	require.NoError(t, err)
	require.Equal(t, p3.URI(), p4.URI())
}

func TestPresignedURLVerifiesAndRejectsUnsafeKeys(t *testing.T) {
	fixed := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	fs := buildLocalForTest(t)
	fs.Now = func() time.Time { return fixed }

	signed, err := fs.PresignedURL(storageabstraction.PhysicalLocation{FSID: "local", BaseDirectory: "foundry/datasets", RelativePath: "rid/file.parquet"}, time.Minute)
	require.NoError(t, err)
	require.Equal(t, "GET", signed.Method)
	require.True(t, strings.HasPrefix(signed.URL, "http://files.local/v1/_internal/local-fs/foundry/datasets/rid/file.parquet?"))
	require.True(t, fs.VerifyLocalSignature("foundry/datasets/rid/file.parquet", fixed.Add(time.Minute), fs.SignLocalKey("foundry/datasets/rid/file.parquet", fixed.Add(time.Minute).Unix())))
	_, err = fs.PresignedURL(storageabstraction.PhysicalLocation{FSID: "local", RelativePath: "../secret"}, time.Minute)
	require.Error(t, err)
}
