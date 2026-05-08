package costmodel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadStreamRateMatchesDoc(t *testing.T) {
	t.Parallel()
	rate, ok := RatePerGB("download_stream")
	require.True(t, ok)
	assert.Equal(t, uint32(2), rate)
}

func TestOCRCharges275PerGBInImageSchema(t *testing.T) {
	t.Parallel()
	rate, ok := RatePerGB("ocr")
	require.True(t, ok)
	assert.Equal(t, uint32(275), rate)
	entry, ok := Entry("ocr")
	require.True(t, ok)
	assert.Equal(t, SchemaImage, entry.Schema)
}

func TestUnknownKeyIsNotBillable(t *testing.T) {
	t.Parallel()
	_, ok := RatePerGB("not_a_real_transformation")
	assert.False(t, ok)
	_, ok = ChargeComputeSeconds("not_a_real_transformation", 1024)
	assert.False(t, ok)
}

func TestChargeRoundsUpAndRespectsMinimum(t *testing.T) {
	t.Parallel()
	// 1 GB through download_stream = 2 compute-seconds exactly.
	got, ok := ChargeComputeSeconds("download_stream", 1024*1024*1024)
	require.True(t, ok)
	assert.Equal(t, uint64(2), got)

	// 1 byte must still register a non-zero charge (rounded to 1).
	got, ok = ChargeComputeSeconds("download_stream", 1)
	require.True(t, ok)
	assert.Equal(t, uint64(1), got)

	// 0 bytes processes nothing.
	got, ok = ChargeComputeSeconds("download_stream", 0)
	require.True(t, ok)
	assert.Equal(t, uint64(0), got)
}

func TestEntriesForImageIncludesAllSchemaRows(t *testing.T) {
	t.Parallel()
	keys := make(map[string]struct{})
	for _, e := range EntriesFor(SchemaImage) {
		keys[e.Key] = struct{}{}
	}
	_, hasAll := keys["download_stream"]
	assert.True(t, hasAll, "All rows must be visible under any schema")
	_, hasOcr := keys["ocr"]
	assert.True(t, hasOcr, "Image rows must be visible")
	_, hasTrans := keys["transcription"]
	assert.False(t, hasTrans, "Audio rows must NOT leak into the Image breakdown")
}

func TestCostTableLengthMatchesRustSource(t *testing.T) {
	t.Parallel()
	// Pinned to the row count in libs/observability/src/cost_model.rs.
	// Update both sides together if the upstream Foundry doc adds rows.
	assert.Equal(t, 44, len(CostTable))
}
