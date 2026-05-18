package domain_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/domain"
)

func TestParseTarget(t *testing.T) {
	t.Parallel()
	cases := map[string]domain.Target{
		"ts":         domain.TargetTypeScript,
		"typescript": domain.TargetTypeScript,
		"TS":         domain.TargetTypeScript,
		"  ts ":      domain.TargetTypeScript,
		"python":     domain.TargetPython,
		"py":         domain.TargetPython,
		"java":       domain.TargetJava,
	}
	for in, want := range cases {
		got, err := domain.ParseTarget(in)
		if err != nil {
			t.Errorf("ParseTarget(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseTarget(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := domain.ParseTarget("rust"); err == nil {
		t.Errorf("expected error for unsupported target")
	}
}

func TestSDKRequestValidate(t *testing.T) {
	t.Parallel()
	ok := domain.SDKRequest{
		TenantID:        uuid.New(),
		OntologyVersion: "v1",
		Target:          domain.TargetTypeScript,
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("expected ok request to validate, got: %v", err)
	}

	cases := map[string]domain.SDKRequest{
		"missing tenant":  {OntologyVersion: "v1", Target: domain.TargetTypeScript},
		"missing version": {TenantID: uuid.New(), Target: domain.TargetTypeScript},
		"bad target":      {TenantID: uuid.New(), OntologyVersion: "v1", Target: "rust"},
	}
	for name, req := range cases {
		if err := req.Validate(); err == nil {
			t.Errorf("expected error for %s", name)
		}
	}
}
