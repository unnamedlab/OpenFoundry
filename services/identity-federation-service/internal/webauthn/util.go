package webauthn

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
)

func uuidNew() uuid.UUID         { return ids.New() }
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func splitCSV(v string) []string {
	out := make([]string, 0)
	for _, s := range strings.Split(v, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
