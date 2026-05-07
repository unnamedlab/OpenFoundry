// GetActionMetrics — full 1:1 port of the metrics endpoint, plus the
// `parse_window_to_seconds` + p95 helpers it relies on. Mirrors the
// Rust source byte-for-byte: same window vocabulary (s/m/h/d/w),
// same kind filter ("action_attempt"), same p95 interpolation, same
// failure-category aggregation.
package actions

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// ActionMetricsResponse mirrors `struct ActionMetricsResponse`. Kept
// local to the handler package because no other surface needs it.
type ActionMetricsResponse struct {
	ActionID          uuid.UUID        `json:"action_id"`
	Window            string           `json:"window"`
	SuccessCount      int64            `json:"success_count"`
	FailureCount      int64            `json:"failure_count"`
	P95DurationMs     *float64         `json:"p95_duration_ms"`
	FailureCategories map[string]int64 `json:"failure_categories"`
}

// GetActionMetrics mirrors `pub async fn get_action_metrics`.
//
// Walks every entry in the action log (paged, eventual consistency
// matching Rust), filters to `kind = "action_attempt"` whose payload
// references this action_id, classifies success/failure and
// aggregates p95 + failure_categories. Returns the same envelope the
// dashboard expects.
func GetActionMetrics(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errBody("missing claims"))
			return
		}
		actionID, err := pathUUID(r, "id")
		if err != nil {
			writeJSON(w, http.StatusNotFound, nil)
			return
		}
		window := strings.TrimSpace(r.URL.Query().Get("window"))
		if window == "" {
			window = "30d"
		}
		windowSeconds, err := parseWindowToSeconds(window)
		if err != nil {
			invalid(w, err.Error())
			return
		}
		windowMs := int64(windowSeconds) * 1_000
		// Overflow guard mirrors the Rust `checked_mul`.
		if windowMs/1_000 != int64(windowSeconds) {
			dbError(w, "window overflow: "+window)
			return
		}
		cutoffMs := time.Now().UTC().UnixMilli() - windowMs
		tenant := domain.TenantFromClaims(claims)
		actionFilter := actionID.String()

		var (
			successCount      int64
			failureCount      int64
			durations         []float64
			failureCategories = map[string]int64{}
			nextToken         *string
			reachedCutoff     bool
		)

		for !reachedCutoff {
			page, err := state.Stores.Actions.ListRecent(r.Context(), tenant,
				storage.Page{Size: 5_000, Token: nextToken}, storage.Eventual())
			if err != nil {
				dbError(w, "failed to aggregate action metrics: "+err.Error())
				return
			}
			for _, entry := range page.Items {
				if entry.RecordedAtMs < cutoffMs {
					reachedCutoff = true
					break
				}
				if entry.Kind != "action_attempt" {
					continue
				}
				payload := decodePayloadStrings(entry.Payload)
				if payload["action_type_id"] != actionFilter {
					continue
				}
				switch payload["status"] {
				case "success":
					successCount++
				case "failure":
					failureCount++
					if cat, ok := payload["failure_type"]; ok && cat != "" {
						failureCategories[cat]++
					}
				}
				if d := decodePayloadNumber(entry.Payload, "duration_ms"); d != nil {
					durations = append(durations, *d)
				}
			}
			if reachedCutoff {
				break
			}
			if page.NextToken == nil {
				break
			}
			nextToken = page.NextToken
		}

		writeJSON(w, http.StatusOK, ActionMetricsResponse{
			ActionID:          actionID,
			Window:            window,
			SuccessCount:      successCount,
			FailureCount:      failureCount,
			P95DurationMs:     percentile95DurationMs(durations),
			FailureCategories: failureCategories,
		})
	}
}

// parseWindowToSeconds mirrors `fn parse_window_to_seconds`. Accepts
// `30d`, `12h`, `45m`, `120s`, `2w`. A bare numeric value is treated
// as days.
func parseWindowToSeconds(input string) (uint64, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return 0, errors.New("window must not be empty")
	}
	idx := -1
	for i, c := range trimmed {
		if c < '0' || c > '9' {
			idx = i
			break
		}
	}
	numberPart := trimmed
	suffix := "d"
	if idx >= 0 {
		numberPart = trimmed[:idx]
		suffix = trimmed[idx:]
	}
	value, err := strconv.ParseUint(numberPart, 10, 64)
	if err != nil {
		return 0, errors.New("invalid window value: " + input)
	}
	var multiplier uint64
	switch suffix {
	case "s":
		multiplier = 1
	case "m":
		multiplier = 60
	case "h":
		multiplier = 3_600
	case "d":
		multiplier = 86_400
	case "w":
		multiplier = 7 * 86_400
	default:
		return 0, errors.New("unsupported window suffix: " + suffix)
	}
	// Overflow-safe multiply.
	if multiplier != 0 && value > ^uint64(0)/multiplier {
		return 0, errors.New("window overflow: " + input)
	}
	return value * multiplier, nil
}

// percentile95DurationMs mirrors `fn percentile_95_duration_ms`.
// Linear interpolation between the two closest ranks; nil for empty
// input.
func percentile95DurationMs(samples []float64) *float64 {
	if len(samples) == 0 {
		return nil
	}
	cp := make([]float64, len(samples))
	copy(cp, samples)
	sort.Float64s(cp)
	rank := 0.95 * float64(len(cp)-1)
	lower := int(rank)
	upper := lower
	if rank > float64(lower) {
		upper = lower + 1
	}
	if upper >= len(cp) {
		upper = len(cp) - 1
	}
	if lower == upper {
		v := cp[lower]
		return &v
	}
	low := cp[lower]
	high := cp[upper]
	v := low + (high-low)*(rank-float64(lower))
	return &v
}

// decodePayloadStrings extracts string fields the metrics aggregator
// reads. Implemented as a tiny scan instead of a full unmarshal so
// we don't allocate a `map[string]any` on every entry.
func decodePayloadStrings(raw []byte) map[string]string {
	out := map[string]string{}
	for _, key := range []string{"action_type_id", "status", "failure_type"} {
		if v, ok := scanJSONStringField(raw, key); ok {
			out[key] = v
		}
	}
	return out
}

// decodePayloadNumber extracts a numeric field from the payload. nil
// when missing or non-numeric.
func decodePayloadNumber(raw []byte, key string) *float64 {
	if v, ok := scanJSONNumberField(raw, key); ok {
		return &v
	}
	return nil
}

// scanJSONStringField reads a top-level `"key":"value"` entry without
// the cost of a full unmarshal. Returns ok=false when the field is
// missing or not a string. Quoting/escaping rules: standard JSON.
func scanJSONStringField(raw []byte, key string) (string, bool) {
	needle := []byte("\"" + key + "\"")
	idx := indexBytes(raw, needle)
	if idx < 0 {
		return "", false
	}
	i := idx + len(needle)
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		i++
	}
	if i >= len(raw) || raw[i] != ':' {
		return "", false
	}
	i++
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		i++
	}
	if i >= len(raw) || raw[i] != '"' {
		return "", false
	}
	i++
	start := i
	for i < len(raw) && raw[i] != '"' {
		if raw[i] == '\\' && i+1 < len(raw) {
			i += 2
			continue
		}
		i++
	}
	if i >= len(raw) {
		return "", false
	}
	return string(raw[start:i]), true
}

// scanJSONNumberField reads a top-level `"key":<number>` entry.
func scanJSONNumberField(raw []byte, key string) (float64, bool) {
	needle := []byte("\"" + key + "\"")
	idx := indexBytes(raw, needle)
	if idx < 0 {
		return 0, false
	}
	i := idx + len(needle)
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		i++
	}
	if i >= len(raw) || raw[i] != ':' {
		return 0, false
	}
	i++
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		i++
	}
	if i >= len(raw) {
		return 0, false
	}
	if raw[i] == '"' {
		// String — not numeric.
		return 0, false
	}
	start := i
	for i < len(raw) && (raw[i] == '-' || raw[i] == '+' || raw[i] == '.' ||
		raw[i] == 'e' || raw[i] == 'E' || (raw[i] >= '0' && raw[i] <= '9')) {
		i++
	}
	if start == i {
		return 0, false
	}
	v, err := strconv.ParseFloat(string(raw[start:i]), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func indexBytes(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
loop:
	for i := 0; i <= len(haystack)-len(needle); i++ {
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				continue loop
			}
		}
		return i
	}
	return -1
}
