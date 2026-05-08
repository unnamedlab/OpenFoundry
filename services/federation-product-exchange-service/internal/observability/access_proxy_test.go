package observability

import (
	"errors"
	"testing"
	"time"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

func TestValidateAccess_Expired(t *testing.T) {
	grant := &models.AccessGrant{
		ExpiresAt:       time.Now().Add(-time.Hour),
		AllowedPurposes: []string{"analytics"},
	}
	err := ValidateAccess(grant, "analytics")
	if !errors.Is(err, ErrAccessGrantExpired) {
		t.Fatalf("want ErrAccessGrantExpired, got %v", err)
	}
}

func TestValidateAccess_PurposeNotAllowed(t *testing.T) {
	grant := &models.AccessGrant{
		ExpiresAt:       time.Now().Add(time.Hour),
		AllowedPurposes: []string{"analytics", "reporting"},
	}
	err := ValidateAccess(grant, "billing")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	want := "purpose 'billing' is not allowed by this contract"
	if err.Error() != want {
		t.Fatalf("want %q, got %q", want, err.Error())
	}
}

func TestValidateAccess_OK(t *testing.T) {
	grant := &models.AccessGrant{
		ExpiresAt:       time.Now().Add(time.Hour),
		AllowedPurposes: []string{"analytics"},
	}
	if err := ValidateAccess(grant, "analytics"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveLimit(t *testing.T) {
	grant := &models.AccessGrant{MaxRowsPerQuery: 100}
	cases := []struct {
		name      string
		requested *int
		want      int
	}{
		{"nil falls back to grant limit", nil, 100},
		{"requested below grant returned as-is", intPtr(50), 50},
		{"requested above grant clamped", intPtr(500), 100},
		{"requested zero clamped to one", intPtr(0), 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveLimit(grant, c.requested); got != c.want {
				t.Fatalf("ResolveLimit: want %d, got %d", c.want, got)
			}
		})
	}
}

func TestResolveLimit_NegativeGrantFallsBackTo1000(t *testing.T) {
	grant := &models.AccessGrant{MaxRowsPerQuery: -5}
	if got := ResolveLimit(grant, nil); got != 1000 {
		t.Fatalf("ResolveLimit: want 1000, got %d", got)
	}
	if got := ResolveLimit(grant, intPtr(2000)); got != 1000 {
		t.Fatalf("ResolveLimit clamp: want 1000, got %d", got)
	}
}

func intPtr(i int) *int { return &i }
