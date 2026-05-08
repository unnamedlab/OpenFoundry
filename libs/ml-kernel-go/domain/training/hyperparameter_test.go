package training

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCandidateSetsDefaultsWhenSearchIsNil(t *testing.T) {
	t.Parallel()
	got := CandidateSets(nil)
	assert.Len(t, got, 3)
	assert.Equal(t, 0.05, got[0]["learning_rate"])
	assert.Equal(t, 250, got[0]["epochs"])
	assert.Equal(t, 0.0, got[0]["l2"])
	assert.Equal(t, 0.12, got[2]["learning_rate"])
}

func TestCandidateSetsDefaultsWhenCandidatesEmpty(t *testing.T) {
	t.Parallel()
	got := CandidateSets(map[string]any{"candidates": []any{}})
	assert.Len(t, got, 3, "empty candidates list still triggers defaults")
}

func TestCandidateSetsHonoursSearchCandidates(t *testing.T) {
	t.Parallel()
	got := CandidateSets(map[string]any{
		"candidates": []any{
			map[string]any{"learning_rate": 0.5},
			map[string]any{"learning_rate": 0.7},
		},
	})
	assert.Len(t, got, 2)
	assert.Equal(t, 0.5, got[0]["learning_rate"])
}

func TestValueAsFloat64FallbackPath(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 0.5, ValueAsFloat64(nil, 0.5))
	assert.Equal(t, 1.0, ValueAsFloat64(true, 1.0), "bools fall through to fallback")
	assert.Equal(t, 0.5, ValueAsFloat64(0.5, 99.0))
	assert.Equal(t, 7.0, ValueAsFloat64(int(7), 99.0))
}

func TestValueAsUint64FallbackPath(t *testing.T) {
	t.Parallel()
	assert.Equal(t, uint64(99), ValueAsUint64(nil, 99))
	assert.Equal(t, uint64(99), ValueAsUint64(-5, 99), "negative ints fall through")
	assert.Equal(t, uint64(7), ValueAsUint64(7, 99))
	assert.Equal(t, uint64(7), ValueAsUint64(7.0, 99))
}
