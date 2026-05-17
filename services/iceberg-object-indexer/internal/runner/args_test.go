package runner

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseArgs_required(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  []string
		want string // substring expected in the error
	}{
		{
			name: "missing source-table",
			raw:  []string{"--target-type-id", "t", "--id-column", "id"},
			want: "--source-table",
		},
		{
			name: "missing target-type-id",
			raw:  []string{"--source-table", "ns.t", "--id-column", "id"},
			want: "--target-type-id",
		},
		{
			name: "missing id-column",
			raw:  []string{"--source-table", "ns.t", "--target-type-id", "t"},
			want: "--id-column",
		},
		{
			name: "negative limit",
			raw: []string{
				"--source-table", "ns.t", "--target-type-id", "t",
				"--id-column", "id", "--limit", "-1",
			},
			want: "--limit must be >= 0",
		},
		{
			name: "invalid log format",
			raw: []string{
				"--source-table", "ns.t", "--target-type-id", "t",
				"--id-column", "id", "--log-format", "yaml",
			},
			want: "--log-format must be",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var errBuf bytes.Buffer
			_, err := ParseArgs("test", tc.raw, &errBuf)
			if err == nil {
				t.Fatalf("ParseArgs(%v) = nil error, want one containing %q", tc.raw, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ParseArgs error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestParseArgs_defaultsAndOverrides(t *testing.T) {
	t.Parallel()
	a, err := ParseArgs("test", []string{
		"--source-table", "lakekeeper.default.t",
		"--target-type-id", "abc-uuid",
		"--id-column", "transaction_id",
		"--limit", "500",
		"--internal-token", "shh",
		"--catalog-warehouse", "openfoundry",
		"--catalog-credential", "lakekeeper:s",
		"--oauth-token-uri", "http://oidc/token",
		"--oauth-scope", "openid",
		"--log-format", "json",
	}, nil)
	if err != nil {
		t.Fatalf("ParseArgs error: %v", err)
	}
	if a.TargetTenant != "default" {
		t.Errorf("TargetTenant default = %q, want %q", a.TargetTenant, "default")
	}
	if a.ObjectDatabaseURL == "" {
		t.Error("ObjectDatabaseURL default must not be empty")
	}
	if a.CatalogName != "lakekeeper" {
		t.Errorf("Catalog default = %q, want %q", a.CatalogName, "lakekeeper")
	}
	if a.Limit != 500 {
		t.Errorf("Limit = %d, want 500", a.Limit)
	}
	if a.InternalToken != "shh" {
		t.Errorf("InternalToken = %q, want %q", a.InternalToken, "shh")
	}
	if a.CatalogCredential != "lakekeeper:s" {
		t.Errorf("CatalogCredential = %q", a.CatalogCredential)
	}
	if a.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want %q", a.LogFormat, "json")
	}
}

func TestParseArgs_smokeSkipsLimitCheck(t *testing.T) {
	t.Parallel()
	// --smoke is independent of other validation; it still requires
	// the three minimums so the CR template is exercised end-to-end.
	a, err := ParseArgs("test", []string{
		"--source-table", "ns.t",
		"--target-type-id", "t",
		"--id-column", "id",
		"--smoke",
	}, nil)
	if err != nil {
		t.Fatalf("ParseArgs error: %v", err)
	}
	if !a.Smoke {
		t.Error("Smoke = false, want true")
	}
}
