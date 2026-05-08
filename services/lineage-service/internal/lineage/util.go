package lineage

import (
	"encoding/json"
	"time"
)

func nowUTC() time.Time { return time.Now().UTC() }

func strPtr(s string) *string { return &s }

func jsonOrNull(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}
