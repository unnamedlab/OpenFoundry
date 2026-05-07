// Tests for the Phase 5C batch helpers — scale-limit math, list-cap
// validation, batched_execution flag extraction.
package actions

import (
	"encoding/json"
	"testing"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

func TestEstimateEditBytesIgnoresWhitespace(t *testing.T) {
	t.Parallel()
	loose := json.RawMessage(`{ "a"  :  1 ,  "b" :  2 }`)
	tight := json.RawMessage(`{"a":1,"b":2}`)
	got, want := estimateEditBytes(loose), len(tight)
	if got != want {
		t.Errorf("expected canonical-bytes %d, got %d", want, got)
	}
}

func TestEstimateEditBytesEmptyZero(t *testing.T) {
	t.Parallel()
	if got := estimateEditBytes(nil); got != 0 {
		t.Errorf("nil rawMessage must be zero, got %d", got)
	}
	if got := estimateEditBytes(json.RawMessage("")); got != 0 {
		t.Errorf("empty rawMessage must be zero, got %d", got)
	}
}

func TestValidateParameterListSizes(t *testing.T) {
	t.Parallel()
	schema := []models.ActionInputField{
		{Name: "owners", PropertyType: "object_reference_list"},
		{Name: "tags", PropertyType: "array"},
	}
	tooManyRefs := json.RawMessage(`{"owners":` + buildJSONList(maxObjectReferenceList+1) + `}`)
	if msg := validateParameterListSizes(schema, tooManyRefs); msg == "" {
		t.Fatal("expected scale-limit message for over-cap object_reference_list")
	}
	tooManyArr := json.RawMessage(`{"tags":` + buildJSONList(maxListPrimitive+1) + `}`)
	if msg := validateParameterListSizes(schema, tooManyArr); msg == "" {
		t.Fatal("expected scale-limit message for over-cap array")
	}
	withinCap := json.RawMessage(`{"owners":[1,2],"tags":["a","b"]}`)
	if msg := validateParameterListSizes(schema, withinCap); msg != "" {
		t.Errorf("unexpected scale-limit error: %s", msg)
	}
}

func TestExtractBatchedExecutionFlag(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		`{"batched_execution":true}`:                   true,
		`{"batched_execution":false}`:                  false,
		`{"operation":{"x":1},"batched_execution":true}`: true,
		`{}`:                                           false,
		`null`:                                         false,
		`""`:                                           false,
	}
	for raw, want := range cases {
		got := extractBatchedExecutionFlag(json.RawMessage(raw))
		if got != want {
			t.Errorf("config %q: got %v, want %v", raw, got, want)
		}
	}
}

func buildJSONList(n int) string {
	out := []byte{'['}
	for i := 0; i < n; i++ {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '0')
	}
	out = append(out, ']')
	return string(out)
}
