package source

import (
	"strings"
	"testing"
)

func TestParseTableIdent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		catalog  string
		ref      string
		want     []string
		errSub   string
	}{
		{
			name:    "strips matching catalog prefix",
			catalog: "lakekeeper",
			ref:     "lakekeeper.default.transactions_clean",
			want:    []string{"default", "transactions_clean"},
		},
		{
			name:    "passes through when prefix differs",
			catalog: "lakekeeper",
			ref:     "default.transactions_clean",
			want:    []string{"default", "transactions_clean"},
		},
		{
			name:    "nested namespace",
			catalog: "lakekeeper",
			ref:     "lakekeeper.warehouse.curated.transactions",
			want:    []string{"warehouse", "curated", "transactions"},
		},
		{
			name:    "rejects empty ref",
			catalog: "lakekeeper",
			ref:     "",
			errSub:  "must not be empty",
		},
		{
			name:    "rejects single-component (no namespace)",
			catalog: "lakekeeper",
			ref:     "transactions_clean",
			errSub:  "at least namespace.table",
		},
		{
			name:    "rejects empty path component",
			catalog: "lakekeeper",
			ref:     "default..transactions",
			errSub:  "empty path component",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseTableIdent(tc.catalog, tc.ref)
			if tc.errSub != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errSub) {
					t.Fatalf("parseTableIdent(%q,%q) error = %v, want substring %q", tc.catalog, tc.ref, err, tc.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTableIdent: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("ident[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
