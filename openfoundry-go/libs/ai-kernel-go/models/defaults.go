package models

import "encoding/json"

func ptrOf[T any](v T) *T { return &v }

func emptyJSONObject() json.RawMessage { return json.RawMessage(`{}`) }

func defaultRawMessage(value json.RawMessage, fallback json.RawMessage) json.RawMessage {
	if len(value) == 0 || string(value) == "null" {
		return append(json.RawMessage(nil), fallback...)
	}
	return value
}
