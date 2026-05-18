package cassandrakernel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

const objectHashBuckets = 64

type osv2PropertyBlob struct {
	Format     string                            `json:"format"`
	Properties map[string]osv2TypedPropertyValue `json:"properties"`
}

type osv2TypedPropertyValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type osv2MarkingsBlob struct {
	Format   string   `json:"format"`
	Markings []string `json:"markings"`
}

// primaryKeyHashBucket returns the stable object-id hash bucket used by OSV2
// physical partitions. The current Cassandra layout uses 64 buckets to keep
// object-type partitions below hot-tablet thresholds while limiting fan-out.
func primaryKeyHashBucket(primaryKey string) int {
	sum := sha256.Sum256([]byte(strings.TrimSpace(primaryKey)))
	return int(sum[0] % objectHashBuckets)
}

// propertyID is the hot-row property identifier used in the typed blob and
// property index. Until ontology schemas supply immutable property RIDs, we use
// a deterministic hash of the API property id/name so the hot row never embeds
// the raw name string.
func propertyID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	sum := sha256.Sum256([]byte(trimmed))
	return "p_" + hex.EncodeToString(sum[:8])
}

func encodePropertiesBlob(raw json.RawMessage) ([]byte, error) {
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(canonical), &obj); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(obj))
	byID := make(map[string]osv2TypedPropertyValue, len(obj))
	for name, value := range obj {
		id := propertyID(name)
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
		byID[id] = osv2TypedPropertyValue{Type: typedValueKind(value), Value: encoded}
	}
	sort.Strings(ids)
	ordered := make(map[string]osv2TypedPropertyValue, len(byID))
	for _, id := range ids {
		ordered[id] = byID[id]
	}
	return json.Marshal(osv2PropertyBlob{Format: "of.osv2.properties.v1", Properties: ordered})
}

func decodePropertiesBlob(blob []byte) (json.RawMessage, error) {
	if len(blob) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if json.Valid(blob) {
		var envelope osv2PropertyBlob
		if err := json.Unmarshal(blob, &envelope); err == nil && envelope.Format == "of.osv2.properties.v1" {
			out := make(map[string]json.RawMessage, len(envelope.Properties))
			for id, value := range envelope.Properties {
				out[id] = value.Value
			}
			encoded, err := json.Marshal(out)
			if err != nil {
				return nil, err
			}
			return json.RawMessage(encoded), nil
		}
		// Backward compatibility for rows written before OSV2.2 when the column
		// held canonical JSON bytes directly.
		return json.RawMessage(append([]byte(nil), blob...)), nil
	}
	return nil, fmt.Errorf("invalid properties_blob JSON")
}

func encodeMarkingsBlob(markings []string) ([]byte, error) {
	cp := append([]string(nil), markings...)
	sort.Strings(cp)
	return json.Marshal(osv2MarkingsBlob{Format: "of.osv2.markings.v1", Markings: cp})
}

func decodeMarkingsBlob(blob []byte) ([]string, error) {
	if len(blob) == 0 {
		return nil, nil
	}
	var envelope osv2MarkingsBlob
	if err := json.Unmarshal(blob, &envelope); err == nil && envelope.Format == "of.osv2.markings.v1" {
		return envelope.Markings, nil
	}
	var legacy []string
	if err := json.Unmarshal(blob, &legacy); err == nil {
		return legacy, nil
	}
	return nil, fmt.Errorf("invalid markings_blob JSON")
}

func typedValueKind(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case float64, float32, int, int64, int32, uint, uint64, uint32, json.Number:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		_ = t
		return "json"
	}
}

func propertyIndexTerms(raw json.RawMessage) ([]propertyIndexTerm, error) {
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(canonical), &obj); err != nil {
		return nil, err
	}
	terms := make([]propertyIndexTerm, 0, len(obj))
	for name, value := range obj {
		term, ok := propertyIndexTermForValue(propertyID(name), value)
		if ok {
			terms = append(terms, term)
		}
	}
	sort.Slice(terms, func(i, j int) bool {
		if terms[i].PropertyID != terms[j].PropertyID {
			return terms[i].PropertyID < terms[j].PropertyID
		}
		return terms[i].ValueKey < terms[j].ValueKey
	})
	return terms, nil
}

type propertyIndexTerm struct {
	PropertyID string
	ValueKind  string
	ValueKey   string
	NullValue  bool
}

func propertyIndexTermForValue(propertyID string, value any) (propertyIndexTerm, bool) {
	kind := typedValueKind(value)
	term := propertyIndexTerm{PropertyID: propertyID, ValueKind: kind, NullValue: kind == "null"}
	switch kind {
	case "null":
		term.ValueKey = "null"
	case "bool":
		if value.(bool) {
			term.ValueKey = "true"
		} else {
			term.ValueKey = "false"
		}
	case "number":
		n, ok := numberValue(value)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return propertyIndexTerm{}, false
		}
		term.ValueKey = sortableNumberKey(n)
	case "string":
		term.ValueKey = strings.ToLower(strings.TrimSpace(value.(string)))
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return propertyIndexTerm{}, false
		}
		term.ValueKey = string(encoded)
	}
	return term, true
}

func numberValue(v any) (float64, bool) {
	switch t := v.(type) {
	case json.Number:
		n, err := t.Float64()
		return n, err == nil
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint64:
		return float64(t), true
	case uint32:
		return float64(t), true
	default:
		return 0, false
	}
}

func sortableNumberKey(n float64) string {
	return strconv.FormatFloat(n+1_000_000_000_000_000, 'f', 6, 64)
}
