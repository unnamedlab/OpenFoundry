package lineage

import "encoding/json"

// MergeMetadata mirrors Rust's `merge_metadata`. The base value is the
// existing metadata column (may be nil when no row has been written
// yet); overlay is the freshly-built JSON snapshot; extra is an
// optional further patch (e.g. caller-supplied metadata).
//
// Both base and extra accept arbitrary `json.RawMessage` (the cooked
// JSON bytes from PostgreSQL JSONB columns), but in practice every
// payload that flows through the lineage path is an object — the
// Rust code panics otherwise too via `as_object_mut`.
func MergeMetadata(base, overlay, extra json.RawMessage) json.RawMessage {
	out := mergeRawMessages(base, overlay)
	if len(extra) > 0 {
		out = mergeRawMessages(out, extra)
	}
	return out
}

func mergeRawMessages(base, patch json.RawMessage) json.RawMessage {
	baseDecoded := decodeOrEmptyObject(base)
	patchDecoded := decodeOrEmptyObject(patch)

	merged := mergeJSON(baseDecoded, patchDecoded)
	encoded, err := json.Marshal(merged)
	if err != nil {
		// Should never fail — both inputs are valid JSON values.
		return json.RawMessage(`{}`)
	}
	return encoded
}

// mergeJSON ports `merge_json` recursively. Object-typed values merge
// key by key; everything else replaces wholesale (same as Rust).
func mergeJSON(target, patch any) any {
	targetObj, targetOk := target.(map[string]any)
	patchObj, patchOk := patch.(map[string]any)
	if !targetOk || !patchOk {
		return patch
	}
	for k, v := range patchObj {
		if existing, ok := targetObj[k].(map[string]any); ok {
			if vMap, ok := v.(map[string]any); ok {
				targetObj[k] = mergeJSON(existing, vMap)
				continue
			}
		}
		targetObj[k] = v
	}
	return targetObj
}

func decodeOrEmptyObject(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return map[string]any{}
	}
	return v
}

// EnsureObject returns a JSON object: when the input is nil/empty or
// already an object, it's returned untouched (or canonicalised when
// nil). Anything else gets replaced with `{}` — same as Rust's
// `ensure_object`.
func EnsureObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return json.RawMessage(`{}`)
	}
	if _, ok := v.(map[string]any); !ok {
		return json.RawMessage(`{}`)
	}
	return raw
}

// MergeIntoObject inserts a key into the (object-typed) JSON value,
// returning the updated bytes. Used by the trigger logic to inject
// `lineage_build` into the caller-supplied context.
func MergeIntoObject(raw json.RawMessage, key string, value any) (json.RawMessage, error) {
	var holder map[string]any
	if len(raw) == 0 {
		holder = map[string]any{}
	} else {
		if err := json.Unmarshal(raw, &holder); err != nil {
			holder = map[string]any{}
		}
	}
	holder[key] = value
	return json.Marshal(holder)
}
