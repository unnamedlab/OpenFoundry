// Package training hosts the pure-logic training-execution helpers.
// This slice ports the small hyperparameter helper module verbatim
// from libs/ml-kernel/src/domain/training/hyperparameter.rs. The
// execute_training entrypoint + runner module land alongside the
// interop port (domain/interop is 769 LOC of pure logic and ships in
// its own slice).
package training

// CandidateSet represents one element in the search-space candidates
// list. The Rust source uses serde_json::Value; the Go side uses a
// generic map so callers can attach any JSON shape (learning_rate,
// epochs, l2 …) without losing structure.
type CandidateSet = map[string]any

// CandidateSets returns the candidate hyperparameter sets to try.
// When `search` carries a non-empty "candidates" array we honour it
// verbatim; otherwise we fall back to the deterministic 3-row default
// matching the Rust source byte-for-byte.
func CandidateSets(search map[string]any) []CandidateSet {
	if search != nil {
		if arr, ok := search["candidates"].([]any); ok && len(arr) > 0 {
			out := make([]CandidateSet, 0, len(arr))
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					out = append(out, m)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return []CandidateSet{
		{"learning_rate": 0.05, "epochs": 250, "l2": 0.0},
		{"learning_rate": 0.08, "epochs": 350, "l2": 0.001},
		{"learning_rate": 0.12, "epochs": 500, "l2": 0.01},
	}
}

// ValueAsFloat64 mirrors fn value_as_f64 — extracts an f64 from a
// JSON-decoded value or returns the supplied fallback.
func ValueAsFloat64(v any, fallback float64) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	}
	return fallback
}

// ValueAsUint64 mirrors fn value_as_u64.
func ValueAsUint64(v any, fallback uint64) uint64 {
	switch x := v.(type) {
	case uint64:
		return x
	case uint32:
		return uint64(x)
	case uint:
		return uint64(x)
	case float64:
		if x >= 0 {
			return uint64(x)
		}
	case int:
		if x >= 0 {
			return uint64(x)
		}
	case int32:
		if x >= 0 {
			return uint64(x)
		}
	case int64:
		if x >= 0 {
			return uint64(x)
		}
	}
	return fallback
}
