package ts_test

import (
	"testing"

	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/generator/ts"
)

func TestMapType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"string", "string"},
		{"text", "string"},
		{"integer", "number"},
		{"long", "number"},
		{"double", "number"},
		{"boolean", "boolean"},
		{"datetime", "string"},
		{"timestamp", "string"},
		{"array<string>", "string[]"},
		{"integer[]", "number[]"},
		{"geo_point", "{ readonly type: \"Point\"; readonly coordinates: readonly [number, number] }"},
		{"", "unknown"},
		{"unobtanium", "unknown"},
	}
	for _, c := range cases {
		got := ts.MapType(c.in)
		if got != c.want {
			t.Errorf("MapType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
