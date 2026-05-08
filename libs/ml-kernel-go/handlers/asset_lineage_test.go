package handlers

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestAssetNodeIDFormat(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	got := assetNodeID("experiment", id.String())
	assert.Equal(t, "experiment:"+id.String(), got)
}

func TestStringFieldFromJSONPaths(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", stringFieldFromJSON(nil, "k"))
	assert.Equal(t, "", stringFieldFromJSON(json.RawMessage(`{}`), "k"))
	assert.Equal(t, "v", stringFieldFromJSON(json.RawMessage(`{"k":"v"}`), "k"))
	assert.Equal(t, "", stringFieldFromJSON(json.RawMessage(`{"k":42}`), "k"))
	assert.Equal(t, "", stringFieldFromJSON(json.RawMessage(`not-json`), "k"))
}

func TestNestedStringFieldFromJSONPaths(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "v", nestedStringFieldFromJSON(json.RawMessage(`{"a":{"b":"v"}}`), "a", "b"))
	assert.Equal(t, "", nestedStringFieldFromJSON(json.RawMessage(`{"a":{}}`), "a", "b"))
	assert.Equal(t, "", nestedStringFieldFromJSON(json.RawMessage(`{}`), "a", "b"))
	assert.Equal(t, "", nestedStringFieldFromJSON(nil, "a", "b"))
}

func TestRawOrNullValue(t *testing.T) {
	t.Parallel()
	assert.Nil(t, rawOrNullValue(nil))
	assert.Nil(t, rawOrNullValue(json.RawMessage(`malformed`)))
	got := rawOrNullValue(json.RawMessage(`{"x":1}`))
	asMap, ok := got.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, float64(1), asMap["x"])
}

func TestUUIDSetSortedIsStable(t *testing.T) {
	t.Parallel()
	a := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	c := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	set := newUUIDSet()
	set.add(c)
	set.add(a)
	set.add(b)
	set.add(a) // duplicate ignored
	got := set.sorted()
	assert.Equal(t, []uuid.UUID{a, b, c}, got)
	assert.Equal(t, 3, set.size())
}

func TestStringSetSortedIsStable(t *testing.T) {
	t.Parallel()
	set := newStringSet()
	set.add("xgboost")
	set.add("scikit-learn")
	set.add("pytorch")
	set.add("xgboost") // duplicate ignored
	got := set.sorted()
	assert.Equal(t, []string{"pytorch", "scikit-learn", "xgboost"}, got)
}
