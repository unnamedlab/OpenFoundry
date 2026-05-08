package scan

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeOpaquePagingStateNilOnEmpty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, encodeOpaquePagingState(nil))
	assert.Nil(t, encodeOpaquePagingState([]byte{}))
}

func TestEncodeDecodeOpaquePagingStateRoundTrip(t *testing.T) {
	t.Parallel()
	in := []byte{0x00, 0x01, 0xff, 0xa5, 0x10}
	tok := encodeOpaquePagingState(in)
	require.NotNil(t, tok)
	assert.Equal(t, base64.StdEncoding.EncodeToString(in), *tok,
		"encoding must use standard base64 — same alphabet as cassandra-kernel.encodePagingState")

	out, err := decodeOpaquePagingState(tok)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestDecodeOpaquePagingStateNilAndEmpty(t *testing.T) {
	t.Parallel()
	out, err := decodeOpaquePagingState(nil)
	require.NoError(t, err)
	assert.Nil(t, out)

	empty := ""
	out, err = decodeOpaquePagingState(&empty)
	require.NoError(t, err, "empty string must collapse to no-token like the Rust impl")
	assert.Nil(t, out)
}

func TestDecodeOpaquePagingStateRejectsGarbage(t *testing.T) {
	t.Parallel()
	bad := "not!!!base64@@@"
	_, err := decodeOpaquePagingState(&bad)
	require.Error(t, err)

	var se *ScanError
	require.True(t, errors.As(err, &se))
	assert.Equal(t, "invalid_resume_token", se.Kind)
	assert.True(t, IsScanError(err))
}
